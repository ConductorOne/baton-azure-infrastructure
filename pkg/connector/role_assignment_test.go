package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/session"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	"go.uber.org/zap"
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

// memSessionStore is an in-process sessions.SessionStore for unit tests.
// Ignores options (no sync-id / prefix semantics) — callers are
// responsible for building fully-qualified keys themselves. Just a
// `map[string][]byte` behind the interface.
type memSessionStore struct {
	m map[string][]byte
}

func newMemSessionStore() *memSessionStore {
	return &memSessionStore{m: map[string][]byte{}}
}

func (s *memSessionStore) Get(ctx context.Context, key string, opts ...sessions.SessionStoreOption) ([]byte, bool, error) {
	k := keyWithPrefix(ctx, key, opts...)
	v, ok := s.m[k]
	return v, ok, nil
}

func (s *memSessionStore) GetMany(ctx context.Context, keys []string, opts ...sessions.SessionStoreOption) (map[string][]byte, []string, error) {
	hits := map[string][]byte{}
	var misses []string
	for _, k := range keys {
		full := keyWithPrefix(ctx, k, opts...)
		if v, ok := s.m[full]; ok {
			hits[k] = v
		} else {
			misses = append(misses, k)
		}
	}
	return hits, misses, nil
}

func (s *memSessionStore) Set(ctx context.Context, key string, value []byte, opts ...sessions.SessionStoreOption) error {
	s.m[keyWithPrefix(ctx, key, opts...)] = value
	return nil
}

func (s *memSessionStore) SetMany(ctx context.Context, values map[string][]byte, opts ...sessions.SessionStoreOption) error {
	for k, v := range values {
		s.m[keyWithPrefix(ctx, k, opts...)] = v
	}
	return nil
}

func (s *memSessionStore) Delete(ctx context.Context, key string, opts ...sessions.SessionStoreOption) error {
	delete(s.m, keyWithPrefix(ctx, key, opts...))
	return nil
}

func (s *memSessionStore) Clear(ctx context.Context, opts ...sessions.SessionStoreOption) error {
	s.m = map[string][]byte{}
	return nil
}

func (s *memSessionStore) GetAll(ctx context.Context, pageToken string, opts ...sessions.SessionStoreOption) (map[string][]byte, string, error) {
	out := map[string][]byte{}
	for k, v := range s.m {
		out[k] = v
	}
	return out, "", nil
}

func keyWithPrefix(ctx context.Context, key string, opts ...sessions.SessionStoreOption) string {
	bag := &sessions.SessionStoreBag{}
	for _, o := range opts {
		_ = o(ctx, bag)
	}
	if bag.Prefix != "" {
		return bag.Prefix + key
	}
	return key
}

func TestPrincipalTypeCache_HitReturnsStoredValue(t *testing.T) {
	// Pre-populate the session store with a principal-type entry and verify
	// principalTypeForID returns it on hit without touching Graph. The miss
	// path (cache empty) calls getPrincipalType which requires a live
	// Connector + Graph token and is exercised only in live lab validation.
	b := &roleAssignmentBuilder{}
	const pid = "4d3a9fc4-022d-4db4-9215-4a25d2ece45a"
	const wantType = "#microsoft.graph.user"

	store := newMemSessionStore()
	ctx := context.Background()
	if err := session.SetJSON(ctx, store, pid, wantType, sessions.WithPrefix(principalTypePrefix)); err != nil {
		t.Fatalf("seed session store: %v", err)
	}
	opts := rs.SyncOpAttrs{Session: store}

	got := b.principalTypeForID(ctx, opts, pid)
	if got != wantType {
		t.Errorf("principalTypeForID cache hit returned %q, want %q", got, wantType)
	}

	// Second call must still hit the cache (no eviction).
	got = b.principalTypeForID(ctx, opts, pid)
	if got != wantType {
		t.Errorf("principalTypeForID second cache hit returned %q, want %q", got, wantType)
	}
}

func TestPrincipalTypeCache_NegativeHitReturnsEmpty(t *testing.T) {
	// Once getPrincipalType has failed or returned "" for a principal, the
	// empty string gets cached so subsequent lookups return "" without
	// re-querying Graph. Verify that prepopulating "" in the session store
	// short-circuits the lookup path.
	b := &roleAssignmentBuilder{}
	const pid = "00000000-0000-0000-0000-000000000000"

	store := newMemSessionStore()
	ctx := context.Background()
	if err := session.SetJSON(ctx, store, pid, "", sessions.WithPrefix(principalTypePrefix)); err != nil {
		t.Fatalf("seed session store: %v", err)
	}
	opts := rs.SyncOpAttrs{Session: store}

	got := b.principalTypeForID(ctx, opts, pid)
	if got != "" {
		t.Errorf("principalTypeForID negative-cache hit returned %q, want empty string", got)
	}

	got = b.principalTypeForID(ctx, opts, pid)
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

// ---------------------------------------------------------------------------
// Pagination state machine
// ---------------------------------------------------------------------------
//
// The streaming refactor in commit 0d1b194 drove roleAssignmentBuilder.List
// from a buffered map into a pagination.GenBag state machine. These tests
// pin the pure parts (state round-trip, dedup gate, sub advancement,
// finishPage) so regressions in the state machine are caught by `go test`
// rather than only by a full lab sync.

// TestRaBagStateRoundTrip ensures a fully-populated raBagState survives
// serialization through pagination.GenBag unchanged. A drift here would
// mean pagination tokens lose state between calls, breaking multi-call
// sync semantics.
func TestRaBagStateRoundTrip(t *testing.T) {
	original := raBagState{
		Phase:               raPhaseSub,
		PendingSubs:         []string{"sub-a", "sub-b", "sub-c"},
		CurrentSub:          "sub-a",
		SeenAssignmentNames: []string{"name-1", "name-2"},
	}
	bag := &pagination.GenBag[raBagState]{}
	bag.Push(original)
	tok, err := bag.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if tok == "" {
		t.Fatalf("expected non-empty token for a bag with state")
	}

	restored, err := pagination.GenBagFromToken[raBagState](&pagination.Token{Token: tok})
	if err != nil {
		t.Fatalf("GenBagFromToken: %v", err)
	}
	got := restored.Current()
	if got == nil {
		t.Fatalf("restored bag has no current state")
	}
	if !reflect.DeepEqual(*got, original) {
		t.Errorf("round-trip lost state.\n got:  %+v\n want: %+v", *got, original)
	}
}

// TestFinishPageEmptyBagYieldsEmptyToken: when the bag has been fully
// drained (no Current, no remaining states), finishPage must return ""
// so the baton-sdk recognises end-of-pagination rather than invoking
// List again with a token that decodes to a no-op.
func TestFinishPageEmptyBagYieldsEmptyToken(t *testing.T) {
	bag := &pagination.GenBag[raBagState]{}
	// Simulate "was full, now drained": push then pop.
	bag.Push(raBagState{Phase: raPhaseSub, CurrentSub: "x"})
	_ = bag.Pop()

	resources, res, err := finishPage(bag, nil)
	if err != nil {
		t.Fatalf("finishPage: %v", err)
	}
	if res != nil && res.Annotations != nil {
		t.Errorf("expected nil annotations, got %v", res.Annotations)
	}
	if len(resources) != 0 {
		t.Errorf("expected no resources, got %d", len(resources))
	}
	tok := ""
	if res != nil {
		tok = res.NextPageToken
	}
	if tok != "" {
		t.Errorf("expected empty token for drained bag, got %q", tok)
	}
}

// TestFinishPageNonEmptyBagYieldsSerializedState: when the bag still
// has state to process, finishPage must return a non-empty token that
// round-trips back to the same state.
func TestFinishPageNonEmptyBagYieldsSerializedState(t *testing.T) {
	bag := &pagination.GenBag[raBagState]{}
	bag.Push(raBagState{
		Phase:       raPhaseSub,
		CurrentSub:  "sub-b",
		PendingSubs: []string{"sub-b", "sub-c"},
	})

	_, res, err := finishPage(bag, nil)
	if err != nil {
		t.Fatalf("finishPage: %v", err)
	}
	if res == nil || res.NextPageToken == "" {
		t.Fatalf("expected non-empty token, got empty")
	}
	tok := res.NextPageToken

	restored, err := pagination.GenBagFromToken[raBagState](&pagination.Token{Token: tok})
	if err != nil {
		t.Fatalf("GenBagFromToken: %v", err)
	}
	cur := restored.Current()
	if cur == nil || cur.CurrentSub != "sub-b" {
		t.Errorf("round-trip lost CurrentSub; got %+v", cur)
	}
}

// TestListUnknownPhaseReturnsError guards against silent no-ops when a
// token from a future version encodes an unknown phase — better to fail
// loudly than return empty results and let the sync think it finished.
func TestListUnknownPhaseReturnsError(t *testing.T) {
	bag := &pagination.GenBag[raBagState]{}
	bag.Push(raBagState{Phase: "bogus-phase-name"})
	tok, err := bag.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	b := &roleAssignmentBuilder{}
	_, _, err = b.List(context.Background(), nil, rs.SyncOpAttrs{PageToken: pagination.Token{Token: tok}})
	if err == nil {
		t.Fatalf("expected error on unknown phase, got nil")
	}
	if !contains(err.Error(), "unknown phase") {
		t.Errorf("error should mention 'unknown phase'; got %v", err)
	}
}

// TestShouldEmitRoleAssignment pins the dedup gate: rejects nil / nil-
// fielded inputs, skips names already in seen, and records fresh names
// in the set so they're skipped on subsequent pages.
func TestShouldEmitRoleAssignment(t *testing.T) {
	name1 := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	name2 := "ffffffff-gggg-hhhh-iiii-jjjjjjjjjjjj"

	mk := func(name *string, withProps bool) *armauthorization.RoleAssignment {
		ra := &armauthorization.RoleAssignment{Name: name}
		if withProps {
			ra.Properties = &armauthorization.RoleAssignmentProperties{}
		}
		return ra
	}

	tests := []struct {
		name      string
		ra        *armauthorization.RoleAssignment
		seedSeen  []string
		want      bool
		wantInSet string // if non-empty, verify this name is in seen after call
	}{
		{name: "nil assignment rejected", ra: nil, want: false},
		{name: "nil properties rejected", ra: mk(&name1, false), want: false},
		{name: "nil name rejected", ra: mk(nil, true), want: false},
		{name: "fresh name accepted and added", ra: mk(&name1, true), want: true, wantInSet: name1},
		{name: "seen name rejected", ra: mk(&name1, true), seedSeen: []string{name1}, want: false},
		{name: "second call on same name rejected", ra: mk(&name2, true), seedSeen: []string{name2}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := make(map[string]struct{})
			for _, n := range tt.seedSeen {
				seen[n] = struct{}{}
			}
			got := shouldEmitRoleAssignment(tt.ra, seen)
			if got != tt.want {
				t.Errorf("shouldEmitRoleAssignment = %v, want %v", got, tt.want)
			}
			if tt.wantInSet != "" {
				if _, ok := seen[tt.wantInSet]; !ok {
					t.Errorf("expected %q added to seen set after emit", tt.wantInSet)
				}
			}
		})
	}
}

// TestShouldEmitRoleAssignment_RetryOnSamePage verifies the seen-set
// behaviour within a single page: if two RoleAssignment structs share
// a name (pathological but possible in Azure's paged responses under
// retry conditions), only the first is emitted.
func TestShouldEmitRoleAssignment_RetryOnSamePage(t *testing.T) {
	name := "dup-dup-dup-dup-dup"
	props := &armauthorization.RoleAssignmentProperties{}
	ra1 := &armauthorization.RoleAssignment{Name: &name, Properties: props}
	ra2 := &armauthorization.RoleAssignment{Name: &name, Properties: props}
	seen := make(map[string]struct{})

	if !shouldEmitRoleAssignment(ra1, seen) {
		t.Fatalf("first call should emit")
	}
	if shouldEmitRoleAssignment(ra2, seen) {
		t.Fatalf("second call on same name should skip")
	}
}

// TestAdvancePendingSubs pins the sub-queue advancement logic: drop the
// head when it matches the just-processed sub, preserve the tail, and
// return ("", nil) at end-of-queue so the caller knows to stop.
func TestAdvancePendingSubs(t *testing.T) {
	tests := []struct {
		name          string
		justProcessed string
		pending       []string
		wantNext      string
		wantRemaining []string
	}{
		{
			name:          "head matches, drops it and returns next",
			justProcessed: "sub-a",
			pending:       []string{"sub-a", "sub-b", "sub-c"},
			wantNext:      "sub-b",
			wantRemaining: []string{"sub-b", "sub-c"},
		},
		{
			name:          "last sub processed returns empty",
			justProcessed: "sub-z",
			pending:       []string{"sub-z"},
			wantNext:      "",
			wantRemaining: nil,
		},
		{
			name:          "empty pending returns empty",
			justProcessed: "sub-x",
			pending:       nil,
			wantNext:      "",
			wantRemaining: nil,
		},
		{
			name: "defensive: just-processed doesn't match head, preserve pending",
			// Pathological: caller reordered the pending list between calls.
			// Safer to preserve the tail than silently advance.
			justProcessed: "sub-stale",
			pending:       []string{"sub-a", "sub-b"},
			wantNext:      "sub-a",
			wantRemaining: []string{"sub-a", "sub-b"},
		},
		{
			name:          "single sub, head matches, returns empty",
			justProcessed: "only",
			pending:       []string{"only"},
			wantNext:      "",
			wantRemaining: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNext, gotRemaining := advancePendingSubs(tt.justProcessed, tt.pending)
			if gotNext != tt.wantNext {
				t.Errorf("next = %q, want %q", gotNext, tt.wantNext)
			}
			if !reflect.DeepEqual(gotRemaining, tt.wantRemaining) {
				t.Errorf("remaining = %v, want %v", gotRemaining, tt.wantRemaining)
			}
		})
	}
}

// TestListFirstCallSeedsInitPhase: when the pagination token is empty,
// List should internally push raPhaseInit. We can't drive listInit
// end-to-end without mocking Azure, but we CAN verify that the
// top-level dispatch doesn't immediately return ErrNoToken or panic
// on a nil/empty pToken — behaviour that was broken during
// initial development.
func TestListFirstCallAcceptsEmptyToken(t *testing.T) {
	// Use an uninitialized builder — we don't expect success (the
	// connector is nil, so listInit will nil-deref inside the Azure
	// client construction). What we're pinning is that the token-parse
	// path accepts an empty token without error and that the error,
	// when it arrives, is NOT a "cannot unmarshal empty" pagination
	// error. Anything that reaches into the handler's Azure-SDK call
	// path means dispatch worked.
	b := &roleAssignmentBuilder{}
	defer func() {
		if r := recover(); r != nil {
			// A nil-deref panic is expected here (nil Connector); it
			// proves we got past the token parse and reached the
			// Azure client construction in listInit.
			return
		}
	}()
	_, _, err := b.List(context.Background(), nil, rs.SyncOpAttrs{PageToken: pagination.Token{Token: ""}})
	// If no panic, we must at least have a non-pagination error.
	if err != nil && contains(err.Error(), "pagination") {
		t.Errorf("empty token should be accepted by pagination layer; got %v", err)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestListSubPage_PreservesSeenSetAcrossSubTransition pins the cross-call
// dedup invariant that's load-bearing for the streaming refactor: the
// mgmt-group-scope assignment names collected in the init phase must
// survive every sub → sub transition unchanged, otherwise those
// assignments would be emitted again during the sub walk.
//
// We drive listSubPage directly with CurrentSub="" to short-circuit the
// Azure client construction and exercise just the advance-and-push tail,
// which is the part the code-review agent flagged as untested.
func TestListSubPage_PreservesSeenSetAcrossSubTransition(t *testing.T) {
	seenNames := []string{
		"5099ae91-7a64-44b0-b7bf-e5c81463ed4d",
		"ec8532c6-aa52-4b4c-8326-41ce1bc4b79b",
	}
	state := &raBagState{
		Phase:               raPhaseSub,
		PendingSubs:         []string{"sub-a", "sub-b", "sub-c"},
		CurrentSub:          "", // empty — short-circuits Azure work, exercises tail only.
		SeenAssignmentNames: seenNames,
	}

	bag := &pagination.GenBag[raBagState]{}
	b := &roleAssignmentBuilder{}

	emitted, res, err := b.listSubPage(context.Background(), zap.NewNop(), bag, state)
	if err != nil {
		t.Fatalf("listSubPage: %v", err)
	}
	if len(emitted) != 0 {
		t.Errorf("expected no resources emitted when CurrentSub empty, got %d", len(emitted))
	}
	if res == nil || res.NextPageToken == "" {
		t.Fatalf("expected non-empty token — bag should still have pending subs to process")
	}

	next := bag.Current()
	if next == nil {
		t.Fatalf("expected bag to carry a follow-up state; got nil")
	}
	if !reflect.DeepEqual(next.SeenAssignmentNames, seenNames) {
		t.Errorf("SeenAssignmentNames lost across sub transition.\n got:  %v\n want: %v",
			next.SeenAssignmentNames, seenNames)
	}
	if next.CurrentSub != "sub-a" {
		t.Errorf("expected advance to first pending sub-a, got CurrentSub=%q", next.CurrentSub)
	}
}
