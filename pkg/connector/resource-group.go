package connector

import (
	"context"
	"net/http"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
)

type resourceGroupBuilder struct {
	cn *Connector
}

func (rg *resourceGroupBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceGroupResourceType
}

func (rg *resourceGroupBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	bag, err := parsePageToken(pToken.Token, &v2.ResourceId{ResourceType: groupResourceType.Id})
	if err != nil {
		return nil, "", nil, err
	}

	reqURL := bag.PageToken()
	if reqURL == "" {
		reqURL = subscriptionURL()
	}

	resp := &SubscriptionList{}
	err = rg.cn.query(ctx, scopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, "", nil, err
	}

	for _, subscription := range resp.Subscription {
		reqURL = resourceGroupURL(subscription.SubscriptionID)
		respGroupList := &ResourceGroupList{}
		errGroupList := rg.cn.query(ctx, scopes, http.MethodGet, reqURL, nil, respGroupList)
		if errGroupList != nil {
			return nil, "", nil, errGroupList
		}

		for _, groupList := range respGroupList.ResourceGroup {
			gr, err := groupListResource(ctx, &groupList, &v2.ResourceId{
				ResourceType: subscriptionsResourceType.Id,
				Resource:     subscription.SubscriptionID,
			})
			if err != nil {
				return nil, "", nil, err
			}

			rv = append(rv, gr)
		}
	}

	pageToken, err := bag.NextToken(resp.NextLink)
	if err != nil {
		return nil, "", nil, err
	}

	return rv, pageToken, nil, nil
}

func (rg *resourceGroupBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (rg *resourceGroupBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func newResourceGroupBuilder(conn *Connector) *resourceGroupBuilder {
	return &resourceGroupBuilder{cn: conn}
}
