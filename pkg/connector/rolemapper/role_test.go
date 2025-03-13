package rolemapper

import (
	"testing"

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
