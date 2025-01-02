package connector

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
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

func TestRoleGrant(t *testing.T) {
	var roleEntitlement string
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	grantEntitlement := "role:39ea64c5-86d5-4c29-8199-5b602c90e1c5:8e3af657-a8ff-443c-a75c-2fe8c4bcb635:members"
	grantPrincipalType := "user"
	grantPrincipal := "72af6288-7040-49ca-a2f0-51ce6ba5a78a"
	_, data, err := parseEntitlementID(grantEntitlement)
	require.Nil(t, err)
	require.NotNil(t, data)

	roleEntitlement = data[2]
	resource, err := getRoleForTesting(ctxTest, data[1], data[2], "local_role", "testing role")
	require.Nil(t, err)

	entitlement := getEntitlementForTesting(resource, grantPrincipalType, roleEntitlement)
	g := &roleBuilder{
		cn: &connTest,
	}
	_, err = g.Grant(ctxTest, &v2.Resource{
		Id: &v2.ResourceId{
			ResourceType: userResourceType.Id,
			Resource:     grantPrincipal,
		},
	}, entitlement)
	require.Nil(t, err)
}

func parseEntitlementID(id string) (*v2.ResourceId, []string, error) {
	parts := strings.Split(id, ":")
	// Need to be at least 3 parts type:entitlement_id:slug
	if len(parts) < 4 || len(parts) > 4 {
		return nil, nil, fmt.Errorf("azure-infrastructure-connector: invalid resource id")
	}

	resourceId := &v2.ResourceId{
		ResourceType: parts[0],
		Resource:     strings.Join(parts[1:len(parts)-1], ":"),
	}

	return resourceId, parts, nil
}

func getRoleForTesting(ctxTest context.Context, subscriptionId, roleId, name, description string) (*v2.Resource, error) {
	strRoleId := fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", subscriptionId, roleId)
	return roleResource(ctxTest, &armauthorization.RoleDefinition{
		ID:   &strRoleId,
		Name: &name,
		Properties: &armauthorization.RoleDefinitionProperties{
			RoleName:    &name,
			Description: &description,
		},
	}, nil)
}

func getEntitlementForTesting(resource *v2.Resource, resourceDisplayName, entitlement string) *v2.Entitlement {
	options := []ent.EntitlementOption{
		ent.WithGrantableTo(userResourceType),
		ent.WithDisplayName(fmt.Sprintf("%s resource %s", resourceDisplayName, entitlement)),
		ent.WithDescription(fmt.Sprintf("%s of %s azure", entitlement, resourceDisplayName)),
	}

	return ent.NewAssignmentEntitlement(resource, entitlement, options...)
}

func TestRoleRevoke(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// --revoke-grant project:10000:Administrators:user:JIRAUSER10103
	revokeGrant := "role:39ea64c5-86d5-4c29-8199-5b602c90e1c5:0105a6b0-4bb9-43d2-982a-12806f9faddb:members:user:72af6288-7040-49ca-a2f0-51ce6ba5a78a"
	data := strings.Split(revokeGrant, ":")
	principalID := &v2.ResourceId{ResourceType: userResourceType.Id, Resource: "4603be3e-9014-4bb4-9bc0-27a1a77b8e82"}
	resource, err := getRoleForTesting(ctxTest, data[1], data[2], "local_role", "testing role")
	require.Nil(t, err)

	gr := grant.NewGrant(resource, "members", principalID)
	annos := annotations.Annotations(gr.Annotations)
	gr.Annotations = annos
	require.NotNil(t, gr)

	// --revoke-grant "role:39ea64c5-86d5-4c29-8199-5b602c90e1c5:0105a6b0-4bb9-43d2-982a-12806f9faddb:members:user:4603be3e-9014-4bb4-9bc0-27a1a77b8e82"
	l := &roleBuilder{
		cn: &connTest,
	}
	_, err = l.Revoke(ctxTest, gr)
	require.Nil(t, err)
}

func TestB2(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	subscriptionID := "39ea64c5-86d5-4c29-8199-5b602c90e1c5" // Replace with your subscription ID
	principalID := "test_resource_group"                     // Replace with the Principal ID you're searching for

	// Create a Resource client
	clientFactory, err := armresources.NewClientFactory(subscriptionID, connTest.token, nil)
	require.Nil(t, err)

	resourceClient := clientFactory.NewResourceGroupsClient()
	// resourceClient := clientFactory.NewResourcesClient()

	// List resources and search for the matching Principal ID
	pager := resourceClient.NewListPager(nil)
	ctx := context.Background()

	for pager.More() {
		page, err := pager.NextPage(ctx)
		require.Nil(t, err)

		for _, resource := range page.Value {
			if strings.Contains(*resource.ID, principalID) {
				log.Println("ok")
			}
		}
	}
}

func TestListingResourceGroupContent(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// Define variables
	resourceGroupName := "test_2_resource_group"
	subscriptionID := "39ea64c5-86d5-4c29-8199-5b602c90e1c5"

	// Authenticate with Azure
	// cred, err := azidentity.NewDefaultAzureCredential(nil)
	// if err != nil {
	// 	log.Fatalf("Failed to get credentials: %v", err)
	// }

	// Create a Resources client
	client, err := armresources.NewClient(subscriptionID, connTest.token, nil)
	require.Nil(t, err)

	// List resources in the resource group
	pager := client.NewListByResourceGroupPager(resourceGroupName, nil)
	log.Printf("Resources in resource group %s:\n", resourceGroupName)

	// Iterate through the pages of results
	for pager.More() {
		page, err := pager.NextPage(ctxTest)
		if err != nil {
			log.Fatalf("Failed to get next page: %v", err)
		}
		for _, resource := range page.Value {
			log.Printf("- Name: %s, Type: %s\n", *resource.Name, *resource.Type)
		}
	}
}
