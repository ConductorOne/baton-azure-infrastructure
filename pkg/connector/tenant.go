package connector

import (
	"context"

	armsubscription "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
)

type tenantBuilder struct {
	cn *Connector
}

func (t *tenantBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return tenantResourceType
}

func (t *tenantBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	clientFactory, err := armsubscription.NewClientFactory(t.cn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	pager := clientFactory.NewTenantsClient().NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, tenant := range page.Value {
			sr, err := tenantResource(ctx, tenant)
			if err != nil {
				return nil, "", nil, err
			}

			rv = append(rv, sr)
		}
	}

	return rv, "", nil, nil
}

func (t *tenantBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (t *tenantBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func newTenantBuilder(conn *Connector) *tenantBuilder {
	return &tenantBuilder{cn: conn}
}
