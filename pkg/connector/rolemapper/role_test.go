package rolemapper

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"

	"github.com/stretchr/testify/require"
)

func TestRoleMapper(t *testing.T) {
	mapper := NewRoleActionMapper("Microsoft.Storage/storageAccounts/", []string{"read", "write", "delete"})
	require.NotNil(t, mapper)

	action := mapper.GetRoleAction("Microsoft.Storage/storageAccounts/*")
	require.Len(t, action, 3)

	action = mapper.GetRoleAction("Microsoft.Storage/storageAccounts/read")
	require.Equal(t, []string{"read"}, action)

	action = mapper.GetRoleAction("Microsoft.Storage/storageAccounts/write")
	require.Equal(t, []string{"write"}, action)

	action = mapper.GetRoleAction("Microsoft.Storage/storageAccounts/delete")
	require.Equal(t, []string{"delete"}, action)

	action = mapper.GetRoleAction("Microsoft.Storage/storageAccounts/listKeys")
	require.Nil(t, action)

	actions := mapper.Actions()
	require.NotNil(t, actions)
	require.EqualValues(t, map[string]string{
		"Microsoft.Storage/storageAccounts/read":   "read",
		"Microsoft.Storage/storageAccounts/write":  "write",
		"Microsoft.Storage/storageAccounts/delete": "delete",
	}, actions)

	action = mapper.GetRoleAction("*/read")
	require.EqualValues(t, []string{"read"}, action)

	action = mapper.GetRoleAction("*")
	require.Len(t, action, 3)
}

func TestMapRoleToAzureRoleAction(t *testing.T) {
	toPointerString := func(s string) *string {
		return &s
	}

	cases := []struct {
		name        string
		mapper      *AzureRoleActionMapper
		permissions []*armauthorization.Permission
		expected    []string
	}{
		{
			name:   "empty permissions",
			mapper: ContainerPermissions,
			permissions: []*armauthorization.Permission{
				{
					Actions:    nil,
					NotActions: nil,
				},
			},
			expected: nil,
		},
		{
			name:   "all scope",
			mapper: ContainerPermissions,
			permissions: []*armauthorization.Permission{
				{
					Actions: []*string{
						toPointerString("Microsoft.Storage/storageAccounts/blobServices/containers/*"),
					},
					NotActions: nil,
				},
			},
			expected: []string{"read", "write", "delete"},
		},
		{
			name:   "all scope 2",
			mapper: ContainerPermissions,
			permissions: []*armauthorization.Permission{
				{
					Actions: []*string{
						toPointerString("*"),
					},
					NotActions: nil,
				},
			},
			expected: []string{"read", "write", "delete"},
		},
	}

	for _, s := range cases {
		t.Run(s.name, func(t *testing.T) {
			action, err := s.mapper.MapRoleToAzureRoleAction(s.permissions)
			require.NoError(t, err)

			require.Len(t, action, len(s.expected))
		})
	}
}
