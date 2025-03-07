package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

var userFields = []string{
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
}

// Users fetches all users from Azure AD.
// Calls https://graph.microsoft.com/beta/users
func (a *AzureClient) Users(ctx context.Context, nextLink string) (*UsersList, error) {
	resp := &UsersList{}
	reqURL := a.QueryBuilder().
		// Note: beta version returns more fields than v1.0
		Version(Beta).
		Add("$select", strings.Join(userFields, ",")).
		Add("$expand", "manager($select=id,employeeId,mail,displayName)").
		Add("$top", "999").
		BuildUrlWithPagination("users", nextLink)

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: error fetching users: %w", err)
	}

	return resp, nil
}

// UserMailboxSetting fetches mailbox settings for a user.
// Calls https://graph.microsoft.com/beta/users/{userId}/mailboxSettings
func (a *AzureClient) UserMailboxSetting(ctx context.Context, userId string) (*MailboxSettings, error) {
	resp := &MailboxSettings{}
	reqURL := a.QueryBuilder().
		// Note: beta version returns more fields than v1.0
		Version(Beta).
		Add("$select", strings.Join([]string{"userPurpose"}, ",")).
		BuildUrl("users", userId, "mailboxSettings")

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: error fetching mailbox setting: %w", err)
	}

	return resp, nil
}
