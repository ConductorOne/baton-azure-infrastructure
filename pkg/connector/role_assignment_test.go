package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAzureErrorClassifiers(t *testing.T) {
	// Construct an azcore.ResponseError via a minimal http.Response. The SDK
	// returns *ResponseError; our classifiers use errors.As to extract it.
	makeRespErr := func(code int) error {
		return &azcore.ResponseError{
			StatusCode: code,
			RawResponse: &http.Response{
				StatusCode: code,
			},
		}
	}
	wrapped := fmt.Errorf("wrapping layer: %w", makeRespErr(http.StatusConflict))

	// baton-sdk middleware sometimes transforms Azure errors into gRPC
	// statuses (observed on live Grant/Revoke: 409 arrives as
	// codes.AlreadyExists with the original 409 embedded in the text).
	grpcConflict := status.Error(codes.AlreadyExists, "Request failed with status 409")
	grpcNotFound := status.Error(codes.NotFound, "Request failed with status 404")
	grpcForbidden := status.Error(codes.PermissionDenied, "Request failed with status 403")

	tests := []struct {
		name      string
		err       error
		forbidden bool
		conflict  bool
		notFound  bool
	}{
		{"nil error", nil, false, false, false},
		{"plain error, no ResponseError", errors.New("boom"), false, false, false},
		{"403 (azcore)", makeRespErr(http.StatusForbidden), true, false, false},
		{"409 (azcore)", makeRespErr(http.StatusConflict), false, true, false},
		{"404 (azcore)", makeRespErr(http.StatusNotFound), false, false, true},
		{"200 is none", makeRespErr(http.StatusOK), false, false, false},
		{"wrapped 409 (azcore) still detected", wrapped, false, true, false},
		{"409 (gRPC status)", grpcConflict, false, true, false},
		{"404 (gRPC status)", grpcNotFound, false, false, true},
		{"403 (gRPC status)", grpcForbidden, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isForbidden(tt.err); got != tt.forbidden {
				t.Errorf("isForbidden = %v, want %v", got, tt.forbidden)
			}
			if got := isConflict(tt.err); got != tt.conflict {
				t.Errorf("isConflict = %v, want %v", got, tt.conflict)
			}
			if got := isNotFound(tt.err); got != tt.notFound {
				t.Errorf("isNotFound = %v, want %v", got, tt.notFound)
			}
		})
	}
}

func TestBatonToAzurePrincipalType(t *testing.T) {
	ptUser := armauthorization.PrincipalTypeUser
	ptSP := armauthorization.PrincipalTypeServicePrincipal
	tests := []struct {
		name string
		in   string
		want *armauthorization.PrincipalType
	}{
		{"user → User", userResourceType.Id, &ptUser},
		{"enterprise_application → ServicePrincipal", enterpriseApplicationResourceType.Id, &ptSP},
		{"managed_identity → ServicePrincipal", managedIdentitylResourceType.Id, &ptSP},
		{"group → unsupported (nil)", groupResourceType.Id, nil},
		{"unknown type → nil", "resource_group", nil},
		{"empty → nil", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := batonToAzurePrincipalType(tt.in)
			switch {
			case got == nil && tt.want == nil:
				// ok
			case got == nil || tt.want == nil:
				t.Errorf("batonToAzurePrincipalType(%q) = %v, want %v", tt.in, got, tt.want)
			case *got != *tt.want:
				t.Errorf("batonToAzurePrincipalType(%q) = %q, want %q", tt.in, *got, *tt.want)
			}
		})
	}
}

func TestSubscriptionFromScope(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"sub-only", "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0", "0ba3df83-67b5-4a08-a561-e65fa74a1aa0"},
		{"rg scope", "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/resourceGroups/rg-apps-web-prd", "0ba3df83-67b5-4a08-a561-e65fa74a1aa0"},
		{"sub-resource scope", "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/resourceGroups/x/providers/Microsoft.KeyVault/vaults/kv/secrets/s", "0ba3df83-67b5-4a08-a561-e65fa74a1aa0"},
		{"mgmt-group scope", "/providers/Microsoft.Management/managementGroups/c1connectors-root", ""},
		{"tenant root", "/", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subscriptionFromScope(tt.in); got != tt.want {
				t.Errorf("subscriptionFromScope(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRoleDefinitionIDForScope(t *testing.T) {
	roleUUID := "acdd72a7-3385-48ef-bd42-f606fba81ae7"
	tests := []struct {
		name  string
		scope string
		want  string
	}{
		{
			name:  "sub-scope: role def is sub-qualified",
			scope: "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0",
			want:  "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/providers/Microsoft.Authorization/roleDefinitions/acdd72a7-3385-48ef-bd42-f606fba81ae7",
		},
		{
			name:  "rg-scope: role def still sub-qualified",
			scope: "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/resourceGroups/rg-apps-web-prd",
			want:  "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/providers/Microsoft.Authorization/roleDefinitions/acdd72a7-3385-48ef-bd42-f606fba81ae7",
		},
		{
			name:  "mgmt-group scope: providers-relative fallback",
			scope: "/providers/Microsoft.Management/managementGroups/c1connectors-root",
			want:  "/providers/Microsoft.Authorization/roleDefinitions/acdd72a7-3385-48ef-bd42-f606fba81ae7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := roleDefinitionIDForScope(tt.scope, roleUUID); got != tt.want {
				t.Errorf("roleDefinitionIDForScope = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrincipalTypeCache_HitReturnsStoredValue(t *testing.T) {
	// Covers the pure cache-hit path of principalTypeForID: when the cache is
	// pre-populated with a string value, the function must return it without
	// touching Graph. The miss path (cache empty OR non-string entry) calls
	// getPrincipalType which requires a live Connector + Graph token and is
	// therefore exercised only in live lab validation.
	b := &roleAssignmentBuilder{}
	const pid = "4d3a9fc4-022d-4db4-9215-4a25d2ece45a"
	const wantType = "#microsoft.graph.user"
	b.principalTypeCache.Store(pid, wantType)

	// Use context.Background() rather than nil — principalTypeForID's cache-hit
	// path doesn't actually consult the context, but staticcheck (SA1012)
	// rightly refuses to let us pass nil to a function that accepts a
	// context.Context.
	ctx := context.Background()

	got := b.principalTypeForID(ctx, pid)
	if got != wantType {
		t.Errorf("principalTypeForID cache hit returned %q, want %q", got, wantType)
	}

	// Second call must still hit the cache (no eviction).
	got = b.principalTypeForID(ctx, pid)
	if got != wantType {
		t.Errorf("principalTypeForID second cache hit returned %q, want %q", got, wantType)
	}
}

func TestPrincipalTypeCache_NegativeHitReturnsEmpty(t *testing.T) {
	// Covers the negative-cache path: once getPrincipalType has failed or
	// returned "" for a principal, subsequent lookups must return "" without
	// re-querying Graph. Without this the same principal triggers a full
	// fallback-chain attempt (and a Warn log) on every role assignment that
	// references it, flooding operator logs at customer scale.
	b := &roleAssignmentBuilder{}
	const pid = "00000000-0000-0000-0000-000000000000"
	b.principalTypeCache.Store(pid, "")

	ctx := context.Background()

	got := b.principalTypeForID(ctx, pid)
	if got != "" {
		t.Errorf("principalTypeForID negative-cache hit returned %q, want empty string", got)
	}

	// Second call must still hit the cache (no promotion, no eviction).
	got = b.principalTypeForID(ctx, pid)
	if got != "" {
		t.Errorf("principalTypeForID second negative-cache hit returned %q, want empty string", got)
	}
}

func TestParsePrincipalFromRoleAssignmentResourceID(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		wantID string
		wantOk bool
	}{
		{
			name:   "well-formed assignment@principal",
			in:     "1559b6b9-1b05-4cbb-86c3-8cf8b47bbee6@4d3a9fc4-022d-4db4-9215-4a25d2ece45a",
			wantID: "4d3a9fc4-022d-4db4-9215-4a25d2ece45a",
			wantOk: true,
		},
		{
			name:   "non-uuid principal still accepted (defensive)",
			in:     "assignment-name@some-principal-id",
			wantID: "some-principal-id",
			wantOk: true,
		},
		{
			name:   "no @ separator",
			in:     "0efe8561-a1f1-4013-a477-3a6bbefe6e25",
			wantID: "",
			wantOk: false,
		},
		{
			name:   "empty string",
			in:     "",
			wantID: "",
			wantOk: false,
		},
		{
			name:   "trailing @ with no principal",
			in:     "assignment@",
			wantID: "",
			wantOk: false,
		},
		{
			name:   "bare @ with nothing on either side",
			in:     "@",
			wantID: "",
			wantOk: false,
		},
		{
			name:   "trailing @ after other text (abc@)",
			in:     "abc@",
			wantID: "",
			wantOk: false,
		},
		{
			name:   "multiple @ uses LastIndex (a@b@c → c)",
			in:     "a@b@c",
			wantID: "c",
			wantOk: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePrincipalFromRoleAssignmentResourceID(tt.in)
			if got != tt.wantID {
				t.Errorf("parsePrincipalFromRoleAssignmentResourceID(%q) id = %q, want %q", tt.in, got, tt.wantID)
			}
			if ok != tt.wantOk {
				t.Errorf("parsePrincipalFromRoleAssignmentResourceID(%q) ok = %v, want %v", tt.in, ok, tt.wantOk)
			}
		})
	}
}

// TestRoleUUIDFromBindingRef pins the extraction of the bare role UUID from a
// ScopeBindingTrait.role_id reference. The role builder emits a composite
// "<uuid>:<subscriptionID>" string (see helper.go:getRoleId); Revoke strips
// the subscription suffix via strings.Index(":") with a `colon > 0` guard.
func TestRoleUUIDFromBindingRef(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "well-formed composite is stripped to UUID",
			in:   "974c5e8b-45b9-4653-ba55-5f855dd0fb88:0ba3df83-67b5-4a08-a561-e65fa74a1aa0",
			want: "974c5e8b-45b9-4653-ba55-5f855dd0fb88",
		},
		{
			name: "bare UUID (no colon) passes through unchanged",
			in:   "acdd72a7-3385-48ef-bd42-f606fba81ae7",
			want: "acdd72a7-3385-48ef-bd42-f606fba81ae7",
		},
		{
			name: "empty string passes through unchanged",
			in:   "",
			want: "",
		},
		{
			// Intentional defensive behavior: the `colon > 0` guard refuses to
			// trim to empty when the colon is at position 0. An empty role
			// UUID would match every assignment's RoleDefinitionID basename
			// during Revoke, so preserving the whole malformed string is
			// safer — it will simply match nothing and fall through to
			// GrantAlreadyRevoked.
			name: "leading colon preserves whole string (not trimmed to empty)",
			in:   ":x:y",
			want: ":x:y",
		},
		{
			name: "trailing colon drops the empty suffix",
			in:   "acdd72a7-3385-48ef-bd42-f606fba81ae7:",
			want: "acdd72a7-3385-48ef-bd42-f606fba81ae7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := roleUUIDFromBindingRef(tt.in); got != tt.want {
				t.Errorf("roleUUIDFromBindingRef(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestIsAzureGUID pins the input validator that guards Grant/Revoke against
// filter-smuggling. Grant and Revoke interpolate principal IDs into ARM paths
// and $filter expressions; anything that is not the canonical GUID shape
// should be refused rather than forwarded.
func TestIsAzureGUID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"canonical lowercase GUID", "4d3a9fc4-022d-4db4-9215-4a25d2ece45a", true},
		{"canonical uppercase GUID", "4D3A9FC4-022D-4DB4-9215-4A25D2ECE45A", true},
		{"mixed case GUID", "4D3A9FC4-022d-4DB4-9215-4A25d2ECE45A", true},
		{"empty string", "", false},
		{"missing dashes", "4d3a9fc4022d4db492154a25d2ece45a", false},
		{"too short", "4d3a9fc4-022d-4db4-9215-4a25d2ece45", false},
		{"too long", "4d3a9fc4-022d-4db4-9215-4a25d2ece45ab", false},
		{"non-hex char", "4d3a9fc4-022d-4db4-9215-4a25d2ece45g", false},
		{"injection attempt with quote", "4d3a9fc4-022d-4db4-9215-4a25d2ece45a' or '1'='1", false},
		{"injection attempt with filter", "x' or atScope() or '", false},
		{"just text", "not-a-guid", false},
		{"leading whitespace", " 4d3a9fc4-022d-4db4-9215-4a25d2ece45a", false},
		{"trailing newline", "4d3a9fc4-022d-4db4-9215-4a25d2ece45a\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAzureGUID(tt.in); got != tt.want {
				t.Errorf("isAzureGUID(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestScopeResourceTypeForAzureScope(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "subscription-only scope",
			in:   "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0",
			want: subscriptionsResourceType.Id,
		},
		{
			name: "resource-group scope",
			in:   "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/resourceGroups/rg-apps-web-prd",
			want: resourceGroupResourceType.Id,
		},
		{
			name: "sub-resource (keyvault secret) — mapped to RG parent per current coverage",
			in:   "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/resourceGroups/rg-apps-api-prd/providers/Microsoft.KeyVault/vaults/kv-apps-api-prd/secrets/portal-db-conn",
			want: resourceGroupResourceType.Id,
		},
		{
			name: "tenant root scope falls through to tenant type",
			in:   "/",
			want: tenantResourceType.Id,
		},
		{
			name: "management-group scope resolves to management_group type",
			in:   "/providers/Microsoft.Management/managementGroups/c1connectors-root",
			want: managementGroupResourceType.Id,
		},
		{
			name: "empty string falls through to tenant",
			in:   "",
			want: tenantResourceType.Id,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scopeResourceTypeForAzureScope(tt.in); got != tt.want {
				t.Errorf("scopeResourceTypeForAzureScope(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMapGraphPrincipalTypeToBaton(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"Graph user odata type", "#microsoft.graph.user", userResourceType.Id},
		{"Graph group odata type", "#microsoft.graph.group", groupResourceType.Id},
		{"Graph servicePrincipal odata type", "#microsoft.graph.servicePrincipal", enterpriseApplicationResourceType.Id},
		{"Azure RBAC 'Application' principal type", "Application", enterpriseApplicationResourceType.Id},
		{"Azure RBAC 'ServicePrincipal' principal type", string(armauthorization.PrincipalTypeServicePrincipal), enterpriseApplicationResourceType.Id},
		{"ManagedIdentity principal type", "ManagedIdentity", managedIdentitylResourceType.Id},
		{"unknown type returns empty (caller drops the grant)", "SomethingElse", ""},
		{"empty string returns empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapGraphPrincipalTypeToBaton(tt.in); got != tt.want {
				t.Errorf("mapGraphPrincipalTypeToBaton(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestScopeResourceRefFromAzureScope(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		wantType string
		wantID   string
	}{
		{
			name:     "subscription-only scope → bare sub GUID",
			scope:    "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0",
			wantType: subscriptionsResourceType.Id,
			wantID:   "0ba3df83-67b5-4a08-a561-e65fa74a1aa0",
		},
		{
			name:     "RG scope → bare RG name",
			scope:    "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/resourceGroups/rg-apps-web-prd",
			wantType: resourceGroupResourceType.Id,
			wantID:   "rg-apps-web-prd",
		},
		{
			name:     "KV-secret sub-resource scope → parent RG name (follow-up: own resource type)",
			scope:    "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/resourceGroups/rg-apps-api-prd/providers/Microsoft.KeyVault/vaults/kv-apps-api-prd/secrets/portal-db-conn",
			wantType: resourceGroupResourceType.Id,
			wantID:   "rg-apps-api-prd",
		},
		{
			name: "storage-container scope → parent RG name",
			scope: "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0" +
				"/resourceGroups/rg-data-lake-prd" +
				"/providers/Microsoft.Storage/storageAccounts/stc1labbronze" +
				"/blobServices/default/containers/raw",
			wantType: resourceGroupResourceType.Id,
			wantID:   "rg-data-lake-prd",
		},
		{
			name:     "mgmt-group scope → full ARM path (matches mgmt_group builder's emission)",
			scope:    "/providers/Microsoft.Management/managementGroups/c1connectors-root",
			wantType: managementGroupResourceType.Id,
			wantID:   "/providers/Microsoft.Management/managementGroups/c1connectors-root",
		},
		{
			name:     "tenant root → tenant type, raw scope as id",
			scope:    "/",
			wantType: tenantResourceType.Id,
			wantID:   "/",
		},
		{
			// Empty input is a defensive case — the ARM API shouldn't hand us
			// one, but if it does we fall through to tenant type with empty
			// id rather than panicking.
			name:     "empty string → tenant fallback with empty id",
			scope:    "",
			wantType: tenantResourceType.Id,
			wantID:   "",
		},
		{
			// Missing `providers/Microsoft.Management/managementGroups/` prefix
			// but otherwise plausible-looking — falls through to tenant default.
			name:     "malformed mgmt-group-ish path falls through to tenant",
			scope:    "/providers/Microsoft.Management/wrongThing/foo",
			wantType: tenantResourceType.Id,
			wantID:   "/providers/Microsoft.Management/wrongThing/foo",
		},
		{
			// Double leading slashes — defensively we don't match `subPrefix`
			// (which requires exactly `/subscriptions/`), so fall through.
			name:     "double slash prefix falls through to tenant",
			scope:    "//subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0",
			wantType: tenantResourceType.Id,
			wantID:   "//subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0",
		},
		{
			// Trailing slash on a subscription-only scope. The tokenizer
			// consumes up to the first `/` after the prefix, yielding the bare
			// sub GUID — the trailing empty segment is benign.
			name:     "subscription scope with trailing slash → bare sub GUID",
			scope:    "/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0/",
			wantType: subscriptionsResourceType.Id,
			wantID:   "0ba3df83-67b5-4a08-a561-e65fa74a1aa0",
		},
		{
			// Garbage input with no recognizable prefix at all. Tenant
			// fallback preserves the raw string as id so downstream
			// inspection can spot the anomaly.
			name:     "totally unrecognized path falls through to tenant",
			scope:    "not-a-scope-at-all",
			wantType: tenantResourceType.Id,
			wantID:   "not-a-scope-at-all",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotID := scopeResourceRefFromAzureScope(tt.scope)
			if gotType != tt.wantType || gotID != tt.wantID {
				t.Errorf("scopeResourceRefFromAzureScope(%q) = (%q, %q), want (%q, %q)",
					tt.scope, gotType, gotID, tt.wantType, tt.wantID)
			}
		})
	}
}
