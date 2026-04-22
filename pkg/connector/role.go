package connector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/session"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

// roleAssignmentsPrefix is the session-store key prefix for the cached
// per-subscription role-assignment list.
const roleAssignmentsPrefix = "azinfra-role-assignments-by-sub:"

// Role builder: baton-azure parity, reference-only.
//
// The role resource type is emitted as descriptive metadata ONLY.
// It carries role-name / description / actions / assignableScopes via the
// TRAIT_ROLE profile so c1 can resolve `ScopeBindingTrait.role_id` against
// it. It does NOT emit per-role entitlements or grants, and provisioning
// (Grant / Revoke) goes through the `role_assignment` builder's sparse-ACL
// path, not through role.
//
// BREAKING: resource IDs and entitlement IDs both change format.
// Before: resource id = "<uuid>:<subscriptionID>", entitlement id =
//         "role:<uuid>:<subscriptionID>:owners" / ":assigned".
// After:  resource id = "<uuid>" (tenant-global), no per-role entitlements.
//
// Existing c1 deployments with catalog bindings or active reviews keyed to
// the old entitlement IDs MUST be re-pointed at the new format (or at the
// `role_assignment` resource, preferred for sparse-ACL). See PR body for
// migration guidance.

type roleBuilder struct {
	conn                  *Connector
	roleDefinitionsClient *armauthorization.RoleDefinitionsClient

	// emitted tracks role UUIDs already emitted during this sync. The role
	// builder is called once per parent scope (subscription or management
	// group); Azure returns the same built-in roles at every scope, so
	// without dedup the same role definition would be emitted 5+ times as
	// separate c1 resources. sync.Map gives us atomic check-and-claim
	// across concurrent List invocations.
	emitted sync.Map
}

func (r *roleBuilder) ResourceType(_ context.Context) *v2.ResourceType {
	return roleResourceType
}

// List enumerates Azure role definitions visible at the parent scope and
// emits each unique role definition exactly once per sync, parented to the
// tenant. Deduplication is keyed on the role UUID: built-in roles visible
// at multiple scopes produce at most one c1 resource per sync.
//
// parentResourceID may be a subscription or management group; we extract
// the ARM scope path either way and let Azure return everything visible at
// that scope. The SkipUnusedRoles flag (opt-in, BATON_SKIP_UNUSED_ROLES)
// filters out role definitions with zero role_assignments in the tenant.
func (r *roleBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	if parentResourceID == nil {
		return nil, nil, nil
	}
	l := ctxzap.Extract(ctx)

	var scope, subscriptionID string
	switch parentResourceID.ResourceType {
	case subscriptionsResourceType.Id:
		subscriptionID = parentResourceID.Resource
		scope = fmt.Sprintf("/subscriptions/%s", subscriptionID)
	case managementGroupResourceType.Id:
		scope = fmt.Sprintf("/providers/Microsoft.Management/managementGroups/%s", parentResourceID.Resource)
	default:
		// Unexpected parent — nothing to list.
		return nil, nil, nil
	}

	// Pre-compute used-role set if skip-unused-roles is on. Only applies
	// when we have a subscriptionID (role_assignments cache is keyed by sub);
	// for management-group scope we sync all visible roles — the filter
	// doesn't compose with mgmt-group scope today and silently degrades.
	var usedRoles map[string]struct{}
	if r.conn.SkipUnusedRoles && subscriptionID != "" {
		assignments, err := r.roleAssignments(ctx, opts, subscriptionID)
		if err != nil {
			return nil, nil, err
		}
		usedRoles = rolesInUse(assignments)
	}

	var out []*v2.Resource
	pager := r.roleDefinitionsClient.NewListPager(scope, nil)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, role := range resp.Value {
			if role.Name == nil {
				continue
			}
			uuid := *role.Name

			// Skip unused roles if the filter is on and this one isn't assigned anywhere.
			if usedRoles != nil {
				if role.ID == nil {
					continue
				}
				if _, ok := usedRoles[*role.ID]; !ok {
					continue
				}
			}

			// Claim emission: first caller to reach this UUID wins; others skip.
			if _, loaded := r.emitted.LoadOrStore(uuid, struct{}{}); loaded {
				continue
			}

			// No parent — roles are tenant-global in Azure; mirrors
			// baton-azure's roleDefinitionBuilder.createResource.
			rsrc, err := roleResource(ctx, role, nil)
			if err != nil {
				l.Warn("role.List: error building role resource — skipping",
					zap.String("uuid", uuid), zap.Error(err))
				continue
			}
			out = append(out, rsrc)
		}
	}

	return out, nil, nil
}

// Entitlements returns nothing. Per-role Owner/Member entitlements are a
// legacy classic-RBAC projection; the sparse-ACL `role_assignment` resource
// is the authoritative grant surface in the post-PR world. See baton-azure's
// roleDefinitionBuilder for the same pattern.
func (r *roleBuilder) Entitlements(_ context.Context, _ *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

// Grants returns nothing. Every (principal, role, scope) triple is carried
// authoritatively by the `role_assignment` resource with ScopeBindingTrait.
// See role_assignment.go.
func (r *roleBuilder) Grants(_ context.Context, _ *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func newRoleBuilder(c *Connector) *roleBuilder {
	return &roleBuilder{
		conn:                  c,
		roleDefinitionsClient: c.roleDefinitionsClient,
	}
}

// roleAssignments returns the per-subscription role-assignment list, consulting
// the session-store cache first. Kept here (rather than on the role_assignment
// builder) because the unused-roles filter on role.List needs it.
func (r *roleBuilder) roleAssignments(ctx context.Context, opts rs.SyncOpAttrs, subscriptionID string) ([]*armauthorization.RoleAssignment, error) {
	if opts.Session != nil {
		if cached, found, err := session.GetJSON[[]*armauthorization.RoleAssignment](ctx, opts.Session, subscriptionID, sessions.WithPrefix(roleAssignmentsPrefix)); err == nil && found {
			return cached, nil
		}
	}

	l := ctxzap.Extract(ctx)
	l.Info("baton-azure-infrastructure: caching role assignments",
		zap.String("subscription_id", subscriptionID))
	start := time.Now()

	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, r.conn.token, r.conn.client.ArmOptions())
	if err != nil {
		return nil, err
	}

	var all []*armauthorization.RoleAssignment
	pager := roleAssignmentsClient.NewListForSubscriptionPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range page.Value {
			if a.Properties == nil || a.Properties.RoleDefinitionID == nil {
				l.Warn("baton-azure-infrastructure: role assignment properties or role definition id are nil")
				continue
			}
			all = append(all, a)
		}
	}

	if opts.Session != nil {
		_ = session.SetJSON(ctx, opts.Session, subscriptionID, all, sessions.WithPrefix(roleAssignmentsPrefix))
	}

	l.Info("baton-azure-infrastructure: role assignments cached successfully",
		zap.String("subscription_id", subscriptionID),
		zap.Duration("duration", time.Since(start)),
		zap.Int("count", len(all)))
	return all, nil
}

// rolesInUse returns the set of role-definition IDs referenced by at least
// one assignment in the list.
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
