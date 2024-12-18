package connector

import (
	"context"
	"os"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/stretchr/testify/require"
)

var (
	azureClientId     = os.Getenv("BATON_AZURE_CLIENT_ID")
	azureClientSecret = os.Getenv("BATON_AZURE_CLIENT_SECRET")
	azureTenantId     = os.Getenv("BATON_AZURE_TENANT_ID")
	ctxTest           = context.Background()
)

func TestUserBuilderList(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	u := &userBuilder{
		cn: &connTest,
	}
	res, _, _, err := u.List(ctxTest, &v2.ResourceId{}, &pagination.Token{})
	require.Nil(t, err)
	require.NotNil(t, res)
}

func TestGroupBuilderList(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	u := &groupBuilder{
		cn: &connTest,
	}
	res, _, _, err := u.List(ctxTest, &v2.ResourceId{}, &pagination.Token{})
	require.Nil(t, err)
	require.NotNil(t, res)
}

func getConnectorForTesting(ctx context.Context, entraTenantId, entraClientSecret, entraClientId string) (Connector, error) {
	useCliCredentials := false
	mailboxSettings := false
	skipAdGroups := false
	cb, err := New(ctx, useCliCredentials, entraTenantId, entraClientId, entraClientSecret, mailboxSettings, skipAdGroups)
	if err != nil {
		return Connector{}, err
	}

	return *cb, nil
}
