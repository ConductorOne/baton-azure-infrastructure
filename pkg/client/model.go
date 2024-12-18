package client

type manager struct {
	Id          string `json:"id"`
	EmployeeId  string `json:"employeeId"`
	Email       string `json:"mail"`
	DisplayName string `json:"displayName"`
}

type MailboxSettings struct {
	UserPurpose string `json:"userPurpose"`
}

type User struct {
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

type UsersList struct {
	Context  string  `json:"@odata.context"`
	NextLink string  `json:"@odata.nextLink"`
	Users    []*User `json:"value"`
}
