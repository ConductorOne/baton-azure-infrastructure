package connector

import (
	"testing"
)

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

	got := b.principalTypeForID(nil, pid)
	if got != wantType {
		t.Errorf("principalTypeForID cache hit returned %q, want %q", got, wantType)
	}

	// Second call must still hit the cache (no eviction).
	got = b.principalTypeForID(nil, pid)
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
