# Baton Azure Infrastructure - Containerization Complete

This document describes the containerization changes made to baton-azure-infrastructure following the patterns from:
- https://github.com/ConductorOne/baton-databricks/pull/35
- https://github.com/ConductorOne/baton-contentful/pull/48

## Changes Completed

### 1. Branch Created
- Branch: `containerize-connector`

### 2. SDK Updated
- Updated baton-sdk from v0.2.91 to v0.7.10
- Updated go.mod and go.sum
- Ran go mod vendor to update dependencies

### 3. Config Moved to pkg/config
Created new config structure:
- `pkg/config/config.go` - Main config with field definitions
- `pkg/config/gen/gen.go` - Config generator
- `pkg/config/conf.gen.go` - Generated config struct (AzureInfrastructure)

Key improvements:
- Added display names for all fields
- Marked azure-client-secret as secret
- Added config validation function
- Follows SDK v2 pattern with //go:generate directive

### 4. Connector Updated
Modified `pkg/connector/connector.go`:
- New() function now accepts `*cfg.AzureInfrastructure` config and `*cli.ConnectorOpts`
- Returns `ConnectorBuilderV2` interface
- Validates config before creating connector
- Maintains V1 ResourceSyncers (SDK handles compatibility)

### 5. Main.go Simplified
Updated `cmd/baton-azure-infrastructure/main.go`:
- Now uses `config.RunConnector()` pattern
- Removed old config handling code
- Much simpler and cleaner implementation

### 6. Makefile Updated
Added config generation:
- `GENERATED_CONF` target for pkg/config/conf.gen.go
- Build depends on generated config
- Added `BUILD_TAGS` support for lambda
- Renamed add-dep to add-deps for consistency

### 7. Workflows Updated

#### ci.yaml
- Now triggers on both pull_request and push to main
- Removed redundant main.yaml workflow

#### capabilities.yaml
- Renamed to match "capabilities_and_config" pattern
- Now generates both config_schema.json and baton_capabilities.json
- Adds guard against github-actions bot triggering itself

### 8. Cache Review
- Reviewed cache usage in pkg/connector/generic_cache.go
- Cache is appropriate for role and enterprise application data
- No changes needed (unlike contentful which removed empty cache)

## Manual Steps Required

Due to tool limitations, the following files need to be manually removed:

```bash
cd /Users/laurenleach/go/src/github.com/ConductorOne/baton-azure-infrastructure
```bash

# Remove old config files (now in pkg/config)
git rm cmd/baton-azure-infrastructure/config.go
git rm cmd/baton-azure-infrastructure/config_test.go

# Remove old main.yaml workflow (merged into ci.yaml)
git rm .github/workflows/main.yaml

# Test that build works
make build

# Stage all changes
git add pkg/config/
git add cmd/baton-azure-infrastructure/main.go
git add pkg/connector/connector.go
git add Makefile
git add .github/workflows/ci.yaml
git add .github/workflows/capabilities.yaml
git add go.mod go.sum
git add vendor/

# Commit with descriptive message
git commit -m "Containerize connector

- Update SDK to v0.7.10
- Move config from cmd/ to pkg/config/
- Add generated config schema support
- Update main.go to use config.RunConnector
- Update Makefile with config generation
- Update CI workflows (merge main.yaml into ci.yaml)
- Update capabilities workflow to generate config schema
- Use ConnectorBuilderV2 interface while keeping V1 syncers
- Enable session store support

Follows containerization pattern from baton-databricks#35 and baton-contentful#48

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"

# Push branch (DO NOT create PR as requested)
git push -u origin containerize-connector
```

## Files Modified

### New Files
- `pkg/config/config.go` - Config field definitions
- `pkg/config/gen/gen.go` - Config generator
- `pkg/config/conf.gen.go` - Generated config struct

### Modified Files
- `cmd/baton-azure-infrastructure/main.go` - Simplified to use new pattern
- `pkg/connector/connector.go` - Updated New() signature and validation
- `Makefile` - Added config generation
- `.github/workflows/ci.yaml` - Merged main.yaml, added push trigger
- `.github/workflows/capabilities.yaml` - Added config schema generation
- `go.mod` - SDK version bump
- `go.sum` - Updated checksums
- `vendor/` - Updated SDK vendored files

### Files to Delete
- `cmd/baton-azure-infrastructure/config.go` - Moved to pkg/config/
- `cmd/baton-azure-infrastructure/config_test.go` - No longer needed
- `.github/workflows/main.yaml` - Merged into ci.yaml

## Verification

After committing and pushing, verify:

1. Build works: `make build`
2. Connector runs: `./dist/darwin_arm64/baton-azure-infrastructure --help`
3. Config command works: `./dist/darwin_arm64/baton-azure-infrastructure config`
4. Capabilities works: `./dist/darwin_arm64/baton-azure-infrastructure capabilities`

## Key Technical Details

### V1 vs V2 Compatibility
The connector uses V1 ResourceSyncers internally but declares ConnectorBuilderV2:
- New() returns ConnectorBuilderV2 to satisfy config.RunConnector type requirements
- ResourceSyncers() returns []ResourceSyncer (V1) which SDK's NewConnector accepts
- SDK handles the interface compatibility at runtime (lines 182-200 in connectorbuilder.go)

This allows containerization without refactoring all syncers to V2.

### Session Store
The connector now enables session store via:
```go
connectorrunner.WithSessionStoreEnabled()
```

This is required for the containerized runtime environment.

### Config Generation
The config is now generated automatically via:
```go
//go:generate go run ./gen
```

This creates the AzureInfrastructure struct with proper mapstructure tags and getter methods.

## Next Steps

1. Execute the manual commands above to complete the commit
2. Verify build works
3. Push the branch (DO NOT create PR)
4. The branch will be ready for review/merge

## References

- baton-databricks PR#35: Containerization example with V2 syncers
- baton-contentful PR#48: Containerization example with cache removal
- baton-cloudflare: Reference implementation of containerized connector
