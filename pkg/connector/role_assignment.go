package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/google/uuid"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const roleAssignmentAssignedEntitlementSlug = "assigned"

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
// loss).
//
// The cache holds negative results too: when a lookup returns "" (either the
// fallback chain was exhausted or it errored), we still store "" so subsequent
// bindings for the same principal don't re-query Graph. Without this, a single
// tenancy-foreign principal that holds N role assignments produces N Graph
// roundtrips and N Warn log lines — enough to swamp operator logs on customer
// syncs.
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
		// Cache the miss so we don't re-query on every subsequent role
		// assignment that references this principal.
		b.principalTypeCache.Store(principalID, "")
		return ""
	}
	b.principalTypeCache.Store(principalID, pt)
	return pt
}

func (b *roleAssignmentBuilder) ResourceType(_ context.Context) *v2.ResourceType {
	return roleAssignmentResourceType
}

// raListPhase identifies which stage of the role_assignment walk the
// pagination bag is currently driving.
type raListPhase string

const (
	// raPhaseInit: first call. Seed pending subscriptions + emit all
	// mgmt-group-scope assignments in a single go (mgmt-group count is
	// small in practice).
	raPhaseInit raListPhase = ""
	// raPhaseSub: per-call, emit one subscription's role assignments
	// (walking Azure's internal pager to exhaustion for that sub) and
	// advance.
	raPhaseSub raListPhase = "sub"
)

// raBagState is the pagination bag state for roleAssignmentBuilder.List.
// It drives a streaming walk: first call does full mgmt-group enumeration
// (yielding stage-2 resources immediately + collecting their names in
// SeenAssignmentNames), subsequent calls each handle one subscription
// (yielding stage-1 resources that weren't already emitted by stage-2).
//
// Memory at rest (serialized into the pagination token between calls) is
// bounded by (a) the list of pending subscription IDs — typically 10-5000
// strings — and (b) the seen-set of mgmt-group-scope assignment names —
// typically well under 1000 entries. Concretely: ~(5K subs × 36 bytes +
// 1K seen × 36 bytes) = ~216 KB serialized in the worst mainstream case.
// No unbounded accumulator.
type raBagState struct {
	Phase               raListPhase `json:"p,omitempty"`
	PendingSubs         []string    `json:"ps,omitempty"`
	CurrentSub          string      `json:"cs,omitempty"`
	SeenAssignmentNames []string    `json:"sn,omitempty"`
}

// List enumerates every Azure role assignment visible to the caller and
// streams them through the baton-sdk pagination contract: each call
// returns one page of resources plus a continuation token.
//
// **Walk strategy** — live-verified against api-version 2022-04-01:
//
//  1. Mgmt-group-scope assignments at tenant-root ARE returned by the
//     subscription walk (NewListForSubscriptionPager) with their
//     authoritative scope — so stage-2's historical job of "overwriting
//     a projected scope" is no longer necessary.
//  2. However, assignments at *intermediate* mgmt groups (between
//     tenant-root and a subscription) are NOT included by the sub walk.
//     The mgmt-group walk still needs to run to pick those up.
//
// So the refactored contract is: stage-2 for completeness (intermediate
// mgmt-group scopes), stage-1 for bulk (subs × descendants), dedup via a
// seen-set carried in the pagination token. No overwrite semantic.
//
// **Memory characteristics (this refactor):**
//
//	Peak in-memory per List call — steady state (phase "sub"): one
//	  subscription's assignments × ~1.5 KB (typical 100-500 assignments
//	  → 150-750 KB; hyperscale sub ~10k → ~15 MB).
//	Peak in-memory per List call — first call (phase "init"): ALL mgmt-
//	  group-scope assignments across every visible mgmt group are
//	  emitted in a single page. In practice mgmt-group-scope assignment
//	  counts are small (lab: 2; enterprise: typically <100) so this is
//	  a one-shot burst. A tenant with thousands of mgmt-group-scope
//	  assignments would need the init phase paginated further; not a
//	  concern for current customer shapes.
//	State carried between calls: list of pending sub IDs + seen-set of
//	  mgmt-group-scope names, bounded at ~216 KB in the worst mainstream
//	  case (5K subs + 1K mgmt-scope assignments).
//
// Replaces the previous in-memory `map[id]*Resource` which grew
// unbounded with tenant size (filed as issue #80; this is the fix).
//
// **Degradation:**
//
//	If the caller lacks tenant-root access, mgmt-group enumeration
//	returns empty + no error — List continues with stage-1 coverage
//	only. Per-sub 403s are logged and skipped (common in mixed-
//	permission enterprise tenants).
func (b *roleAssignmentBuilder) List(ctx context.Context, _ *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	bag, err := pagination.GenBagFromToken[raBagState](pToken)
	if err != nil {
		return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: role_assignment pagination: %w", err)
	}

	// First-call seed: kick off with the init phase.
	if bag.Current() == nil {
		bag.Push(raBagState{Phase: raPhaseInit})
	}

	state := bag.Pop()

	switch state.Phase {
	case raPhaseInit:
		return b.listInit(ctx, l, bag)
	case raPhaseSub:
		return b.listSubPage(ctx, l, bag, state)
	default:
		return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: role_assignment pagination: unknown phase %q", state.Phase)
	}
}

// listInit handles the first pagination call: enumerate subscriptions,
// emit every mgmt-group-scope assignment in a single page, and seed the
// bag with the pending-sub list + the mgmt-group-scope seen-set.
func (b *roleAssignmentBuilder) listInit(ctx context.Context, l *zap.Logger, bag *pagination.GenBag[raBagState]) ([]*v2.Resource, string, annotations.Annotations, error) {
	// Enumerate subscriptions (IDs only; we walk each one in its own page
	// during the sub phase).
	subsPager := b.conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	var pendingSubs []string
	for subsPager.More() {
		subsPage, err := subsPager.NextPage(ctx)
		if err != nil {
			return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: listing subscriptions: %w", err)
		}
		for _, sub := range subsPage.Value {
			if sub == nil || sub.SubscriptionID == nil || *sub.SubscriptionID == "" {
				continue
			}
			pendingSubs = append(pendingSubs, *sub.SubscriptionID)
		}
	}

	var firstSubID string
	if len(pendingSubs) > 0 {
		firstSubID = pendingSubs[0]
	}

	// Walk all mgmt groups, emit their assignments, collect names for dedup.
	var emitted []*v2.Resource
	var seenNames []string
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
			for _, mg := range mgs {
				if mg == nil || mg.ID == nil {
					continue
				}
				mgScope := *mg.ID
				pager := raClient.NewListForScopePager(mgScope, nil)
				for pager.More() {
					page, pagerErr := pager.NextPage(ctx)
					if pagerErr != nil {
						// Degrade gracefully on per-mgmt-group 403 (common
						// for SPs without Management Group Reader at this
						// scope).
						if isForbidden(pagerErr) {
							l.Warn("baton-azure-infrastructure: forbidden listing role assignments at mgmt-group scope; skipping",
								zap.String("mgScope", mgScope), zap.Error(pagerErr))
							break
						}
						return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: listing role assignments at mgmt-group %s: %w", mgScope, pagerErr)
					}
					for _, ra := range page.Value {
						if ra == nil || ra.Properties == nil || ra.Name == nil {
							continue
						}
						resource, resErr := roleAssignmentResource(firstSubID, ra)
						if resErr != nil {
							return nil, "", nil, resErr
						}
						if resource == nil {
							continue
						}
						emitted = append(emitted, resource)
						seenNames = append(seenNames, *ra.Name)
					}
				}
			}
		}
	}

	// Seed the sub-walk phase if we have subs to visit.
	if len(pendingSubs) > 0 {
		bag.Push(raBagState{
			Phase:               raPhaseSub,
			PendingSubs:         pendingSubs,
			CurrentSub:          pendingSubs[0],
			SeenAssignmentNames: seenNames,
		})
	}

	return finishPage(bag, emitted)
}

// listSubPage handles one call's worth of work in the sub phase: walk
// every Azure page of the current subscription's role assignments (the
// Azure SDK pager doesn't expose its continuation token publicly, so we
// process one whole sub per baton call rather than one Azure page),
// dedup against the mgmt-group-scope seen-set, then advance to the next
// pending sub.
func (b *roleAssignmentBuilder) listSubPage(ctx context.Context, l *zap.Logger, bag *pagination.GenBag[raBagState], state *raBagState) ([]*v2.Resource, string, annotations.Annotations, error) {
	subID := state.CurrentSub
	seen := make(map[string]struct{}, len(state.SeenAssignmentNames))
	for _, n := range state.SeenAssignmentNames {
		seen[n] = struct{}{}
	}

	var emitted []*v2.Resource
	if subID != "" {
		raClient, err := armauthorization.NewRoleAssignmentsClient(subID, b.conn.token, b.conn.client.ArmOptions())
		if err != nil {
			return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: role assignments client: %w", err)
		}
		pager := raClient.NewListForSubscriptionPager(nil)
	subPager:
		for pager.More() {
			page, pagerErr := pager.NextPage(ctx)
			if pagerErr != nil {
				// Graceful 403 on a sub we can see but can't read
				// role-assignments on.
				if isForbidden(pagerErr) {
					l.Warn("baton-azure-infrastructure: forbidden listing role assignments for sub; skipping",
						zap.String("subID", subID), zap.Error(pagerErr))
					break subPager
				}
				return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: listing role assignments for sub %s: %w", subID, pagerErr)
			}
			for _, ra := range page.Value {
				if !shouldEmitRoleAssignment(ra, seen) {
					continue
				}
				resource, resErr := roleAssignmentResource(subID, ra)
				if resErr != nil {
					return nil, "", nil, resErr
				}
				if resource == nil {
					// Unlike the seen-set short-circuit, roleAssignmentResource
					// may return (nil, nil) for malformed input even if the
					// name passed shouldEmitRoleAssignment's shape checks;
					// keep the seen-set entry we just added so we don't
					// retry on a later Azure pager page *within this same
					// List call* for the same name. (The seen-set is
					// reconstructed from state.SeenAssignmentNames on every
					// List call and only carries mgmt-group-scope names
					// across calls — sub-scope names don't need cross-call
					// dedup because each sub is visited exactly once per
					// sync.)
					continue
				}
				emitted = append(emitted, resource)
			}
		}
	}

	// Advance to the next pending sub (or signal completion).
	nextSub, remaining := advancePendingSubs(subID, state.PendingSubs)
	if nextSub != "" {
		bag.Push(raBagState{
			Phase:               raPhaseSub,
			PendingSubs:         remaining,
			CurrentSub:          nextSub,
			SeenAssignmentNames: state.SeenAssignmentNames,
		})
	}

	return finishPage(bag, emitted)
}

// shouldEmitRoleAssignment is the dedup gate used by listSubPage: it
// rejects nil / malformed assignments and assignments whose names are
// already in `seen`, and records successful candidates in the set. Kept
// as a pure helper for unit-testability without mocking the Azure SDK.
func shouldEmitRoleAssignment(ra *armauthorization.RoleAssignment, seen map[string]struct{}) bool {
	if ra == nil || ra.Properties == nil || ra.Name == nil {
		return false
	}
	if _, dup := seen[*ra.Name]; dup {
		return false
	}
	seen[*ra.Name] = struct{}{}
	return true
}

// advancePendingSubs drops the just-processed sub (if it still heads
// the pending list — defensive against callers that reorder) and
// returns the next sub to visit plus the updated pending list. A "",
// nil return signals end-of-walk.
func advancePendingSubs(justProcessed string, pending []string) (string, []string) {
	remaining := pending
	if len(remaining) > 0 && remaining[0] == justProcessed {
		remaining = remaining[1:]
	}
	if len(remaining) == 0 {
		return "", nil
	}
	return remaining[0], remaining
}

// finishPage serializes the bag and returns the (resources, nextToken,
// annotations, error) tuple in the shape List expects.
func finishPage(bag *pagination.GenBag[raBagState], emitted []*v2.Resource) ([]*v2.Resource, string, annotations.Annotations, error) {
	nextToken, err := bag.Marshal()
	if err != nil {
		return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: role_assignment pagination marshal: %w", err)
	}
	// When the bag is fully drained Marshal returns a non-empty
	// empty-states envelope; the SDK's end-of-pagination signal is an
	// empty string, so flatten to "" when there's no more work.
	if bag.Current() == nil {
		nextToken = ""
	}
	return emitted, nextToken, nil, nil
}

func (b *roleAssignmentBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return []*v2.Entitlement{
		ent.NewAssignmentEntitlement(
			resource,
			roleAssignmentAssignedEntitlementSlug,
			ent.WithDisplayName(fmt.Sprintf("Assigned: %s", resource.DisplayName)),
			ent.WithDescription(fmt.Sprintf("Principals assigned to this role binding (%s)", resource.DisplayName)),
			// Groups are intentionally omitted from GrantableTo: baton-azure's
			// policy (matched by batonToAzurePrincipalType below) rejects group
			// principals at Grant time. Advertising groups here would show
			// "Grant to group" in the c1 UI and then fail at provision with a
			// confusing error.
			ent.WithGrantableTo(userResourceType, managedIdentitylResourceType, enterpriseApplicationResourceType),
		),
	}, "", nil, nil
}

// batonToAzurePrincipalType maps a baton principal resource-type id to the
// typed Azure PrincipalType enum (as of armauthorization v2). Returns nil
// for principal types we refuse to grant — baton-azure's policy ("C1 doesn't
// support provisioning to Groups") treats groups as unsupported, and any
// unrecognised baton type also returns nil.
func batonToAzurePrincipalType(batonResourceType string) *armauthorization.PrincipalType {
	switch batonResourceType {
	case userResourceType.Id:
		pt := armauthorization.PrincipalTypeUser
		return &pt
	case enterpriseApplicationResourceType.Id, managedIdentitylResourceType.Id:
		pt := armauthorization.PrincipalTypeServicePrincipal
		return &pt
	default:
		// group and anything else: not supported for provisioning.
		return nil
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

	// Gatekeeper: we accept only baton principal types Azure recognises
	// (user / service principal / managed identity). Groups are rejected per
	// baton-azure's policy ("C1 doesn't support provisioning to Groups"). The
	// armauthorization v2 Create call accepts a typed PrincipalType, which we
	// pass through verbatim (saves Azure a server-side Graph lookup and keeps
	// Grant working for callers whose SPs lack Graph read permission).
	azurePrincipalType := batonToAzurePrincipalType(principal.Id.ResourceType)
	if azurePrincipalType == nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: principal type %q not supported for role assignment grants (groups are unsupported by design)", principal.Id.ResourceType)
	}

	principalID := principal.Id.Resource
	if !isAzureGUID(principalID) {
		// Defense against filter-smuggling: Azure $filter and role-assignment
		// ARM paths interpolate principalID as a string. Everything originating
		// from Azure is a GUID; if c1 forwards something else the only
		// reasonable response is refuse.
		return nil, fmt.Errorf("baton-azure-infrastructure: refusing Grant: principal id %q is not a GUID", principalID)
	}

	// ScopeBindingTrait values hold BUILDER-FORMAT resource ids (bare sub
	// GUID / bare RG name / full ARM path for mgmt-group), not Azure ARM
	// scope paths. Reconstruct the full ARM scope + role-definition path
	// before talking to Azure.
	roleUUID := roleUUIDFromBindingRef(scopeTrait.RoleId.Resource)
	// subscription ID is packed into role_id's composite ("<uuid>:<sub>")
	// because the role builder emits it there; resource_group refs in the
	// binding carry only the bare RG name, so we rely on the role_id to
	// recover the sub.
	subscriptionID := subscriptionFromBindingRoleRef(scopeTrait.RoleId.Resource)
	if subscriptionID == "" {
		// Fallback for mgmt-group-scope bindings whose role_id has no sub
		// component (the colon-less case): pick any sub the SP can see.
		s, err := firstVisibleSubscription(ctx, b.conn)
		if err != nil {
			return nil, fmt.Errorf("baton-azure-infrastructure: cannot resolve a subscription for Grant client: %w", err)
		}
		subscriptionID = s
	}
	scope := armScopeFromBindingRef(scopeTrait.ScopeResourceId.ResourceType, scopeTrait.ScopeResourceId.Resource, subscriptionID)
	roleDefinitionID := roleDefinitionIDForScope(scope, roleUUID)

	raClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, b.conn.token, b.conn.client.ArmOptions())
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: role assignments client: %w", err)
	}

	assignmentName := uuid.New().String()
	params := armauthorization.RoleAssignmentCreateParameters{
		Properties: &armauthorization.RoleAssignmentProperties{
			PrincipalID:      &principalID,
			RoleDefinitionID: &roleDefinitionID,
			PrincipalType:    azurePrincipalType,
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

// Revoke removes the principal's role assignment at the binding's scope.
//
// The Azure assignment name baked into the binding's resource ID (the
// "<name>@<principalID>" prefix) identifies the *originally-synced* assignment
// — not any assignment subsequently created by Grant, which generates a fresh
// UUID each call. Using that name for DELETE would revoke the wrong principal
// whenever Revoke fires between a Grant and the next resync. Instead we query
// at scope by principal, match the role locally, and DELETE by the discovered
// assignment name — matching baton-azure's Revoke pattern.
//
// Idempotent: if no matching assignment is found, returns GrantAlreadyRevoked.
func (b *roleAssignmentBuilder) Revoke(ctx context.Context, g *v2.Grant) (annotations.Annotations, error) {
	if g == nil || g.Principal == nil || g.Entitlement == nil || g.Entitlement.Resource == nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: grant missing principal or entitlement")
	}
	if g.Principal.Id == nil || g.Principal.Id.Resource == "" {
		return nil, fmt.Errorf("baton-azure-infrastructure: grant principal missing id")
	}

	scopeTrait, err := rs.GetScopeBindingTrait(g.Entitlement.Resource)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: grant entitlement missing ScopeBindingTrait: %w", err)
	}
	if scopeTrait.RoleId == nil || scopeTrait.ScopeResourceId == nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: ScopeBindingTrait missing role or scope")
	}

	principalID := g.Principal.Id.Resource
	if !isAzureGUID(principalID) {
		// Defense against filter-smuggling: the $filter below interpolates
		// principalID directly. Azure-origin IDs are always GUIDs; refuse
		// anything else rather than risk widening the list scope.
		return nil, fmt.Errorf("baton-azure-infrastructure: refusing Revoke: principal id %q is not a GUID", principalID)
	}

	// ScopeBindingTrait holds builder-format resource ids (bare GUID / bare
	// name / full ARM path), not Azure ARM scopes. Reconstruct before use.
	// The subscription ID is packed into role_id's composite; scope_resource_id
	// carries only the bare RG name / GUID (or full ARM path for mgmt-group).
	roleUUID := roleUUIDFromBindingRef(scopeTrait.RoleId.Resource)
	subscriptionID := subscriptionFromBindingRoleRef(scopeTrait.RoleId.Resource)
	if subscriptionID == "" {
		s, err := firstVisibleSubscription(ctx, b.conn)
		if err != nil {
			return nil, fmt.Errorf("baton-azure-infrastructure: cannot resolve a subscription for Revoke client: %w", err)
		}
		subscriptionID = s
	}
	scope := armScopeFromBindingRef(scopeTrait.ScopeResourceId.ResourceType, scopeTrait.ScopeResourceId.Resource, subscriptionID)

	raClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, b.conn.token, b.conn.client.ArmOptions())
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: role assignments client: %w", err)
	}

	// Azure $filter for role_assignments at api-version 2022-04-01 accepts
	// exactly ONE of `atScope()`, `principalId eq '{value}'`, or
	// `assignedTo('{value}')` — combinations return 400 UnsupportedQuery
	// (live-verified). We use `atScope()` (narrows the wire payload to
	// assignments at exactly this scope, not descendants/ancestors) and
	// match the principal + role UUID locally. Each scope usually holds
	// well under a hundred assignments so this is cheap.
	filter := "atScope()"
	pager := raClient.NewListForScopePager(scope, &armauthorization.RoleAssignmentsClientListForScopeOptions{
		Filter: &filter,
	})
	var assignmentName string
	for pager.More() && assignmentName == "" {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return annotations.New(&v2.GrantAlreadyRevoked{}), nil
			}
			return nil, fmt.Errorf("baton-azure-infrastructure: listing role assignments for revoke (scope=%s principal=%s): %w",
				scope, principalID, err)
		}
		for _, ra := range page.Value {
			if ra == nil || ra.Properties == nil || ra.Name == nil ||
				ra.Properties.RoleDefinitionID == nil || ra.Properties.PrincipalID == nil {
				continue
			}
			// Match principal + role locally (atScope() already narrowed
			// the server response to this exact scope).
			if *ra.Properties.PrincipalID == principalID &&
				path.Base(*ra.Properties.RoleDefinitionID) == roleUUID {
				assignmentName = *ra.Name
				break
			}
		}
	}

	if assignmentName == "" {
		// No assignment for this (principal, role) at scope — already revoked
		// or never existed.
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}

	deleteResp, err := raClient.Delete(ctx, scope, assignmentName, nil)
	if err != nil {
		// 404 is defense-in-depth for malformed scope / api-version skew.
		// ARM's documented 204-on-missing behaviour arrives via err==nil
		// and is handled below.
		if isNotFound(err) {
			return annotations.New(&v2.GrantAlreadyRevoked{}), nil
		}
		return nil, fmt.Errorf("baton-azure-infrastructure: delete role assignment %s at scope %s: %w",
			assignmentName, scope, err)
	}

	// Azure's DELETE is idempotent: 204 on a non-existent assignment returns
	// with err == nil and an empty RoleAssignment body. Distinguish "we
	// actually deleted it" (response carries the deleted assignment) from
	// "already gone" (empty response) so c1 can surface the
	// GrantAlreadyRevoked annotation in the latter case. This matters when
	// the filter-list-then-delete pattern races against Azure eventual
	// consistency: the list returns a ghost entry, delete 204s, and the
	// caller needs to know it was effectively a no-op.
	if deleteResp.Name == nil {
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
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
//
// The check spans two error-representation paths: raw azcore.ResponseError
// (how the Azure SDK normally surfaces status codes) and the gRPC-status
// shape that baton-sdk's middleware wraps Azure errors into
// (`status.Code(err) == codes.AlreadyExists` for 409). Covering both keeps
// Grant idempotent whether the SDK or the middleware won the race.
func isConflict(err error) bool {
	if err == nil {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) && respErr.StatusCode == http.StatusConflict {
		return true
	}
	return status.Code(err) == codes.AlreadyExists
}

// isNotFound detects the Azure 404 returned when a role assignment does not
// exist at the specified scope. Used to make Revoke idempotent. Same dual
// detection logic as isConflict — see that function's docstring.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) && respErr.StatusCode == http.StatusNotFound {
		return true
	}
	return status.Code(err) == codes.NotFound
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

	// role_id points at the role_definition resource. This connector's roleBuilder
	// (pkg/connector/role.go + helper.go:getRoleId) emits role resources with
	// composite IDs of the form "<roleUUID>:<subscriptionID>" — not bare UUIDs —
	// so the ScopeBindingTrait reference has to match that format exactly or c1
	// cannot resolve the role from the binding. We build the same composite here
	// using the current subscription context.
	roleUUID := path.Base(*props.RoleDefinitionID)
	roleResourceID := fmt.Sprintf("%s:%s", roleUUID, subscriptionID)
	roleScopeResourceID := &v2.ResourceId{
		ResourceType: roleResourceType.Id,
		Resource:     roleResourceID,
	}

	// scope_resource_id must reference a resource ID in the format the matching
	// resource type's builder actually emits — otherwise c1 sees a dangling
	// reference and can't resolve the binding's scope for display or tree
	// navigation. The builders in this connector emit:
	//   - subscription resources  → ID = bare subscription GUID
	//   - resource_group resources → ID = bare RG name (not ARM path)
	//   - management_group resources → ID = full ARM path (what managementGroupResource emits)
	// Sub-resource scopes (KV secret / storage container / SB queue) have no
	// dedicated resource type yet; we fall back to the parent RG name so at
	// least the ancestor resolves. This is a documented follow-up (NOTES.md
	// item B — sub-resource resource types).
	scopeTypeID, scopeIDValue := scopeResourceRefFromAzureScope(*props.Scope)
	scopeResourceID := &v2.ResourceId{
		ResourceType: scopeTypeID,
		Resource:     scopeIDValue,
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
	t, _ := scopeResourceRefFromAzureScope(scope)
	return t
}

// scopeResourceRefFromAzureScope returns (resourceType, resourceID) for an
// ARM scope, where resourceID matches exactly what the corresponding
// resource-type's builder emits as its id — otherwise the ScopeBindingTrait
// reference dangles in the c1z. Extraction rules per type:
//
//   - management_group → full ARM path (managementGroupResource emits ID = mg.ID)
//   - resource_group   → bare RG name (resourceGroupBuilder emits ID = rg.Name)
//   - subscription     → bare subscription GUID (subscriptionBuilder emits ID = subID)
//   - anything below an RG (sub-resource scope: KV secret, storage container,
//     SB queue, etc.) → falls back to the parent RG so at least the ancestor
//     resolves; follow-up work (NOTES.md item B) will materialize these as
//     dedicated resource types with their own IDs.
//   - tenant root / unknown → tenant type with the raw scope as ID (best-effort
//     until a richer tenant/root model exists).
func scopeResourceRefFromAzureScope(scope string) (string, string) {
	const mgPrefix = "/providers/Microsoft.Management/managementGroups/"
	const subPrefix = "/subscriptions/"
	const rgToken = "/resourceGroups/"

	switch {
	case strings.HasPrefix(scope, mgPrefix):
		return managementGroupResourceType.Id, scope
	case strings.HasPrefix(scope, subPrefix):
		rest := scope[len(subPrefix):]
		subID := rest
		if i := strings.Index(rest, "/"); i >= 0 {
			subID = rest[:i]
		}
		// Is there a resourceGroups/<rgName> segment?
		if idx := strings.Index(scope, rgToken); idx >= 0 {
			after := scope[idx+len(rgToken):]
			rgName := after
			if j := strings.Index(after, "/"); j >= 0 {
				rgName = after[:j]
			}
			return resourceGroupResourceType.Id, rgName
		}
		return subscriptionsResourceType.Id, subID
	default:
		return tenantResourceType.Id, scope
	}
}

// roleUUIDFromBindingRef peels the role UUID off the composite
// "<roleUUID>:<subscriptionID>" identifier that the role builder emits for
// ScopeBindingTrait.role_id (see helper.go:getRoleId). Revoke uses the bare
// UUID to match against Azure assignments' RoleDefinitionID path suffix.
//
// Defensive behavior:
//   - Bare UUID (no colon): returned unchanged.
//   - Well-formed composite ("<uuid>:<sub>"): UUID is returned.
//   - Leading-colon input (":x:y"): the `colon > 0` guard preserves the
//     whole string rather than silently trimming to empty — an empty role
//     UUID would match every assignment's basename, which would be worse
//     than a no-op.
func roleUUIDFromBindingRef(id string) string {
	if colon := strings.Index(id, ":"); colon > 0 {
		return id[:colon]
	}
	return id
}

// azureGUIDRegexp matches the canonical 8-4-4-4-12 hex representation that
// Azure uses for object IDs (principal ID, role UUID, subscription ID). Used
// by isAzureGUID for input validation on Grant/Revoke principal IDs before
// those strings are interpolated into ARM $filter expressions or path
// segments.
var azureGUIDRegexp = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isAzureGUID reports whether s is formatted like a canonical Azure object
// GUID. Azure emits all principal IDs in this shape, so a non-match means the
// caller is passing something we did not intend to proxy — either a bug or a
// filter-injection attempt against ARM. Either way, refusing is safer than
// interpolating.
func isAzureGUID(s string) bool {
	return azureGUIDRegexp.MatchString(s)
}

// subscriptionFromBindingRoleRef extracts the subscription GUID encoded in
// ScopeBindingTrait.role_id, whose format is "<roleUUID>:<subscriptionID>"
// per the role builder's emission (see helper.go:getRoleId). Returns ""
// when no subscription component is present (e.g. mgmt-group-only roles
// that the builder might emit without a sub suffix).
func subscriptionFromBindingRoleRef(compositeRoleID string) string {
	if colon := strings.Index(compositeRoleID, ":"); colon > 0 && colon < len(compositeRoleID)-1 {
		return compositeRoleID[colon+1:]
	}
	return ""
}

// armScopeFromBindingRef reconstructs the Azure ARM scope string from a
// ScopeBindingTrait.scope_resource_id. The binding carries BUILDER-FORMAT ids
// (bare sub GUID / bare RG name / full ARM path for mgmt-group) rather than
// ARM scopes, so Grant and Revoke need to rebuild the path before calling
// Azure. For resource_group the sub is not in the binding; callers pass it
// in (typically recovered from the role_id composite via
// subscriptionFromBindingRoleRef).
func armScopeFromBindingRef(scopeResourceType, scopeResourceID, subscriptionID string) string {
	switch scopeResourceType {
	case subscriptionsResourceType.Id:
		return "/subscriptions/" + scopeResourceID
	case resourceGroupResourceType.Id:
		return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionID, scopeResourceID)
	case managementGroupResourceType.Id:
		// management_group builder emits the full ARM path as the resource
		// id, so nothing to reconstruct.
		return scopeResourceID
	default:
		// Tenant root or unrecognised; pass through unchanged. Better to
		// send a path Azure rejects than to silently mutate.
		return scopeResourceID
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
	case "#microsoft.graph.servicePrincipal", "Application", string(armauthorization.PrincipalTypeServicePrincipal):
		return enterpriseApplicationResourceType.Id
	case "ManagedIdentity":
		return managedIdentitylResourceType.Id
	default:
		return ""
	}
}
