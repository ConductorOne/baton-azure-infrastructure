package connector

import (
	"context"

	armsubscription "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
)

type subscriptionBuilder struct {
	cn *Connector
}

func (s *subscriptionBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return subscriptionsResourceType
}

func (s *subscriptionBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	clientFactory, err := armsubscription.NewClientFactory(s.cn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	pager := clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, subscription := range page.Value {
			sr, err := subscriptionResource(ctx, subscription)
			if err != nil {
				return nil, "", nil, err
			}

			rv = append(rv, sr)
		}
	}

	return rv, "", nil, nil
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
