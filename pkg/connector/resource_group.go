package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	armresources "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	uuid "github.com/google/uuid"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type resourceGroupBuilder struct {
	conn *Connector
}

func (rg *resourceGroupBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceGroupResourceType
}

func (rg *resourceGroupBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	pagerSubscriptions := rg.conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pagerSubscriptions.More() {
		page, err := pagerSubscriptions.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, subscription := range page.Value {
			client, err := armresources.NewResourceGroupsClient(*subscription.SubscriptionID, rg.conn.token, nil)
			if err != nil {
				return nil, "", nil, err
			}

			for pager := client.NewListPager(nil); pager.More(); {
				page, err := pager.NextPage(ctx)
				if err != nil {
					return nil, "", nil, err
				}

				// NOTE: The service desides how many items to return on a page.
				// If a page has 0 items, then, get the next page.
				// Other clients may be adding/deleting items from the collection while
				// this code is paging; some items may be skipped or returned multiple times.
				for _, resourceGroup := range page.Value {
					gr, err := resourceGroupResource(ctx, *subscription.SubscriptionID, resourceGroup, &v2.ResourceId{
						ResourceType: subscriptionsResourceType.Id,
						Resource:     StringValue(subscription.SubscriptionID),
					})
					if err != nil {
						return nil, "", nil, err
					}

					rv = append(rv, gr)
				}
			}
		}
	}

	return rv, "", nil, nil
}

func getEntitlementId(resource *v2.Resource, ctype string) string {
	return strings.Join(
		[]string{
			resourceGroupResourceType.Id,
			resource.Id.Resource,
			ctype,
		},
		":")
}

func (rg *resourceGroupBuilder) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement
	// lstRoles, err := getAllRoles(ctx, rg.conn)
	// if err != nil {
	// 	return nil, "", nil, err
	// }

	// for _, subscriptionID := range lstSubscriptions {
	// 	for _, roleId := range lstRoles {
	v := &v2.Entitlement{
		Id:          getEntitlementId(resource, typeOwners),
		Resource:    resource,
		DisplayName: fmt.Sprintf("%s Resource Group Owner", resource.DisplayName),
		Description: fmt.Sprintf("Owner of %s resource group", resource.DisplayName),
		GrantableTo: []*v2.ResourceType{userResourceType},
		Purpose:     v2.Entitlement_PURPOSE_VALUE_PERMISSION,
		Slug:        "owner",
	}
	rv = append(rv, v)

	v = &v2.Entitlement{
		Id:          getEntitlementId(resource, typeAssigned),
		Resource:    resource,
		DisplayName: fmt.Sprintf("%s Resource Group Assignment", resource.DisplayName),
		Description: fmt.Sprintf("Assigned to %s resource group", resource.DisplayName),
		GrantableTo: []*v2.ResourceType{userResourceType, groupResourceType},
		Purpose:     v2.Entitlement_PURPOSE_VALUE_ASSIGNMENT,
		Slug:        "assigned",
	}
	rv = append(rv, v)
	// 	}
	// }

	return rv, "", nil, nil
}

func (rg *resourceGroupBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	var (
		subscriptionID, resourceGroupName, roleID string
		rv                                        []*v2.Grant
		gr                                        *v2.Grant
		principalId                               *v2.ResourceId
	)
	arr := strings.Split(resource.Id.Resource, ":")
	if len(arr) > 0 || len(arr) < 1 {
		subscriptionID = arr[1]
		resourceGroupName = arr[0]
	}

	// Create a Role Assignments Client
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, rg.conn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	// Define the resource group scope
	scope := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionID, resourceGroupName)
	// List role assignments for the resource group
	pagerResourceGroup := roleAssignmentsClient.NewListForScopePager(scope, nil)

	// Iterate through the role assignments
	for pagerResourceGroup.More() {
		page, err := pagerResourceGroup.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, assignment := range page.Value {
			principalType, err := getPrincipalType(ctx, rg.conn, *assignment.Properties.PrincipalID)
			if err != nil {
				continue
			}

			principalId = getPrincipalResourceType(principalType, assignment)
			// roleDefinitionID := assignment.Properties.RoleDefinitionID
			// roleID = resourceGroupName + ":" + getRoleIdForResourceGroup(roleDefinitionID)
			roleID = resource.Id.Resource
			roleRes, err := rs.NewResource(
				*assignment.Name,
				resourceGroupResourceType,
				roleID,
			)
			if err != nil {
				return nil, "", nil, err
			}

			gr = grant.NewGrant(roleRes, typeAssigned, principalId)
			rv = append(rv, gr)
		}
	}

	return rv, "", nil, nil
}

func (rg *resourceGroupBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	if principal.Id.ResourceType != userResourceType.Id {
		l.Warn(
			"azure-infrastructure-connector: only users can be granted role membership",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)

		return nil, fmt.Errorf("azure-infrastructure-connector: only users can be granted role membership")
	}

	role := entitlement.Resource.Id.Resource
	roleIDs := strings.Split(role, ":")
	if len(roleIDs) < 2 || len(roleIDs) > 2 {
		return nil, fmt.Errorf("invalid role id")
	}

	subscriptionId := roleIDs[1]
	roleId := "312a565d-c81f-4fd8-895a-4e21e48d571c" // (( ??))
	resourceGroupId := roleIDs[0]
	principalID := principal.Id.Resource // Object ID of the user, group, or service principal

	// Initialize the client
	client, err := armauthorization.NewRoleAssignmentsClient(subscriptionId, rg.conn.token, nil)
	if err != nil {
		return nil, err
	}

	// Define your resource scope
	scope := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionId, resourceGroupId)
	// Define the details of the role assignment
	roleDefinitionID := fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", subscriptionId, roleId)

	// Create a role assignment name (must be unique)
	roleAssignmentId := uuid.New().String()
	// Prepare role assignment parameters
	parameters := armauthorization.RoleAssignmentCreateParameters{
		Properties: &armauthorization.RoleAssignmentProperties{
			PrincipalID:      &principalID,
			RoleDefinitionID: &roleDefinitionID,
		},
	}

	// Create the role assignment
	// In azure, you do not directly add users to resource groups. Instead, you assigned roles
	// to users for the resource group, which gives them specific permissions.
	resp, err := client.Create(ctx, scope, roleAssignmentId, parameters, nil)
	if err != nil {
		return nil, err
	}

	l.Warn("Role membership has been created.",
		zap.String("ID", *resp.ID),
		zap.String("Type", *resp.Type),
		zap.String("Name", *resp.Name),
		zap.String("PrincipalID", *resp.Properties.PrincipalID),
		zap.String("RoleDefinitionID", *resp.Properties.RoleDefinitionID),
		zap.String("RoleDefinitionID", *resp.Properties.Scope),
	)

	return nil, nil
}

func (rg *resourceGroupBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	principal := grant.Principal
	if principal.Id.ResourceType != userResourceType.Id {
		l.Warn(
			"azure-infrastructure-connector: only users can have role membership revoked",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, fmt.Errorf("azure-infrastructure-connector: only users can have role membership revoked")
	}

	// Replace with your subscription ID and role assignment ID
	roleID := "0105a6b0-4bb9-43d2-982a-12806f9faddb" // Full resource ID of the role assignment to delete
	scope := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", "subsID", "resGroupId")
	roleDefinitionID := fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", "subsID", roleID)
	roleAssignmentID := roleDefinitionID
	// Create a RoleAssignmentsClient
	client, err := armauthorization.NewRoleAssignmentsClient("subsID", rg.conn.token, nil)
	if err != nil {
		return nil, err
	}

	// Delete the role assignment
	_, err = client.Delete(ctx, scope, roleAssignmentID, nil)
	if err != nil {
		return nil, err
	}

	l.Warn("Role assignment successfully revoked.",
		zap.String("roleAssignmentID", roleAssignmentID),
	)

	return nil, nil
}

func newResourceGroupBuilder(c *Connector) *resourceGroupBuilder {
	return &resourceGroupBuilder{
		conn: c,
	}
}
