package connector

import (
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
)

// Helpers to build test fixtures ergonomically.
func ptr[T any](v T) *T { return &v }

func mgEntity(id, name, typ, parentID, tenantID string) *armmanagementgroups.EntityInfo {
	e := &armmanagementgroups.EntityInfo{
		ID:   ptr(id),
		Name: ptr(name),
		Type: ptr(typ),
		Properties: &armmanagementgroups.EntityInfoProperties{
			TenantID: ptr(tenantID),
		},
	}
	if parentID != "" {
		e.Properties.Parent = &armmanagementgroups.EntityParentGroupInfo{
			ID: ptr(parentID),
		}
	}
	return e
}

// TestHierarchyKeyForEntity pins the key format — this is the invariant that
// allows the scope-resource builders to look up a parent by their own
// builder-emitted ID.
func TestHierarchyKeyForEntity(t *testing.T) {
	tests := []struct {
		name   string
		entity *armmanagementgroups.EntityInfo
		want   string
	}{
		{
			name:   "management group keyed by ARM path",
			entity: mgEntity("/providers/Microsoft.Management/managementGroups/c1connectors-root", "c1connectors-root", "Microsoft.Management/managementGroups", "", "tenant-guid"),
			want:   "/providers/Microsoft.Management/managementGroups/c1connectors-root",
		},
		{
			name:   "subscription keyed by bare GUID (from Name)",
			entity: mgEntity("/subscriptions/0ba3df83-67b5-4a08-a561-e65fa74a1aa0", "0ba3df83-67b5-4a08-a561-e65fa74a1aa0", "Microsoft.Management/managementGroups/subscriptions", "", "tenant-guid"),
			want:   "0ba3df83-67b5-4a08-a561-e65fa74a1aa0",
		},
		{
			name: "subscription keyed from ID when Name is empty",
			entity: &armmanagementgroups.EntityInfo{
				ID:         ptr("/subscriptions/fallback-guid"),
				Name:       ptr(""),
				Type:       ptr("Microsoft.Management/managementGroups/subscriptions"),
				Properties: &armmanagementgroups.EntityInfoProperties{},
			},
			want: "fallback-guid",
		},
		{
			name: "unknown entity type returns empty",
			entity: &armmanagementgroups.EntityInfo{
				ID:   ptr("/something/else"),
				Name: ptr("weird"),
				Type: ptr("Microsoft.Something/else"),
			},
			want: "",
		},
		{name: "nil entity returns empty", entity: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hierarchyKeyForEntity(tt.entity); got != tt.want {
				t.Errorf("hierarchyKeyForEntity = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestHierarchyParentResourceID pins the "no parent → tenant" fallback and
// the "parent → management_group" path. Both are load-bearing for the tree
// view rendering correctly.
func TestHierarchyParentResourceID(t *testing.T) {
	tests := []struct {
		name   string
		entity *armmanagementgroups.EntityInfo
		want   *v2.ResourceId
	}{
		{
			name: "parent present → management_group ResourceId",
			entity: mgEntity(
				"/providers/Microsoft.Management/managementGroups/child",
				"child",
				"Microsoft.Management/managementGroups",
				"/providers/Microsoft.Management/managementGroups/parent",
				"tenant-guid",
			),
			want: &v2.ResourceId{
				ResourceType: managementGroupResourceType.Id,
				Resource:     "/providers/Microsoft.Management/managementGroups/parent",
			},
		},
		{
			name:   "no parent → tenant ResourceId from TenantID",
			entity: mgEntity("/providers/Microsoft.Management/managementGroups/root", "root", "Microsoft.Management/managementGroups", "", "419b7f5f-33e5-477e-af7b-063ba4381e18"),
			want: &v2.ResourceId{
				ResourceType: tenantResourceType.Id,
				Resource:     "419b7f5f-33e5-477e-af7b-063ba4381e18",
			},
		},
		{
			name:   "no parent and no tenant → nil (malformed, don't set parent)",
			entity: &armmanagementgroups.EntityInfo{ID: ptr("/x"), Name: ptr("x"), Type: ptr("Microsoft.Management/managementGroups"), Properties: &armmanagementgroups.EntityInfoProperties{}},
			want:   nil,
		},
		{name: "nil entity → nil", entity: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hierarchyParentResourceID(tt.entity)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("hierarchyParentResourceID = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestBuildHierarchyIndex walks an end-to-end shape: a tenant root, an
// intermediate mgmt group, and a subscription under the intermediate. Pin
// the full lookup table so a future reviewer sees the invariant at a
// glance: every emitted resource type gets the parent its builder expects.
func TestBuildHierarchyIndex(t *testing.T) {
	const (
		tenantGUID = "419b7f5f-33e5-477e-af7b-063ba4381e18"
		rootID     = "/providers/Microsoft.Management/managementGroups/" + tenantGUID
		childID    = "/providers/Microsoft.Management/managementGroups/c1connectors-root"
		subGUID    = "0ba3df83-67b5-4a08-a561-e65fa74a1aa0"
		subFullID  = "/subscriptions/" + subGUID
	)
	entities := []*armmanagementgroups.EntityInfo{
		mgEntity(rootID, tenantGUID, "Microsoft.Management/managementGroups", "", tenantGUID),
		mgEntity(childID, "c1connectors-root", "Microsoft.Management/managementGroups", rootID, tenantGUID),
		mgEntity(subFullID, subGUID, "Microsoft.Management/managementGroups/subscriptions", childID, tenantGUID),
		nil,            // nil entries are tolerated
		{Type: nil},    // malformed — skipped
		{ID: ptr("x")}, // missing Type — skipped
	}
	idx := buildHierarchyIndex(entities)

	want := hierarchyIndex{
		rootID:  {ResourceType: tenantResourceType.Id, Resource: tenantGUID},
		childID: {ResourceType: managementGroupResourceType.Id, Resource: rootID},
		subGUID: {ResourceType: managementGroupResourceType.Id, Resource: childID},
	}
	if !reflect.DeepEqual(idx, want) {
		t.Errorf("buildHierarchyIndex mismatch:\n got:  %+v\n want: %+v", idx, want)
	}

	// Cross-check the lookup contract: each emitted resource's builder-ID
	// should resolve to a parent.
	if idx[rootID].ResourceType != tenantResourceType.Id {
		t.Errorf("tenant root mgmt group must point at tenant resource, got %v", idx[rootID])
	}
	if idx[childID].Resource != rootID {
		t.Errorf("intermediate mgmt group must point at parent mgmt ARM path, got %v", idx[childID])
	}
	if idx[subGUID].Resource != childID {
		t.Errorf("subscription must point at its containing mgmt group, got %v", idx[subGUID])
	}
}
