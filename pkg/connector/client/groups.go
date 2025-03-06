package client

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
)

var groupFields = []string{
	"classification",
	"description",
	"displayName",
	"groupTypes",
	"id",
	"mail",
	"mailEnabled",
	"onPremisesSecurityIdentifier",
	"onPremisesSyncEnabled",
	"securityEnabled",
	"securityIdentifier",
	"isAssignableToRole",
	"isManagementRestricted",
	"createdDateTime",
}

func (a *AzureClient) Groups(ctx context.Context, nextLink string) (*GroupsList, error) {
	builder := NewAzureQueryBuilder().
		// Note: beta version returns more fields than v1.0
		Version(Beta).
		Add("$select", strings.Join(groupFields, ",")).
		Add("$top", "999")

	if a.skipAdGroups {
		builder.
			Add("$filter", "(onPremisesSyncEnabled ne true)").
			Add("$count", "true") // Required to prevent MS Graph from returning a 400
	}

	reqURL := builder.BuildUrlWithPagination("groups", nextLink)

	resp := &GroupsList{}
	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: error fetching groups: %w", err)
	}

	return resp, nil
}

func (a *AzureClient) GroupOwners(ctx context.Context, groupId string) (*MembershipList, error) {
	// NOTE: We use the Beta URL here because in the v1.0 docs there is this note (last checked December 2024)
	// -----------------------------------------------------------------------------------------------
	// *** Important ***
	//
	//   This API has a known issue where service principals are not listed as group
	//   members in v1.0. Use this API on the beta endpoint instead or the
	//   /groups/{id}?members API.
	//
	// https://learn.microsoft.com/en-us/graph/api/group-list-members?view=graph-rest-1.0&tabs=http
	//
	// NOTE #2: This applies to both the members and owners endpoints.
	reqURL := NewAzureQueryBuilder().
		Version(Beta).
		Add("$select", strings.Join([]string{"id"}, ",")).
		BuildUrl("groups", groupId, "owners")

	resp := &MembershipList{}
	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: error fetching groups owners: %w", err)
	}

	return resp, nil
}

func (a *AzureClient) GroupMembers(ctx context.Context, groupId string, nextLink string) (*MembershipList, error) {
	// NOTE: We use the Beta URL here because in the v1.0 docs there is this note (last checked December 2024)
	// -----------------------------------------------------------------------------------------------
	// *** Important ***
	//
	//   This API has a known issue where service principals are not listed as group
	//   members in v1.0. Use this API on the beta endpoint instead or the
	//   /groups/{id}?members API.
	//
	// https://learn.microsoft.com/en-us/graph/api/group-list-members?view=graph-rest-1.0&tabs=http
	//
	// NOTE #2: This applies to both the members and owners endpoints.
	builder := NewAzureQueryBuilder().
		Version(Beta).
		Add("$select", strings.Join([]string{
			"id",
			"servicePrincipalType",
			"onPremisesSyncEnabled",
		}, ",")).
		Add("$top", "999")

	if a.skipAdGroups {
		builder.
			Add("$filter", "(onPremisesSyncEnabled ne true)").
			Add("$count", "true") // Required to prevent MS Graph from returning a 400
	}

	reqURL := builder.BuildUrlWithPagination(path.Join("groups", groupId, "members"), nextLink)

	resp := &MembershipList{}
	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: error fetching groups owners: %w", err)
	}

	return resp, nil
}

func (a *AzureClient) GroupAddOwner(ctx context.Context, groupId string, refUrl string) error {
	reqURL := NewAzureQueryBuilder().
		Version(V1).
		BuildUrl("groups", groupId, "owners", "$ref")

	body := &Assignment{ObjectRef: refUrl}

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodPost, reqURL, body, nil)
	if err != nil {
		return err
	}

	return nil
}

func (a *AzureClient) GroupRemoveOwner(ctx context.Context, groupId string, userId string) error {
	reqURL := NewAzureQueryBuilder().
		Version(V1).
		BuildUrl("groups", groupId, "owners", userId, "$ref")

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodDelete, reqURL, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func (a *AzureClient) GroupAddMember(ctx context.Context, groupId string, refUrl string) error {
	reqURL := NewAzureQueryBuilder().
		Version(V1).
		BuildUrl("groups", groupId, "members", "$ref")

	body := &Assignment{ObjectRef: refUrl}

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodPost, reqURL, body, nil)
	if err != nil {
		return err
	}

	return nil
}

func (a *AzureClient) GroupRemoveMember(ctx context.Context, groupId string, userId string) error {
	reqURL := NewAzureQueryBuilder().
		Version(V1).
		BuildUrl("groups", groupId, "members", userId, "$ref")

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodDelete, reqURL, nil, nil)
	if err != nil {
		return err
	}

	return nil
}
