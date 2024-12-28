package connector

import (
	"context"
	"os"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/google/uuid"
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

func TestSubscriptionBuilderList(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	s := &subscriptionBuilder{
		cn: &connTest,
	}
	_, _, _, err = s.List(ctxTest, &v2.ResourceId{}, &pagination.Token{})
	require.Nil(t, err)
}

func TestTenantBuilderList(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	tn := &tenantBuilder{
		cn: &connTest,
	}
	_, _, _, err = tn.List(ctxTest, &v2.ResourceId{}, &pagination.Token{})
	require.Nil(t, err)
}

func TestResourceGroupBuilderList(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	rg := &resourceGroupBuilder{
		cn: &connTest,
	}
	_, _, _, err = rg.List(ctxTest, &v2.ResourceId{}, &pagination.Token{})
	require.Nil(t, err)
}

func TestRoleBuilderList(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	r := &roleBuilder{
		cn: &connTest,
	}
	_, _, _, err = r.List(ctxTest, &v2.ResourceId{}, &pagination.Token{})
	require.Nil(t, err)
}

func TestRoleAssignment(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// Initialize the client
	client, err := armauthorization.NewRoleAssignmentsClient("39ea64c5-86d5-4c29-8199-5b602c90e1c5", connTest.token, nil)
	require.Nil(t, err)

	// Define your resource's scope
	scope := "/subscriptions/39ea64c5-86d5-4c29-8199-5b602c90e1c5/resourceGroups/test_resource_group"
	// Define the details of the role assignment
	roleDefinitionID := "/subscriptions/39ea64c5-86d5-4c29-8199-5b602c90e1c5/providers/Microsoft.Authorization/roleDefinitions/0105a6b0-4bb9-43d2-982a-12806f9faddb"
	// Object ID of the user, group, or service principal
	principalID := "e4e9c5ae-2937-408b-ba3c-0f58cf417f0a"

	// Create a role assignment name (must be unique)
	roleAssignmentName := uuid.New().String()

	// Prepare role assignment parameters
	parameters := armauthorization.RoleAssignmentCreateParameters{
		Properties: &armauthorization.RoleAssignmentProperties{
			PrincipalID:      &principalID,
			RoleDefinitionID: &roleDefinitionID,
		},
	}

	// Create the role assignment
	resp, err := client.Create(ctxTest, scope, roleAssignmentName, parameters, nil)
	require.Nil(t, err)
	require.NotNil(t, resp)
}

func TestRoleGrants(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	r := &roleBuilder{
		cn: &connTest,
	}
	_, _, _, err = r.Grants(ctxTest, &v2.Resource{}, &pagination.Token{})
	require.Nil(t, err)
}

func TestSubscriptionGrants(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	s := &subscriptionBuilder{
		cn: &connTest,
	}
	res, err := rs.NewResource(
		"Azure subscription 1",
		subscriptionsResourceType,
		"39ea64c5-86d5-4c29-8199-5b602c90e1c5",
	)
	require.Nil(t, err)

	_, _, _, err = s.Grants(ctxTest, res, &pagination.Token{})
	require.Nil(t, err)
}
