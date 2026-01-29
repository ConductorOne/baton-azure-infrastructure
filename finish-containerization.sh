#!/bin/bash
# Complete the containerization of baton-azure-infrastructure
# This script finishes what Claude started

set -e

echo "=== Containerization Finalization Script ==="
echo ""

cd "$(dirname "$0")"

echo "Step 1: Removing old config files..."
git rm cmd/baton-azure-infrastructure/config.go
git rm cmd/baton-azure-infrastructure/config_test.go
echo "✓ Old config files removed"
echo ""

echo "Step 2: Removing old main.yaml workflow..."
git rm .github/workflows/main.yaml
echo "✓ Old workflow removed"
echo ""

echo "Step 3: Testing build..."
make build
echo "✓ Build successful"
echo ""

echo "Step 4: Staging all changes..."
git add pkg/config/
git add cmd/baton-azure-infrastructure/main.go
git add pkg/connector/connector.go
git add Makefile
git add .github/workflows/ci.yaml
git add .github/workflows/capabilities.yaml
git add go.mod go.sum
git add vendor/
echo "✓ Changes staged"
echo ""

echo "Step 5: Creating commit..."
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
echo "✓ Commit created"
echo ""

echo "Step 6: Pushing branch..."
git push -u origin containerize-connector
echo "✓ Branch pushed"
echo ""

echo "=== Containerization Complete ==="
echo ""
echo "Branch 'containerize-connector' has been pushed to remote."
echo "You can now create a PR if needed, or the branch is ready for review."
echo ""
echo "To verify everything works:"
echo "  1. Run: make build"
echo "  2. Run: ./dist/*/baton-azure-infrastructure --help"
echo "  3. Run: ./dist/*/baton-azure-infrastructure config"
echo ""
