package connector

const (
	ownersStr     = "owners"
	appRoleStr    = "appRole"
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
