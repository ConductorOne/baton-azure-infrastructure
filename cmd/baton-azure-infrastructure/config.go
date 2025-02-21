package main

import (
	"fmt"

	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/spf13/viper"
)

var (
	useCliCredentials = field.BoolField("use-cli-credentials", field.WithDescription("If true, uses the az cli to auth"))
	azureClientSecret = field.StringField("azure-client-secret", field.WithDescription("Azure Client Secret"))
	azureTenantId     = field.StringField("azure-tenant-id", field.WithDescription("Azure Tenant ID"))
	azureClientId     = field.StringField("azure-client-id", field.WithDescription("Azure Client ID"))
	mailboxSettings   = field.BoolField("mailboxSettings", field.WithDescription("If true, attempt to get mailbox settings for users to determine user purpose"))
	skipAdGroups      = field.BoolField("skip-ad-groups", field.WithDescription("If true, skip syncing Windows Server Active Directory groups"))
)

var ConfigurationFields = []field.SchemaField{
	useCliCredentials,
	azureClientSecret,
	azureTenantId,
	azureClientId,
	mailboxSettings,
	skipAdGroups,
}

var FieldRelationships = []field.SchemaFieldRelationship{
	field.FieldsMutuallyExclusive(useCliCredentials, azureClientId),
	field.FieldsMutuallyExclusive(useCliCredentials, azureClientSecret),
}

var cfg = field.NewConfiguration(ConfigurationFields, FieldRelationships...)

// validateConfig is run after the configuration is loaded, and should return an error if it isn't valid.
func ValidateConfig(v *viper.Viper) error {
	useCliCredentials := v.GetBool(useCliCredentials.FieldName)
	azureClientSecret := v.GetString(azureClientSecret.FieldName)
	azureClientId := v.GetString(azureClientId.FieldName)
	if useCliCredentials && (azureClientSecret != "" || azureClientId != "") {
		return fmt.Errorf("use-cli-credentials and azure-client-secret/azure-client-id are mutually exclusive")
	}
	return nil
}
