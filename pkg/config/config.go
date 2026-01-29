package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	useCliCredentialsField = field.BoolField(
		"use-cli-credentials",
		field.WithDisplayName("Use CLI Credentials"),
		field.WithDescription("If true, uses the az cli to auth"),
	)
	azureClientSecretField = field.StringField(
		"azure-client-secret",
		field.WithDisplayName("Azure Client Secret"),
		field.WithDescription("Azure Client Secret"),
		field.WithIsSecret(true),
	)
	azureTenantIdField = field.StringField(
		"azure-tenant-id",
		field.WithDisplayName("Azure Tenant ID"),
		field.WithDescription("Azure Tenant ID"),
	)
	azureClientIdField = field.StringField(
		"azure-client-id",
		field.WithDisplayName("Azure Client ID"),
		field.WithDescription("Azure Client ID"),
	)
	mailboxSettingsField = field.BoolField(
		"mailboxSettings",
		field.WithDisplayName("Mailbox Settings"),
		field.WithDescription("If true, attempt to get mailbox settings for users to determine user purpose"),
	)
	skipAdGroupsField = field.BoolField(
		"skip-ad-groups",
		field.WithDisplayName("Skip AD Groups"),
		field.WithDescription("If true, skip syncing Windows Server Active Directory groups"),
	)
	graphDomainField = field.StringField(
		"graph-domain",
		field.WithDisplayName("Graph Domain"),
		field.WithDescription("Domain for Microsoft Graph API"),
		field.WithDefaultValue("graph.microsoft.com"),
	)
	skipUnusedRolesField = field.BoolField(
		"skip-unused-roles",
		field.WithDisplayName("Skip Unused Roles"),
		field.WithDescription("Skip unused roles"),
		field.WithDefaultValue(false),
	)
	configurationFields = []field.SchemaField{
		useCliCredentialsField,
		azureClientSecretField,
		azureTenantIdField,
		azureClientIdField,
		mailboxSettingsField,
		skipAdGroupsField,
		graphDomainField,
		skipUnusedRolesField,
	}
	fieldRelationships = []field.SchemaFieldRelationship{
		field.FieldsMutuallyExclusive(useCliCredentialsField, azureClientIdField),
		field.FieldsMutuallyExclusive(useCliCredentialsField, azureClientSecretField),
	}
)

//go:generate go run ./gen
var Config = field.NewConfiguration(
	configurationFields,
	field.WithConstraints(fieldRelationships...),
	field.WithConnectorDisplayName("Azure Infrastructure"),
)

// ValidateConfig is run after the configuration is loaded, and should return an error if it isn't valid.
func ValidateConfig(config *AzureInfrastructure) error {
	if config.UseCliCredentials && (config.AzureClientSecret != "" || config.AzureClientId != "") {
		return fmt.Errorf("use-cli-credentials and azure-client-secret/azure-client-id are mutually exclusive")
	}

	host := config.GraphDomain
	if host == "" {
		host = "graph.microsoft.com"
	}

	if !slices.Contains(client.ValidHosts, host) {
		return fmt.Errorf(
			"baton-azure-infrastructure: invalid host: %s should be one of %s",
			host,
			strings.Join(client.ValidHosts, ","),
		)
	}

	return nil
}
