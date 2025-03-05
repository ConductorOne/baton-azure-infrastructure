package connector

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"

	azcore "github.com/Azure/azure-sdk-for-go/sdk/azcore"
	azidentity "github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	armsubscription "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	uhttp "github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type Connector struct {
	token                 azcore.TokenCredential
	httpClient            *uhttp.BaseHttpClient
	MailboxSettings       bool
	SkipAdGroups          bool
	organizationIDs       []string
	roleDefinitionsClient *armauthorization.RoleDefinitionsClient
	clientFactory         *armsubscription.ClientFactory
	client                *client.AzureClient
}

// ResourceSyncers returns a ResourceSyncer for each resource type that should be synced from the upstream service.
func (d *Connector) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	syncers := []connectorbuilder.ResourceSyncer{
		newUserBuilder(d),
		newGroupBuilder(d),
		newSubscriptionBuilder(d),
		newTenantBuilder(d),
		newResourceGroupBuilder(d),
		newManagedIdentityBuilder(d),
		newEnterpriseApplicationsBuilder(d),
		newRoleBuilder(d),
	}
	return syncers
}

// Asset takes an input AssetRef and attempts to fetch it using the connector's authenticated http client
// It streams a response, always starting with a metadata object, following by chunked payloads for the asset.
func (d *Connector) Asset(ctx context.Context, asset *v2.AssetRef) (string, io.ReadCloser, error) {
	return "", nil, nil
}

// Metadata returns metadata about the connector.
func (d *Connector) Metadata(ctx context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "Microsoft Azure",
		Description: "Connector for Microsoft Azure",
	}, nil
}

// Validate is called to ensure that the connector is properly configured. It should exercise any API credentials
// to be sure that they are valid.
func (d *Connector) Validate(ctx context.Context) (annotations.Annotations, error) {
	return nil, nil
}

func NewConnectorFromToken(ctx context.Context,
	httpClient *http.Client,
	token azcore.TokenCredential,
	mailboxSettings bool,
	skipAdGroups bool,
) (*Connector, error) {
	baseClient, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return nil, err
	}

	clientFactory, err := armsubscription.NewClientFactory(token, nil)
	if err != nil {
		return nil, err
	}

	azureClient, err := client.NewAzureClient(ctx, httpClient, token)
	if err != nil {
		return nil, err
	}

	c := &Connector{
		token:           token,
		httpClient:      baseClient,
		MailboxSettings: mailboxSettings,
		SkipAdGroups:    skipAdGroups,
		clientFactory:   clientFactory,
		client:          azureClient,
	}

	organizationIDs, err := c.getOrganizationIDs(ctx)
	if err != nil {
		return nil, err
	}
	c.organizationIDs = organizationIDs

	roleDefinitionsClient, err := c.getRoleDefinitionsClient()
	if err != nil {
		return nil, err
	}
	c.roleDefinitionsClient = roleDefinitionsClient

	return c, nil
}

func (d *Connector) getRoleDefinitionsClient() (*armauthorization.RoleDefinitionsClient, error) {
	client, err := armauthorization.NewRoleDefinitionsClient(d.token, nil)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (d *Connector) getOrganizationIDs(ctx context.Context) ([]string, error) {
	resp := &Organizations{}
	reqURL := d.buildBetaURL("organization", nil)
	err := d.query(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-microsoft-entra: failed to get organization ID: %w", err)
	}

	organizationIDs := []string{}
	for _, org := range resp.Value {
		organizationIDs = append(organizationIDs, org.ID)
	}

	return organizationIDs, nil
}

// New returns a new instance of the connector.
func New(ctx context.Context, useCliCredentials bool, tenantID, clientID, clientSecret string, mailboxSettings bool, skipAdGroups bool) (*Connector, error) {
	var cred azcore.TokenCredential
	httpClient, err := uhttp.NewClient(
		ctx,
		[]uhttp.Option{
			uhttp.WithLogger(true, ctxzap.Extract(ctx)),
		}...,
	)
	if err != nil {
		return nil, err
	}

	switch {
	case useCliCredentials:
		cred, err = azidentity.NewAzureCLICredential(nil)
	case !IsEmpty(tenantID) && !IsEmpty(clientID) && !IsEmpty(clientSecret):
		cred, err = azidentity.NewClientSecretCredential(tenantID,
			clientID,
			clientSecret,
			&azidentity.ClientSecretCredentialOptions{
				ClientOptions: azcore.ClientOptions{
					Transport: httpClient,
				},
			})
	default:
		cred, err = azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: httpClient,
			},
			TenantID: tenantID,
		})
	}
	if err != nil {
		return nil, err
	}

	return NewConnectorFromToken(ctx,
		httpClient,
		cred,
		mailboxSettings,
		skipAdGroups,
	)
}
