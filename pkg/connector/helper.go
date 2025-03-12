package connector

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/conductorone/baton-sdk/pkg/types/grant"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	expSlices "golang.org/x/exp/slices"
)

// https://learn.microsoft.com/en-us/graph/api/resources/approleassignment?view=graph-rest-1.0
//
//	 	The identifier (id) for the app role which is assigned to the principal. This app role must be
//			exposed in the appRoles property on the resource application's service principal (resourceId).
//			If the resource application has not declared any app roles, a default app role ID of
//			00000000-0000-0000-0000-000000000000 can be specified to signal that the principal is assigned
//			to the resource app without any specific app roles. Required on create
var defaultAppRoleAssignmentID string = "00000000-0000-0000-0000-000000000000"

const (
	managerIDProfileKey          = "managerId"
	managerEmailProfileKey       = "managerEmail"
	supervisorIDProfileKey       = "supervisorEId"
	supervisorEmailProfileKey    = "supervisorEmail"
	supervisorFullNameProfileKey = "supervisor"
)

// Create a new connector resource for an Entra User.
func userResource(ctx context.Context, u *client.User, parentResourceID *v2.ResourceId, userTraitOptions ...rs.UserTraitOption) (*v2.Resource, error) {
	primaryEmail := fetchEmailAddresses(u.Email, u.UserPrincipalName)
	profile := map[string]interface{}{
		"id":                u.ID,
		"email":             primaryEmail,
		"displayName":       u.DisplayName,
		"title":             u.JobTitle,
		"jobTitle":          u.JobTitle,
		"userPrincipalName": u.UserPrincipalName,
		"accountEnabled":    u.AccountEnabled,
		"employeeId":        u.EmployeeID,
		// TODO: why are we setting employeeId twice?
		"employeeNumber": u.EmployeeID,
		"department":     u.Department,
	}

	if u.Manager != nil {
		profile[managerIDProfileKey] = u.Manager.Id
		profile[managerEmailProfileKey] = u.Manager.Email
		profile[supervisorIDProfileKey] = u.Manager.EmployeeId
		profile[supervisorEmailProfileKey] = u.Manager.Email
		profile[supervisorFullNameProfileKey] = u.Manager.DisplayName
	}

	options := []rs.UserTraitOption{
		rs.WithEmail(primaryEmail, true),
		rs.WithUserProfile(profile),
	}

	options = append(options, userTraitOptions...)
	if !IsEmpty(u.UserPrincipalName) {
		options = append(options, rs.WithUserLogin(u.UserPrincipalName))
	}

	if u.AccountEnabled {
		options = append(options, rs.WithStatus(v2.UserTrait_Status_STATUS_ENABLED))
	} else {
		options = append(options, rs.WithStatus(v2.UserTrait_Status_STATUS_DISABLED))
	}

	ret, err := rs.NewUserResource(
		u.DisplayName,
		userResourceType,
		u.ID,
		options,
		rs.WithParentResourceID(parentResourceID),
		rs.WithAnnotation(&v2.ExternalLink{
			Url: userURL(u),
		}),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func userURL(u *client.User) string {
	return (&url.URL{
		Scheme:   "https",
		Host:     "entra.microsoft.com",
		Path:     "/",
		Fragment: path.Join("view/Microsoft_AAD_UsersAndTenants/UserProfileMenuBlade/~/overview/userId", u.ID),
	}).String()
}

func parsePageToken(i string, resourceID *v2.ResourceId) (*pagination.Bag, error) {
	b := &pagination.Bag{}
	err := b.Unmarshal(i)
	if err != nil {
		return nil, err
	}

	if b.Current() == nil {
		b.Push(pagination.PageState{
			ResourceTypeID: resourceID.ResourceType,
			ResourceID:     resourceID.Resource,
		})
	}

	return b, nil
}

func fetchEmailAddresses(email string, upn string) string {
	var upnEmail string
	primaryEmail := email
	addr, err := mail.ParseAddress(upn)
	if err == nil {
		upnEmail = addr.Address
	}

	if IsEmpty(primaryEmail) && !IsEmpty(upnEmail) {
		primaryEmail = upnEmail
	}

	return primaryEmail
}

func groupResource(ctx context.Context, g *client.Group, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"object_id":           g.ID,
		"group_type":          groupTypeValue(g),
		"membership_type":     membershipTypeValue(g),
		"mail_enabled":        g.MailEnabled,
		"security_enabled":    g.SecurityEnabled,
		"security_identifier": g.SecurityIdentifier,
	}

	if !IsEmpty(g.Mail) {
		profile["mail"] = g.Mail
	}

	if !IsEmpty(g.Classification) {
		profile["classification"] = g.Classification
	}

	if g.OnPremisesSecurityIdentifier != nil {
		profile["on_premises_security_identifier"] = *g.OnPremisesSecurityIdentifier
	}

	if g.OnPremisesSyncEnabled {
		profile["on_premises_sync_enabled"] = g.OnPremisesSyncEnabled
	}

	groupTraitOptions := []rs.GroupTraitOption{rs.WithGroupProfile(profile)}
	rv, err := rs.NewGroupResource(
		g.DisplayName,
		groupResourceType,
		g.ID,
		groupTraitOptions,
		rs.WithAnnotation(&v2.ExternalLink{
			Url: groupURL(g),
		}),
	)
	if err != nil {
		return nil, err
	}

	return rv, nil
}

func IsEmpty(field string) bool {
	return field == ""
}

func groupURL(g *client.Group) string {
	return (&url.URL{
		Scheme:   "https",
		Host:     "entra.microsoft.com",
		Path:     "/",
		Fragment: path.Join("view/Microsoft_AAD_IAM/GroupDetailsMenuBlade/~/Overview/groupId/", g.ID),
	}).String()
}

func groupTypeValue(g *client.Group) string {
	if slices.Contains(g.GroupTypes, "Unified") {
		return "microsoft_365"
	}

	if g.MailEnabled && g.SecurityEnabled {
		return "mail_enabled_security"
	}

	if g.SecurityEnabled {
		return "security"
	}

	if g.MailEnabled {
		return "distribution"
	}

	return ""
}

func membershipTypeValue(g *client.Group) string {
	if slices.Contains(g.GroupTypes, "DynamicMembership") {
		return "dynamic"
	}

	return "assigned"
}

func fmtResourceGrant(resourceID *v2.ResourceId, principalId *v2.ResourceId, permission string) string {
	return fmt.Sprintf(
		"%s-grant:%s:%s:%s:%s",
		resourceID.ResourceType,
		resourceID.Resource,
		principalId.ResourceType,
		principalId.Resource,
		permission,
	)
}

func getGroupGrants(ctx context.Context, resp *client.MembershipList, resource *v2.Resource, ps *pagination.PageState) ([]*v2.Grant, error) {
	grants, err := ConvertErr(resp.Members, func(gm *client.Membership) (*v2.Grant, error) {
		var annos annotations.Annotations
		objectID := resource.Id.GetResource()
		rid := &v2.ResourceId{Resource: gm.Id}
		switch gm.Type {
		case odataTypeGroup:
			rid.ResourceType = groupResourceType.Id
			annos.Update(&v2.GrantExpandable{
				EntitlementIds: []string{
					fmt.Sprintf("group:%s:members", rid.Resource),
				},
			})
		case odataTypeUser:
			rid.ResourceType = userResourceType.Id
		case odataTypeServicePrincipal:
			switch gm.ServicePrincipalType {
			case spTypeApplication:
				rid.ResourceType = enterpriseApplicationResourceType.Id
			case spTypeManagedIdentity:
				rid.ResourceType = managedIdentitylResourceType.Id
			case spTypeLegacy, spTypeSocialIdp, "":
				// https://learn.microsoft.com/en-us/graph/api/resources/serviceprincipal?view=graph-rest-1.0
				fallthrough
			default:
				return nil, nil
			}
		default:
			return nil, nil
		}
		ur := &v2.Resource{Id: rid}
		return &v2.Grant{
			Id: fmtResourceGrant(resource.Id, ur.Id, objectID+":"+ps.ResourceTypeID),
			Entitlement: &v2.Entitlement{
				Id:       fmt.Sprintf("group:%s:%s", resource.Id.Resource, ps.ResourceTypeID),
				Resource: resource,
			},
			Principal:   ur,
			Annotations: annos,
		}, nil
	})

	return grants, err
}

func getGroupGrantURL(principal *v2.Resource) string {
	return (&url.URL{
		Scheme: "https",
		Host:   "graph.microsoft.com",
		Path:   path.Join("v1.0", "directoryObjects", principal.Id.Resource),
	}).String()
}

// https://learn.microsoft.com/es-es/rest/api/subscription/subscriptions/list?view=rest-subscription-2021-10-01&tabs=HTTP
func subscriptionResource(ctx context.Context, s *armsubscription.Subscription) (*v2.Resource, error) {
	var appTraitOpts []rs.AppTraitOption
	profile := map[string]interface{}{
		"subscriptionId": StringValue(s.SubscriptionID),
		"displayName":    StringValue(s.DisplayName),
		"state":          StringValue((*string)(s.State)),
	}

	appTraitOpts = append(appTraitOpts, rs.WithAppProfile(profile))
	return rs.NewAppResource(
		StringValue(s.DisplayName),
		subscriptionsResourceType,
		StringValue(s.SubscriptionID),
		appTraitOpts,
		rs.WithAnnotation(&v2.V1Identifier{
			Id: StringValue(s.SubscriptionID),
		}),
		rs.WithAnnotation(
			&v2.ChildResourceType{ResourceTypeId: resourceGroupResourceType.Id},
			&v2.ChildResourceType{ResourceTypeId: roleResourceType.Id},
			&v2.ChildResourceType{ResourceTypeId: storageAccountResourceType.Id},
		))
}

// https://learn.microsoft.com/es-es/rest/api/subscription/tenants/list?view=rest-subscription-2021-10-01&tabs=HTTP
func tenantResource(ctx context.Context, t *armsubscription.TenantIDDescription) (*v2.Resource, error) {
	var opts []rs.ResourceOption
	profile := map[string]interface{}{
		"tenantId":       StringValue(t.ID),
		"tenantCategory": StringValue(t.TenantID),
	}

	tenantTraitOptions := []rs.AppTraitOption{
		rs.WithAppProfile(profile),
	}

	opts = append(opts, rs.WithAppTrait(tenantTraitOptions...))
	resource, err := rs.NewResource(
		StringValue(t.TenantID),
		tenantResourceType,
		StringValue(t.TenantID),
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func getResourceGroupID(name, subscriptionID, roleID string) string {
	return name + ":" + subscriptionID + ":" + roleID
}

// https://learn.microsoft.com/es-es/rest/api/resources/resource-groups/list?view=rest-resources-2021-04-01
func resourceGroupResource(ctx context.Context, rg *armresources.ResourceGroup, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	var opts []rs.ResourceOption
	profile := map[string]interface{}{
		"id":       StringValue(rg.ID),
		"name":     StringValue(rg.Name),
		"type":     StringValue(rg.Type),
		"location": StringValue(rg.Location),
	}

	groupListTraitOptions := []rs.GroupTraitOption{
		rs.WithGroupProfile(profile),
	}

	opts = append(opts, rs.WithGroupTrait(groupListTraitOptions...), rs.WithParentResourceID(parentResourceID))
	resource, err := rs.NewResource(
		StringValue(rg.Name),
		resourceGroupResourceType,
		StringValue(rg.Name),
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func roleAssignmentResourceGroupResource(ctx context.Context, subscriptionID, roleID string, rg *armresources.ResourceGroup, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	var opts []rs.ResourceOption
	profile := map[string]interface{}{
		"id":       StringValue(rg.ID),
		"name":     StringValue(rg.Name),
		"type":     StringValue(rg.Type),
		"location": StringValue(rg.Location),
	}

	groupListTraitOptions := []rs.GroupTraitOption{
		rs.WithGroupProfile(profile),
	}

	opts = append(opts, rs.WithGroupTrait(groupListTraitOptions...), rs.WithParentResourceID(parentResourceID))
	resource, err := rs.NewResource(
		StringValue(rg.Name),
		roleAssignmentResourceGroupType,
		getResourceGroupID(
			StringValue(rg.Name),
			subscriptionID,
			roleID,
		),
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

// StringValue returns the value of the string pointer passed in or
// "" if the pointer is nil.
func StringValue(v *string) string {
	if v != nil {
		return *v
	}

	return ""
}

// BoolValue returns the value of the bool pointer passed in or
// false if the pointer is nil.
func BoolValue(v *bool) bool {
	if v != nil {
		return *v
	}

	return false
}

func roleResource(ctx context.Context, role *armauthorization.RoleDefinition, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	var (
		strRoleID string
		opts      []rs.ResourceOption
	)

	var permissionsActions []any
	var permissionsNotActions []any
	for _, permission := range role.Properties.Permissions {
		for _, action := range permission.Actions {
			permissionsActions = append(permissionsActions, *action)
		}

		for _, action := range permission.NotActions {
			permissionsNotActions = append(permissionsNotActions, *action)
		}
	}

	var assignedScopes []any
	for _, scope := range role.Properties.AssignableScopes {
		assignedScopes = append(assignedScopes, *scope)
	}

	strRoleID = getRoleId(role.ID) // roleID + subscriptionID
	profile := map[string]interface{}{
		"id":                      strRoleID,
		"name":                    StringValue(role.Properties.RoleName),
		"description":             StringValue(role.Properties.Description),
		"type":                    StringValue(role.Properties.RoleType),
		"role_definition_id":      StringValue(role.ID),
		"permissions_actions":     permissionsActions,
		"permissions_not_actions": permissionsNotActions,
		"assigned_scopes":         assignedScopes,
	}

	roleTraitOptions := []rs.RoleTraitOption{
		rs.WithRoleProfile(profile),
	}

	opts = append(opts, rs.WithRoleTrait(roleTraitOptions...), rs.WithParentResourceID(parentResourceID))
	resource, err := rs.NewRoleResource(
		StringValue(role.Properties.RoleName),
		roleResourceType,
		strRoleID,
		roleTraitOptions,
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func getRoleId(roleID *string) string {
	if strings.Contains(StringValue(roleID), "/") {
		arr := strings.Split(StringValue(roleID), "/")
		if len(arr) > 0 {
			return arr[len(arr)-1] + ":" + arr[2] // roleID + subscriptionID
		}
	}

	return ""
}

func getPrincipalType(ctx context.Context, cn *Connector, principalID string) (string, error) {
	var (
		principalData map[string]interface{}
		mapEndPoint   = []string{"directoryObjects", "users", "groups", "servicePrincipals"}
	)

	for _, endpoint := range mapEndPoint {
		builderUrl := cn.client.QueryBuilder().
			Version(client.V1).
			BuildUrl(endpoint, principalID)

		err := cn.client.FromPath(ctx, builderUrl, &principalData)
		if err != nil {
			return "", err
		}

		if principalType, ok := principalData["@odata.type"].(string); ok {
			switch principalType {
			// Service Principal can be an Enterprise Application or Managed Identity.
			case "#microsoft.graph.servicePrincipal":
				if servicePrincipalType, ok := principalData["servicePrincipalType"].(string); ok {
					return servicePrincipalType, nil
				}
			default:
				return principalType, nil
			}
		}
	}

	return "", nil
}

func managedIdentityResource(ctx context.Context, sp *client.ServicePrincipal, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := make(map[string]interface{})
	profile["id"] = sp.ID
	profile["app_id"] = sp.AppId
	options := []rs.UserTraitOption{
		rs.WithUserProfile(profile),
		rs.WithAccountType(v2.UserTrait_ACCOUNT_TYPE_SERVICE),
	}

	if !IsEmpty(sp.Info.LogoUrl) {
		options = append(options, rs.WithUserIcon(&v2.AssetRef{
			Id: sp.Info.LogoUrl,
		}))
	}

	if sp.AccountEnabled {
		options = append(options, rs.WithStatus(v2.UserTrait_Status_STATUS_ENABLED))
	} else {
		options = append(options, rs.WithStatus(v2.UserTrait_Status_STATUS_DISABLED))
	}
	ret, err := rs.NewUserResource(
		sp.GetDisplayName(),
		managedIdentitylResourceType,
		sp.ID,
		options,
		rs.WithParentResourceID(parentResourceID),
		rs.WithAnnotation(&v2.ExternalLink{
			Url: sp.ExternalURL(),
		}),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func enterpriseApplicationResource(ctx context.Context, app *client.ServicePrincipal, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := make(map[string]interface{})
	profile["id"] = app.ID
	profile["app_id"] = app.AppId
	if expSlices.Contains(app.Tags, "WindowsAzureActiveDirectoryIntegratedApp") {
		profile["is_integrated"] = true
	}

	if expSlices.Contains(app.Tags, "HideApp") {
		profile["hidden_app"] = true
	}

	options := []rs.AppTraitOption{
		rs.WithAppProfile(profile),
	}
	if !IsEmpty(app.Info.LogoUrl) {
		options = append(options, rs.WithAppLogo(&v2.AssetRef{
			Id: app.Info.LogoUrl,
		}))
	}

	if !IsEmpty(app.Homepage) {
		options = append(options, rs.WithAppHelpURL(app.Homepage))
	}

	// NOTE: use in case you want to mark the azure owned apps as hidden
	// if app.AppOwnerOrganizationId == microsoftBuiltinAppsOwnerID {
	// 	options = append(options, rs.WithAppFlags(v2.AppTrait_APP_FLAG_HIDDEN))
	// }

	ret, err := rs.NewAppResource(
		app.GetDisplayName(),
		enterpriseApplicationResourceType,
		app.ID,
		options,
		rs.WithParentResourceID(parentResourceID),
		rs.WithAnnotation(&v2.ExternalLink{
			Url: app.ExternalURL(),
		}),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func getAllRoles(ctx context.Context, conn *Connector, subscriptionID string) ([]string, error) {
	lstRoles := []string{}
	// Initialize the RoleDefinitionsClient
	roleDefinitionsClient, err := armauthorization.NewRoleDefinitionsClient(conn.token, nil)
	if err != nil {
		return nil, err
	}

	scope := fmt.Sprintf("/subscriptions/%s", subscriptionID)
	// Get the list of role definitions
	pagerRoles := roleDefinitionsClient.NewListPager(scope, nil)
	for pagerRoles.More() {
		resp, err := pagerRoles.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		// Iterate over role definitions
		for _, role := range resp.Value {
			lstRoles = append(lstRoles, *role.Name)
		}
	}

	return lstRoles, nil
}

func getPrincipalIDResource(principalType string, assignment *armauthorization.RoleAssignment) *v2.ResourceId {
	var principalId *v2.ResourceId
	switch principalType {
	case "#microsoft.graph.user":
		principalId = &v2.ResourceId{
			ResourceType: userResourceType.Id,
			Resource:     *assignment.Properties.PrincipalID,
		}
	case "#microsoft.graph.group":
		principalId = &v2.ResourceId{
			ResourceType: resourceGroupResourceType.Id,
			Resource:     *assignment.Properties.PrincipalID,
		}
	case "Application":
		principalId = &v2.ResourceId{
			ResourceType: enterpriseApplicationResourceType.Id,
			Resource:     *assignment.Properties.PrincipalID,
		}
	case "ManagedIdentity":
		principalId = &v2.ResourceId{
			ResourceType: managedIdentitylResourceType.Id,
			Resource:     *assignment.Properties.PrincipalID,
		}
	}
	return principalId
}

func getResourceGroups(ctx context.Context, conn *Connector) ([]string, error) {
	lstResourceGroups := []string{}
	pagerSubscriptions := conn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pagerSubscriptions.More() {
		page, err := pagerSubscriptions.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, subscription := range page.Value {
			resourceGroupsClient, err := armresources.NewResourceGroupsClient(*subscription.SubscriptionID, conn.token, nil)
			if err != nil {
				return nil, err
			}

			for pager := resourceGroupsClient.NewListPager(nil); pager.More(); {
				page, err := pager.NextPage(ctx)
				if err != nil {
					return nil, err
				}

				for _, groupList := range page.Value {
					lstResourceGroups = append(lstResourceGroups, *groupList.Name)
				}
			}
		}
	}

	return lstResourceGroups, nil
}

func getAssignmentID(ctx context.Context, conn *Connector, scope, subscriptionID, roleId, principalID string) (string, error) {
	// Create a Role Assignments Client
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, conn.token, nil)
	if err != nil {
		return "", err
	}

	pagerResourceGroup := roleAssignmentsClient.NewListForScopePager(scope, nil)
	// Iterate through the role assignments
	for pagerResourceGroup.More() {
		page, err := pagerResourceGroup.NextPage(ctx)
		if err != nil {
			return "", err
		}

		for _, assignment := range page.Value {
			roleDefinitionID := subscriptionRoleId(subscriptionID, roleId)
			if *assignment.Properties.PrincipalID == principalID &&
				*assignment.Properties.RoleDefinitionID == roleDefinitionID {
				return *assignment.Name, nil
			}
		}
	}

	return "", fmt.Errorf("role assignment not found")
}

func subscriptionRoleId(subscriptionID, roleID string) string {
	return fmt.Sprintf(
		"/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s",
		subscriptionID,
		roleID,
	)
}

type storageResourceSplitIdData struct {
	subscriptionID            string
	resourceGroupName         string
	resourceProviderNamespace string
	resourceType              string
	resourceName              string
}

func newStorageResourceSplitIdDataFromConnectorId(connectorId string) (*storageResourceSplitIdData, error) {
	splitValue := strings.Split(connectorId, ":")

	if len(splitValue) != 5 {
		return nil, fmt.Errorf("invalid storage resource split id")
	}

	return &storageResourceSplitIdData{
		subscriptionID:            splitValue[0],
		resourceGroupName:         splitValue[1],
		resourceProviderNamespace: splitValue[2],
		resourceType:              splitValue[3],
		resourceName:              splitValue[4],
	}, nil
}

func (s *storageResourceSplitIdData) ConnectorId() string {
	return fmt.Sprintf(
		"%s:%s:%s:%s:%s",
		s.subscriptionID,
		s.resourceGroupName,
		s.resourceProviderNamespace,
		s.resourceType,
		s.resourceName,
	)
}

func (s *storageResourceSplitIdData) AzureId() string {
	return fmt.Sprintf(
		"/subscriptions/%s/resourceGroups/%s/providers/%s/%s/%s",
		s.subscriptionID,
		s.resourceGroupName,
		s.resourceProviderNamespace,
		s.resourceType,
		s.resourceName,
	)
}

func newStorageResourceSplitIdDataFromAzureId(id string) (*storageResourceSplitIdData, error) {
	splits := strings.Split(id, "/")
	// By docs the value should be
	// Ex - /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/{resourceProviderNamespace}/{resourceType}/{resourceName}
	if len(splits) != 9 {
		return nil, fmt.Errorf(
			"unexpected number of splits, ex: '/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/{resourceProviderNamespace}/{resourceType}/{resourceName}', got %s",
			id,
		)
	}

	return &storageResourceSplitIdData{
		subscriptionID:            splits[2],
		resourceGroupName:         splits[4],
		resourceProviderNamespace: splits[6],
		resourceType:              splits[7],
		resourceName:              splits[8],
	}, nil
}

func storageAccountResource(ctx context.Context, account *armstorage.Account, parent *v2.ResourceId) (*v2.Resource, error) {
	idData, err := newStorageResourceSplitIdDataFromAzureId(StringValue(account.ID))
	if err != nil {
		return nil, err
	}

	profile := map[string]interface{}{
		"id":                  StringValue(account.ID),
		"name":                StringValue(account.Name),
		"location":            StringValue(account.Location),
		"type":                StringValue(account.Type),
		"resource_group_name": idData.resourceGroupName,
	}

	if account.Kind != nil {
		profile["kind"] = string(*account.Kind)
	}

	if account.SKU != nil {
		if account.SKU.Name != nil {
			profile["sku_name"] = string(*account.SKU.Name)
		}

		if account.SKU.Tier != nil {
			profile["sku_tier"] = string(*account.SKU.Tier)
		}
	}

	if account.Identity != nil {
		if account.Identity.Type != nil {
			profile["identity_type"] = string(*account.Identity.Type)
		}

		if account.Identity.PrincipalID != nil {
			profile["identity_principal_id"] = StringValue(account.Identity.PrincipalID)
		}

		if account.Identity.TenantID != nil {
			profile["identity_tenant_id"] = StringValue(account.Identity.TenantID)
		}
	}

	appTraits := []rs.AppTraitOption{
		rs.WithAppProfile(profile),
	}

	return rs.NewResource(
		StringValue(account.Name),
		storageAccountResourceType,
		idData.ConnectorId(),
		rs.WithAppTrait(appTraits...),
		rs.WithParentResourceID(parent),
		rs.WithAnnotation(
			&v2.ChildResourceType{ResourceTypeId: containerResourceType.Id},
		),
	)
}

func grantFromRoleAssigment(
	resource *v2.Resource,
	entitlementName string,
	subscriptionID string,
	in *armauthorization.RoleAssignment,
) (*v2.Grant, error) {
	if in.Properties.RoleDefinitionID == nil {
		return nil, fmt.Errorf("role definition id is nil")
	}

	// RoleDefinitionID example value
	// /subscriptions/{subscriptionId}/providers/Microsoft.Authorization/roleDefinitions/{roleId}
	splitValues := strings.Split(StringValue(in.Properties.RoleDefinitionID), "/")
	if len(splitValues) != 7 {
		return nil, fmt.Errorf("invalid role definition id %s", StringValue(in.Properties.RoleDefinitionID))
	}
	roleIdFromSplit := splitValues[len(splitValues)-1]

	// roleID : subscriptionID
	roleId, err := rs.NewResourceID(
		roleResourceType,
		fmt.Sprintf("%s:%s", roleIdFromSplit, subscriptionID),
	)
	if err != nil {
		return nil, err
	}

	var grantOpts []grant.GrantOption
	// TODO: review this grant Expandable operation
	grantOpts = append(grantOpts, grant.WithAnnotation(&v2.GrantExpandable{
		EntitlementIds: []string{
			fmt.Sprintf("role:%s:owners", roleId.Resource),
			fmt.Sprintf("role:%s:assigned", roleId.Resource),
		},
		Shallow: true,
	}))

	return grant.NewGrant(
		resource,
		entitlementName,
		roleId,
		grantOpts...,
	), nil
}
