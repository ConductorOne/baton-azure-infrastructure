package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"path"
	"strings"

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
