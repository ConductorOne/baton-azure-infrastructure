package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"path"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	armresources "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	armsubscription "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	"github.com/conductorone/baton-azure-infrastructure/pkg/internal/slices"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	pagination "github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	zap "go.uber.org/zap"
	expSlices "golang.org/x/exp/slices"
)

const (
	managerIDProfileKey          = "managerId"
	employeeNumberProfileKey     = "employeeNumber"
	managerEmailProfileKey       = "managerEmail"
	supervisorIDProfileKey       = "supervisorEId"
	supervisorEmailProfileKey    = "supervisorEmail"
	supervisorFullNameProfileKey = "supervisor"
)

var graphReadScopes = []string{
	"https://graph.microsoft.com/.default",
}

// Create a new connector resource for an Entra User.
func userResource(ctx context.Context, u *user, parentResourceID *v2.ResourceId, userTraitOptions ...rs.UserTraitOption) (*v2.Resource, error) {
	primaryEmail := fetchEmailAddresses(u.Email, u.UserPrincipalName)
	profile := make(map[string]interface{})
	profile["id"] = u.ID
	profile["mail"] = primaryEmail
	profile["displayName"] = u.DisplayName
	profile["title"] = u.JobTitle
	profile["jobTitle"] = u.JobTitle
	profile["userPrincipalName"] = u.UserPrincipalName
	profile["accountEnabled"] = u.AccountEnabled
	profile["employeeId"] = u.EmployeeID
	profile[employeeNumberProfileKey] = u.EmployeeID
	profile["department"] = u.Department
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
	if u.UserPrincipalName != "" {
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

func userURL(u *user) string {
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

	if primaryEmail == "" && upnEmail != "" {
		primaryEmail = upnEmail
	}

	return primaryEmail
}

func setUserKeys() url.Values {
	v := url.Values{}
	v.Set("$select", strings.Join([]string{
		"id",
		"displayName",
		"mail",
		"userPrincipalName",
		"jobTitle",
		"manager",
		"accountEnabled",
		"employeeType",
		"employeeHireDate",
		"employeeId",
		"department",
	}, ","))
	v.Set("$expand", "manager($select=id,employeeId,mail,displayName)")
	v.Set("$top", "999")
	return v
}

func setUserResponseKeys() url.Values {
	v := url.Values{}
	v.Set("$select", strings.Join([]string{
		"userPurpose",
	}, ","))
	return v
}

func groupResource(ctx context.Context, g *group, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
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

func groupURL(g *group) string {
	return (&url.URL{
		Scheme:   "https",
		Host:     "entra.microsoft.com",
		Path:     "/",
		Fragment: path.Join("view/Microsoft_AAD_IAM/GroupDetailsMenuBlade/~/Overview/groupId/", g.ID),
	}).String()
}

func groupTypeValue(g *group) string {
	if expSlices.Contains(g.GroupTypes, "Unified") {
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

func membershipTypeValue(g *group) string {
	if expSlices.Contains(g.GroupTypes, "DynamicMembership") {
		return "dynamic"
	}

	return "assigned"
}

func setGroupKeys() url.Values {
	v := url.Values{}
	v.Set("$select", strings.Join([]string{
		"classification",
		"description",
		"displayName",
		"groupTypes",
		"id",
		"mail",
		"mailEnabled",
		"onPremisesSecurityIdentifier",
		"onPremisesSyncEnabled",
		"securityEnabled",
		"securityIdentifier",
		"isAssignableToRole",
		"isManagementRestricted",
		"createdDateTime",
	}, ","))
	v.Set("$top", "999")
	return v
}

func setMemberQuery() url.Values {
	memberQuery := url.Values{}
	memberQuery.Set("$select", strings.Join([]string{
		"id",
		"servicePrincipalType",
		"onPremisesSyncEnabled",
	}, ","))
	memberQuery.Set("$top", "999")
	return memberQuery
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

func getGroupGrants(ctx context.Context, resp *membershipList, resource *v2.Resource, g *groupBuilder, ps *pagination.PageState) ([]*v2.Grant, error) {
	grants, err := slices.ConvertErr(resp.Members, func(gm *membership) (*v2.Grant, error) {
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
				if !g.knownServicePrincipalTypes[gm.ServicePrincipalType] {
					// Only log once per sync per type, to reduce log spam and datadog costs
					ctxzap.Extract(ctx).Warn(
						"Grants: unsupported ServicePrincipalType type on Group Membership",
						zap.String("type", gm.ServicePrincipalType),
						zap.String("objectID", objectID),
						zap.Any("membership", gm),
					)
					g.knownServicePrincipalTypes[gm.ServicePrincipalType] = true
				}

				return nil, nil
			}
		default:
			if !g.knownGroupMembershipTypes[gm.Type] {
				// Only log once per sync per type, to reduce log spam and datadog costs
				ctxzap.Extract(ctx).Warn(
					"Grants: unsupported resource type on Group Membership",
					zap.String("type", gm.Type),
					zap.String("objectID", objectID),
					zap.Any("membership", gm),
				)
				g.knownGroupMembershipTypes[gm.Type] = true
			}
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

func (a *assignment) MarshalToReader() (*bytes.Reader, error) {
	data, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
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
		}))
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
	strRoleID = getRoleId(role.ID)
	profile := map[string]interface{}{
		"id":                 strRoleID,
		"name":               StringValue(role.Properties.RoleName),
		"description":        StringValue(role.Properties.Description),
		"type":               StringValue(role.Properties.RoleType),
		"role-definition-id": StringValue(role.ID),
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
			return arr[2] + ":" + arr[len(arr)-1]
		}
	}

	return ""
}

func getPrincipalType(ctx context.Context, cn *Connector, principalID string) (string, error) {
	var (
		index         = 0
		principalData map[string]interface{}
		mapEndPoint   = []string{"directoryObjects", "users", "groups", "servicePrincipals"}
	)
	for index < len(mapEndPoint) {
		reqURL := cn.buildURL(fmt.Sprintf("%s/%s", mapEndPoint[index], principalID), nil)
		resp := &principalData
		err := cn.query(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
		if err != nil {
			return "", err
		}

		if principalType, ok := principalData["@odata.type"].(string); ok {
			return principalType, nil
		}

		index++
	}

	return "", nil
}

func managedIdentityResource(ctx context.Context, sp *servicePrincipal, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := make(map[string]interface{})
	profile["id"] = sp.ID
	profile["app_id"] = sp.AppId
	options := []rs.UserTraitOption{
		rs.WithUserProfile(profile),
		rs.WithAccountType(v2.UserTrait_ACCOUNT_TYPE_SERVICE),
	}

	if sp.Info.LogoUrl != "" {
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
		sp.getDisplayName(),
		managedIdentitylResourceType,
		sp.ID,
		options,
		rs.WithParentResourceID(parentResourceID),
		rs.WithAnnotation(&v2.ExternalLink{
			Url: sp.externalURL(),
		}),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func setManagedIdentityKeys() url.Values {
	v := url.Values{}
	v.Set("$select", strings.Join(servicePrincipalSelect, ","))
	v.Set("$filter", "servicePrincipalType eq 'ManagedIdentity'")
	v.Set("$top", "999")
	return v
}

func setEnterpriseApplicationsKeys() url.Values {
	v := url.Values{}
	v.Set("$select", strings.Join(servicePrincipalSelect, ","))
	v.Set("$filter", "servicePrincipalType eq 'Application' AND accountEnabled eq true")
	v.Set("$top", "999")
	return v
}

func enterpriseApplicationResource(ctx context.Context, app *servicePrincipal, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
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

	if app.Info.LogoUrl != "" {
		options = append(options, rs.WithAppLogo(&v2.AssetRef{
			Id: app.Info.LogoUrl,
		}))
	}
	if app.Homepage != "" {
		options = append(options, rs.WithAppHelpURL(app.Homepage))
	}

	if app.AppOwnerOrganizationId == microsoftBuiltinAppsOwnerID {
		options = append(options, rs.WithAppFlags(v2.AppTrait_APP_FLAG_HIDDEN))
	}

	ret, err := rs.NewAppResource(
		app.getDisplayName(),
		enterpriseApplicationResourceType,
		app.ID,
		options,
		rs.WithParentResourceID(parentResourceID),
		rs.WithAnnotation(&v2.ExternalLink{
			Url: app.externalURL(),
		}),
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}
