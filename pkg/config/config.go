package config

//go:generate go run ./gen

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	UseCliCredentialsField = field.BoolField(
		"use-cli-credentials",
		field.WithDescription("If true, uses the az cli to auth"),
	)

	AzureClientSecretField = field.StringField(
		"azure-client-secret",
		field.WithDescription("Azure Client Secret"),
	)

	AzureTenantIdField = field.StringField(
		"azure-tenant-id",
		field.WithDescription("Azure Tenant ID"),
	)

	AzureClientIdField = field.StringField(
		"azure-client-id",
		field.WithDescription("Azure Client ID"),
	)

	MailboxSettingsField = field.BoolField(
		"mailboxSettings",
		field.WithDescription("If true, attempt to get mailbox settings for users to determine user purpose"),
	)

	SkipAdGroupsField = field.BoolField(
		"skip-ad-groups",
		field.WithDescription("If true, skip syncing Windows Server Active Directory groups"),
	)

	GraphDomainField = field.StringField(
		"graph-domain",
		field.WithDescription("Domain for Microsoft Graph API"),
		field.WithDefaultValue("graph.microsoft.com"),
	)

	SkipUnusedRolesField = field.BoolField(
		"skip-unused-roles",
		field.WithDescription("Skip unused roles"),
		field.WithDefaultValue(false),
	)

	SkipStorageContainerSyncField = field.BoolField(
		"skip-sync-storage-containers",
		field.WithDescription("If true, storage containers is skipped"),
		field.WithDefaultValue(false),
	)

	EnableSyncExternalResourcesViaBatonIDField = field.BoolField(
		"enable-sync-external-resources-via-baton-id",
		field.WithDescription(`If true, the connector will use baton id to sync users and groups from external resources.
		 This could break the sync if the Baton ID external resource is not set up correctly.`),
		field.WithDefaultValue(false),
	)

	SkipEntraIDP2LicenseFeaturesField = field.BoolField(
		"skip-entra-id-p2-license-features",
		field.WithDescription("If true, skips the features that require a 'Microsoft Entra ID P2' or 'Microsoft Entra ID Governance' license on the tenant."),
		field.WithDefaultValue(false),
	)

	// ConfigurationFields defines the external configuration required for the
	// connector to run.
	ConfigurationFields = []field.SchemaField{
		UseCliCredentialsField,
		AzureClientSecretField,
		AzureTenantIdField,
		AzureClientIdField,
		MailboxSettingsField,
		SkipAdGroupsField,
		GraphDomainField,
		SkipUnusedRolesField,
		SkipStorageContainerSyncField,
		EnableSyncExternalResourcesViaBatonIDField,
		SkipEntraIDP2LicenseFeaturesField,
	}

	// FieldRelationships defines relationships between the fields.
	FieldRelationships = []field.SchemaFieldRelationship{
		field.FieldsMutuallyExclusive(UseCliCredentialsField, AzureClientIdField),
		field.FieldsMutuallyExclusive(UseCliCredentialsField, AzureClientSecretField),
	}

	// Config is the configuration schema for the connector.
	Config = field.Configuration{
		Fields:      ConfigurationFields,
		Constraints: FieldRelationships,
	}
)
