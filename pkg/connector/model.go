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
