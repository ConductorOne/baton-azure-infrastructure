package connector

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/conductorone/baton-azure-infrastructure/pkg/internal/slices"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"

	"github.com/stretchr/testify/require"
)

var (
	azureClientId              = os.Getenv("BATON_AZURE_CLIENT_ID")
	azureClientSecret          = os.Getenv("BATON_AZURE_CLIENT_SECRET")
	azureTenantId              = os.Getenv("BATON_AZURE_TENANT_ID")
	ctxTest                    = context.Background()
	grantPrincipalForTesting   = "72af6288-7040-49ca-a2f0-51ce6ba5a78a"
	grantPrincipalForTestingV2 = "e7f6b650-1cd5-4859-a258-1de497c29de3"
	roleForTesting             = "11102f94-c441-49e6-a78b-ef80e0188abc"
	subscriptionIDForTesting   = "39ea64c5-86d5-4c29-8199-5b602c90e1c5"
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

func TestRoleAssignmentResourceGroupBuilderList(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	ra := &roleAssignmentResourceGroupBuilder{
		conn: &connTest,
	}
	_, _, _, err = ra.List(ctxTest, &v2.ResourceId{}, &pagination.Token{})
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

	for _, rl := range lstRoles {
		roleDefinitionID := fmt.Sprintf(
			"/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s",
			subscriptionIDForTesting,
			rl,
		)
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
		subscriptionIDForTesting,
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

func parseRoleAssignmentEntitlementID(id string) (*v2.ResourceId, []string, error) {
	parts := strings.Split(id, ":")
	// Need to be at least 3 parts type:entitlement_id:slug
	if len(parts) < 4 || len(parts) > 5 {
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

func getRoleAssignmentResourceGroupForTesting(ctxTest context.Context, subscriptionId, roleId, resourceGroupName, description string) (*v2.Resource, error) {
	strRoleId := fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", subscriptionId, roleId)
	return roleAssignmentResourceGroupResource(ctxTest,
		subscriptionId,
		roleId,
		&armresources.ResourceGroup{
			ID:   &strRoleId,
			Name: &resourceGroupName,
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

	// ________________________________________________________________
	// | resource-name | resource-id | subscription-id | entitlement |
	// ----------------------------------------------------------------
	// role:11102f94-c441-49e6-a78b-ef80e0188abc:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned
	grantEntitlement := "role:11102f94-c441-49e6-a78b-ef80e0188abc:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned"
	grantPrincipalType := "user"
	grantPrincipal := grantPrincipalForTestingV2
	_, grantEntitlementIDs, err := parseEntitlementID(grantEntitlement)
	require.Nil(t, err)
	require.NotNil(t, grantEntitlementIDs)

	roleEntitlement = grantEntitlementIDs[3]
	resource, err := getRoleForTesting(ctxTest,
		grantEntitlementIDs[2],
		grantEntitlementIDs[1],
		"AcrDelete",
		"testing role",
	)
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

	// ________________________________________________________________________________________________
	// | resource-name | resource-id | subscription-id | entitlement | principal-type | principal-id |
	// ------------------------------------------------------------------------------------------------
	// role:11102f94-c441-49e6-a78b-ef80e0188abc:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned:user:e7f6b650-1cd5-4859-a258-1de497c29de3
	revokeGrant := "role:11102f94-c441-49e6-a78b-ef80e0188abc:39ea64c5-86d5-4c29-8199-5b602c90e1c5:assigned:user:e7f6b650-1cd5-4859-a258-1de497c29de3"
	revokeGrantIDs := strings.Split(revokeGrant, ":")
	principalID := &v2.ResourceId{ResourceType: userResourceType.Id, Resource: revokeGrantIDs[5]}
	resource, err := getRoleForTesting(ctxTest,
		revokeGrantIDs[2],
		revokeGrantIDs[1],
		"AcrDelete",
		"testing role",
	)
	require.Nil(t, err)

	gr := grant.NewGrant(resource, typeAssigned, principalID)
	annos := annotations.Annotations(gr.Annotations)
	gr.Annotations = annos
	require.NotNil(t, gr)

	l := &roleBuilder{
		conn: &connTest,
	}
	_, err = l.Revoke(ctxTest, gr)
	require.Nil(t, err)
}

func TestRoleAssignmentResourceGroupGrant(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// ---------------------------------------------------------------------------
	// resource-name | resourceGroup-id | subscription-id | role-id | roleEntitlement |
	// ---------------------------------------------------------------------------
	// resource_group_role_assignment:test_2_resource_group:39ea64c5-86d5-4c29-8199-5b602c90e1c5:11102f94-c441-49e6-a78b-ef80e0188abc:assigned
	grantEntitlement := "resource_group_role_assignment:test_2_resource_group:39ea64c5-86d5-4c29-8199-5b602c90e1c5:11102f94-c441-49e6-a78b-ef80e0188abc:assigned"
	grantPrincipalType := "user"
	grantPrincipal := "e4e9c5ae-2937-408b-ba3c-0f58cf417f0a"
	_, grantEntitlementIDs, err := parseRoleAssignmentEntitlementID(grantEntitlement)
	require.Nil(t, err)
	require.NotNil(t, grantEntitlementIDs)

	roleEntitlement := grantEntitlementIDs[4]
	resource, err := getRoleAssignmentResourceGroupForTesting(ctxTest,
		grantEntitlementIDs[2],
		grantEntitlementIDs[3],
		grantEntitlementIDs[1],
		"testing role",
	)
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

func TestRoleAssignmentResourceGroupRevoke(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	// -----------------------------------------------------------------------------------------------------------
	// resource-name | resourceGroup-id | subscription-id | role-id | roleEntitlement | principal-type | principal-id
	// -----------------------------------------------------------------------------------------------------------
	// resource_group_role_assignment:test_2_resource_group:39ea64c5-86d5-4c29-8199-5b602c90e1c5:11102f94-c441-49e6-a78b-ef80e0188abc:assigned:user:e4e9c5ae-2937-408b-ba3c-0f58cf417f0a
	revokeGrant := "resource_group_role_assignment:test_2_resource_group:39ea64c5-86d5-4c29-8199-5b602c90e1c5:11102f94-c441-49e6-a78b-ef80e0188abc:assigned:user:e4e9c5ae-2937-408b-ba3c-0f58cf417f0a"
	revokeGrantIDs := strings.Split(revokeGrant, ":")
	principalID := &v2.ResourceId{ResourceType: userResourceType.Id, Resource: revokeGrantIDs[6]}
	resource, err := getRoleAssignmentResourceGroupForTesting(ctxTest,
		revokeGrantIDs[2],
		revokeGrantIDs[3],
		revokeGrantIDs[1],
		"testing role",
	)
	require.Nil(t, err)

	gr := grant.NewGrant(resource, typeAssigned, principalID)
	annos := annotations.Annotations(gr.Annotations)
	gr.Annotations = annos
	require.NotNil(t, gr)

	l := &roleAssignmentResourceGroupBuilder{
		conn: &connTest,
	}
	_, err = l.Revoke(ctxTest, gr)
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

func TestGetPrincipalType(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	// Authenticate with Microsoft Graph
	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	principalID := grantPrincipalForTesting
	_, err = getPrincipalType(ctxTest, &connTest, principalID)
	require.Nil(t, err)
}

func TestListAllRoles(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	_, err = getAllRoles(ctxTest, &connTest, "")
	require.Nil(t, err)
}

func TestEnterpriseApplicationsGrants(t *testing.T) {
	if azureTenantId == "" && azureClientSecret == "" && azureClientId == "" {
		t.Skip()
	}

	connTest, err := getConnectorForTesting(ctxTest, azureTenantId, azureClientSecret, azureClientId)
	require.Nil(t, err)

	s := &enterpriseApplicationsBuilder{
		conn: &connTest,
	}
	reqURL := connTest.buildURL("servicePrincipals", setEnterpriseApplicationsKeys())
	resp := &servicePrincipalsList{}
	err = connTest.query(ctxTest, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	require.Nil(t, err)

	entApps, err := slices.ConvertErr(resp.Value, func(app *servicePrincipal) (*v2.Resource, error) {
		return enterpriseApplicationResource(ctxTest, app, nil)
	})
	require.Nil(t, err)

	for _, res := range entApps {
		_, _, _, err = s.Grants(ctxTest, res, &pagination.Token{})
		require.Nil(t, err)
	}
}
