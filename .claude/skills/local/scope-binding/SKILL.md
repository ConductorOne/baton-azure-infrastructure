---
name: scope-binding
description: How to emit TRAIT_SCOPE_BINDING + ScopeBindingTrait from this connector so the c1 uplift classifies the app as SPARSE/HYBRID. Covers SDK contract, cross-link ID-format invariants, and the concrete reference in pkg/connector/role_assignment.go.
---

# ScopeBinding Emission (baton-azure-infrastructure)

Use this skill when touching anything related to `role_assignment`, `ScopeBindingTrait`, or sparse-ACL classification. Wrong emission → app classified CLASSIC silently, no error, no lint failure.

## The 3-point emission contract

For the c1 uplift (proposed PR ductone/c1#16540) to classify this app as `SPARSE` or `HYBRID`, the connector must:

1. **Declare** a resource type with `Trait.TRAIT_SCOPE_BINDING = 7` in its `Traits` slice.
   - In this repo: `pkg/connector/resource_types.go`, `roleAssignmentResourceType`.
2. **Attach** a `ScopeBindingTrait` annotation to every binding instance, populated with:
   - `role_id`: the `ResourceId` of the role resource
   - `scope_resource_id`: the `ResourceId` of the scope (sub / RG / mgmt-group / KV / storage container / …)
3. **Emit grants** via the binding: `principal → binding → "assigned"`.

SDK helpers (baton-sdk):
- `rs.NewScopeBindingResource(name, rt, id, opts...)` — constructor, attaches the annotation.
- `rs.WithRoleScopeRoleId(*v2.ResourceId)`, `rs.WithRoleScopeResourceId(*v2.ResourceId)` — opts.
- `rs.GetScopeBindingTrait(resource)` — retrieval during Grant/Revoke.

Reference implementation in this repo: `pkg/connector/role_assignment.go` (List + Entitlements + Grants + Grant + Revoke).

## Cross-link ID-format invariant (load-bearing)

**The `role_id` and `scope_resource_id` inside `ScopeBindingTrait` MUST match the exact ResourceId format the sibling builders emit.** A mismatch silently produces a c1z with dangling references that classify as HYBRID but break the sparse-ACL UX.

Current builder-emitted IDs in this connector (verified against lab 2026-04-17):

| Resource type | ID format | Builder |
|---|---|---|
| `role` | `<roleDefinitionUUID>:<subscriptionID>` (composite) | `pkg/connector/role.go` |
| `subscription` | bare subscription GUID | `pkg/connector/subscription.go` |
| `resource_group` | bare RG name | `pkg/connector/resource_group.go` |
| `management_group` | full ARM path (`/providers/Microsoft.Management/managementGroups/<id>`) | `pkg/connector/management_group.go` |

When constructing a `ScopeBindingTrait`:
- `role_id`: use `<uuid>:<subID>` — **not** the bare UUID from ARM. See `pkg/connector/role_assignment.go` helper `roleResourceIDForScope`.
- `scope_resource_id`: use the extractor `scopeResourceRefFromAzureScope(scope)` → returns `(resourceType, resourceID)` matching the above table — **not** the raw ARM path.

If you add a new scope resource type (KV secret, storage container, SB queue, etc.), you must:
1. Define the resource type.
2. Add a case in `scopeResourceRefFromAzureScope` mapping its ARM scope prefix to the builder's ID format.
3. Verify cross-links with `baton -f /tmp/out.c1z resources --resource-type <new-type>` + grep the c1z for `scope_resource_id` values.

## Gaps vs the RFC (don't assume these exist)

- `inheritance_mode` enum (DESCENDANTS / THIS_LEVEL_ONLY / …) — **not in the proto yet.** Everything is implicitly DESCENDANTS.
- `condition: NormalizedCondition` on `ScopeBindingTrait` — not yet present; Azure conditional role assignments are lossy.
- `RoleTrait.role_scope_conditions` exists at the role level but is structurally different.

## Gate new emission behind a flag

This connector exposes `--sync-role-assignments` (default `false`) in `pkg/config/config.go`. New emission work that changes the trait surface should pipe through the same gate until the rollout is confirmed stable in c1.

## Verification loop (run against the lab)

```bash
cd /home/c1/azure-baton/repos/baton-azure-infrastructure
go build -o /tmp/baton-azure-infra ./cmd/baton-azure-infrastructure

set -a; source /home/c1/azure-baton/.secrets/baton-runtime-sp.env; set +a
/tmp/baton-azure-infra --sync-role-assignments -f /tmp/out.c1z
# (BATON_AZURE_TENANT_ID / BATON_AZURE_CLIENT_ID / BATON_AZURE_CLIENT_SECRET are auto-bound)

# Confirm emission
baton -f /tmp/out.c1z resource-types | grep -E 'role_assignment|management_group'
baton -f /tmp/out.c1z stats

# Confirm cross-links resolve (no dangling ScopeBindingTrait refs)
# Every role_id in ScopeBindingTrait must exist as a `role` resource; every
# scope_resource_id must exist as its declared resource type.
```

Live-validated during PR #79 rollout (commit `725a0e8`): all emitted `ScopeBindingTrait` cross-links resolved against sibling role/subscription/resource_group/management_group resources. Regenerate and re-verify after any change to builder ID formats.
