package connector

import (
	"context"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type tenantBuilder struct {
	conn *Connector
}

func (t *tenantBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return tenantResourceType
}

func (t *tenantBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	var rv []*v2.Resource
	pager := t.conn.clientFactory.NewTenantsClient().NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, err
		}

		for _, tenant := range page.Value {
			sr, err := tenantResource(ctx, tenant)
			if err != nil {
				return nil, nil, err
			}

			rv = append(rv, sr)
		}
	}

	return rv, nil, nil
}

func (t *tenantBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func (t *tenantBuilder) Grants(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func newTenantBuilder(conn *Connector) *tenantBuilder {
	return &tenantBuilder{
		conn: conn,
	}
}
