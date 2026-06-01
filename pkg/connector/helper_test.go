package connector

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
)

func TestSplitId(t *testing.T) {
	id := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/{resourceProviderNamespace}/{resourceType}/{resourceName}"

	resp, err := newStorageResourceSplitIdDataFromAzureId(id)
	require.NoError(t, err)

	require.Equal(t, "{subscriptionId}", resp.subscriptionID)
	require.Equal(t, "{resourceGroupName}", resp.resourceGroupName)
	require.Equal(t, "{resourceProviderNamespace}", resp.resourceProviderNamespace)
	require.Equal(t, "{resourceType}", resp.resourceType)
	require.Equal(t, "{resourceName}", resp.resourceName)

	require.Equal(t, id, resp.AzureId())

	connectorId, err := newStorageResourceSplitIdDataFromConnectorId(resp.ConnectorId())
	require.NoError(t, err)
	require.Equal(t, id, connectorId.AzureId())
}

func TestManagedIdentityNHIDetail(t *testing.T) {
	cases := []struct {
		name     string
		altNames []string
		expected string
	}{
		{
			name:     "user assigned",
			altNames: []string{"isExplicit=True", "/subscriptions/x/resourceGroups/y/providers/Microsoft.ManagedIdentity/userAssignedIdentities/z"},
			expected: "azure.user_assigned_mi",
		},
		{
			name:     "system assigned",
			altNames: []string{"isExplicit=False", "/subscriptions/x/resourceGroups/y/providers/Microsoft.Compute/virtualMachines/z"},
			expected: "azure.system_assigned_mi",
		},
		{
			name:     "no discriminator falls back to generic",
			altNames: nil,
			expected: "azure.managed_identity",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := &client.ServicePrincipal{AlternativeNames: c.altNames}
			require.Equal(t, c.expected, managedIdentityNHIDetail(sp))
		})
	}
}
