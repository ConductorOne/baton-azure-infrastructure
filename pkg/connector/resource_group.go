package connector

import (
	"context"

	armresources "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
)

type resourceGroupBuilder struct {
	conn *Connector
}

func (rg *resourceGroupBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceGroupResourceType
}

func (rg *resourceGroupBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	if parentResourceID == nil {
		return nil, "", nil, nil
	}

	var rv []*v2.Resource
	subscriptionID := parentResourceID.Resource

	client, err := armresources.NewResourceGroupsClient(
		subscriptionID,
		rg.conn.token,
		rg.conn.client.ArmOptions(),
	)
	if err != nil {
		return nil, "", nil, err
	}

	for pager := client.NewListPager(nil); pager.More(); {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		// NOTE: The service desides how many items to return on a page.
		// If a page has 0 items, then, get the next page.
		// Other clients may be adding/deleting items from the collection while
		// this code is paging; some items may be skipped or returned multiple times.
		for _, resourceGroup := range page.Value {
			gr, err := resourceGroupResource(ctx,
				resourceGroup,
				&v2.ResourceId{
					ResourceType: subscriptionsResourceType.Id,
					Resource:     StringValue(&subscriptionID),
				})
			if err != nil {
				return nil, "", nil, err
			}

			rv = append(rv, gr)
		}
	}

	return rv, "", nil, nil
}

func (rg *resourceGroupBuilder) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (rg *resourceGroupBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func newResourceGroupBuilder(c *Connector) *resourceGroupBuilder {
	return &resourceGroupBuilder{
		conn: c,
	}
}
