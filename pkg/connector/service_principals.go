package connector

import (
	"fmt"
	"net/url"
)

const (
	ownersStr     = "owners"
	assignmentStr = "assignment"
)

var servicePrincipalSelect = []string{
	"accountEnabled",
	"appDisplayName",
	"appRoles",
	"appId",
	"appOwnerOrganizationId",
	"description",
	"displayName",
	"homepage",
	"id",
	"info",
	"tags",
}

type servicePrincipal struct {
	AccountEnabled         bool   `json:"accountEnabled"`
	AppDisplayName         string `json:"appDisplayName"`
	AppId                  string `json:"appId"`
	AppOwnerOrganizationId string `json:"appOwnerOrganizationId"`
	Description            string `json:"description"`
	DisplayName            string `json:"displayName"`
	Homepage               string `json:"homepage"`
	ID                     string `json:"id"`
	Info                   struct {
		LogoUrl string `json:"logoUrl"`
	} `json:"info"`
	ServicePrincipalType string     `json:"servicePrincipalType"`
	Tags                 []string   `json:"tags"`
	AppRoles             []*appRole `json:"appRoles"`
}

type appRole struct {
	AllowedMemberTypes []string `json:"allowedMemberTypes"` // "User" or "Application"
	Description        string   `json:"description"`
	DisplayName        string   `json:"displayName"`
	Id                 string   `json:"id"`
	IsEnabled          bool     `json:"isEnabled"`
	Value              string   `json:"value"`
}

type servicePrincipalsList struct {
	Context  string              `json:"@odata.context"`
	NextLink string              `json:"@odata.nextLink"`
	Value    []*servicePrincipal `json:"value"`
}

func (sp *servicePrincipal) getDisplayName() string {
	if sp.DisplayName != "" {
		return sp.DisplayName
	}
	return sp.AppDisplayName
}

func (sp *servicePrincipal) externalURL() string {
	return (&url.URL{
		Scheme: "https",
		Host:   "entra.microsoft.com",
		Path:   "/",
		Fragment: fmt.Sprintf(
			"view/Microsoft_AAD_IAM/ManagedAppMenuBlade/~/Overview/objectId/%s/appId/%s/preferredSingleSignOnMode~/null/servicePrincipalType/%s",
			sp.ID,
			sp.AppId,
			sp.ServicePrincipalType,
		),
	}).String()
}
