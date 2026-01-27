package main

import (
	"context"
	"fmt"
	"os"

	cfg "github.com/conductorone/baton-azure-infrastructure/pkg/config"
	"github.com/conductorone/baton-azure-infrastructure/pkg/connector"
	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/connectorrunner"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()
	_, cmd, err := config.DefineConfiguration(ctx, "baton-azure-infrastructure", getConnector, cfg.Config,
		connectorrunner.WithDefaultCapabilitiesConnectorBuilder(&connector.Connector{}),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	cmd.Version = version
	cmd.MarkFlagsMutuallyExclusive("use-cli-credentials", "azure-client-secret")
	err = cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func getConnector(ctx context.Context, v *viper.Viper) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)
	if err := ValidateConfig(v); err != nil {
		return nil, err
	}

	useCliCredentials := v.GetBool(cfg.UseCliCredentialsField.FieldName)
	azureTenantId := v.GetString(cfg.AzureTenantIdField.FieldName)
	azureClientSecret := v.GetString(cfg.AzureClientSecretField.FieldName)
	azureClientId := v.GetString(cfg.AzureClientIdField.FieldName)
	mailboxSettings := v.GetBool(cfg.MailboxSettingsField.FieldName)
	skipAdGroups := v.GetBool(cfg.SkipAdGroupsField.FieldName)
	graphDomain := v.GetString(cfg.GraphDomainField.FieldName)
	skipUnusedRoles := v.GetBool(cfg.SkipUnusedRolesField.FieldName)
	skipStorageContainerSync := v.GetBool(cfg.SkipStorageContainerSyncField.FieldName)
	enableSyncExternalResourcesViaBatonID := v.GetBool(cfg.EnableSyncExternalResourcesViaBatonIDField.FieldName)
	skipEntraIDP2LicenseFeatures := v.GetBool(cfg.SkipEntraIDP2LicenseFeaturesField.FieldName)

	cb, err := connector.New(
		ctx,
		useCliCredentials,
		azureTenantId,
		azureClientId,
		azureClientSecret,
		mailboxSettings,
		skipAdGroups,
		graphDomain,
		skipUnusedRoles,
		skipStorageContainerSync,
		enableSyncExternalResourcesViaBatonID,
		skipEntraIDP2LicenseFeatures,
	)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}

	c, err := connectorbuilder.NewConnector(ctx, cb)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}

	return c, nil
}
