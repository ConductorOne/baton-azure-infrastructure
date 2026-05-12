---
name: pre-push-gauntlet
description: The exact checks this repo's CI runs. Always run them locally before `git push` to avoid red-X PRs and review churn. Complements the reactive `connector/fix-ci-checks.md` skill with preventive steps.
---

# Pre-Push CI Gauntlet (baton-azure-infrastructure)

Run this EXACT sequence before every `git push` on any PR touching `pkg/` or `cmd/`. Skipping it produces the embarrassing red-X that happened on PR #78 and PR #79 (5 separate preventable lint misses).

The managed `connector/fix-ci-checks.md` skill covers *reactive* CI recovery. This skill is the *preventive* counterpart.

## Sequence

```bash
cd /home/c1/azure-baton/repos/baton-azure-infrastructure

# 1. Formatting (FAST — must be clean)
gofmt -l ./pkg ./cmd 2>&1
# Expect: empty output

# 2. vet (FAST — must be clean)
go vet ./... 2>&1
# Expect: empty output

# 3. golangci-lint (SLOW ~30-90s — what CI actually runs)
/home/c1/go/bin/golangci-lint run ./... 2>&1 | tail -20
# Expect: "0 issues."

# 4. Tests (MEDIUM — must all pass)
go test ./... 2>&1 | tail -10
# Expect: "ok" on every package with tests, no FAIL

# 5. Build (FAST — must succeed)
go build -o /tmp/baton-azure-infrastructure ./cmd/baton-azure-infrastructure 2>&1
# Expect: empty output
```

## Lint findings `golangci-lint` catches that `go vet` doesn't

These are the ones this repo has produced on recent PRs:

| Linter | Finding | Example fix |
|---|---|---|
| `nilerr` | `err != nil` checked but `nil` returned without logging | Add `l.Debug(...)` or return the error |
| `nonamedreturns` | Named returns are disallowed | Drop the names; use local vars |
| `goconst` | Repeated string literal — promote to constant | `const azurePrincipalTypeUser = "User"` |
| `gocritic.dupBranchBody` | `if/else` with identical bodies | Collapse to single branch |
| `staticcheck.SA1012` | Passing `nil` as `context.Context` | Use `context.Background()` |
| `nolintlint` | Unused or stale `//nolint:` directive | Remove the directive |

## Supplementary for ScopeBinding / provisioning PRs

```bash
# Live-sync against the lab
set -a; source /home/c1/azure-baton/.secrets/baton-runtime-sp.env; set +a
/tmp/baton-azure-infrastructure \
  --sync-role-assignments \
  -f /tmp/out.c1z --log-level debug

baton -f /tmp/out.c1z resource-types
baton -f /tmp/out.c1z stats

# Grant/Revoke idempotency (if SP has write perms):
#   duplicate Grant → GrantAlreadyExists annotation, no error
#   Revoke of missing → GrantAlreadyRevoked annotation, no error
```

## Don't

- Don't push and "let CI tell me what's wrong" — wastes CI minutes and reviewer round-trips.
- Don't bypass with `--no-verify` on commit or push.
- Don't skip step 3 because `go vet` passed — `vet` misses 90% of what `golangci-lint` catches on this repo.

## Why this skill exists

The project-level `connector/fix-ci-checks.md` tells you how to *recover* after CI fails. This skill tells you how to *not fail*. They compose.

## Live integration test (ScopeBinding / Grant / Revoke)

Requires lab SP creds and writeable access to /home/c1/azure-baton/.secrets/baton-runtime-sp.env.
Briefly elevate the SP to User Access Administrator on the target
subscription before running; de-elevate after.

    set -a; source /home/c1/azure-baton/.secrets/baton-runtime-sp.env; set +a
    export AZURE_LIVE_TEST_RG=rg-apps-web-prd
    export AZURE_LIVE_TEST_PRINCIPAL=<test-SP-object-id>
    export AZURE_LIVE_TEST_ROLE_UUID=acdd72a7-3385-48ef-bd42-f606fba81ae7   # Reader
    go test -tags=live -v -run TestLiveGrantRevoke ./pkg/connector

Pinned expected output:
    Grant#1: success
    Grant#2 (duplicate): GrantAlreadyExists ✓
    Revoke#1: success
    Revoke#2 (missing): GrantAlreadyRevoked ✓
