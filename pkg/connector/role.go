package connector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/session"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	"github.com/google/uuid"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

// roleAssignmentsPrefix is the session-store key prefix for the cached
// per-subscription role-assignment list. The value is the raw
// []*armauthorization.RoleAssignment page from ARM; callers that need a
// fast "is this role in use" lookup rebuild the roleID set on read (see
// rolesInUse).
const roleAssignmentsPrefix = "azinfra-role-assignments-by-sub:"

type roleBuilder struct {
	conn                  *Connector
	roleDefinitionsClient *armauthorization.RoleDefinitionsClient
}

func (r *roleBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return roleResourceType
}

func (r *roleBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	if parentResourceID == nil {
		return nil, nil, nil
	}
	var rv []*v2.Resource
	subscriptionID := parentResourceID.Resource

	// Compute the used-role set lazily if SkipUnusedRoles is on. Rebuilt from
	// the session-store cache, which is populated once per sync on first
	// call via roleAssignments().
	var usedRoles map[string]struct{}

	scope := fmt.Sprintf("/subscriptions/%s", subscriptionID)
	// Get the list of role definitions
	pagerRoles := r.roleDefinitionsClient.NewListPager(scope, nil)
	for pagerRoles.More() {
		resp, err := pagerRoles.NextPage(ctx)
		if err != nil {
			return nil, nil, err
		}

		// Iterate over role definitions
		for _, role := range resp.Value {
			if r.conn.SkipUnusedRoles {
				if role.ID == nil {
					continue
				}

				if usedRoles == nil {
					assignments, err := r.roleAssignments(ctx, opts, subscriptionID)
					if err != nil {
						return nil, nil, err
					}
					usedRoles = rolesInUse(assignments)
				}
				if _, ok := usedRoles[*role.ID]; !ok {
					// not in use, should be skipped
					continue
				}
			}

			rsrc, err := roleResource(ctx, role, &v2.ResourceId{
				ResourceType: subscriptionsResourceType.Id,
				Resource:     StringValue(&subscriptionID),
			})
			if err != nil {
				return nil, nil, err
			}

			rv = append(rv, rsrc)
		}
	}

	return rv, nil, nil
}

func (r *roleBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
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

	return rv, nil, nil
}

// Grants returns no direct grants. In the sparse-ACL / ScopeBinding model,
// the role_assignment resource type is the authoritative carrier for
// (principal, role, scope) triples — the same information that classic
// role.Grants used to emit as user → role:<id>:assigned grants. Emitting
// both paths produces redundant data that c1's sparse-ACL read path doesn't
// need and that inflates c1z payloads by ~67% on real tenants.
//
// The role resource type is still useful (it exposes role-name / description
// metadata and the :assigned entitlement for c1 to resolve against), so the
// builder stays registered — it just no longer projects access data that
// role_assignment already carries.
//
// See role_assignment.go for the authoritative grant emission path.
func (r *roleBuilder) Grants(_ context.Context, _ *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, nil, nil
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
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionId, r.conn.token, r.conn.client.ArmOptions())
	if err != nil {
		return nil, err
	}

	// Define your scope
	scope := fmt.Sprintf("/subscriptions/%s", subscriptionId)
	// Define the details of the role assignment
	roleDefinitionID := subscriptionRoleId(subscriptionId, roleId)
	// Create a role assignment name (must be unique)
	roleAssignmentId := uuid.New().String()
	// Prepare role assignment parameters. PrincipalType is User because the
	// early guard above restricts this Grant path to userResourceType only.
	// armauthorization v2+ accepts PrincipalType on Create and uses it to
	// skip Azure's server-side Graph lookup — both a perf win and a
	// compatibility fix for SPs without Graph read permission.
	ptUser := armauthorization.PrincipalTypeUser
	parameters := armauthorization.RoleAssignmentCreateParameters{
		Properties: &armauthorization.RoleAssignmentProperties{
			PrincipalID:      &principalID,
			RoleDefinitionID: &roleDefinitionID,
			PrincipalType:    &ptUser,
		},
	}

	// Create the role assignment. 409 Conflict is non-fatal idempotency:
	// the assignment already exists with these parameters. Return the
	// GrantAlreadyExists annotation so c1 can distinguish from a fresh
	// grant.
	resp, err := roleAssignmentsClient.Create(ctx, scope, roleAssignmentId, parameters, nil)
	if err != nil {
		if isConflict(err) {
			return annotations.New(&v2.GrantAlreadyExists{}), nil
		}
		return nil, fmt.Errorf("azure-infrastructure-connector: create role assignment (scope=%s role=%s principal=%s): %w",
			scope, roleDefinitionID, principalID, err)
	}

	if resp.ID != nil {
		l.Info("azure-infrastructure-connector: role membership created",
			zap.String("id", *resp.ID),
			zap.String("name", *resp.Name),
			zap.String("principal_id", *resp.Properties.PrincipalID),
			zap.String("role_definition_id", *resp.Properties.RoleDefinitionID),
			zap.String("scope", *resp.Properties.Scope),
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
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionId, r.conn.token, r.conn.client.ArmOptions())
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
		conn:                  c,
		roleDefinitionsClient: c.roleDefinitionsClient,
	}
}

// roleAssignments returns the per-subscription role-assignment list,
// consulting the session-store cache first. On miss it walks
// armauthorization's NewListForSubscriptionPager to exhaustion and
// populates the cache. Containers that die between runs don't re-fetch
// on the next sync; opts.Session being nil (test harness) degrades to
// a live ARM walk without caching.
func (r *roleBuilder) roleAssignments(ctx context.Context, opts rs.SyncOpAttrs, subscriptionID string) ([]*armauthorization.RoleAssignment, error) {
	if opts.Session != nil {
		if cached, found, err := session.GetJSON[[]*armauthorization.RoleAssignment](ctx, opts.Session, subscriptionID, sessions.WithPrefix(roleAssignmentsPrefix)); err == nil && found {
			return cached, nil
		}
	}

	l := ctxzap.Extract(ctx)
	l.Info(
		"baton-azure-infrastructure: caching role assignments",
		zap.String("subscription_id", subscriptionID),
	)
	start := time.Now()

	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, r.conn.token, r.conn.client.ArmOptions())
	if err != nil {
		return nil, err
	}

	// Iterate over all role assignments. armauthorization v2 renamed
	// NewListPager → NewListForSubscriptionPager (the subscription-id comes
	// from the client constructor); the returned pager semantics are
	// unchanged.
	var all []*armauthorization.RoleAssignment
	pagerRoles := roleAssignmentsClient.NewListForSubscriptionPager(nil)
	for pagerRoles.More() {
		page, err := pagerRoles.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, assignment := range page.Value {
			// Original guard was `Properties == nil && RoleDefinitionID != nil`,
			// which meant the inner dereference on the second clause would panic
			// *exactly* when the outer nil-check was true. Needs to be `||` with
			// `== nil` on both sides: skip when either Properties or the
			// RoleDefinitionID pointer is missing.
			if assignment.Properties == nil || assignment.Properties.RoleDefinitionID == nil {
				l.Warn("baton-azure-infrastructure: role assignment properties or role definition id are nil")
				continue
			}
			all = append(all, assignment)
		}
	}

	if opts.Session != nil {
		_ = session.SetJSON(ctx, opts.Session, subscriptionID, all, sessions.WithPrefix(roleAssignmentsPrefix))
	}

	l.Info(
		"baton-azure-infrastructure: role assignments cached successfully",
		zap.String("subscription_id", subscriptionID),
		zap.Duration("duration", time.Since(start)),
		zap.Int("count", len(all)),
	)

	return all, nil
}

// rolesInUse returns the set of role-definition IDs referenced by at
// least one assignment in the list. Cheap to rebuild — O(N) over the
// slice. Cheaper than persisting the set separately in the session store.
func rolesInUse(assignments []*armauthorization.RoleAssignment) map[string]struct{} {
	used := make(map[string]struct{}, len(assignments))
	for _, a := range assignments {
		if a == nil || a.Properties == nil || a.Properties.RoleDefinitionID == nil {
			continue
		}
		used[*a.Properties.RoleDefinitionID] = struct{}{}
	}
	return used
}
