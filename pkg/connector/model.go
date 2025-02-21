package connector

type manager struct {
	Id          string `json:"id,omitempty"`
	EmployeeId  string `json:"employeeId,omitempty"`
	Email       string `json:"mail,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type mailboxSettings struct {
	UserPurpose string `json:"userPurpose,omitempty"`
}

type user struct {
	ID                string   `json:"id,omitempty"`
	Email             string   `json:"mail,omitempty"`
	DisplayName       string   `json:"displayName,omitempty"`
	UserPrincipalName string   `json:"userPrincipalName,omitempty"`
	JobTitle          string   `json:"jobTitle,omitempty"`
	AccountEnabled    bool     `json:"accountEnabled,omitempty"`
	EmployeeType      string   `json:"employeeType,omitempty"`
	EmployeeID        string   `json:"employeeId,omitempty"`
	Department        string   `json:"department,omitempty"`
	Manager           *manager `json:"manager,omitempty"`
}

type usersList struct {
	Context  string  `json:"@odata.context"`
	NextLink string  `json:"@odata.nextLink"`
	Users    []*user `json:"value,omitempty"`
}

// https://learn.microsoft.com/en-us/graph/api/resources/group?view=graph-rest-1.0#properties
type group struct {
	Classification               string   `json:"classification,omitempty"`
	Description                  string   `json:"description,omitempty"`
	DisplayName                  string   `json:"displayName,omitempty"`
	GroupTypes                   []string `json:"groupTypes,omitempty"`
	ID                           string   `json:"id,omitempty"`
	Mail                         string   `json:"mail,omitempty"`
	MailEnabled                  bool     `json:"mailEnabled,omitempty"`
	OnPremisesSecurityIdentifier *string  `json:"onPremisesSecurityIdentifier,omitempty"`
	OnPremisesSyncEnabled        bool     `json:"onPremisesSyncEnabled,omitempty"`
	SecurityEnabled              bool     `json:"securityEnabled,omitempty"`
	SecurityIdentifier           string   `json:"securityIdentifier,omitempty"`
	IsAssignableToRole           bool     `json:"isAssignableToRole,omitempty"`
	IsManagementRestricted       bool     `json:"isManagementRestricted,omitempty"`
	CreatedDateTime              string   `json:"createdDateTime,omitempty"`
}

type groupsList struct {
	Context  string   `json:"@odata.context"`
	NextLink string   `json:"@odata.nextLink"`
	Groups   []*group `json:"value"`
}

type membershipList struct {
	Context  string        `json:"@odata.context"`
	NextLink string        `json:"@odata.nextLink"`
	Members  []*membership `json:"value"`
}

type membership struct {
	Id                     string `json:"id,omitempty"`
	Type                   string `json:"@odata.type"`
	ServicePrincipalType   string `json:"servicePrincipalType,omitempty"`
	AppOwnerOrganizationId string `json:"appOwnerOrganizationId,omitempty"`
	OnPremisesSyncEnabled  bool   `json:"onPremisesSyncEnabled,omitempty"`
}

type assignment struct {
	ObjectRef string `json:"@odata.id"`
}

type servicePrincipal struct {
	AccountEnabled         bool                 `json:"accountEnabled,omitempty"`
	AppDisplayName         string               `json:"appDisplayName,omitempty"`
	AppId                  string               `json:"appId,omitempty"`
	AppOwnerOrganizationId string               `json:"appOwnerOrganizationId,omitempty"`
	Description            string               `json:"description,omitempty"`
	DisplayName            string               `json:"displayName,omitempty"`
	Homepage               string               `json:"homepage,omitempty"`
	ID                     string               `json:"id,omitempty"`
	Info                   info                 `json:"info"`
	ServicePrincipalType   string               `json:"servicePrincipalType,omitempty"`
	Tags                   []string             `json:"tags,omitempty"`
	AppRoles               []*appRole           `json:"appRoles,omitempty"`
	AppRolesAssignedTo     []*appRoleAssignment `json:"appRoleAssignedTo,omitempty"`
}

type info struct {
	LogoUrl string `json:"logoUrl"`
}

type appRole struct {
	AllowedMemberTypes []string `json:"allowedMemberTypes,omitempty"` // "User" or "Application"
	Description        string   `json:"description,omitempty"`
	DisplayName        string   `json:"displayName,omitempty"`
	Id                 string   `json:"id,omitempty"`
	IsEnabled          bool     `json:"isEnabled,omitempty"`
	Value              string   `json:"value,omitempty"`
}

type servicePrincipalsList struct {
	Context  string              `json:"@odata.context"`
	NextLink string              `json:"@odata.nextLink"`
	Value    []*servicePrincipal `json:"value,omitempty"`
}

type Organizations struct {
	Value []*Organization `json:"value"`
}

type Organization struct {
	ID string `json:"id"`
}

type appRoleAssignment struct {
	AppRoleId            string `json:"appRoleId"`
	CreatedDateTime      string `json:"createdDateTime"`
	DeletedDateTime      string `json:"deletedDateTime"`
	Id                   string `json:"id"`
	PrincipalDisplayName string `json:"principalDisplayName"`
	PrincipalId          string `json:"principalId"`
	PrincipalType        string `json:"principalType"` // The type of the assigned principal. This can either be User, Group, or ServicePrincipal. Read-only.
	ResourceDisplayName  string `json:"resourceDisplayName"`
	ResourceId           string `json:"resourceId"`
}
