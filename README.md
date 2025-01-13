![Baton Logo](./docs/images/baton-logo.png)

# `baton-azure-infrastructure` [![Go Reference](https://pkg.go.dev/badge/github.com/conductorone/baton-azure-infrastructure.svg)](https://pkg.go.dev/github.com/conductorone/baton-azure-infrastructure) ![main ci](https://github.com/conductorone/baton-azure-infrastructure/actions/workflows/main.yaml/badge.svg)

`baton-azure-infrastructure` is a connector for built using the [Baton SDK](https://github.com/conductorone/baton-sdk).

Check out [Baton](https://github.com/conductorone/baton) to learn more the project in general.

# Getting Started

## brew

```
brew install conductorone/baton/baton conductorone/baton/baton-azure-infrastructure
baton-azure-infrastructure
baton resources
```

## docker

```
docker run --rm -v $(pwd):/out -e BATON_DOMAIN_URL=domain_url -e BATON_API_KEY=apiKey -e BATON_USERNAME=username ghcr.io/conductorone/baton-azure-infrastructure:latest -f "/out/sync.c1z"
docker run --rm -v $(pwd):/out ghcr.io/conductorone/baton:latest -f "/out/sync.c1z" resources
```

## source

```
go install github.com/conductorone/baton/cmd/baton@main
go install github.com/conductorone/baton-azure-infrastructure/cmd/baton-azure-infrastructure@main

baton-azure-infrastructure

baton resources
```

# Data Model

`baton-azure-infrastructure` will pull down information about the following resources:
- Users (entra users)
- Groups (entra groups)
- Roles (azure roles)
- Tenants (azure tenants)
- Enterprice Applications (entra service principals)
- Managed Identities (entra service principals)
- Resource Groups (azure resource groups)

We also introduced resource_group_role_assignment(resource group ID, subscription ID and role ID) for provisioning resource Groups.

## Resourceset-bindings, custom roles and members(Users or Groups) usage:

- Let's use some IDs for this example
```
Resource Group `test_resource_group`
Subscription `39ea64c5-86d5-4c29-8199-5b602c90e1c5`
Role `11102f94-c441-49e6-a78b-ef80e0188abc`
Principal `e4e9c5ae-2937-408b-ba3c-0f58cf417f0a`
```

- Granting resource group roles for users.
```
BATON_AZURE_CLIENT_ID='client_Id' \
BATON_AZURE_CLIENT_SECRET='clien_secret' \
BATON_AZURE_TENANT_ID='tenant_Id' baton-azure-infrastructure \
--grant-entitlement 'resource_group_role_assignment:test_resource_group:39ea64c5-86d5-4c29-8199-5b602c90e1c5:11102f94-c441-49e6-a78b-ef80e0188abc:assigned' --grant-principal-type 'user' --grant-principal 'e4e9c5ae-2937-408b-ba3c-0f58cf417f0a' 
```

In the previous example we granted the custom role `cr0kuwv5507zJCtSy697` to user `00ujp5a9z0rMTsPRW697`.

- Revoking resource group role grants
```
BATON_AZURE_CLIENT_ID='client_Id' \
BATON_AZURE_CLIENT_SECRET='clien_secret' \
BATON_AZURE_TENANT_ID='tenant_Id' baton-azure-infrastructure \
--revoke-grant 'resource_group_role_assignment:test_resource_group:39ea64c5-86d5-4c29-8199-5b602c90e1c5:11102f94-c441-49e6-a78b-ef80e0188abc:assigned:user:e4e9c5ae-2937-408b-ba3c-0f58cf417f0a'

```

# Contributing, Support and Issues

We started Baton because we were tired of taking screenshots and manually
building spreadsheets. We welcome contributions, and ideas, no matter how
small&mdash;our goal is to make identity and permissions sprawl less painful for
everyone. If you have questions, problems, or ideas: Please open a GitHub Issue!

See [CONTRIBUTING.md](https://github.com/ConductorOne/baton/blob/main/CONTRIBUTING.md) for more details.

# `baton-azure-infrastructure` Command Line Usage

```
baton-azure-infrastructure

Usage:
  baton-azure-infrastructure [flags]
  baton-azure-infrastructure [command]

Available Commands:
  capabilities       Get connector capabilities
  completion         Generate the autocompletion script for the specified shell
  help               Help about any command

Flags:
      --azure-client-id string       Azure Client ID ($BATON_AZURE_CLIENT_ID)
      --azure-client-secret string   Azure Client Secret ($BATON_AZURE_CLIENT_SECRET)
      --azure-tenant-id string       Azure Tenant ID ($BATON_AZURE_TENANT_ID)
      --client-id string             The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string         The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
  -f, --file string                  The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
  -h, --help                         help for baton-azure-infrastructure
      --log-format string            The output format for logs: json, console ($BATON_LOG_FORMAT) (default "json")
      --log-level string             The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
      --mailboxSettings              If true, attempt to get mailbox settings for users to determine user purpose ($BATON_MAILBOXSETTINGS)
  -p, --provisioning                 This must be set in order for provisioning actions to be enabled ($BATON_PROVISIONING)
      --skip-ad-groups               If true, skip syncing Windows Server Active Directory groups ($BATON_SKIP_AD_GROUPS)
      --skip-full-sync               This must be set to skip a full sync ($BATON_SKIP_FULL_SYNC)
      --ticketing                    This must be set to enable ticketing support ($BATON_TICKETING)
      --use-cli-credentials          If true, uses the az cli to auth ($BATON_USE_CLI_CREDENTIALS)
  -v, --version                      version for baton-azure-infrastructure

Use "baton-azure-infrastructure [command] --help" for more information about a command.
```
