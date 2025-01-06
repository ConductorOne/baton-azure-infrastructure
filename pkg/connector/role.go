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
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	uuid "github.com/google/uuid"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	zap "go.uber.org/zap"
)

type roleBuilder struct {
	conn *Connector
}

func (r *roleBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return roleResourceType
}

func (r *roleBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	pagerSubscriptions := r.conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pagerSubscriptions.More() {
		page, err := pagerSubscriptions.NextPage(ctx)
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
			pagerRoles := client.NewListPager(scope, nil)
			for pagerRoles.More() {
				resp, err := pagerRoles.NextPage(ctx)
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
	rv = append(rv, ent.NewAssignmentEntitlement(resource, typeAssigned, options...))

	return rv, "", nil, nil
}

func getSubscriptionID(resource *v2.Resource) string {
	arr := strings.Split(resource.Id.Resource, ":")
	if len(arr) > 0 || len(arr) < 1 {
		return arr[1]
	}

	return ""
}

func (r *roleBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	var (
		subscriptionID, roleID string
		rv                     []*v2.Grant
		gr                     *v2.Grant
		principalId            *v2.ResourceId
	)
	subscriptionID = getSubscriptionID(resource)
	// Create a Role Assignments Client
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, r.conn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	// Iterate over all role assignments
	pagerRoles := roleAssignmentsClient.NewListPager(nil)
	for pagerRoles.More() {
		page, err := pagerRoles.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, assignment := range page.Value {
			principalType, err := getPrincipalType(ctx, r.conn, *assignment.Properties.PrincipalID)
			if err != nil {
				continue
			}

			principalId = getPrincipalResourceType(principalType, assignment)
			// roleDefinitionID := assignment.Properties.RoleDefinitionID
			// roleID = resourceGroupName + ":" + getRoleIdForResourceGroup(roleDefinitionID)
			roleID = resource.Id.Resource
			roleRes, err := rs.NewResource(
				*assignment.Name,
				roleResourceType,
				roleID,
			)
			if err != nil {
				return nil, "", nil, err
			}

			gr = grant.NewGrant(roleRes, typeAssigned, principalId)
			rv = append(rv, gr)
			// fmt.Printf("Role Assignment ID: %s\n", *assignment.ID)
			// fmt.Printf("Principal ID: %s\n", *assignment.Properties.PrincipalID)
			// fmt.Printf("Role Definition ID: %s\n", *assignment.Properties.RoleDefinitionID)
			// fmt.Printf("Scope: %s\n", *assignment.Properties.Scope)
			// fmt.Println("------")
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

	entitlementResource := entitlement.Resource.Id.Resource
	if !strings.Contains(entitlementResource, ":") {
		return nil, fmt.Errorf("invalid role id")
	}

	entitlementIDs := strings.Split(entitlement.Resource.Id.Resource, ":")
	if len(entitlementIDs) < 2 || len(entitlementIDs) > 2 {
		return nil, fmt.Errorf("invalid role id")
	}

	roleId := entitlementIDs[0]
	subscriptionId := entitlementIDs[1]
	principalID := principal.Id.Resource // Object ID of the user, group, or service principal

	// Initialize the client
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionId, r.conn.token, nil)
	if err != nil {
		return nil, err
	}

	// Define your resource scope
	scope := fmt.Sprintf("/subscriptions/%s", subscriptionId)
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

func (r *roleBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	principal := grant.Principal
	entitlement := grant.Entitlement
	if principal.Id.ResourceType != userResourceType.Id {
		l.Warn(
			"azure-infrastructure-connector: only users can have role membership revoked",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, fmt.Errorf("azure-infrastructure-connector: only users can have role membership revoked")
	}

	// principalID := principal.Id.Resource
	entitlementResource := entitlement.Resource.Id.Resource
	if !strings.Contains(entitlementResource, ":") {
		return nil, fmt.Errorf("invalid role id")
	}

	entitlementIDs := strings.Split(entitlement.Resource.Id.Resource, ":")
	if len(entitlementIDs) < 2 || len(entitlementIDs) > 2 {
		return nil, fmt.Errorf("invalid role id")
	}

	subscriptionId := entitlementIDs[1]
	roleID := entitlementIDs[0]
	scope := fmt.Sprintf("/subscriptions/%s", subscriptionId)
	// Full resource ID of the role assignment to delete
	roleDefinitionID := fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", subscriptionId, roleID)
	roleAssignmentID := roleDefinitionID
	// Create a RoleAssignmentsClient
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionId, r.conn.token, nil)
	if err != nil {
		return nil, err
	}

	// Prepare role assignment parameters
	parameters := armauthorization.RoleAssignmentsClientDeleteOptions{}

	// Delete the role assignment
	_, err = roleAssignmentsClient.Delete(ctx, scope, roleAssignmentID, &parameters)
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
