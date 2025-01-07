#!/bin/bash

set -exo pipefail

 # CI test for use with CI Tailscale account
if [ -z "$BATON_AZURE_INFRASTRUCTURE" ]; then
  echo "BATON_AZURE_INFRASTRUCTURE not set. using baton-azure-infrastructure"
  BATON_AZURE_INFRASTRUCTURE=baton-azure-infrastructure
fi
if [ -z "$BATON" ]; then
  echo "BATON not set. using baton"
  BATON=baton
fi

# Error on unbound variables now that we've set BATON & BATON_AZURE_INFRASTRUCTURE
set -u

# Sync
$BATON_AZURE_INFRASTRUCTURE

# Grant entitlement
$BATON_AZURE_INFRASTRUCTURE --grant-entitlement="$BATON_ENTITLEMENT" --grant-principal="$BATON_PRINCIPAL" --grant-principal-type="$BATON_PRINCIPAL_TYPE"

# Check for grant before revoking
$BATON_AZURE_INFRASTRUCTURE
$BATON grants --entitlement="$BATON_ENTITLEMENT" --output-format=json | jq --exit-status ".grants[] | select( .principal.id.resource == \"$BATON_PRINCIPAL\" )"

# Grant already-granted entitlement
$BATON_AZURE_INFRASTRUCTURE --grant-entitlement="$BATON_ENTITLEMENT" --grant-principal="$BATON_PRINCIPAL" --grant-principal-type="$BATON_PRINCIPAL_TYPE"

# Get grant ID
BATON_GRANT=$($BATON grants --entitlement="$BATON_ENTITLEMENT" --output-format=json | jq --raw-output --exit-status ".grants[] | select( .principal.id.resource == \"$BATON_PRINCIPAL\" ).grant.id")

# Revoke grant
$BATON_AZURE_INFRASTRUCTURE --revoke-grant="$BATON_GRANT"

# Revoke already-revoked grant
$BATON_AZURE_INFRASTRUCTURE --revoke-grant="$BATON_GRANT"

# Check grant was revoked
$BATON_AZURE_INFRASTRUCTURE
$BATON grants --entitlement="$BATON_ENTITLEMENT" --output-format=json | jq --exit-status "if .grants then [ .grants[] | select( .principal.id.resource == \"$BATON_PRINCIPAL\" ) ] | length == 0 else . end"

# Re-grant entitlement
$BATON_AZURE_INFRASTRUCTURE --grant-entitlement="$BATON_ENTITLEMENT" --grant-principal="$BATON_PRINCIPAL" --grant-principal-type="$BATON_PRINCIPAL_TYPE"

# Check grant was re-granted
$BATON_AZURE_INFRASTRUCTURE
$BATON grants --entitlement="$BATON_ENTITLEMENT" --output-format=json | jq --exit-status ".grants[] | select( .principal.id.resource == \"$BATON_PRINCIPAL\" )"
