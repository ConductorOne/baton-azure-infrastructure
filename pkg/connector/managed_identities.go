package connector

import (
	"context"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type managedIdentityBuilder struct {
	client *client.AzureClient
}

func (m *managedIdentityBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return managedIdentitylResourceType
}

func (m *managedIdentityBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, attrs resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	resp, err := m.client.ListServicePrincipalsManagedIdentity(ctx, attrs.PageToken.Token)
	if err != nil {
		return nil, nil, err
	}

	users, err := ConvertErr(resp.Value, func(mi *client.ServicePrincipal) (*v2.Resource, error) {
		return managedIdentityResource(ctx, mi, parentResourceID)
	})
	if err != nil {
		return nil, nil, err
	}

	return users, &resource.SyncOpResults{NextPageToken: resp.NextLink}, nil
}

// Entitlements always returns an empty slice for managed identities.
func (m *managedIdentityBuilder) Entitlements(_ context.Context, _ *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	return nil, nil, nil
}

// Grants always returns an empty slice for managed identities since they don't have any entitlements.
func (m *managedIdentityBuilder) Grants(ctx context.Context, _ *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	return nil, nil, nil
}

func newManagedIdentityBuilder(c *Connector) *managedIdentityBuilder {
	return &managedIdentityBuilder{
		client: c.client,
	}
}
