#!/bin/bash
set -e

echo "Removing old config files from cmd/"
git rm cmd/baton-azure-infrastructure/config.go cmd/baton-azure-infrastructure/config_test.go

echo "Removing old main.yaml workflow"
git rm .github/workflows/main.yaml

echo "Adding new config files"
git add pkg/config/

echo "Testing build"
make build

echo "Staging all changes"
git add -A

echo "Creating commit"
git commit -m "$(cat <<'EOF'
Containerize connector

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

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>
EOF
)"

echo "Pushing to remote"
git push -u origin containerize-connector

echo "Done! Branch containerize-connector has been pushed."
