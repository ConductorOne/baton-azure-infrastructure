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
	uuid "github.com/google/uuid"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	zap "go.uber.org/zap"
)

type roleBuilder struct {
	cn *Connector
}

func (r *roleBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return roleResourceType
}

func (r *roleBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	pagerSubscription := r.cn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pagerSubscription.More() {
		page, err := pagerSubscription.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, subscription := range page.Value {
			// Initialize the RoleDefinitionsClient
			client, err := armauthorization.NewRoleDefinitionsClient(r.cn.token, nil)
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
	return nil, "", nil, nil
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
	resourceGroupId := "test_resource_group"
	principalID := principal.Id.Resource // Object ID of the user, group, or service principal
	// Initialize the client
	client, err := armauthorization.NewRoleAssignmentsClient(subscriptionId, r.cn.token, nil)
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
	return nil, nil
}

func newRoleBuilder(conn *Connector) *roleBuilder {
	return &roleBuilder{cn: conn}
}
