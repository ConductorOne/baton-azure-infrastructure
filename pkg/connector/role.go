package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	uuid "github.com/google/uuid"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	zap "go.uber.org/zap"
)

const (
	resGroupId = "test_resource_group"
	subsID     = "39ea64c5-86d5-4c29-8199-5b602c90e1c5"
)

type roleBuilder struct {
	conn *Connector
}

func (r *roleBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return roleResourceType
}

func (r *roleBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	pagerSubscription := r.conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pagerSubscription.More() {
		page, err := pagerSubscription.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, subscription := range page.Value {
			// Initialize the RoleDefinitionsClient
			client, err := armauthorization.NewRoleDefinitionsClient(r.conn.token, nil)
			if err != nil {
				return nil, "", nil, err
			}

			// Define the scope (use "/" for subscription-level roles)
			scope := fmt.Sprintf("/subscriptions/%s", *subscription.SubscriptionID)
			// Get the list of role definitions
			pager := client.NewListPager(scope, nil)
			for pager.More() {
				resp, err := pager.NextPage(ctx)
				if err != nil {
					return nil, "", nil, err
				}

				// Iterate over role definitions
				for _, role := range resp.Value {
					rs, err := roleResource(ctx, role, &v2.ResourceId{
						ResourceType: subscriptionsResourceType.Id,
						Resource:     StringValue(subscription.SubscriptionID),
					})
					if err != nil {
						return nil, "", nil, err
					}

					rv = append(rv, rs)
				}
			}
		}
	}

	return rv, "", nil, nil
}

func (r *roleBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement
	options := []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Role Owner", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Owner of %s role", resource.DisplayName)),
		ent.WithGrantableTo(userResourceType),
	}
	rv = append(rv, ent.NewPermissionEntitlement(resource, typeOwners, options...))

	options = []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Role Member", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Member of %s role", resource.DisplayName)),
		ent.WithGrantableTo(userResourceType, groupResourceType),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, typeMembers, options...))

	return rv, "", nil, nil
}

func (r *roleBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	var (
		principalId *v2.ResourceId
		roleID      string
		rv          []*v2.Grant
		gr          *v2.Grant
	)
	lstSubscriptions, err := getAvailableSubscriptions(ctx, r.conn)
	if err != nil {
		return nil, "", nil, err
	}

	lstResourceGroups, err := getResourceGroups(ctx, r.conn)
	if err != nil {
		return nil, "", nil, err
	}

	for _, subscriptionID := range lstSubscriptions {
		for _, resourceGroupName := range lstResourceGroups {
			// Create a Role Assignments Client
			roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, r.conn.token, nil)
			if err != nil {
				return nil, "", nil, err
			}

			// Define the resource group scope
			scope := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionID, resourceGroupName)
			// List role assignments for the resource group
			pager := roleAssignmentsClient.NewListForScopePager(scope, nil)

			// Iterate through the role assignments
			for pager.More() {
				page, err := pager.NextPage(ctx)
				if err != nil {
					return nil, "", nil, err
				}

				for _, assignment := range page.Value {
					principalType, err := getPrincipalType(ctx, r.conn, *assignment.Properties.PrincipalID)
					if err != nil {
						continue
					}

					principalId = getPrincipalResourceType(principalType, assignment)
					roleDefinitionID := assignment.Properties.RoleDefinitionID
					roleID = resourceGroupName + ":" + getRoleIdForResourceGroup(roleDefinitionID)
					roleRes, err := rs.NewResource(
						*assignment.Name,
						resourceGroupResourceType,
						roleID,
					)
					if err != nil {
						return nil, "", nil, err
					}

					gr = grant.NewGrant(roleRes, typeMembers, principalId)
					rv = append(rv, gr)
				}
			}
		}
	}

	return rv, "", nil, nil
}

func (r *roleBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
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

	subscriptionId := roleIDs[0]
	roleId := roleIDs[1]
	resourceGroupId := resGroupId
	principalID := principal.Id.Resource // Object ID of the user, group, or service principal

	// Initialize the client
	client, err := armauthorization.NewRoleAssignmentsClient(subscriptionId, r.conn.token, nil)
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

func (r *roleBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
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
	scope := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subsID, resGroupId)
	roleDefinitionID := fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", subsID, roleID)
	roleAssignmentID := roleDefinitionID
	// Create a RoleAssignmentsClient
	client, err := armauthorization.NewRoleAssignmentsClient(subsID, r.conn.token, nil)
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

func newRoleBuilder(c *Connector) *roleBuilder {
	return &roleBuilder{
		conn: c,
	}
}
