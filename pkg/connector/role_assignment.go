package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/google/uuid"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	roleAssignmentAssignedEntitlementSlug = "assigned"

	// Azure RBAC principal-type strings accepted by the roleAssignments Create
	// API (see RoleAssignmentProperties.PrincipalType). Also used by the
	// Graph-odata-type to baton-type mapping since Graph returns the same
	// tokens for SPs/MIs in some paths.
	azurePrincipalTypeUser             = "User"
	azurePrincipalTypeServicePrincipal = "ServicePrincipal"
)

// roleAssignmentBuilder emits one resource per actual Azure role assignment
// visible to the SP, carrying a ScopeBindingTrait annotation that pairs the
// role definition with the scope resource. The TRAIT_SCOPE_BINDING on this
// resource type is what triggers c1's SPARSE / HYBRID classification in uplift
// (ductone/c1#16540).
//
// Modeled after baton-azure/pkg/connector/role_assignments.go but uses the
// azure-sdk-for-go typed armauthorization client rather than hand-rolled REST.
type roleAssignmentBuilder struct {
	conn *Connector

	// principalTypeCache memoizes getPrincipalType(ctx, conn, principalID) results
	// across calls. A single principal typically holds many role assignments, so
	// without this cache Grants() would hit Microsoft Graph's directoryObjects
	// endpoint once per binding — throttling risk at customer scale. sync.Map
	// because Grants may be invoked concurrently per resource.
	principalTypeCache sync.Map // map[principalID string] -> principalType string
}

func newRoleAssignmentBuilder(conn *Connector) *roleAssignmentBuilder {
	return &roleAssignmentBuilder{conn: conn}
}

// principalTypeForID resolves the Graph directoryObjects type for a principal,
// consulting the per-builder cache first. Returns "" on lookup failure so the
// caller can drop the grant without propagating the error (degrade-gracefully
// pattern used elsewhere in this connector). Failures are logged at Warn so
// dropped grants are visible in production (Debug would hide silent data
// loss); Warn fires at most once per principal because successful lookups
// populate the cache.
func (b *roleAssignmentBuilder) principalTypeForID(ctx context.Context, principalID string) string {
	if v, ok := b.principalTypeCache.Load(principalID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	pt, err := getPrincipalType(ctx, b.conn, principalID)
	if err != nil {
		ctxzap.Extract(ctx).Warn(
			"baton-azure-infrastructure: getPrincipalType failed; dropping grant for this principal",
			zap.String("principal_id", principalID),
			zap.Error(err),
		)
		return ""
	}
	b.principalTypeCache.Store(principalID, pt)
	return pt
}

func (b *roleAssignmentBuilder) ResourceType(_ context.Context) *v2.ResourceType {
	return roleAssignmentResourceType
}

// List enumerates every Azure role assignment visible to the caller and emits
// one baton resource per unique assignment. The walk has two stages:
//
//  1. Sub-and-below: for each subscription the SP can see, NewListPager
//     returns assignments whose scope is at the subscription or descendants.
//     The ARM API also returns ancestor-inherited assignments here, but
//     represents them with a *projected* subscription-level scope rather than
//     their actual (mgmt-group) scope.
//  2. Mgmt-group-scope: for each management group, NewListForScopePager
//     returns assignments whose scope is at that mgmt group.
//
// Because stage-1 projects ancestor-inherited assignments to the subscription
// scope (the same assignment GUID shows up there with a different scope
// string), we dedup by assignment name + principal ID and let stage-2
// *overwrite* any stage-1 entry for the same assignment. Mgmt-group walk
// has the authoritative scope; stage-1's projection is lossy.
//
// If the caller lacks tenant-root access, mgmt-group enumeration returns
// empty + no error (degrade-gracefully pattern) and List continues with
// stage-1 coverage only.
func (b *roleAssignmentBuilder) List(ctx context.Context, _ *v2.ResourceId, _ *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	// Map keyed by resource.Id.Resource (= "<assignmentName>@<principalID>").
	// Stage-1 populates; stage-2 overwrites when it finds a better scope.
	byID := make(map[string]*v2.Resource)

	put := func(resource *v2.Resource, overwrite bool) {
		if resource == nil {
			return
		}
		if _, exists := byID[resource.Id.Resource]; exists && !overwrite {
			return
		}
		byID[resource.Id.Resource] = resource
	}

	// Stage 1: walk each subscription. A subscription-scoped RoleAssignmentsClient
	// returns all assignments at the sub or below (RG and resource scopes).
	subsPager := b.conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	var firstSubID string
	for subsPager.More() {
		subsPage, err := subsPager.NextPage(ctx)
		if err != nil {
			return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: listing subscriptions: %w", err)
		}
		for _, sub := range subsPage.Value {
			if sub == nil || sub.SubscriptionID == nil {
				continue
			}
			subID := *sub.SubscriptionID
			if firstSubID == "" {
				firstSubID = subID
			}

			raClient, err := armauthorization.NewRoleAssignmentsClient(subID, b.conn.token, b.conn.client.ArmOptions())
			if err != nil {
				return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: role assignments client: %w", err)
			}
			raPager := raClient.NewListPager(nil)
			for raPager.More() {
				raPage, err := raPager.NextPage(ctx)
				if err != nil {
					return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: listing role assignments for sub %s: %w", subID, err)
				}
				for _, ra := range raPage.Value {
					if ra == nil || ra.Properties == nil {
						continue
					}
					resource, err := roleAssignmentResource(subID, ra)
					if err != nil {
						return nil, "", nil, err
					}
					put(resource, false)
				}
			}
		}
	}

	// Stage 2: walk each management group for assignments *at* that scope.
	// armauthorization.RoleAssignmentsClient needs a subscription ID for
	// construction even though NewListForScopePager accepts any scope; reuse
	// firstSubID from stage 1. If no subscriptions were visible, skip stage 2.
	l := ctxzap.Extract(ctx)
	if firstSubID != "" {
		mgs, err := listManagementGroups(ctx, b.conn.token, b.conn.client.ArmOptions())
		if err != nil {
			return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: listing management groups: %w", err)
		}
		if len(mgs) > 0 {
			raClient, err := armauthorization.NewRoleAssignmentsClient(firstSubID, b.conn.token, b.conn.client.ArmOptions())
			if err != nil {
				return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: role assignments client for mgmt-group walk: %w", err)
			}
			// No atScope filter here: NewListForScopePager against a mgmt-group
			// scope returns assignments at that scope plus descendants. Stage 2
			// overwrites stage-1 entries for the same assignment because stage-1
			// projects ancestor-inherited assignments onto the subscription scope
			// (a lossy representation); the mgmt-group walk carries the actual
			// scope. Descendant-scope assignments that both walks surface get
			// overwritten with the same value (no-op).
			for _, mg := range mgs {
				if mg == nil || mg.ID == nil {
					continue
				}
				mgScope := *mg.ID
				mgEmitted := 0
				mgPageCount := 0
				mgRawSeen := 0
				pager := raClient.NewListForScopePager(mgScope, nil)
				for pager.More() {
					page, err := pager.NextPage(ctx)
					if err != nil {
						// If this particular mgmt-group scope 403s, warn and continue
						// — matches the graceful pattern in baton-azure's
						// role_assignments.go:213.
						if isForbidden(err) {
							l.Warn("baton-azure-infrastructure: forbidden listing role assignments at mgmt-group scope; skipping",
								zap.String("mgScope", mgScope), zap.Error(err))
							break
						}
						return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: listing role assignments at mgmt-group %s: %w", mgScope, err)
					}
					mgPageCount++
					mgRawSeen += len(page.Value)
					for _, ra := range page.Value {
						if ra == nil || ra.Properties == nil {
							continue
						}
						resource, err := roleAssignmentResource(firstSubID, ra)
						if err != nil {
							return nil, "", nil, err
						}
						if resource == nil {
							continue
						}
						// Always overwrite: stage-2 scope is authoritative over
						// stage-1's projection. mgEmitted counts every assignment
						// the mgmt-group walk touched (either newly added or
						// corrected from the sub walk's projection).
						put(resource, true)
						mgEmitted++
					}
				}
				l.Debug("baton-azure-infrastructure: mgmt-group scope walked",
					zap.String("mgScope", mgScope),
					zap.Int("pages", mgPageCount),
					zap.Int("rawSeen", mgRawSeen),
					zap.Int("emitted", mgEmitted))
			}
		}
	}

	// Flatten the dedup map to a stable slice.
	rv := make([]*v2.Resource, 0, len(byID))
	for _, r := range byID {
		rv = append(rv, r)
	}
	return rv, "", nil, nil
}

func (b *roleAssignmentBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return []*v2.Entitlement{
		ent.NewAssignmentEntitlement(
			resource,
			roleAssignmentAssignedEntitlementSlug,
			ent.WithDisplayName(fmt.Sprintf("Assigned: %s", resource.DisplayName)),
			ent.WithDescription(fmt.Sprintf("Principals assigned to this role binding (%s)", resource.DisplayName)),
			ent.WithGrantableTo(userResourceType, groupResourceType, managedIdentitylResourceType, enterpriseApplicationResourceType),
		),
	}, "", nil, nil
}

// batonToAzurePrincipalType maps a baton principal resource-type id to the
// Azure principal-type string expected by the roleAssignments create API.
// baton-azure rejects Group principals for provisioning ("C1 doesn't support
// provisioning to Groups"); we match that policy by returning "" here.
func batonToAzurePrincipalType(batonResourceType string) string {
	switch batonResourceType {
	case userResourceType.Id:
		return azurePrincipalTypeUser
	case enterpriseApplicationResourceType.Id, managedIdentitylResourceType.Id:
		return azurePrincipalTypeServicePrincipal
	default:
		// group and anything else: not supported for provisioning.
		return ""
	}
}

// Grants: decode the principal ID from the resource ID and emit a grant.
// We resolve principal type via the per-builder cache (backed by
// getPrincipalType, which hits Graph directoryObjects) so that repeat
// principals across many bindings don't each incur a Graph roundtrip.
func (b *roleAssignmentBuilder) Grants(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	principalID, parsedOK := parsePrincipalFromRoleAssignmentResourceID(resource.Id.Resource)
	if !parsedOK || principalID == "" {
		return nil, "", nil, nil
	}
	principalType := b.principalTypeForID(ctx, principalID)
	if principalType == "" {
		// Graph lookup failed or principal is unknown (logged at Warn inside
		// principalTypeForID). Emit no grant rather than failing the whole
		// sync — matches the degrade-gracefully pattern used elsewhere in
		// this connector.
		return nil, "", nil, nil
	}
	principalResourceType := mapGraphPrincipalTypeToBaton(principalType)
	if principalResourceType == "" {
		return nil, "", nil, nil
	}
	principalResourceID := &v2.ResourceId{
		ResourceType: principalResourceType,
		Resource:     principalID,
	}
	gr := grant.NewGrant(resource, roleAssignmentAssignedEntitlementSlug, principalResourceID)
	return []*v2.Grant{gr}, "", nil, nil
}

// Grant attaches a principal to an existing role_assignment binding by creating
// a new Azure role assignment at the binding's scope with the binding's role.
// Idempotent: if the assignment already exists, returns a GrantAlreadyExists
// annotation with a nil error.
//
// Entity sources (per the baton-sdk connector review criteria):
//   - WHO:  principal.Id.Resource (Azure object id of user / service principal / managed identity)
//   - WHAT: entitlement.Resource's ScopeBindingTrait supplies role + scope
//   - Groups are explicitly unsupported as grant targets (matches baton-azure).
func (b *roleAssignmentBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	if principal == nil || principal.Id == nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: principal missing resource id")
	}
	if entitlement == nil || entitlement.Resource == nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: entitlement missing resource")
	}

	scopeTrait, err := rs.GetScopeBindingTrait(entitlement.Resource)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: entitlement missing ScopeBindingTrait: %w", err)
	}
	if scopeTrait.RoleId == nil || scopeTrait.ScopeResourceId == nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: ScopeBindingTrait missing role or scope")
	}

	azurePrincipalType := batonToAzurePrincipalType(principal.Id.ResourceType)
	if azurePrincipalType == "" {
		return nil, fmt.Errorf("baton-azure-infrastructure: principal type %q not supported for role assignment grants (groups are unsupported by design)", principal.Id.ResourceType)
	}

	principalID := principal.Id.Resource
	scope := scopeTrait.ScopeResourceId.Resource
	roleDefinitionID := roleDefinitionIDForScope(scope, scopeTrait.RoleId.Resource)
	subscriptionID := subscriptionFromScope(scope)
	if subscriptionID == "" {
		// Fall back to any sub the SP can see. Create()'s subscription ID param
		// is only used for pipeline construction, not the API path.
		s, err := firstVisibleSubscription(ctx, b.conn)
		if err != nil {
			return nil, fmt.Errorf("baton-azure-infrastructure: cannot resolve a subscription for Grant client: %w", err)
		}
		subscriptionID = s
	}

	raClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, b.conn.token, b.conn.client.ArmOptions())
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: role assignments client: %w", err)
	}

	assignmentName := uuid.New().String()
	params := armauthorization.RoleAssignmentCreateParameters{
		Properties: &armauthorization.RoleAssignmentProperties{
			PrincipalID:      &principalID,
			RoleDefinitionID: &roleDefinitionID,
		},
	}

	_, err = raClient.Create(ctx, scope, assignmentName, params, nil)
	if err != nil {
		if isConflict(err) {
			return annotations.New(&v2.GrantAlreadyExists{}), nil
		}
		return nil, fmt.Errorf("baton-azure-infrastructure: create role assignment (scope=%s role=%s principal=%s): %w",
			scope, roleDefinitionID, principalID, err)
	}

	return nil, nil
}

// Revoke removes the principal's grant on a role_assignment binding. Finds the
// actual Azure role assignment by querying at the binding's scope, then
// DELETE's by name. Idempotent: returns GrantAlreadyRevoked if the assignment
// is not found.
func (b *roleAssignmentBuilder) Revoke(ctx context.Context, g *v2.Grant) (annotations.Annotations, error) {
	if g == nil || g.Principal == nil || g.Entitlement == nil || g.Entitlement.Resource == nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: grant missing principal or entitlement")
	}

	scopeTrait, err := rs.GetScopeBindingTrait(g.Entitlement.Resource)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: grant entitlement missing ScopeBindingTrait: %w", err)
	}
	if scopeTrait.RoleId == nil || scopeTrait.ScopeResourceId == nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: ScopeBindingTrait missing role or scope")
	}

	// The baton resource ID for a role_assignment is "<assignmentName>@<principalID>",
	// so we can recover the Azure assignment name directly without a round-trip.
	rid := g.Entitlement.Resource.Id.Resource
	atIdx := strings.LastIndex(rid, "@")
	if atIdx <= 0 {
		return nil, fmt.Errorf("baton-azure-infrastructure: role_assignment resource id %q has no assignment-name prefix", rid)
	}
	assignmentName := rid[:atIdx]

	scope := scopeTrait.ScopeResourceId.Resource
	subscriptionID := subscriptionFromScope(scope)
	if subscriptionID == "" {
		s, err := firstVisibleSubscription(ctx, b.conn)
		if err != nil {
			return nil, fmt.Errorf("baton-azure-infrastructure: cannot resolve a subscription for Revoke client: %w", err)
		}
		subscriptionID = s
	}

	raClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, b.conn.token, b.conn.client.ArmOptions())
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: role assignments client: %w", err)
	}

	_, err = raClient.Delete(ctx, scope, assignmentName, nil)
	if err != nil {
		if isNotFound(err) {
			return annotations.New(&v2.GrantAlreadyRevoked{}), nil
		}
		return nil, fmt.Errorf("baton-azure-infrastructure: delete role assignment %s at scope %s: %w",
			assignmentName, scope, err)
	}

	return nil, nil
}

// roleDefinitionIDForScope builds the fully-qualified roleDefinitionId path
// that the Azure ARM create call expects. Callers pass us just the role UUID
// from the ScopeBindingTrait (see roleAssignmentResource).
func roleDefinitionIDForScope(scope, roleUUID string) string {
	// Role definitions can be referenced by their sub-scoped or tenant-scoped
	// path. For consistency we use the scope's subscription (if any) so the
	// caller can grant built-in roles at that scope; if the scope has no
	// subscription component (mgmt-group, tenant root) we fall back to a
	// providers-relative path which the API still accepts.
	if sub := subscriptionFromScope(scope); sub != "" {
		return fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", sub, roleUUID)
	}
	return fmt.Sprintf("/providers/Microsoft.Authorization/roleDefinitions/%s", roleUUID)
}

// subscriptionFromScope extracts the subscription GUID from an ARM scope path,
// or returns "" for scopes that don't live under a subscription (management
// groups, tenant root).
func subscriptionFromScope(scope string) string {
	const prefix = "/subscriptions/"
	if !strings.HasPrefix(scope, prefix) {
		return ""
	}
	rest := scope[len(prefix):]
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i]
	}
	return rest
}

// firstVisibleSubscription returns any subscription ID the SP can see; used
// only as a RoleAssignmentsClient construction parameter for mgmt-group-scope
// and tenant-root-scope operations (the client's sub-id field is unused by
// the Create/Delete code paths but the constructor requires it).
func firstVisibleSubscription(ctx context.Context, conn *Connector) (string, error) {
	pager := conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return "", err
		}
		for _, sub := range page.Value {
			if sub != nil && sub.SubscriptionID != nil && *sub.SubscriptionID != "" {
				return *sub.SubscriptionID, nil
			}
		}
	}
	return "", fmt.Errorf("no visible subscriptions")
}

// isConflict detects the Azure 409 returned when a role assignment with the
// specified parameters already exists. Used to make Grant idempotent.
func isConflict(err error) bool {
	if err == nil {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		return respErr.StatusCode == http.StatusConflict
	}
	return false
}

// isNotFound detects the Azure 404 returned when a role assignment does not
// exist at the specified scope. Used to make Revoke idempotent.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		return respErr.StatusCode == http.StatusNotFound
	}
	return false
}

// roleAssignmentResource builds a baton resource for an Azure role assignment,
// attaching a ScopeBindingTrait annotation (role → scope) so the c1 uplift
// stamps AppAccessModel = SPARSE / HYBRID.
func roleAssignmentResource(subscriptionID string, ra *armauthorization.RoleAssignment) (*v2.Resource, error) {
	if ra == nil || ra.Name == nil || ra.Properties == nil {
		return nil, nil
	}
	props := ra.Properties
	if props.PrincipalID == nil || props.RoleDefinitionID == nil || props.Scope == nil {
		return nil, nil
	}

	// Stable ID = assignmentName@principalID. Encoding principalID lets Grants()
	// reconstruct the grant without a second ARM roundtrip.
	resourceID := fmt.Sprintf("%s@%s", *ra.Name, *props.PrincipalID)

	// role_id points at the role_definition resource. baton-azure stores roles
	// keyed by their UUID (last path segment of the roleDefinitionId ARM path),
	// so we match that here so principal→role joins work cross-connector.
	roleUUID := path.Base(*props.RoleDefinitionID)
	roleScopeResourceID := &v2.ResourceId{
		ResourceType: roleResourceType.Id,
		Resource:     roleUUID,
	}

	// scope_resource_id points at whatever scope level the assignment lives at.
	// For scopes this connector doesn't emit as resources (mgmt groups, tenant
	// root, fine-grained sub-resource), the reference still carries the raw ARM
	// path so c1 can navigate / label even without a materialized resource.
	scopeResourceID := &v2.ResourceId{
		ResourceType: scopeResourceTypeForAzureScope(*props.Scope),
		Resource:     *props.Scope,
	}

	scopeBindingOpts := []rs.ScopeBindingTraitOption{
		rs.WithRoleScopeRoleId(roleScopeResourceID),
		rs.WithRoleScopeResourceId(scopeResourceID),
	}

	displayName := fmt.Sprintf("Role %s @ %s", roleUUID, *props.Scope)

	return rs.NewResource(
		displayName,
		roleAssignmentResourceType,
		resourceID,
		rs.WithScopeBindingTrait(scopeBindingOpts...),
		rs.WithDescription(fmt.Sprintf(
			"Azure role assignment %s of role %s at scope %s (principal %s, subscription %s)",
			*ra.Name, roleUUID, *props.Scope, *props.PrincipalID, subscriptionID)),
	)
}

// scopeResourceTypeForAzureScope picks the best-matching baton resource type
// for an ARM scope path. Mgmt-group scopes resolve to managementGroupResourceType
// so role_assignment bindings at that scope can reference the emitted mgmt
// group. For scopes this connector still doesn't materialize (sub-resource
// like KV secrets, Service Bus queues — follow-up work) we fall through to
// the nearest parent we do emit.
func scopeResourceTypeForAzureScope(scope string) string {
	switch {
	case strings.HasPrefix(scope, "/providers/Microsoft.Management/managementGroups/"):
		return managementGroupResourceType.Id
	case strings.HasPrefix(scope, "/subscriptions/") && strings.Contains(scope, "/resourceGroups/"):
		return resourceGroupResourceType.Id
	case strings.HasPrefix(scope, "/subscriptions/"):
		return subscriptionsResourceType.Id
	default:
		return tenantResourceType.Id
	}
}

// parsePrincipalFromRoleAssignmentResourceID returns the principal ID portion
// of a role_assignment resource ID (format: "<assignmentName>@<principalID>").
// The second return value is false when the input does not match the expected
// "name@principal" format.
func parsePrincipalFromRoleAssignmentResourceID(id string) (string, bool) {
	idx := strings.LastIndex(id, "@")
	if idx == -1 || idx == len(id)-1 {
		return "", false
	}
	return id[idx+1:], true
}

// mapGraphPrincipalTypeToBaton translates the type strings returned by
// getPrincipalType (which calls the Graph directoryObjects endpoint) into baton
// resource type IDs. Kept in sync with getPrincipalIDResource in helper.go.
func mapGraphPrincipalTypeToBaton(graphType string) string {
	switch graphType {
	case "#microsoft.graph.user":
		return userResourceType.Id
	case "#microsoft.graph.group":
		return groupResourceType.Id
	case "#microsoft.graph.servicePrincipal", "Application", azurePrincipalTypeServicePrincipal:
		return enterpriseApplicationResourceType.Id
	case "ManagedIdentity":
		return managedIdentitylResourceType.Id
	default:
		return ""
	}
}
