package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	azcore "github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	uuid "github.com/google/uuid"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	zap "go.uber.org/zap"
)

type roleBuilder struct {
	conn                  *Connector
	roleDefinitionsClient *armauthorization.RoleDefinitionsClient
	// key subscriptionID
	// value array of role assignments for that subscriptionID
	subIdRoleAssignmentsCache map[string][]*armauthorization.RoleAssignment
	mu                        sync.RWMutex
}

func (r *roleBuilder) cacheGet(id string) ([]*armauthorization.RoleAssignment, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.subIdRoleAssignmentsCache[id]
	return value, ok
}

func (r *roleBuilder) cacheSet(id string, value []*armauthorization.RoleAssignment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subIdRoleAssignmentsCache[id] = value
}

func (r *roleBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return roleResourceType
}

func (r *roleBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	if parentResourceID == nil {
		return nil, "", nil, nil
	}
	var rv []*v2.Resource
	subscriptionID := parentResourceID.Resource

	scope := fmt.Sprintf("/subscriptions/%s", subscriptionID)
	// Get the list of role definitions
	pagerRoles := r.roleDefinitionsClient.NewListPager(scope, nil)
	for pagerRoles.More() {
		resp, err := pagerRoles.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		// Iterate over role definitions
		for _, role := range resp.Value {
			rs, err := roleResource(ctx, role, &v2.ResourceId{
				ResourceType: subscriptionsResourceType.Id,
				Resource:     StringValue(&subscriptionID),
			})
			if err != nil {
				return nil, "", nil, err
			}

			rv = append(rv, rs)
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

func (r *roleBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	var (
		subscriptionID, roleID string
		rv                     []*v2.Grant
		gr                     *v2.Grant
		principalId            *v2.ResourceId
	)
	arr := strings.Split(resource.Id.Resource, ":")
	if len(arr) == 2 {
		subscriptionID = arr[1]
		roleID = arr[0]
	}

	err := r.cacheRoleAssignments(ctx, subscriptionID)
	if err != nil {
		return nil, "", nil, err
	}

	assignments, _ := r.cacheGet(subscriptionID)
	for _, assignment := range assignments {
		roleDefinitionID := fmt.Sprintf(
			"/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s",
			subscriptionID,
			roleID)
		if roleDefinitionID != *assignment.Properties.RoleDefinitionID {
			continue
		}

		principalType, err := getPrincipalType(ctx, r.conn, *assignment.Properties.PrincipalID)
		if err != nil {
			continue
		}

		principalId = getPrincipalIDResource(principalType, assignment)
		gr = grant.NewGrant(resource, typeAssigned, principalId)
		rv = append(rv, gr)
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
	if len(entitlementIDs) != 2 {
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

	// Define your scope
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
	resp, err := roleAssignmentsClient.Create(ctx, scope, roleAssignmentId, parameters, nil)
	if err != nil {
		var azureErr *azcore.ResponseError
		switch {
		case errors.As(err, &azureErr):
			if azureErr.StatusCode == http.StatusConflict {
				l.Warn(
					"azure-infrastructure-connector: failed to perform request",
					zap.Int("StatusCode", azureErr.StatusCode),
					zap.String("ErrorCode", azureErr.ErrorCode),
					zap.String("ErrorMsg", azureErr.Error()),
				)
			}
		default:
			return nil, err
		}
	}

	if resp.ID != nil {
		l.Warn("Role membership has been created.",
			zap.String("ID", *resp.ID),
			zap.String("Type", *resp.Type),
			zap.String("Name", *resp.Name),
			zap.String("PrincipalID", *resp.Properties.PrincipalID),
			zap.String("RoleDefinitionID", *resp.Properties.RoleDefinitionID),
			zap.String("RoleDefinitionID", *resp.Properties.Scope),
		)
	}

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

	principalID := principal.Id.Resource
	entitlementResource := entitlement.Resource.Id.Resource
	if !strings.Contains(entitlementResource, ":") {
		return nil, fmt.Errorf("%s", invalidRoleID)
	}

	entitlementIDs := strings.Split(entitlement.Resource.Id.Resource, ":")
	if len(entitlementIDs) != 2 {
		return nil, fmt.Errorf("%s", invalidRoleID)
	}

	// Prepare role assignment parameters
	roleID := entitlementIDs[0]
	subscriptionId := entitlementIDs[1]
	scope := fmt.Sprintf("/subscriptions/%s", subscriptionId)
	// role assignment to delete
	roleAssignmentName, err := getAssignmentID(ctx,
		r.conn,
		scope,
		subscriptionId,
		roleID,
		principalID,
	)
	if err != nil {
		return nil, err
	}

	// Create a RoleAssignmentsClient
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionId, r.conn.token, nil)
	if err != nil {
		return nil, err
	}

	// Delete the role assignment
	roleAssignmentResponse, err := roleAssignmentsClient.Delete(ctx, scope, roleAssignmentName, nil)
	if err != nil {
		return nil, err
	}

	if roleAssignmentResponse.ID == nil {
		return nil, fmt.Errorf("failed to revoke role assignment %s scope: %s", roleID, scope)
	}

	l.Warn("Role assignment successfully revoked.",
		zap.String("roleAssignmentID", roleAssignmentName),
		zap.String("ID", *roleAssignmentResponse.ID),
	)

	return nil, nil
}

func newRoleBuilder(c *Connector) *roleBuilder {
	return &roleBuilder{
		conn:                      c,
		roleDefinitionsClient:     c.roleDefinitionsClient,
		subIdRoleAssignmentsCache: make(map[string][]*armauthorization.RoleAssignment),
	}
}

func (r *roleBuilder) cacheRoleAssignments(ctx context.Context, subscriptionID string) error {
	if _, ok := r.cacheGet(subscriptionID); ok {
		return nil
	}

	// Create a Role Assignments Client
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, r.conn.token, nil)
	if err != nil {
		return err
	}

	// Iterate over all role assignments
	pagerRoles := roleAssignmentsClient.NewListPager(nil)
	for pagerRoles.More() {
		page, err := pagerRoles.NextPage(ctx)
		if err != nil {
			return err
		}

		assignments, _ := r.cacheGet(subscriptionID)
		r.cacheSet(subscriptionID, append(assignments, page.Value...))
	}

	return nil
}
