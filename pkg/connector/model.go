package connector

import "time"

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

type SubscriptionPolicies struct {
	LocationPlacementID string `json:"locationPlacementId,omitempty"`
	QuotaID             string `json:"quotaId,omitempty"`
	SpendingLimit       string `json:"spendingLimit,omitempty"`
}

type ManagedByTenants struct {
	TenantID string `json:"tenantId,omitempty"`
}

type Tags struct {
	TagKey1 string `json:"tagKey1,omitempty"`
	TagKey2 string `json:"tagKey2,omitempty"`
}

type SubscriptionList struct {
	Subscription []Subscription `json:"value,omitempty"`
	Count        Count          `json:"count,omitempty"`
	NextLink     string         `json:"nextLink,omitempty"`
}

type Subscription struct {
	ID                   string               `json:"id,omitempty"`
	SubscriptionID       string               `json:"subscriptionId,omitempty"`
	TenantID             string               `json:"tenantId,omitempty"`
	DisplayName          string               `json:"displayName,omitempty"`
	State                string               `json:"state,omitempty"`
	SubscriptionPolicies SubscriptionPolicies `json:"subscriptionPolicies,omitempty"`
	AuthorizationSource  string               `json:"authorizationSource,omitempty"`
	ManagedByTenants     []ManagedByTenants   `json:"managedByTenants,omitempty"`
	Tags                 Tags                 `json:"tags,omitempty"`
}
type Count struct {
	Type  string `json:"type,omitempty"`
	Value int    `json:"value,omitempty"`
}

type Promotions struct {
	Category    string    `json:"category,omitempty"`
	EndDateTime time.Time `json:"endDateTime,omitempty"`
}

type TenantList struct {
	Tenant   []tenant `json:"value,omitempty"`
	NextLink string   `json:"nextLink,omitempty"`
}

type tenant struct {
	ID             string `json:"id,omitempty"`
	TenantID       string `json:"tenantId,omitempty"`
	TenantCategory string `json:"tenantCategory,omitempty"`
}
