package main

import (
	"fmt"
	"slices"
	"strings"

	cfg "github.com/conductorone/baton-azure-infrastructure/pkg/config"
	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"

	"github.com/spf13/viper"
)

// ValidateConfig is run after the configuration is loaded, and should return an error if it isn't valid.
func ValidateConfig(v *viper.Viper) error {
	useCliCredentials := v.GetBool(cfg.UseCliCredentialsField.FieldName)
	azureClientSecret := v.GetString(cfg.AzureClientSecretField.FieldName)
	azureClientId := v.GetString(cfg.AzureClientIdField.FieldName)
	if useCliCredentials && (azureClientSecret != "" || azureClientId != "") {
		return fmt.Errorf("use-cli-credentials and azure-client-secret/azure-client-id are mutually exclusive")
	}

	host := v.GetString(cfg.GraphDomainField.FieldName)

	if !slices.Contains(client.ValidHosts, host) {
		return fmt.Errorf(
			"baton-azure-infrastructure: invalid host: %s should be one of %s",
			host,
			strings.Join(client.ValidHosts, ","),
		)
	}

	return nil
}
