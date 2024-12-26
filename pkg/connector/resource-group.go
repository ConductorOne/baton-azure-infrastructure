package connector

import (
	"context"

	armresources "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	armsubscription "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
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
	clientFactory, err := armsubscription.NewClientFactory(rg.cn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	pagerSubscriptions := clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pagerSubscriptions.More() {
		page, err := pagerSubscriptions.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, subscription := range page.Value {
			client, err := armresources.NewResourceGroupsClient(*subscription.SubscriptionID, rg.cn.token, nil)
			if err != nil {
				return nil, "", nil, err
			}

			for pager := client.NewListPager(nil); pager.More(); {
				page, err := pager.NextPage(ctx)
				if err != nil {
					return nil, "", nil, err
				}

				// NOTE: The service desides how many items to return on a page.
				// If a page has 0 items, go get the next page.
				// Other clients may be adding/deleting items from the collection while
				// this code is paging; some items may be skipped or returned multiple times.
				for _, groupList := range page.Value {
					gr, err := groupListResource(ctx, groupList, &v2.ResourceId{
						ResourceType: subscriptionsResourceType.Id,
						Resource:     StringValue(subscription.SubscriptionID),
					})
					if err != nil {
						return nil, "", nil, err
					}

					rv = append(rv, gr)
				}
			}
		}
	}

	return rv, "", nil, nil
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
