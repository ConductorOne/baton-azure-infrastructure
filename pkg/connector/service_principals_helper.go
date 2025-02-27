package connector

import (
	"fmt"
	"net/url"
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
