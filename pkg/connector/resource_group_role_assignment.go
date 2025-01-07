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
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	uuid "github.com/google/uuid"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type roleAssignmentResourceGroupBuilder struct {
	conn *Connector
}

func (ra *roleAssignmentResourceGroupBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return roleAssignmentResourceGroupType
}

func (ra *roleAssignmentResourceGroupBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	pagerSubscriptions := ra.conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pagerSubscriptions.More() {
		page, err := pagerSubscriptions.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, subscription := range page.Value {
			lstRoles, err := getAllRoles(ctx, ra.conn, *subscription.SubscriptionID)
			if err != nil {
				return nil, "", nil, err
			}

			client, err := armresources.NewResourceGroupsClient(*subscription.SubscriptionID, ra.conn.token, nil)
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
					for _, roleID := range lstRoles {
						gr, err := roleAssignmentResourceGroupResource(ctx,
							*subscription.SubscriptionID,
							roleID,
							resourceGroup,
							&v2.ResourceId{
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
	}

	return rv, "", nil, nil
}

func (ra *roleAssignmentResourceGroupBuilder) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement
	options := []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Resource Group Owner", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Owner of %s resource group", resource.DisplayName)),
		ent.WithGrantableTo(userResourceType),
	}
	rv = append(rv, ent.NewPermissionEntitlement(resource, typeOwners, options...))

	options = []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Resource Group Member", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Assigned to %s resource group", resource.DisplayName)),
		ent.WithGrantableTo(userResourceType, groupResourceType),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, typeAssigned, options...))

	return rv, "", nil, nil
}

func (ra *roleAssignmentResourceGroupBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	var (
		rv                                        []*v2.Grant
		gr                                        *v2.Grant
		principalId                               *v2.ResourceId
		subscriptionID, resourceGroupName, roleID string
	)
	arr := strings.Split(resource.Id.Resource, ":")
	if len(arr) > 0 && len(arr) < 4 {
		subscriptionID = arr[1]
		resourceGroupName = arr[0]
		roleID = arr[2]
	}

	// Create a Role Assignments Client
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, ra.conn.token, nil)
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
			roleDefinitionID := fmt.Sprintf(
				"/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s",
				subscriptionID,
				roleID)
			if roleDefinitionID != *assignment.Properties.RoleDefinitionID {
				continue
			}

			principalType, err := getPrincipalType(ctx, ra.conn, *assignment.Properties.PrincipalID)
			if err != nil {
				continue
			}

			principalId = getPrincipalIDResource(principalType, assignment)
			gr = grant.NewGrant(resource, typeAssigned, principalId)
			rv = append(rv, gr)
		}
	}

	return rv, "", nil, nil
}

func (ra *roleAssignmentResourceGroupBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
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
	if len(roleIDs) > 0 && len(roleIDs) < 4 {
		return nil, fmt.Errorf("invalid role id")
	}

	resourceGroupId := roleIDs[0]
	subscriptionId := roleIDs[1]
	roleId := roleIDs[2]
	principalID := principal.Id.Resource // Object ID of the user, group, or service principal
	// Initialize the client
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionId, ra.conn.token, nil)
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
	resp, err := roleAssignmentsClient.Create(ctx, scope, roleAssignmentId, parameters, nil)
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

func (ra *roleAssignmentResourceGroupBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
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
	client, err := armauthorization.NewRoleAssignmentsClient("subsID", ra.conn.token, nil)
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

func newRoleAssignmentResourceGroupBuilder(c *Connector) *roleAssignmentResourceGroupBuilder {
	return &roleAssignmentResourceGroupBuilder{
		conn: c,
	}
}
