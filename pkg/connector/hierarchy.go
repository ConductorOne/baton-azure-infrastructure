package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

// Entity-type strings returned by the Azure management-groups entities API in
// EntityInfo.Type. Checked via HasSuffix so case/vendor differences (e.g.
// "Microsoft.Management/managementGroups") don't cause false negatives.
const (
	entityTypeManagementGroupSuffix = "managementGroups"
	entityTypeSubscriptionSuffix    = "subscriptions"
)

// hierarchyIndex is the resolved parent map keyed by:
//
//	management_group entities → keyed by full ARM path (matches the
//	  management_group resource builder's ID format)
//	subscription entities → keyed by bare subscription GUID (matches
//	  the subscription resource builder's ID format)
//
// Values are the ResourceId of each entity's parent, chosen to match the
// IDs emitted by the parent resource type's builder:
//
//	management_group with a mgmt-group parent → (management_group, parent ARM path)
//	management_group at tenant root (Parent==nil) → (tenant, tenantGUID)
//	subscription → (management_group, parent ARM path)
//
// Missing entries mean the entity's parent couldn't be resolved (SP lacks
// mgmt-group-read permission, or the API didn't return it); callers should
// omit parentResourceId in that case so the SDK doesn't store a dangling
// reference.
type hierarchyIndex map[string]*v2.ResourceId

// errMgmtGroupReadRequired is returned when the SP lacks Management Group
// Reader (403 from the entities API). Surfaced to the operator as a
// fatal error during connector init when --sync-role-assignments=true,
// and silently logged as a degradation otherwise.
var errMgmtGroupReadRequired = fmt.Errorf(
	"baton-azure-infrastructure: --sync-role-assignments requires the " +
		"service principal to have 'Management Group Reader' (or equivalent) " +
		"at the tenant root management group. Grant this role at " +
		"/providers/Microsoft.Management/managementGroups/{tenantId} and retry")

// ensureHierarchy eagerly loads the tenant hierarchy and propagates any
// permission or API error. Used during connector init when
// --sync-role-assignments=true so the operator sees an actionable error
// at startup rather than a silent tree-view regression at sync time.
//
// Shares the sync.Once with hierarchy(): whichever fires first populates
// the cache; the other becomes a no-op. Caller should invoke at most
// one of these per Connector lifetime.
func (c *Connector) ensureHierarchy(ctx context.Context) error {
	var loadErr error
	c.hierarchyOnce.Do(func() {
		idx, err := loadHierarchy(ctx, c.token, c.client.ArmOptions())
		c.hierarchyCache = idx
		loadErr = err
	})
	return loadErr
}

// hierarchy returns the memoized tenant hierarchy, loading on first call
// if not yet populated. Errors are swallowed: this is the graceful-
// degrade path used by callers for which mgmt-group-read is a nice-to-
// have rather than a hard requirement (e.g. subscriptionBuilder when
// --sync-role-assignments is off).
//
// When --sync-role-assignments=true the connector init path calls
// ensureHierarchy first; this method then returns the cached result.
func (c *Connector) hierarchy(ctx context.Context) hierarchyIndex {
	c.hierarchyOnce.Do(func() {
		idx, err := loadHierarchy(ctx, c.token, c.client.ArmOptions())
		if err != nil {
			ctxzap.Extract(ctx).Warn(
				"baton-azure-infrastructure: hierarchy load failed; scope parents will be unset",
				zap.Error(err))
		}
		c.hierarchyCache = idx
	})
	return c.hierarchyCache
}

// loadHierarchy fetches the entities list + returns the keyed parent
// index. Errors are returned, not swallowed — caller decides whether
// to treat them as fatal. A 403 from the entities API is translated to
// errMgmtGroupReadRequired so the fatal path can give the operator a
// pointer to the actual permission needed.
func loadHierarchy(ctx context.Context, token azcore.TokenCredential, armOpts *arm.ClientOptions) (hierarchyIndex, error) {
	client, err := armmanagementgroups.NewEntitiesClient(token, armOpts)
	if err != nil {
		return hierarchyIndex{}, fmt.Errorf("baton-azure-infrastructure: constructing entities client: %w", err)
	}

	var entities []*armmanagementgroups.EntityInfo
	pager := client.NewListPager(nil)
	for pager.More() {
		page, pagerErr := pager.NextPage(ctx)
		if pagerErr != nil {
			if isForbidden(pagerErr) {
				// Wrap both so callers using errors.Is / errors.As can match
				// either the sentinel (for user-facing messaging) or the
				// underlying Azure response error.
				return hierarchyIndex{}, fmt.Errorf("%w: %w", errMgmtGroupReadRequired, pagerErr)
			}
			return hierarchyIndex{}, fmt.Errorf("baton-azure-infrastructure: listing management-group entities: %w", pagerErr)
		}
		entities = append(entities, page.Value...)
	}
	return buildHierarchyIndex(entities), nil
}

// buildHierarchyIndex is the pure part of hierarchy construction: takes a
// slice of EntityInfo and returns the keyed parent-ResourceId map. Unit-
// tested directly without needing a live Azure tenant.
func buildHierarchyIndex(entities []*armmanagementgroups.EntityInfo) hierarchyIndex {
	idx := make(hierarchyIndex, len(entities))
	for _, e := range entities {
		key := hierarchyKeyForEntity(e)
		if key == "" {
			continue
		}
		parent := hierarchyParentResourceID(e)
		if parent == nil {
			continue
		}
		idx[key] = parent
	}
	return idx
}

// hierarchyKeyForEntity returns the key under which an entity is indexed,
// matching the resource-builder's emitted ID format:
//
//	management_group → full ARM path
//	subscription → bare GUID
//
// Returns "" for unrecognized entity types or malformed input.
func hierarchyKeyForEntity(e *armmanagementgroups.EntityInfo) string {
	if e == nil || e.ID == nil || e.Type == nil {
		return ""
	}
	switch {
	case strings.HasSuffix(*e.Type, entityTypeManagementGroupSuffix):
		return *e.ID
	case strings.HasSuffix(*e.Type, entityTypeSubscriptionSuffix):
		// Subscription entity ID from this API is "/subscriptions/<GUID>";
		// the subscription builder's resource ID is the bare GUID — that's
		// what lives in EntityInfo.Name, which is also the last path
		// segment of the ID. Prefer Name; fall back to path suffix.
		if e.Name != nil && *e.Name != "" {
			return *e.Name
		}
		const subPrefix = "/subscriptions/"
		if strings.HasPrefix(*e.ID, subPrefix) {
			return (*e.ID)[len(subPrefix):]
		}
		return ""
	default:
		return ""
	}
}

// hierarchyParentResourceID builds the parent ResourceId for an entity
// based on EntityInfo.Properties.Parent:
//
//	Parent present → (management_group, Parent.ID)
//	Parent nil     → the entity is the tenant root mgmt group;
//	                 parent is (tenant, TenantID)
//
// Returns nil if malformed (no way to determine a parent).
func hierarchyParentResourceID(e *armmanagementgroups.EntityInfo) *v2.ResourceId {
	if e == nil || e.Properties == nil {
		return nil
	}
	if e.Properties.Parent != nil && e.Properties.Parent.ID != nil && *e.Properties.Parent.ID != "" {
		return &v2.ResourceId{
			ResourceType: managementGroupResourceType.Id,
			Resource:     *e.Properties.Parent.ID,
		}
	}
	// No parent in the mgmt-group tree → this is the tenant root mgmt
	// group. Its conceptual parent in the baton resource graph is the
	// tenant resource, keyed by the AAD TenantID.
	if e.Properties.TenantID != nil && *e.Properties.TenantID != "" {
		return &v2.ResourceId{
			ResourceType: tenantResourceType.Id,
			Resource:     *e.Properties.TenantID,
		}
	}
	return nil
}
