package connector

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
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

// List walks every subscription the SP can see and enumerates all role
// assignments visible to the caller (NewListPager returns assignments at
// subscription scope and descendants, per Azure semantics). Returns one
// baton resource per assignment.
func (b *roleAssignmentBuilder) List(ctx context.Context, _ *v2.ResourceId, _ *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource

	subsPager := b.conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
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
					if resource != nil {
						rv = append(rv, resource)
					}
				}
			}
		}
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
// for an ARM scope path. For scopes this connector doesn't emit, we fall
// through to the nearest parent we do emit.
func scopeResourceTypeForAzureScope(scope string) string {
	switch {
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
	case "#microsoft.graph.servicePrincipal", "Application", "ServicePrincipal":
		return enterpriseApplicationResourceType.Id
	case "ManagedIdentity":
		return managedIdentitylResourceType.Id
	default:
		return ""
	}
}
