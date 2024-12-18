package connector

type manager struct {
	Id          string `json:"id"`
	EmployeeId  string `json:"employeeId"`
	Email       string `json:"mail"`
	DisplayName string `json:"displayName"`
}

type mailboxSettings struct {
	UserPurpose string `json:"userPurpose"`
}

type user struct {
	ID                string   `json:"id"`
	Email             string   `json:"mail"`
	DisplayName       string   `json:"displayName"`
	UserPrincipalName string   `json:"userPrincipalName"`
	JobTitle          string   `json:"jobTitle"`
	AccountEnabled    bool     `json:"accountEnabled"`
	EmployeeType      string   `json:"employeeType"`
	EmployeeID        string   `json:"employeeId"`
	Department        string   `json:"department"`
	Manager           *manager `json:"manager,omitempty"`
}

type usersList struct {
	Context  string  `json:"@odata.context"`
	NextLink string  `json:"@odata.nextLink"`
	Users    []*user `json:"value"`
}

// https://learn.microsoft.com/en-us/graph/api/resources/group?view=graph-rest-1.0#properties
type group struct {
	Classification               string   `json:"classification"`
	Description                  string   `json:"description"`
	DisplayName                  string   `json:"displayName"`
	GroupTypes                   []string `json:"groupTypes"`
	ID                           string   `json:"id"`
	Mail                         string   `json:"mail"`
	MailEnabled                  bool     `json:"mailEnabled"`
	OnPremisesSecurityIdentifier *string  `json:"onPremisesSecurityIdentifier"`
	OnPremisesSyncEnabled        bool     `json:"onPremisesSyncEnabled"`
	SecurityEnabled              bool     `json:"securityEnabled"`
	SecurityIdentifier           string   `json:"securityIdentifier"`
	IsAssignableToRole           bool     `json:"isAssignableToRole"`
	IsManagementRestricted       bool     `json:"isManagementRestricted"`
	CreatedDateTime              string   `json:"createdDateTime"`
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
	Id                     string `json:"id"`
	Type                   string `json:"@odata.type"`
	ServicePrincipalType   string `json:"servicePrincipalType"`
	AppOwnerOrganizationId string `json:"appOwnerOrganizationId"`
	OnPremisesSyncEnabled  bool   `json:"onPremisesSyncEnabled"`
}
