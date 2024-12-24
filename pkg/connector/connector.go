package connector

import (
	"context"
	"io"
	"net/http"

	azcore "github.com/Azure/azure-sdk-for-go/sdk/azcore"
	azidentity "github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	uhttp "github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type Connector struct {
	token           azcore.TokenCredential
	httpClient      *uhttp.BaseHttpClient
	MailboxSettings bool
	SkipAdGroups    bool
}

// ResourceSyncers returns a ResourceSyncer for each resource type that should be synced from the upstream service.
func (d *Connector) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	return []connectorbuilder.ResourceSyncer{
		newUserBuilder(d),
		newGroupBuilder(d),
		newSubscriptionBuilder(d),
	}
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

func NewConnectorFromToken(ctx context.Context, httpClient *http.Client, token azcore.TokenCredential, mailboxSettings bool, skipAdGroups bool) (*Connector, error) {
	client, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return nil, err
	}

	return &Connector{
		token:           token,
		httpClient:      client,
		MailboxSettings: mailboxSettings,
		SkipAdGroups:    skipAdGroups,
	}, nil
}

// New returns a new instance of the connector.
func New(ctx context.Context, useCliCredentials bool, tenantID, clientID, clientSecret string, mailboxSettings bool, skipAdGroups bool) (*Connector, error) {
	var cred azcore.TokenCredential
	uhttpOptions := []uhttp.Option{
		uhttp.WithLogger(true, ctxzap.Extract(ctx)),
	}
	httpClient, err := uhttp.NewClient(
		ctx,
		uhttpOptions...,
	)
	if err != nil {
		return nil, err
	}

	switch {
	case useCliCredentials:
		cred, err = azidentity.NewAzureCLICredential(nil)
	case !IsEmpty(tenantID) && !IsEmpty(clientID) && !IsEmpty(clientSecret):
		cred, err = azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, &azidentity.ClientSecretCredentialOptions{
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

	return NewConnectorFromToken(ctx, httpClient, cred, mailboxSettings, skipAdGroups)
}
