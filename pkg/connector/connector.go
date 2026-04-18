package connector

import (
	"context"
	"io"
	"net/http"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/uhttp"

	"github.com/sourcegraph/conc/iter"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"

	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type Connector struct {
	token                                 azcore.TokenCredential
	MailboxSettings                       bool
	SkipAdGroups                          bool
	organizationIDs                       []string
	roleDefinitionsClient                 *armauthorization.RoleDefinitionsClient
	clientFactory                         *armsubscription.ClientFactory
	client                                *client.AzureClient
	SkipUnusedRoles                       bool
	skipStorageContainerSync              bool
	enableSyncExternalResourcesViaBatonID bool
	skipEntraIDP2LicenseFeatures          bool
	syncRoleAssignments                   bool

	// hierarchyOnce + hierarchyCache memoize one call to
	// armmanagementgroups.EntitiesClient per sync. Builders that need to
	// set parentResourceId on scope resources (managementGroupBuilder,
	// subscriptionBuilder) consult this via (*Connector).hierarchy(ctx).
	// See hierarchy.go for the construction + degrade-gracefully logic.
	hierarchyOnce  sync.Once
	hierarchyCache hierarchyIndex
}

// ResourceSyncers returns a ResourceSyncer for each resource type that should be synced from the upstream service.
func (d *Connector) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	syncers := []connectorbuilder.ResourceSyncer{}

	// If we are syncing external resources via baton id, we don't need to sync users and groups.
	if !d.enableSyncExternalResourcesViaBatonID {
		syncers = append(syncers, newUserBuilder(d), newGroupBuilder(d), newManagedIdentityBuilder(d))
	}

	syncers = append(
		syncers,
		newSubscriptionBuilder(d),
		newTenantBuilder(d),
		newResourceGroupBuilder(d),
		newEnterpriseApplicationsBuilder(d),
		newRoleBuilder(d),
		newStorageAccountBuilder(d),
	)

	// Opt-in: emit Azure role assignments as TRAIT_SCOPE_BINDING resources so
	// the c1 uplift (PR ductone/c1#16540) can classify this app as SPARSE or
	// HYBRID and route it through the sparse-ACL UX. Default-off so existing
	// deployments don't silently re-classify on upgrade. management_group is
	// registered alongside so that role_assignment resources whose scope is a
	// mgmt group reference a real emitted parent resource in the c1z.
	if d.syncRoleAssignments {
		syncers = append(syncers,
			newRoleAssignmentBuilder(d),
			newManagementGroupBuilder(d),
		)
	}

	if !d.skipStorageContainerSync {
		syncers = append(syncers, newContainerBuilder(d))
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

func NewConnectorFromToken(
	ctx context.Context,
	httpClient *http.Client,
	token azcore.TokenCredential,
	mailboxSettings bool,
	skipAdGroups bool,
	graphDomain string,
	skipUnusedRoles bool,
	skipStorageContainerSync bool,
	syncExternalResourcesViaBatonID bool,
	skipEntraIDP2LicenseFeatures bool,
	syncRoleAssignments bool,
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
		token:                                 token,
		MailboxSettings:                       mailboxSettings,
		SkipAdGroups:                          skipAdGroups,
		clientFactory:                         clientFactory,
		client:                                azureClient,
		organizationIDs:                       organizationIDs,
		SkipUnusedRoles:                       skipUnusedRoles,
		skipStorageContainerSync:              skipStorageContainerSync,
		roleDefinitionsClient:                 roleDefinitionsClient,
		enableSyncExternalResourcesViaBatonID: syncExternalResourcesViaBatonID,
		skipEntraIDP2LicenseFeatures:          skipEntraIDP2LicenseFeatures,
		syncRoleAssignments:                   syncRoleAssignments,
	}

	return c, nil
}

// New returns a new instance of the connector.
func New(
	ctx context.Context,
	useCliCredentials bool,
	tenantID,
	clientID,
	clientSecret string,
	mailboxSettings bool,
	skipAdGroups bool,
	graphDomain string,
	skipUnusedRoles bool,
	skipStorageContainerSync bool,
	enableSyncExternalResourcesViaBatonID bool,
	skipEntraIDP2LicenseFeatures bool,
	syncRoleAssignments bool,
) (*Connector, error) {
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

	return NewConnectorFromToken(
		ctx,
		httpClient,
		cred,
		mailboxSettings,
		skipAdGroups,
		graphDomain,
		skipUnusedRoles,
		skipStorageContainerSync,
		enableSyncExternalResourcesViaBatonID,
		skipEntraIDP2LicenseFeatures,
		syncRoleAssignments,
	)
}
