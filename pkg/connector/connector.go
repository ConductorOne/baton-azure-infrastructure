package connector

import (
	"context"
	"io"
	"net/http"

	cfg "github.com/conductorone/baton-azure-infrastructure/pkg/config"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/cli"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/uhttp"

	"github.com/sourcegraph/conc/iter"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type Connector struct {
	token                 azcore.TokenCredential
	MailboxSettings       bool
	SkipAdGroups          bool
	organizationIDs       []string
	roleDefinitionsClient *armauthorization.RoleDefinitionsClient
	clientFactory         *armsubscription.ClientFactory
	client                *client.AzureClient
	SkipUnusedRoles       bool
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
		newStorageAccountBuilder(d),
		newContainerBuilder(d),
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
	// Add any validation logic here if needed
	return nil, nil
}

func NewConnectorFromToken(
	ctx context.Context,
	httpClient *http.Client,
	token azcore.TokenCredential,
	mailboxSettings bool,
	skipAdGroups bool,
	graphDomain string,
	skipUnusedRoles bool,
) (*Connector, error) {
	azureClient, err := client.NewAzureClient(ctx, httpClient, token, skipAdGroups, graphDomain)
	if err != nil {
		return nil, err
	}

	organizations, err := azureClient.GetOrganizations(ctx)
	if err != nil {
		return nil, err
	}
	organizationIDs := iter.Map(organizations, func(t *client.Organization) string {
		return t.ID
	})

	clientFactory, err := armsubscription.NewClientFactory(token, azureClient.ArmOptions())
	if err != nil {
		return nil, err
	}

	roleDefinitionsClient, err := armauthorization.NewRoleDefinitionsClient(token, azureClient.ArmOptions())
	if err != nil {
		return nil, err
	}

	c := &Connector{
		token:                 token,
		MailboxSettings:       mailboxSettings,
		SkipAdGroups:          skipAdGroups,
		clientFactory:         clientFactory,
		client:                azureClient,
		organizationIDs:       organizationIDs,
		SkipUnusedRoles:       skipUnusedRoles,
		roleDefinitionsClient: roleDefinitionsClient,
	}

	return c, nil
}

// New returns a new instance of the connector.
func New(ctx context.Context, config *cfg.AzureInfrastructure, opts *cli.ConnectorOpts) (connectorbuilder.ConnectorBuilderV2, []connectorbuilder.Opt, error) {
	// Validate config
	err := cfg.ValidateConfig(config)
	if err != nil {
		return nil, nil, err
	}

	var cred azcore.TokenCredential
	httpClient, err := uhttp.NewClient(
		ctx,
		[]uhttp.Option{
			uhttp.WithLogger(true, ctxzap.Extract(ctx)),
		}...,
	)
	if err != nil {
		return nil, nil, err
	}

	switch {
	case config.UseCliCredentials:
		cred, err = azidentity.NewAzureCLICredential(nil)
	case !IsEmpty(config.AzureTenantId) && !IsEmpty(config.AzureClientId) && !IsEmpty(config.AzureClientSecret):
		cred, err = azidentity.NewClientSecretCredential(
			config.AzureTenantId,
			config.AzureClientId,
			config.AzureClientSecret,
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
			TenantID: config.AzureTenantId,
		})
	}
	if err != nil {
		return nil, nil, err
	}

	connector, err := NewConnectorFromToken(
		ctx,
		httpClient,
		cred,
		config.Mailboxsettings,
		config.SkipAdGroups,
		config.GraphDomain,
		config.SkipUnusedRoles,
	)
	if err != nil {
		return nil, nil, err
	}

	return connector, nil, nil
}
