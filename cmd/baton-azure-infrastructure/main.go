package main

import (
	"context"

	cfg "github.com/conductorone/baton-azure-infrastructure/pkg/config"
	"github.com/conductorone/baton-azure-infrastructure/pkg/connector"
	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorrunner"
)

var version = "dev"

func main() {
	ctx := context.Background()

	config.RunConnector(ctx, "baton-azure-infrastructure", version, cfg.Config, connector.New, connectorrunner.WithSessionStoreEnabled())
}
