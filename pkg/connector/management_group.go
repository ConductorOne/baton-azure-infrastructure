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
)

// managementGroupBuilder emits one resource per Azure management group visible
// to the caller. Management groups are pure scope-carrier resources (no
// entitlements, no grants of their own) — they exist so that role_assignment
// resources whose scope is a management group can reference a real, emitted
// parent in the c1z. Without this builder, ScopeBindingTrait annotations
// pointing at mgmt-group scopes dangle.
//
// Gated on the same --sync-role-assignments flag as roleAssignmentBuilder;
// there's no reason to emit mgmt groups without the role assignments that
// reference them.
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
	mgs, err := listManagementGroups(ctx, b.conn.token, b.conn.client.ArmOptions())
	if err != nil {
		return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: listing management groups: %w", err)
	}

	rv := make([]*v2.Resource, 0, len(mgs))
	for _, mg := range mgs {
		resource, err := managementGroupResource(mg)
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
func managementGroupResource(mg *armmanagementgroups.ManagementGroupInfo) (*v2.Resource, error) {
	if mg == nil || mg.ID == nil || mg.Name == nil {
		return nil, nil
	}

	// mg.ID is the ARM path (e.g. "/providers/Microsoft.Management/managementGroups/c1connectors-root")
	// mg.Name is the short identifier (e.g. "c1connectors-root" or a GUID for Tenant Root Group)
	displayName := StringValue(mg.Name)
	if mg.Properties != nil && mg.Properties.DisplayName != nil && *mg.Properties.DisplayName != "" {
		displayName = *mg.Properties.DisplayName
	}

	return rs.NewResource(
		displayName,
		managementGroupResourceType,
		StringValue(mg.ID),
	)
}

// listManagementGroups enumerates every management group the caller can see.
// Returns an empty slice + nil error if the caller lacks tenant-root access
// (403) — mirrors role_assignments.go:213's degrade-gracefully pattern for the
// analogous tenant-root 403 during role assignment enumeration. Any other
// error is propagated.
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
func isForbidden(err error) bool {
	if err == nil {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		return respErr.StatusCode == http.StatusForbidden
	}
	return false
}
