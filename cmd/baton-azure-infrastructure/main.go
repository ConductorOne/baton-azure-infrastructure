package main

import (
	"context"
	"fmt"
	"os"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector"
	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()
	_, cmd, err := config.DefineConfiguration(ctx, "baton-azure-infrastructure", getConnector, cfg)
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

	useCliCredentials := v.GetBool(useCliCredentials.FieldName)
	azureTenantId := v.GetString(azureTenantId.FieldName)
	azureClientSecret := v.GetString(azureClientSecret.FieldName)
	azureClientId := v.GetString(azureClientId.FieldName)
	mailboxSettings := v.GetBool(mailboxSettings.FieldName)
	skipAdGroups := v.GetBool(skipAdGroups.FieldName)
	cb, err := connector.New(ctx, useCliCredentials, azureTenantId, azureClientId, azureClientSecret, mailboxSettings, skipAdGroups)
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
