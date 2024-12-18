package connector

import (
	"context"
	"net/mail"
	"net/url"
	"path"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	pagination "github.com/conductorone/baton-sdk/pkg/pagination"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
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
func userResource(ctx context.Context, u *client.User, parentResourceID *v2.ResourceId, userTraitOptions ...resource.UserTraitOption) (*v2.Resource, error) {
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

	options := []resource.UserTraitOption{
		resource.WithEmail(primaryEmail, true),
		resource.WithUserProfile(profile),
	}

	options = append(options, userTraitOptions...)
	if u.UserPrincipalName != "" {
		options = append(options, resource.WithUserLogin(u.UserPrincipalName))
	}

	if u.AccountEnabled {
		options = append(options, resource.WithStatus(v2.UserTrait_Status_STATUS_ENABLED))
	} else {
		options = append(options, resource.WithStatus(v2.UserTrait_Status_STATUS_DISABLED))
	}

	ret, err := resource.NewUserResource(
		u.DisplayName,
		userResourceType,
		u.ID,
		options,
		resource.WithParentResourceID(parentResourceID),
		resource.WithAnnotation(&v2.ExternalLink{
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
