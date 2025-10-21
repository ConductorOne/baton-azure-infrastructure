package client

import (
	"fmt"
	"net/url"
	"time"
)

type GraphResponse[T any] struct {
	Context  string `json:"@odata.context"`
	NextLink string `json:"@odata.nextLink"`
	Value    T
}

type ErrorResponse struct {
	Error *GraphError `json:"error,omitempty"`
}

func (er *ErrorResponse) Message() string {
	if er.Error != nil {
		return fmt.Sprintf("code: %s; message: %s; innerError: %v", er.Error.Code, er.Error.Message, er.Error.InnerError)
	}
	return "empty error"
}

type GraphError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	InnerError *struct {
		Code      string `json:"code"`
		RequestID string `json:"request-id"`
		Date      string `json:"date"`
	} `json:"innerError,omitempty"`
	Details []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"details,omitempty"`
}

func (e *GraphError) Error() string {
	return fmt.Sprintf("code: %s, message: %s, innerError: %v, details: %v", e.Code, e.Message, e.InnerError, e.Details)
}

type Manager struct {
	Id          string `json:"id,omitempty"`
	EmployeeId  string `json:"employeeId,omitempty"`
	Email       string `json:"mail,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type MailboxSettings struct {
	UserPurpose string `json:"userPurpose,omitempty"`
}

type User struct {
	ID                string   `json:"id,omitempty"`
	Email             string   `json:"mail,omitempty"`
	DisplayName       string   `json:"displayName,omitempty"`
	UserPrincipalName string   `json:"userPrincipalName,omitempty"`
	JobTitle          string   `json:"jobTitle,omitempty"`
	AccountEnabled    bool     `json:"accountEnabled,omitempty"`
	EmployeeType      string   `json:"employeeType,omitempty"`
	EmployeeID        string   `json:"employeeId,omitempty"`
	Department        string   `json:"department,omitempty"`
	Manager           *Manager `json:"manager,omitempty"`
}

type UsersList struct {
	Context  string  `json:"@odata.context"`
	NextLink string  `json:"@odata.nextLink"`
	Users    []*User `json:"value,omitempty"`
}

// https://learn.microsoft.com/en-us/graph/api/resources/group?view=graph-rest-1.0#properties
type Group struct {
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

type GroupsList struct {
	Context  string   `json:"@odata.context"`
	NextLink string   `json:"@odata.nextLink"`
	Groups   []*Group `json:"value"`
}

type MembershipList struct {
	Context  string        `json:"@odata.context"`
	NextLink string        `json:"@odata.nextLink"`
	Members  []*Membership `json:"value"`
}

type Membership struct {
	Id                     string `json:"id,omitempty"`
	Type                   string `json:"@odata.type"`
	ServicePrincipalType   string `json:"servicePrincipalType,omitempty"`
	AppOwnerOrganizationId string `json:"appOwnerOrganizationId,omitempty"`
	OnPremisesSyncEnabled  bool   `json:"onPremisesSyncEnabled,omitempty"`
}

type Assignment struct {
	ObjectRef string `json:"@odata.id"`
}

type ServicePrincipal struct {
	AccountEnabled         bool                 `json:"accountEnabled,omitempty"`
	AppDisplayName         string               `json:"appDisplayName,omitempty"`
	AppId                  string               `json:"appId,omitempty"`
	AppOwnerOrganizationId string               `json:"appOwnerOrganizationId,omitempty"`
	Description            string               `json:"description,omitempty"`
	DisplayName            string               `json:"displayName,omitempty"`
	Homepage               string               `json:"homepage,omitempty"`
	ID                     string               `json:"id,omitempty"`
	Info                   Info                 `json:"info"`
	ServicePrincipalType   string               `json:"servicePrincipalType,omitempty"`
	Tags                   []string             `json:"tags,omitempty"`
	AppRoles               []*AppRole           `json:"appRoles,omitempty"`
	AppRolesAssignedTo     []*AppRoleAssignment `json:"appRoleAssignedTo,omitempty"`
}

type Info struct {
	LogoUrl string `json:"logoUrl"`
}

type AppRole struct {
	AllowedMemberTypes []string `json:"allowedMemberTypes,omitempty"` // "User" or "Application"
	Description        string   `json:"description,omitempty"`
	DisplayName        string   `json:"displayName,omitempty"`
	Id                 string   `json:"id,omitempty"`
	IsEnabled          bool     `json:"isEnabled,omitempty"`
	Value              string   `json:"value,omitempty"`
}

type ServicePrincipalsList struct {
	Context  string              `json:"@odata.context"`
	NextLink string              `json:"@odata.nextLink"`
	Value    []*ServicePrincipal `json:"value,omitempty"`
}

type Organizations struct {
	Value []Organization `json:"value"`
}

type Organization struct {
	ID string `json:"id"`
}

type AppRoleAssignment struct {
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

func (sp *ServicePrincipal) GetDisplayName() string {
	if sp.DisplayName != "" {
		return sp.DisplayName
	}

	return sp.AppDisplayName
}

func (sp *ServicePrincipal) ExternalURL() string {
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

type PMIPrivilegedAccessResource struct {
	Id                 string     `json:"id"`
	ExternalId         string     `json:"externalId"`
	Type               string     `json:"type"`
	DisplayName        string     `json:"displayName"`
	Status             string     `json:"status"`
	OnboardDateTime    *time.Time `json:"onboardDateTime"`
	RegisteredDateTime *time.Time `json:"registeredDateTime"`
}

type PMIRoleAssigment struct {
	Id                         string    `json:"id"`
	ResourceId                 string    `json:"resourceId"`
	RoleDefinitionId           string    `json:"roleDefinitionId"`
	SubjectId                  string    `json:"subjectId"`
	StartDateTime              time.Time `json:"startDateTime"`
	EndDateTime                time.Time `json:"endDateTime"`
	MemberType                 string    `json:"memberType"`
	AssignmentState            string    `json:"assignmentState"`
	Status                     string    `json:"status"`
	RoleDefinitionOdataContext string    `json:"roleDefinition@odata.context"`
	RoleDefinition             struct {
		Id          string `json:"id"`
		ResourceId  string `json:"resourceId"`
		ExternalId  string `json:"externalId"`
		TemplateId  string `json:"templateId"`
		DisplayName string `json:"displayName"`
		Type        string `json:"type"`
	} `json:"roleDefinition"`
	SubjectOdataContext string `json:"subject@odata.context"`
	Subject             struct {
		Id          string `json:"id"`
		Type        string `json:"type"`
		DisplayName string `json:"displayName"`
	} `json:"subject"`
}
