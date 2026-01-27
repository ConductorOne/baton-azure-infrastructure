package main

import (
	cfg "github.com/conductorone/baton-azure-infrastructure/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/config"
)

func main() {
	config.Generate("azure-infrastructure", cfg.Config)
}
