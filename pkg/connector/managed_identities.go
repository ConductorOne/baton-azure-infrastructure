package connector

import (
	"context"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type managedIdentityBuilder struct {
	client *client.AzureClient
}

func (m *managedIdentityBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return managedIdentitylResourceType
}

func (m *managedIdentityBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	resp, err := m.client.ListServicePrincipalsManagedIdentity(ctx, opts.PageToken.Token)
	if err != nil {
		return nil, nil, err
	}

	users, err := ConvertErr(resp.Value, func(mi *client.ServicePrincipal) (*v2.Resource, error) {
		return managedIdentityResource(ctx, mi, parentResourceID)
	})
	if err != nil {
		return nil, nil, err
	}

	return users, &rs.SyncOpResults{NextPageToken: resp.NextLink}, nil
}

// Entitlements always returns an empty slice for managed identities.
func (m *managedIdentityBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

// Grants always returns an empty slice for managed identities since they don't have any entitlements.
func (m *managedIdentityBuilder) Grants(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func newManagedIdentityBuilder(c *Connector) *managedIdentityBuilder {
	return &managedIdentityBuilder{
		client: c.client,
	}
}
