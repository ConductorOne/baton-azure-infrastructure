package connector

import (
	"context"
	"net/http"

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
	bag, err := parsePageToken(pToken.Token, &v2.ResourceId{ResourceType: groupResourceType.Id})
	if err != nil {
		return nil, "", nil, err
	}

	reqURL := bag.PageToken()
	if reqURL == "" {
		reqURL = tenantURL()
	}

	resp := &TenantList{}
	err = t.cn.query(ctx, scopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, "", nil, err
	}

	for _, tenant := range resp.Tenant {
		sr, err := tenantResource(ctx, &tenant)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, sr)
	}

	pageToken, err := bag.NextToken(resp.NextLink)
	if err != nil {
		return nil, "", nil, err
	}

	return rv, pageToken, nil, nil
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
