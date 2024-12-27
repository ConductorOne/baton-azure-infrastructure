package connector

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
)

type roleBuilder struct {
	cn *Connector
}

func (r *roleBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return roleResourceType
}

func (r *roleBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	// Initialize the RoleDefinitionsClient
	client, err := armauthorization.NewRoleDefinitionsClient(r.cn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	// Define the scope (use "/" for subscription-level roles)
	scope := "/subscriptions/39ea64c5-86d5-4c29-8199-5b602c90e1c5"
	// Get the list of role definitions
	pager := client.NewListPager(scope, nil)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		// Iterate over role definitions
		for _, role := range resp.Value {
			sr, err := roleResource(ctx, role)
			if err != nil {
				return nil, "", nil, err
			}

			rv = append(rv, sr)
		}
	}

	return rv, "", nil, nil
}

func (r *roleBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (r *roleBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func newRoleBuilder(conn *Connector) *roleBuilder {
	return &roleBuilder{cn: conn}
}
