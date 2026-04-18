![Baton Logo](./docs/images/baton-logo.png)

#

`baton-azure-infrastructure` [![Go Reference](https://pkg.go.dev/badge/github.com/conductorone/baton-azure-infrastructure.svg)](https://pkg.go.dev/github.com/conductorone/baton-azure-infrastructure) ![ci](https://github.com/conductorone/baton-azure-infrastructure/actions/workflows/ci.yaml/badge.svg) ![verify](https://github.com/conductorone/baton-azure-infrastructure/actions/workflows/verify.yaml/badge.svg)

`baton-azure-infrastructure` is a connector for built using the [Baton SDK](https://github.com/conductorone/baton-sdk).

Check out [Baton](https://github.com/conductorone/baton) to learn more the project in general.

# Requirements

- You need a Microsoft tenant. You get one with an [Azure free trial](https://azure.microsoft.com/pricing/free-trial/).
- Some of the more specific data is accessed via the Privileged Identity Management API.
   For this, the tenant requires the corresponding Premium License such as "Microsoft Entra ID P2" or "Microsoft Entra ID Governance". Not having one of them implies that a portion of the data will not be visible.
- Once you have a tenant, you need to create an application in Azure AD. You can follow the
  instructions [here](https://docs.microsoft.com/en-us/azure/active-directory/develop/quickstart-register-app).
- When you create the application, you will get a `client_id` and a `client_secret`. You will need these to authenticate
  with the Azure API.
    - Needs permissions on Microsoft Graph
        - Application.Read.All
        - Directory.Read.All
        - Group.Read.All
        - Organization.Read.All
        - ServicePrincipalEndpoint.Read.All
        - User.Read
        - User.Read.All
        - PrivilegedAccess.Read.AzureAD [it also requires the corresponding Premium License in order to access]
        - PrivilegedAccess.Read.AzureADGroup [it also requires the corresponding Premium License in order to access]
        - PrivilegedAccess.Read.AzureResources [it also requires the corresponding Premium License in order to access]
    - Needs to add the application as Reader in the Azure Subscription that you want to sync
- Then you will need to get the `tenant_id` of your Azure AD tenant. You can find this in the Azure Entra ID Overview
  page [here](https://portal.azure.com/#blade/Microsoft_AAD_IAM/ActiveDirectoryMenuBlade/Overview).

Finally, you will need to set the following environment variables:

```
export BATON_AZURE_CLIENT_ID=<client_id>
export BATON_AZURE_CLIENT_SECRET=<client_secret>
export BATON_AZURE_TENANT_ID=<tenant_id>
```

# Getting Started

## brew

```
brew install conductorone/baton/baton conductorone/baton/baton-azure-infrastructure
baton-azure-infrastructure
baton resources
```

## docker

```
docker run --rm -v $(pwd):/out -e BATON_AZURE_CLIENT_ID=client_Id -e BATON_AZURE_CLIENT_SECRET=client_secret -e BATON_AZURE_TENANT_ID=tenant_Id ghcr.io/conductorone/baton-azure-infrastructure:latest -f "/out/sync.c1z"
docker run --rm -v $(pwd):/out ghcr.io/conductorone/baton:latest -f "/out/sync.c1z" resources
```

## source

```
go install github.com/conductorone/baton/cmd/baton@main
go install github.com/conductorone/baton-azure-infrastructure/cmd/baton-azure-infrastructure@main

BATON_AZURE_CLIENT_SECRET=<client_secret>
BATON_AZURE_CLIENT_ID=<client_id>
BATON_AZURE_TENANT_ID=<tenant_id>
baton-azure-infrastructure

baton resources
```

# Data Model

`baton-azure-infrastructure` will pull down information about the following resources:

- Users (entra users)
- Groups (entra groups)
- Roles (azure roles)
- Tenants (azure tenants)
- Enterprise Applications (entra service principals)
- Managed Identities (entra service principals)
- Resource Groups (azure resource groups)
- Management Groups (azure management groups)
- Role Assignments (azure RBAC role assignments — emitted as scope-binding resources with `TRAIT_SCOPE_BINDING` when `--sync-role-assignments` is enabled; unlocks sparse-ACL / hybrid classification in the C1 uplift flow)

We also introduced resource_group_role_assignment(resource group ID, subscription ID and role ID) for provisioning
resource Groups.

### Optional permissions

- To sync management-group-scoped role assignments, the application must have **Management Group Reader** at the relevant management-group scope (or higher).
- To provision `role_assignment` resources, the application must have write on `Microsoft.Authorization/roleAssignments` at the target scope — the built-in **User Access Administrator** role is sufficient.

## resource_group_role_assignment usage:

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
BATON_AZURE_CLIENT_SECRET='client_secret' \
BATON_AZURE_TENANT_ID='tenant_Id' baton-azure-infrastructure \
--grant-entitlement 'resource_group_role_assignment:test_resource_group:39ea64c5-86d5-4c29-8199-5b602c90e1c5:11102f94-c441-49e6-a78b-ef80e0188abc:assigned' --grant-principal-type 'user' --grant-principal 'e4e9c5ae-2937-408b-ba3c-0f58cf417f0a'
```

In the previous example we granted the resource group role `11102f94-c441-49e6-a78b-ef80e0188abc` to user
`e4e9c5ae-2937-408b-ba3c-0f58cf417f0a`.

- Revoking resource group role grants

```
BATON_AZURE_CLIENT_ID='client_Id' \
BATON_AZURE_CLIENT_SECRET='client_secret' \
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
  config             Get the connector config schema
  health-check       Check the health of a running connector
  help               Help about any command

Flags:
      --auth-method string                               ($BATON_AUTH_METHOD)
      --azure-client-id string                           Azure Client ID ($BATON_AZURE_CLIENT_ID)
      --azure-client-secret string                       Azure Client Secret ($BATON_AZURE_CLIENT_SECRET)
      --azure-tenant-id string                           Azure Tenant ID ($BATON_AZURE_TENANT_ID)
      --client-id string                                 The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string                             The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
      --enable-sync-external-resources-via-baton-id      If true, the connector will use baton id to sync users and groups from external resources.
                                                         		 This could break the sync if the Baton ID external resource is not set up correctly. ($BATON_ENABLE_SYNC_EXTERNAL_RESOURCES_VIA_BATON_ID)
      --external-resource-c1z string                     The path to the c1z file to sync external baton resources with ($BATON_EXTERNAL_RESOURCE_C1Z)
      --external-resource-entitlement-id-filter string   The entitlement that external users, groups must have access to sync external baton resources ($BATON_EXTERNAL_RESOURCE_ENTITLEMENT_ID_FILTER)
  -f, --file string                                      The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
      --graph-domain string                              Domain for Microsoft Graph API ($BATON_GRAPH_DOMAIN) (default "graph.microsoft.com")
      --health-check                                     Enable the HTTP health check endpoint ($BATON_HEALTH_CHECK)
      --health-check-port int                            Port for the HTTP health check endpoint ($BATON_HEALTH_CHECK_PORT) (default 8081)
  -h, --help                                             help for baton-azure-infrastructure
      --http-timeout-seconds int                         HTTP client timeout in seconds (max 1800) ($BATON_HTTP_TIMEOUT_SECONDS) (default 300)
      --log-format string                                The output format for logs: json, console ($BATON_LOG_FORMAT) (default "json")
      --log-level string                                 The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
      --log-level-debug-expires-at string                The timestamp indicating when debug-level logging should expire ($BATON_LOG_LEVEL_DEBUG_EXPIRES_AT)
      --mailboxSettings                                  If true, attempt to get mailbox settings for users to determine user purpose ($BATON_MAILBOXSETTINGS)
      --otel-collector-endpoint string                   The endpoint of the OpenTelemetry collector to send observability data to (used for both tracing and logging if specific endpoints are not provided) ($BATON_OTEL_COLLECTOR_ENDPOINT)
      --parallel-sync                                    Deprecated: use --workers instead. ($BATON_PARALLEL_SYNC)
  -p, --provisioning                                     This must be set in order for provisioning actions to be enabled ($BATON_PROVISIONING)
      --skip-ad-groups                                   If true, skip syncing Windows Server Active Directory groups ($BATON_SKIP_AD_GROUPS)
      --skip-entitlements-and-grants                     This must be set to skip syncing of entitlements and grants ($BATON_SKIP_ENTITLEMENTS_AND_GRANTS)
      --skip-entra-id-p2-license-features                If true, skips the features that require a 'Microsoft Entra ID P2' or 'Microsoft Entra ID Governance' license on the tenant. ($BATON_SKIP_ENTRA_ID_P2_LICENSE_FEATURES)
      --skip-full-sync                                   This must be set to skip a full sync ($BATON_SKIP_FULL_SYNC)
      --skip-sync-storage-containers                     If true, storage containers is skipped ($BATON_SKIP_SYNC_STORAGE_CONTAINERS)
      --skip-unused-roles                                Skip unused roles ($BATON_SKIP_UNUSED_ROLES)
      --sync-resource-types strings                      The resource type IDs to sync ($BATON_SYNC_RESOURCE_TYPES)
      --sync-resources strings                           The resource IDs to sync ($BATON_SYNC_RESOURCES)
      --sync-role-assignments                            If true, sync Azure role assignments as scope-binding resources (emits TRAIT_SCOPE_BINDING, enabling sparse-ACL / hybrid classification in c1 uplift). ($BATON_SYNC_ROLE_ASSIGNMENTS)
      --ticketing                                        This must be set to enable ticketing support ($BATON_TICKETING)
      --use-cli-credentials                              If true, uses the az cli to auth ($BATON_USE_CLI_CREDENTIALS)
  -v, --version                                          version for baton-azure-infrastructure
      --workers int                                      The number of sync workers to use. -1 for auto-detect, 0 for sequential, >0 for parallel ($BATON_WORKERS)

Use "baton-azure-infrastructure [command] --help" for more information about a command.
```
