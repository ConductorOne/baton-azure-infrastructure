package connector

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"

	"github.com/conductorone/baton-sdk/pkg/types/grant"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
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
//
// idx is the optional tenant-hierarchy lookup (from (*Connector).hierarchy).
// When present, supplies the parentResourceId — the containing management
// group — so the sparse-ACL tree view renders tenant → mgmt-group → sub as
// one coherent tree rather than disconnected roots. When empty, the sub is
// emitted without a parent.
func subscriptionResource(ctx context.Context, s *armsubscription.Subscription, idx hierarchyIndex) (*v2.Resource, error) {
	var appTraitOpts []rs.AppTraitOption
	profile := map[string]interface{}{
		"subscriptionId": StringValue(s.SubscriptionID),
		"displayName":    StringValue(s.DisplayName),
		"state":          StringValue((*string)(s.State)),
	}

	appTraitOpts = append(appTraitOpts, rs.WithAppProfile(profile))

	opts := []rs.ResourceOption{
		rs.WithAnnotation(&v2.V1Identifier{
			Id: StringValue(s.SubscriptionID),
		}),
		rs.WithAnnotation(
			&v2.ChildResourceType{ResourceTypeId: resourceGroupResourceType.Id},
			&v2.ChildResourceType{ResourceTypeId: roleResourceType.Id},
			&v2.ChildResourceType{ResourceTypeId: storageAccountResourceType.Id},
		),
	}
	if parent := idx[StringValue(s.SubscriptionID)]; parent != nil {
		opts = append(opts, rs.WithParentResourceID(parent))
	}

	return rs.NewAppResource(
		StringValue(s.DisplayName),
		subscriptionsResourceType,
		StringValue(s.SubscriptionID),
		appTraitOpts,
		opts...,
	)
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
// resourceGroupResource emits a c1 resource for an Azure resource_group.
//
// BREAKING (from older azure-infra shape): the c1 resource ID used to be the
// bare `rg.Name`, which silently collapsed RGs that shared a name across
// subscriptions. Azure RG names are only unique within a subscription; two
// subs can each have an `rg-apps-web-prd`, and both are distinct resources.
// The old ID scheme lost that distinction — one RG row in c1z for both real
// Azure resources — producing undercounted inventory and ambiguous scope
// references on role_assignments.
//
// New format: "<subscriptionID>:<rgName>". Globally unique, matches the
// colon-composite convention storage_account already uses. Callers that
// parse this ID (armScopeFromBindingRef for Grant/Revoke, subscription
// recovery for scope reconstruction) split on the first ":".
//
// parentResourceID must carry the subscription — we read subscriptionID
// from it rather than requiring a separate parameter.
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

	subscriptionID := ""
	if parentResourceID != nil {
		subscriptionID = parentResourceID.Resource
	}
	rgID := StringValue(rg.Name)
	if subscriptionID != "" {
		rgID = subscriptionID + ":" + StringValue(rg.Name)
	}

	opts = append(opts, rs.WithGroupTrait(groupListTraitOptions...), rs.WithParentResourceID(parentResourceID))
	resource, err := rs.NewResource(
		StringValue(rg.Name),
		resourceGroupResourceType,
		rgID,
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

// getRoleId returns the bare role definition UUID for use as a c1 resource
// ID. The input is an ARM role definition path like
// "/subscriptions/<sub>/providers/Microsoft.Authorization/roleDefinitions/<uuid>"
// or "/providers/Microsoft.Authorization/roleDefinitions/<uuid>" for tenant-
// root roles.
//
// BREAKING (post-PR-83): previously returned "<uuid>:<subscriptionID>" to
// carry scope context in the resource identity. Dropped in favor of tenant-
// global bare UUID since roles are tenant-global in Azure and scope lives
// on role_assignment resources via ScopeBindingTrait. Callers that were
// parsing the trailing ":<sub>" to recover the subscription must instead
// consult the role_assignment's ScopeBindingTrait.scope_resource_id.
func getRoleId(roleID *string) string {
	s := StringValue(roleID)
	if s == "" {
		return ""
	}
	if idx := strings.LastIndex(s, "/"); idx >= 0 && idx < len(s)-1 {
		return s[idx+1:]
	}
	return s
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

		// Try each endpoint in turn. A 404/403 on one endpoint is common (guest
		// users and cross-tenant SPs miss directoryObjects) — fall through to
		// the next endpoint instead of aborting the whole lookup.
		if err := cn.client.FromPath(ctx, builderUrl, &principalData); err != nil {
			continue
		}

		if principalType, ok := principalData["@odata.type"].(string); ok {
			switch principalType {
			// Service Principal can be an Enterprise Application or Managed Identity.
			case "#microsoft.graph.servicePrincipal":
				if servicePrincipalType, ok := principalData["servicePrincipalType"].(string); ok {
					return servicePrincipalType, nil
				}
				// Defensive fallback when Graph returns a servicePrincipal
				// object without the servicePrincipalType field. Default to
				// Application so mapGraphPrincipalTypeToBaton routes it to
				// enterprise_application rather than dropping the grant.
				// The caller's negative cache (role_assignment.go) would
				// otherwise permanently skip this principal for the rest of
				// the sync — silent data loss.
				return spTypeApplication, nil
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
	roleDefinitionsClient, err := armauthorization.NewRoleDefinitionsClient(
		conn.token,
		conn.client.ArmOptions(),
	)
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
		// Entra/AD groups (Graph directory groups) are routed to
		// groupResourceType — NOT resourceGroupResourceType (which is the
		// Azure ARM "resource group" concept, a completely different thing
		// that uses bare RG names like "rg-apps-web-prd" as its id). The
		// earlier mapping produced ~900 dangling principal references per
		// lab sync because Entra group GUIDs never resolve as Azure RG
		// names; found during cross-link validation on this PR.
		principalId = &v2.ResourceId{
			ResourceType: groupResourceType.Id,
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
			resourceGroupsClient, err := armresources.NewResourceGroupsClient(
				*subscription.SubscriptionID,
				conn.token,
				conn.client.ArmOptions(),
			)
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
	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, conn.token, conn.client.ArmOptions())
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

func storageAccountResource(ctx context.Context, account *armstorage.Account, parent *v2.ResourceId, skipContainersSync bool) (*v2.Resource, error) {
	l := ctxzap.Extract(ctx)

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

	opts := []rs.ResourceOption{
		rs.WithAppTrait(appTraits...),
		rs.WithParentResourceID(parent),
	}

	// https://github.com/Azure/PSRule.Rules.Azure/pull/467/commits/56e6a72ff636a5f766658085dd529fed93e94073
	if !skipContainersSync && account.Kind != nil &&
		*account.Kind != armstorage.KindFileStorage {
		childAnnotation := rs.WithAnnotation(
			&v2.ChildResourceType{ResourceTypeId: containerResourceType.Id},
		)

		opts = append(opts, childAnnotation)
	} else {
		l.Debug("skipping child resource type for file storage account", zap.String("account", StringValue(account.Name)))
	}

	return rs.NewResource(
		StringValue(account.Name),
		storageAccountResourceType,
		idData.ConnectorId(),
		opts...,
	)
}

// grantFromRole and grantFromRoleAssigment were removed as part of the
// sparse-ACL completion (PR #83). They emitted grants on action entitlements
// of storage_account / container / resource_group with a role resource as
// principal and a GrantExpandable annotation pointing at per-role Owner /
// Member entitlements. Post-sparse-ACL those per-role entitlements no longer
// exist and the expansion has nothing to expand from — the grants were dead
// projections of information already carried by role_assignment resources
// with ScopeBindingTrait. See role_assignment.go for the authoritative path.

func grantFromEligibleAssignment(ctx context.Context, resource *v2.Resource, assigment client.PMIRoleAssigment) (*v2.Grant, error) {
	l := ctxzap.Extract(ctx)

	grantOpts := []grant.GrantOption{
		grant.WithAnnotation(&v2.GrantImmutable{}),
		grant.WithGrantMetadata(map[string]interface{}{
			"displayName": assigment.RoleDefinition.DisplayName,
		}),
	}

	var id *v2.ResourceId
	switch assigment.Subject.Type {
	case "User":
		id = &v2.ResourceId{
			Resource:     assigment.Subject.Id,
			ResourceType: userResourceType.Id,
		}
	case "Group":
		id = &v2.ResourceId{
			Resource:     assigment.Subject.Id,
			ResourceType: groupResourceType.Id,
		}

		grantOpts = append(grantOpts, grant.WithAnnotation(&v2.GrantExpandable{
			EntitlementIds: []string{
				fmt.Sprintf("group:%s:owners", assigment.Subject.Id),
				fmt.Sprintf("group:%s:members", assigment.Subject.Id),
			},
		}))
	default:
		l.Error("unsupported principal type", zap.String("principalType", assigment.Subject.Type))
		return nil, nil
	}

	return grant.NewGrant(
		resource,
		typeEligible,
		id,
		grantOpts...,
	), nil
}
