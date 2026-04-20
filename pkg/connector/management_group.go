package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// managementGroupBuilder emits one resource per Azure management group visible
// to the caller. Management groups are pure scope-carrier resources (no
// entitlements, no grants of their own) — they exist so that role_assignment
// resources whose scope is a management group can reference a real, emitted
// parent in the c1z. Without this builder, ScopeBindingTrait annotations
// pointing at mgmt-group scopes dangle.
//
// Gated by the same OptInRequired annotation as roleAssignmentResourceType so
// c1 admins opt both types in together; there's no reason to emit mgmt groups
// without the role assignments that reference them.
type managementGroupBuilder struct {
	conn *Connector
}

func newManagementGroupBuilder(conn *Connector) *managementGroupBuilder {
	return &managementGroupBuilder{conn: conn}
}

func (b *managementGroupBuilder) ResourceType(_ context.Context) *v2.ResourceType {
	return managementGroupResourceType
}

func (b *managementGroupBuilder) List(ctx context.Context, _ *v2.ResourceId, _ *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	// Hard-require the hierarchy: management-group resources must carry parent
	// wiring for c1's tree UX to render them correctly. Fail fast here if the
	// SP lacks Management Group Reader. See hierarchy.go for the shared memo.
	if err := b.conn.ensureHierarchy(ctx); err != nil {
		return nil, "", nil, err
	}

	mgs, err := b.conn.managementGroups(ctx)
	if err != nil {
		return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: listing management groups: %w", err)
	}

	// One-shot tenant-hierarchy load so managementGroupResource can set
	// parentResourceId correctly for nested mgmt groups (and tenant-root mgmt
	// groups point at the tenant resource). If this call 403s, the index is
	// empty and parents are simply omitted — prior disconnected-roots
	// behavior preserved as degradation.
	idx := b.conn.hierarchy(ctx)

	rv := make([]*v2.Resource, 0, len(mgs))
	for _, mg := range mgs {
		resource, err := managementGroupResource(mg, idx)
		if err != nil {
			return nil, "", nil, err
		}
		if resource != nil {
			rv = append(rv, resource)
		}
	}
	return rv, "", nil, nil
}

func (b *managementGroupBuilder) Entitlements(_ context.Context, _ *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (b *managementGroupBuilder) Grants(_ context.Context, _ *v2.Resource, _ *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

// managementGroupResource builds a baton resource for an Azure management group.
// The baton resource ID is the ARM scope path so ScopeBindingTrait.scopeResourceId
// references (which carry the ARM path verbatim) resolve correctly.
//
// idx is the optional tenant-hierarchy lookup (from (*Connector).hierarchy).
// When present, it supplies the parentResourceId — nested mgmt groups point
// at their parent mgmt group, the tenant root mgmt group points at the
// tenant resource. When empty (SP lacks mgmt-group-read or caller didn't
// load it), parentResourceId is left unset and the resource renders as a
// root in the tree.
func managementGroupResource(mg *armmanagementgroups.ManagementGroupInfo, idx hierarchyIndex) (*v2.Resource, error) {
	if mg == nil || mg.ID == nil || mg.Name == nil {
		return nil, nil
	}

	// mg.ID is the ARM path (e.g. "/providers/Microsoft.Management/managementGroups/c1connectors-root")
	// mg.Name is the short identifier (e.g. "c1connectors-root" or a GUID for Tenant Root Group)
	displayName := StringValue(mg.Name)
	if mg.Properties != nil && mg.Properties.DisplayName != nil && *mg.Properties.DisplayName != "" {
		displayName = *mg.Properties.DisplayName
	}

	var opts []rs.ResourceOption
	if parent := idx[StringValue(mg.ID)]; parent != nil {
		opts = append(opts, rs.WithParentResourceID(parent))
	}

	return rs.NewResource(
		displayName,
		managementGroupResourceType,
		StringValue(mg.ID),
		opts...,
	)
}

// managementGroups returns the memoized list of management groups visible
// to the caller. Both managementGroupBuilder.List and
// roleAssignmentBuilder.listInit need this; without memoization each sync
// does the full pager walk twice. The cache lives for the connector's
// lifetime (one sync).
func (c *Connector) managementGroups(ctx context.Context) ([]*armmanagementgroups.ManagementGroupInfo, error) {
	c.mgmtGroupsOnce.Do(func() {
		c.mgmtGroupsCache, c.mgmtGroupsErr = listManagementGroups(ctx, c.token, c.client.ArmOptions())
	})
	return c.mgmtGroupsCache, c.mgmtGroupsErr
}

// listManagementGroups enumerates every management group the caller can see.
// Returns an empty slice + nil error if the caller lacks tenant-root access
// (403) — mirrors role_assignments.go:213's degrade-gracefully pattern for the
// analogous tenant-root 403 during role assignment enumeration. Any other
// error is propagated.
//
// Callers should usually go through (*Connector).managementGroups instead
// to amortize the pager walk across multiple builders.
func listManagementGroups(ctx context.Context, token azcore.TokenCredential, armOpts *arm.ClientOptions) ([]*armmanagementgroups.ManagementGroupInfo, error) {
	client, err := armmanagementgroups.NewClient(token, armOpts)
	if err != nil {
		return nil, fmt.Errorf("managementgroups client: %w", err)
	}

	var out []*armmanagementgroups.ManagementGroupInfo
	pager := client.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isForbidden(err) {
				l := ctxzap.Extract(ctx)
				l.Warn("baton-azure-infrastructure: caller lacks access to management groups; skipping mgmt-group-scope coverage", zap.Error(err))
				return nil, nil
			}
			return nil, fmt.Errorf("listing management groups: %w", err)
		}
		out = append(out, page.Value...)
	}
	return out, nil
}

// isForbidden detects the Azure ARM 403 that indicates the SP lacks access at
// the queried scope. Used by the mgmt-group walker to degrade gracefully when
// the caller has subscription-level creds but not tenant-root creds.
// isForbidden detects 403 on the two error shapes Azure calls arrive in
// through this connector: raw azcore.ResponseError (SDK-native) and
// gRPC-status PermissionDenied (baton-sdk middleware shape). Covering both
// matters because list-walk errors and Grant/Revoke errors travel
// different paths.
func isForbidden(err error) bool {
	if err == nil {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) && respErr.StatusCode == http.StatusForbidden {
		return true
	}
	return status.Code(err) == codes.PermissionDenied
}
