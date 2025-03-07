package connector

import (
	"testing"

	"github.com/stretchr/testify/require"
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
