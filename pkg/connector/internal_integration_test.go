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
	azureClientId            = os.Getenv("BATON_AZURE_CLIENT_ID")
	azureClientSecret        = os.Getenv("BATON_AZURE_CLIENT_SECRET")
	azureTenantId            = os.Getenv("BATON_AZURE_TENANT_ID")
	ctxTest                  = context.Background()
	grantPrincipalForTesting = "72af6288-7040-49ca-a2f0-51ce6ba5a78a"
	roleForTesting           = "11102f94-c441-49e6-a78b-ef80e0188abc"
	subscriptionIDForTesting = "39ea64c5-86d5-4c29-8199-5b602c90e1c5"
)

func TestUserBuilderList(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	u := &userBuilder{
		conn: &connTest,
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
		conn: &connTest,
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
		conn: &connTest,
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
		conn: &connTest,
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
		conn: &connTest,
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
		conn: &connTest,
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
		conn: &connTest,
	}

	lstRoles, err := getAllRoles(ctxTest, &connTest, subscriptionIDForTesting)
	require.Nil(t, err)

	roleDefinitionID := fmt.Sprintf(
		"/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s",
		subscriptionIDForTesting,
		roleForTesting,
	)
	for _, rl := range lstRoles {
		rs, err := roleResource(ctxTest, &armauthorization.RoleDefinition{
			ID: &roleDefinitionID,
			Properties: &armauthorization.RoleDefinitionProperties{
				RoleName:    &rl,
				Description: &rl,
				RoleType:    &rl,
			},
		}, nil)
		require.Nil(t, err)

		_, _, _, err = r.Grants(ctxTest, rs, nil)
		require.Nil(t, err)
	}
}

func TestRoleAssignmentResourceGroupGrants(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	r := &roleAssignmentResourceGroupBuilder{
		conn: &connTest,
	}
	lstResourceGroups, err := getResourceGroups(ctxTest, &connTest)
	require.Nil(t, err)

	for _, rg := range lstResourceGroups {
		gr, err := roleAssignmentResourceGroupResource(ctxTest,
			subscriptionIDForTesting,
			roleForTesting,
			&armresources.ResourceGroup{
				ID:   &rg,
				Name: &rg,
			}, nil)
		require.Nil(t, err)

		_, _, _, err = r.Grants(ctxTest, gr, nil)
		require.Nil(t, err)
	}
}

func TestSubscriptionGrants(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	s := &subscriptionBuilder{
		conn: &connTest,
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

func getRoleAssignmentResourceGroupForTesting(ctxTest context.Context, subscriptionId, roleId, name, description string) (*v2.Resource, error) {
	strRoleId := fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", subscriptionId, roleId)
	return roleAssignmentResourceGroupResource(ctxTest,
		subscriptionId,
		roleId,
		&armresources.ResourceGroup{
			ID:   &strRoleId,
			Name: &name,
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

func TestRoleGrant(t *testing.T) {
	var roleEntitlement string
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// role:c2f4ef07-c644-48eb-af81-4b1b4947fb11:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned:user:72af6288-7040-49ca-a2f0-51ce6ba5a78a
	grantEntitlement := "role:11102f94-c441-49e6-a78b-ef80e0188abc:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned"
	grantPrincipalType := "user"
	grantPrincipal := grantPrincipalForTesting
	_, data, err := parseEntitlementID(grantEntitlement)
	require.Nil(t, err)
	require.NotNil(t, data)

	roleEntitlement = data[3]
	resource, err := getRoleForTesting(ctxTest, data[2], data[1], "AcrDelete", "testing role")
	require.Nil(t, err)

	entitlement := getEntitlementForTesting(resource, grantPrincipalType, roleEntitlement)
	g := &roleBuilder{
		conn: &connTest,
	}
	_, err = g.Grant(ctxTest, &v2.Resource{
		Id: &v2.ResourceId{
			ResourceType: userResourceType.Id,
			Resource:     grantPrincipal,
		},
	}, entitlement)
	require.Nil(t, err)
}

func TestRoleRevoke(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// role:c2f4ef07-c644-48eb-af81-4b1b4947fb11:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned:user:72af6288-7040-49ca-a2f0-51ce6ba5a78a
	revokeGrant := "role:c2f4ef07-c644-48eb-af81-4b1b4947fb11:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned:user:72af6288-7040-49ca-a2f0-51ce6ba5a78a"
	data := strings.Split(revokeGrant, ":")
	principalID := &v2.ResourceId{ResourceType: userResourceType.Id, Resource: "72af6288-7040-49ca-a2f0-51ce6ba5a78a"}
	resource, err := getRoleForTesting(ctxTest, data[2], data[1], "AcrDelete", "testing role")
	require.Nil(t, err)

	gr := grant.NewGrant(resource, typeAssigned, principalID)
	annos := annotations.Annotations(gr.Annotations)
	gr.Annotations = annos
	require.NotNil(t, gr)

	// --revoke-grant "role:c2f4ef07-c644-48eb-af81-4b1b4947fb11:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned:user:72af6288-7040-49ca-a2f0-51ce6ba5a78a"
	l := &roleBuilder{
		conn: &connTest,
	}
	_, err = l.Revoke(ctxTest, gr)
	require.Nil(t, err)
}

func TestListingResourceGroupContent(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// Define variables
	resourceGroupName := "test_resource_group"
	subscriptionID := "39ea64c5-86d5-4c29-8199-5b602c90e1c5"

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

func TestGetPrincipalType(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	// Authenticate with Microsoft Graph
	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// principalID := "eeffc762-5afc-472e-bdc1-c27c9ec62d02"
	principalID := "72af6288-7040-49ca-a2f0-51ce6ba5a78a"
	_, err = getPrincipalType(ctxTest, &connTest, principalID)
	require.Nil(t, err)
}

func TestListAllRoles(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	// Authenticate with Microsoft Graph
	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	_, err = getAllRoles(ctxTest, &connTest, "")
	require.Nil(t, err)
}

func TestRoleAssignmentResourceGroupGrant(t *testing.T) {
	var roleEntitlement string
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// resource_group:test_2_resource_group:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned:user:72af6288-7040-49ca-a2f0-51ce6ba5a78a
	grantEntitlement := "resource_group:test_2_resource_group:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned"
	grantPrincipalType := "user"
	grantPrincipal := "72af6288-7040-49ca-a2f0-51ce6ba5a78a"
	_, data, err := parseEntitlementID(grantEntitlement)
	require.Nil(t, err)
	require.NotNil(t, data)

	roleEntitlement = data[2]
	resource, err := getRoleAssignmentResourceGroupForTesting(ctxTest, data[2], data[1], "test_resource_group", "testing role")
	require.Nil(t, err)

	entitlement := getEntitlementForTesting(resource, grantPrincipalType, roleEntitlement)
	g := &roleAssignmentResourceGroupBuilder{
		conn: &connTest,
	}
	_, err = g.Grant(ctxTest, &v2.Resource{
		Id: &v2.ResourceId{
			ResourceType: userResourceType.Id,
			Resource:     grantPrincipal,
		},
	}, entitlement)
	require.Nil(t, err)
}

func TestResourceGroupEntitlements(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	rg := &roleAssignmentResourceGroupBuilder{
		conn: &connTest,
	}

	lstResourceGroups, err := getResourceGroups(ctxTest, &connTest)
	require.Nil(t, err)

	for _, rgs := range lstResourceGroups {
		rs, err := roleAssignmentResourceGroupResource(ctxTest,
			subscriptionIDForTesting,
			roleForTesting,
			&armresources.ResourceGroup{
				ID:   &rgs,
				Name: &rgs,
			}, nil)
		require.Nil(t, err)

		_, _, _, err = rg.Entitlements(ctxTest, rs, nil)
		require.Nil(t, err)
	}
}
