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
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type Connector struct {
	token                        azcore.TokenCredential
	MailboxSettings              bool
	SkipAdGroups                 bool
	organizationIDs              []string
	roleDefinitionsClient        *armauthorization.RoleDefinitionsClient
	clientFactory                *armsubscription.ClientFactory
	client                       *client.AzureClient
	SkipUnusedRoles              bool
	skipStorageContainerSync     bool
	skipEntraIDP2LicenseFeatures bool

	// hierarchyOnce + hierarchyCache memoize one call to
	// armmanagementgroups.EntitiesClient per sync. Builders that need to
	// set parentResourceId on scope resources (managementGroupBuilder,
	// subscriptionBuilder) consult this via (*Connector).hierarchy(ctx).
	// See hierarchy.go for the construction + degrade-gracefully logic.
	hierarchyOnce  sync.Once
	hierarchyCache hierarchyIndex

	// mgmtGroupsOnce + mgmtGroupsCache + mgmtGroupsErr memoize one call
	// to armmanagementgroups.NewClient().NewListPager() per sync. Both
	// managementGroupBuilder.List and roleAssignmentBuilder.listInit
	// need the raw management-group list; without this memo each sync
	// does the full pager walk twice. See (*Connector).managementGroups
	// in management_group.go for the construction.
	mgmtGroupsOnce  sync.Once
	mgmtGroupsCache []*armmanagementgroups.ManagementGroupInfo
	mgmtGroupsErr   error
}

// ResourceSyncers returns a ResourceSyncer for each resource type that should be synced from the upstream service.
//
// All builders are registered unconditionally. Gating is handled by:
//   - OptInRequired annotations on role_assignment + management_group resource
//     types → c1 admin UI renders an unchecked checkbox; c1 only includes these
//     types in SyncFull.SyncResourceTypeIds after opt-in. See resource_types.go.
//   - baton-sdk's --sync-resource-types CLI flag for standalone runs — pass a
//     comma-separated list to restrict which builders are dispatched.
//   - Pair-with-entra deployments deselect user/group/managed_identity via the
//     c1 admin UI (or --sync-resource-types) rather than a bespoke connector flag.
func (d *Connector) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncerV2 {
	syncers := []connectorbuilder.ResourceSyncerV2{
		newUserBuilder(d),
		newGroupBuilder(d),
		newManagedIdentityBuilder(d),
		newSubscriptionBuilder(d),
		newTenantBuilder(d),
		newResourceGroupBuilder(d),
		newEnterpriseApplicationsBuilder(d),
		newRoleBuilder(d),
		newStorageAccountBuilder(d),
		newRoleAssignmentBuilder(d),
		newManagementGroupBuilder(d),
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
	skipEntraIDP2LicenseFeatures bool,
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

	// Management Group Reader is a hard prerequisite for the role_assignment +
	// management_group builders (they need the tenant→mgmt-group→sub hierarchy
	// to wire scope parents cleanly). That check now lives in each builder's
	// first-List path rather than here at init — unopted-in deployments must
	// not fail at startup just because an optional type lacks permissions.
	return &Connector{
		token:                        token,
		MailboxSettings:              mailboxSettings,
		SkipAdGroups:                 skipAdGroups,
		clientFactory:                clientFactory,
		client:                       azureClient,
		organizationIDs:              organizationIDs,
		SkipUnusedRoles:              skipUnusedRoles,
		skipStorageContainerSync:     skipStorageContainerSync,
		roleDefinitionsClient:        roleDefinitionsClient,
		skipEntraIDP2LicenseFeatures: skipEntraIDP2LicenseFeatures,
	}, nil
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
	skipEntraIDP2LicenseFeatures bool,
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
		skipEntraIDP2LicenseFeatures,
	)
}
