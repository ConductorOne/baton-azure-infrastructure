package main

import (
	"context"
	"fmt"
	"os"

	cfg "github.com/conductorone/baton-azure-infrastructure/pkg/config"
	"github.com/conductorone/baton-azure-infrastructure/pkg/connector"
	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()
	_, cmd, err := config.DefineConfiguration(ctx, "baton-azure-infrastructure", getConnector, cfg.Config)
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

func getConnector(ctx context.Context, ac *cfg.AzureInfrastructure) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)
	if err := cfg.ValidateConfig(ac); err != nil {
		return nil, err
	}

	cb, err := connector.New(
		ctx,
		ac.UseCliCredentials,
		ac.AzureTenantId,
		ac.AzureClientId,
		ac.AzureClientSecret,
		ac.Mailboxsettings,
		ac.SkipAdGroups,
		ac.GraphDomain,
		ac.SkipUnusedRoles,
		ac.SkipSyncStorageContainers,
		ac.EnableSyncExternalResourcesViaBatonId,
		ac.SkipEntraIdP2LicenseFeatures,
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
