package config

//go:generate go run ./gen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"github.com/conductorone/baton-sdk/pkg/field"
)

// Field group constants. Used by c1's FieldGroupNameMapping in
// `c1/pkg/builtin_connectors/builtin_deployment.go`.
const (
	ServicePrincipalFieldGroup = "azure-infrastructure-group-service-principal"
	CLICredentialsFieldGroup   = "azure-infrastructure-group-cli-credentials" //nolint:gosec // field-group name, not a credential
)

var (
	// Auth-specific fields.
	azureClientIDField = field.StringField(
		"azure-client-id",
		field.WithDisplayName("Client ID"),
		field.WithDescription("Azure Client ID"),
		field.WithPlaceholder("Your Azure application (client) ID"),
	)
	azureClientSecretField = field.StringField(
		"azure-client-secret",
		field.WithDisplayName("Client secret"),
		field.WithDescription("Azure Client Secret"),
		field.WithPlaceholder("Your Azure application client secret"),
		field.WithIsSecret(true),
	)
	azureTenantIDField = field.StringField(
		"azure-tenant-id",
		field.WithDisplayName("Tenant ID"),
		field.WithDescription("Azure Tenant ID"),
		field.WithPlaceholder("Your Azure directory (tenant) ID"),
	)
	useCliCredentialsField = field.BoolField(
		"use-cli-credentials",
		field.WithDisplayName("Use CLI credentials"),
		field.WithDescription("If true, uses the az cli to auth. Local-dev only; not intended for production c1 deployments."),
		field.WithHidden(true),
	)

	// Common optional fields.
	mailboxSettingsField = field.BoolField(
		"mailboxSettings",
		field.WithDisplayName("Mailbox settings"),
		field.WithDescription("If true, attempt to get mailbox settings for users to determine user purpose"),
	)
	skipAdGroupsField = field.BoolField(
		"skip-ad-groups",
		field.WithDisplayName("Skip AD groups"),
		field.WithDescription("If true, skip syncing Windows Server Active Directory groups"),
	)
	graphDomainField = field.StringField(
		"graph-domain",
		field.WithDisplayName("Graph domain"),
		field.WithDescription("Domain for Microsoft Graph API"),
		field.WithDefaultValue("graph.microsoft.com"),
	)
	skipUnusedRolesField = field.BoolField(
		"skip-unused-roles",
		field.WithDisplayName("Skip unused roles"),
		field.WithDescription("Skip unused roles"),
		field.WithDefaultValue(false),
	)
	skipSyncStorageContainersField = field.BoolField(
		"skip-sync-storage-containers",
		field.WithDisplayName("Skip sync storage containers"),
		field.WithDescription("If true, storage containers is skipped"),
		field.WithDefaultValue(false),
	)
	skipEntraIDP2LicenseFeaturesField = field.BoolField(
		"skip-entra-id-p2-license-features",
		field.WithDisplayName("Skip Entra ID P2 License Features"),
		field.WithDescription("If true, skips the features that require a 'Microsoft Entra ID P2' or 'Microsoft Entra ID Governance' license on the tenant."),
		field.WithDefaultValue(false),
	)

	// Deprecated: retained for c1 schema compatibility. The flag is
	// no-op in this connector — its former behavior (skip user / group /
	// managed_identity sync) is now achieved by deselecting those
	// resource types via the SDK's built-in `--sync-resource-types`
	// filter or the c1 admin UI. Removing this field would orphan the
	// matching entry in c1's AzureInfrastructureConfigSchema; keep the
	// field name for schema match and document the deprecation.
	deprecatedExternalResourcesDescription = "Deprecated. No-op in this connector. " +
		"To defer user/group/managed_identity principals to a paired directory connector " +
		"(e.g. baton-microsoft-entra), deselect those resource types in the c1 admin UI " +
		"(or use --sync-resource-types locally)."
	enableSyncExternalResourcesViaBatonIDField = field.BoolField(
		"enable-sync-external-resources-via-baton-id",
		field.WithDisplayName("Enable sync external resources via Baton ID"),
		field.WithDescription(deprecatedExternalResourcesDescription),
		field.WithDefaultValue(false),
	)

	ConfigurationFields = []field.SchemaField{
		useCliCredentialsField,
		azureClientSecretField,
		azureTenantIDField,
		azureClientIDField,
		mailboxSettingsField,
		skipAdGroupsField,
		graphDomainField,
		skipUnusedRolesField,
		skipSyncStorageContainersField,
		enableSyncExternalResourcesViaBatonIDField,
		skipEntraIDP2LicenseFeaturesField,
	}
)

var Config = field.NewConfiguration(
	ConfigurationFields,
	field.WithConnectorDisplayName("Azure Infrastructure"),
	field.WithHelpUrl("/docs/baton/azure-infrastructure"),
	field.WithIconUrl("/static/app-icons/azure-infrastructure.svg"),
	field.WithIsDirectory(true),
	// Pairs with baton-microsoft-entra in the same c1 app: entra emits
	// user / group / managed_identity principals; this connector's
	// role_assignment grants reference those principals via c1's implicit
	// baton-ID cross-connector matching. Must match c1's existing
	// AzureInfrastructureConfigSchema (builtin_connectors/azure_infrastructure.go).
	field.WithSupportsExternalResources(true),
	field.WithRequiresExternalConnector(false),
	field.WithConstraints(
		field.FieldsMutuallyExclusive(useCliCredentialsField, azureClientIDField),
		field.FieldsMutuallyExclusive(useCliCredentialsField, azureClientSecretField),
	),
	field.WithFieldGroups([]field.SchemaFieldGroup{
		{
			Name:        ServicePrincipalFieldGroup,
			DisplayName: "Service principal",
			HelpText:    "Authenticate with a service principal (client ID + client secret + tenant ID). Recommended for production deployments.",
			Fields: []field.SchemaField{
				azureClientIDField,
				azureClientSecretField,
				azureTenantIDField,
				mailboxSettingsField,
				skipAdGroupsField,
				graphDomainField,
				skipUnusedRolesField,
				skipSyncStorageContainersField,
				enableSyncExternalResourcesViaBatonIDField,
				skipEntraIDP2LicenseFeaturesField,
			},
			Default: true,
		},
		{
			Name:        CLICredentialsFieldGroup,
			DisplayName: "Azure CLI credentials",
			HelpText:    "Authenticate using the local Azure CLI session. Intended for local development; not suitable for c1 cloud deployments.",
			Fields: []field.SchemaField{
				useCliCredentialsField,
				azureTenantIDField,
				mailboxSettingsField,
				skipAdGroupsField,
				graphDomainField,
				skipUnusedRolesField,
				skipSyncStorageContainersField,
				enableSyncExternalResourcesViaBatonIDField,
				skipEntraIDP2LicenseFeaturesField,
			},
			Default: false,
		},
	}),
)

func ValidateConfig(c *AzureInfrastructure) error {
	if c.UseCliCredentials && (c.AzureClientSecret != "" || c.AzureClientId != "") {
		return fmt.Errorf("use-cli-credentials and azure-client-secret/azure-client-id are mutually exclusive")
	}

	if !slices.Contains(client.ValidHosts, c.GraphDomain) {
		return fmt.Errorf(
			"baton-azure-infrastructure: invalid host: %s should be one of %s",
			c.GraphDomain,
			strings.Join(client.ValidHosts, ","),
		)
	}

	return nil
}
