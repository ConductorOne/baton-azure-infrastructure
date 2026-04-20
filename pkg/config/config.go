package config

//go:generate go run ./gen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"github.com/conductorone/baton-sdk/pkg/field"
)

var Config = field.NewConfiguration(
	[]field.SchemaField{
		field.BoolField(
			"use-cli-credentials",
			field.WithDescription("If true, uses the az cli to auth"),
		),
		field.StringField(
			"azure-client-secret",
			field.WithDescription("Azure Client Secret"),
		),
		field.StringField(
			"azure-tenant-id",
			field.WithDescription("Azure Tenant ID"),
		),
		field.StringField(
			"azure-client-id",
			field.WithDescription("Azure Client ID"),
		),
		field.BoolField(
			"mailboxSettings",
			field.WithDescription("If true, attempt to get mailbox settings for users to determine user purpose"),
		),
		field.BoolField(
			"skip-ad-groups",
			field.WithDescription("If true, skip syncing Windows Server Active Directory groups"),
		),
		field.StringField(
			"graph-domain",
			field.WithDescription("Domain for Microsoft Graph API"),
			field.WithDefaultValue("graph.microsoft.com"),
		),
		field.BoolField(
			"skip-unused-roles",
			field.WithDescription("Skip unused roles"),
			field.WithDefaultValue(false),
		),
		field.BoolField(
			"skip-sync-storage-containers",
			field.WithDescription("If true, storage containers is skipped"),
			field.WithDefaultValue(false),
		),
		field.BoolField(
			"skip-entra-id-p2-license-features",
			field.WithDescription("If true, skips the features that require a 'Microsoft Entra ID P2' or 'Microsoft Entra ID Governance' license on the tenant."),
			field.WithDefaultValue(false),
		),
		// INVESTIGATION FLAG - do not merge. Hidden from c1 UI + --help; settable
		// via BATON_DEDUP_CLASSIC_RBAC_EMISSIONS=true for local comparison runs.
		// When enabled, role.Grants returns nil, dropping the classic per-(role,
		// principal) fan-out that duplicates the sparse role_assignment binding.
		// Other builders (subscription already nil, storage_account complex PIM
		// mixing) not gated by this prototype.
		field.BoolField(
			"dedup-classic-rbac-emissions",
			field.WithDescription("INVESTIGATION ONLY. Suppress classic role-builder Grants fan-out (duplicates sparse role_assignment when opted in). Hidden env var; do not ship."),
			field.WithDefaultValue(false),
			field.WithHidden(true),
		),
	},
	field.WithConstraints(
		field.FieldsMutuallyExclusive(
			field.BoolField("use-cli-credentials"),
			field.StringField("azure-client-id"),
		),
		field.FieldsMutuallyExclusive(
			field.BoolField("use-cli-credentials"),
			field.StringField("azure-client-secret"),
		),
	),
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
