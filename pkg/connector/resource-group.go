package connector

import (
	"context"
	"fmt"
	"strings"

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
	var rv []*v2.Resource
	pagerSubscriptions := rg.conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pagerSubscriptions.More() {
		page, err := pagerSubscriptions.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, subscription := range page.Value {
			client, err := armresources.NewResourceGroupsClient(*subscription.SubscriptionID, rg.conn.token, nil)
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
				for _, groupList := range page.Value {
					gr, err := resourceGroupResource(ctx, *subscription.SubscriptionID, groupList, &v2.ResourceId{
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

func getEntitlementId(resource *v2.Resource, roleId, subscriptionID, ctype string) string {
	return strings.Join(
		[]string{
			resourceGroupResourceType.Id,
			resource.Id.Resource,
			subscriptionID,
			roleId,
			ctype,
		},
		":")
}

func (rg *resourceGroupBuilder) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement
	lstRoles, err := getAllRoles(ctx, rg.conn)
	if err != nil {
		return nil, "", nil, err
	}

	lstSubscriptions, err := getAvailableSubscriptions(ctx, rg.conn)
	if err != nil {
		return nil, "", nil, err
	}

	for _, subscriptionID := range lstSubscriptions {
		for _, roleId := range lstRoles {
			v := &v2.Entitlement{
				Id:          getEntitlementId(resource, roleId, subscriptionID, typeOwners),
				Resource:    resource,
				DisplayName: fmt.Sprintf("%s Resource Group Owner", resource.DisplayName),
				Description: fmt.Sprintf("Owner of %s resource group", resource.DisplayName),
				GrantableTo: []*v2.ResourceType{userResourceType},
				Purpose:     v2.Entitlement_PURPOSE_VALUE_PERMISSION,
				Slug:        "owner",
			}
			rv = append(rv, v)

			v = &v2.Entitlement{
				Id:          getEntitlementId(resource, roleId, subscriptionID, typeMembers),
				Resource:    resource,
				DisplayName: fmt.Sprintf("%s Resource Group Assignment", resource.DisplayName),
				Description: fmt.Sprintf("Assigned to %s resource group", resource.DisplayName),
				GrantableTo: []*v2.ResourceType{userResourceType, groupResourceType},
				Purpose:     v2.Entitlement_PURPOSE_VALUE_ASSIGNMENT,
				Slug:        "assigned",
			}
			rv = append(rv, v)
		}
	}

	return rv, "", nil, nil
}

func (rg *resourceGroupBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func newResourceGroupBuilder(c *Connector) *resourceGroupBuilder {
	return &resourceGroupBuilder{
		conn: c,
	}
}
