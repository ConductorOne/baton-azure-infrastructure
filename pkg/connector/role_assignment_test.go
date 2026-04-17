package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
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

	tests := []struct {
		name      string
		err       error
		forbidden bool
		conflict  bool
		notFound  bool
	}{
		{"nil error", nil, false, false, false},
		{"plain error, no ResponseError", errors.New("boom"), false, false, false},
		{"403", makeRespErr(http.StatusForbidden), true, false, false},
		{"409", makeRespErr(http.StatusConflict), false, true, false},
		{"404", makeRespErr(http.StatusNotFound), false, false, true},
		{"200 is none", makeRespErr(http.StatusOK), false, false, false},
		{"wrapped 409 still detected", wrapped, false, true, false},
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
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"user → User", userResourceType.Id, "User"},
		{"enterprise_application → ServicePrincipal", enterpriseApplicationResourceType.Id, "ServicePrincipal"},
		{"managed_identity → ServicePrincipal", managedIdentitylResourceType.Id, "ServicePrincipal"},
		{"group → unsupported (empty)", groupResourceType.Id, ""},
		{"unknown type → empty", "resource_group", ""},
		{"empty → empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := batonToAzurePrincipalType(tt.in); got != tt.want {
				t.Errorf("batonToAzurePrincipalType(%q) = %q, want %q", tt.in, got, tt.want)
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
		{"Azure RBAC 'ServicePrincipal' principal type", "ServicePrincipal", enterpriseApplicationResourceType.Id},
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
