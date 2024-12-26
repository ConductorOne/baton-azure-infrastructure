package connector

import (
	"context"
	"net/http"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
)

type subscriptionBuilder struct {
	cn *Connector
}

var scopes = []string{"https://management.azure.com/.default"}

func (s *subscriptionBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return subscriptionsResourceType
}

func (s *subscriptionBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
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
	err = s.cn.query(ctx, scopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, "", nil, err
	}

	for _, subscription := range resp.Subscription {
		sr, err := subscriptionResource(ctx, &subscription)
		if err != nil {
			return nil, "", nil, err
		}

		reqURL = resourceGroupURL(subscription.SubscriptionID)
		respGroupList := &ResourceGroupList{}
		errGroupList := s.cn.query(ctx, scopes, http.MethodGet, reqURL, nil, respGroupList)
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

		rv = append(rv, sr)
	}

	pageToken, err := bag.NextToken(resp.NextLink)
	if err != nil {
		return nil, "", nil, err
	}

	return rv, pageToken, nil, nil
}

func (s *subscriptionBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (s *subscriptionBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func newSubscriptionBuilder(conn *Connector) *subscriptionBuilder {
	return &subscriptionBuilder{cn: conn}
}
