package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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

// ListServicePrincipals represents a list of service principals, nextLink is used to get the next page of results.
func (a *AzureClient) ListServicePrincipals(ctx context.Context, nextLink string) (*ServicePrincipalsList, error) {
	nextLink = a.QueryBuilder().
		// TODO: Validate if is V1 or BETA
		Version(V1).
		Add("$select", strings.Join(servicePrincipalSelect, ",")).
		Add("$filter", "servicePrincipalType eq 'Application' AND accountEnabled eq true").
		Add("$top", "999").
		Add("$expand", "appRoleAssignedTo").
		BuildUrlWithPagination("servicePrincipals", nextLink)

	resp := &ServicePrincipalsList{}

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, nextLink, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastrucure: failed to get service principals: %w", err)
	}

	return resp, nil
}

func (a *AzureClient) ServicePrincipal(ctx context.Context, id string) (*ServicePrincipal, error) {
	url := a.QueryBuilder().
		Version(Beta).
		Add("$expand", "appRoleAssignedTo").
		BuildUrl("servicePrincipals", id)

	resp := &ServicePrincipal{}

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, url, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastrucure: failed to get service principal: %w", err)
	}

	return resp, nil
}

func (a *AzureClient) ServicePrincipalOwners(ctx context.Context, id string) (*MembershipList, error) {
	// NOTE: We use the Beta URL here because in the v1.0 docs there is this note (last checked August 2023)
	//
	// Important
	//
	//   This API has a known issue where service principals are not listed as group
	//   members in v1.0. Use this API on the beta endpoint instead or the
	//   /groups/{id}?members API.
	//
	// https://learn.microsoft.com/en-us/graph/api/group-list-members?view=graph-rest-1.0&tabs=http
	//
	// NOTE #2: This applies to both the members and owners endpoints.
	builder := a.QueryBuilder().
		Version(Beta).
		Add("$select", strings.Join([]string{"id"}, ","))

	if a.skipAdGroups {
		builder.
			Add("$filter", "(onPremisesSyncEnabled ne true)").
			// Required to prevent MS Graph from returning a 400
			Add("$count", "true")
	}

	ownersURL := builder.BuildUrl("servicePrincipals", id, "owners")

	resp := &MembershipList{}

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, ownersURL, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastrucure: failed to get service principal owners: %w", err)
	}

	return resp, nil
}

// ServicePrincipalGrantAppRoleAssignment adds an owner to a service principal
// https://learn.microsoft.com/en-us/graph/api/serviceprincipal-post-approleassignedto?view=graph-rest-1.0&tabs=http
func (a *AzureClient) ServicePrincipalGrantAppRoleAssignment(
	ctx context.Context,
	resourceId string,
	appRoleId string,
	principalID string,
) error {
	url := a.QueryBuilder().
		// TODO: Should be v1 or beta?
		// Docs say V1, but old code is on Beta...
		Version(V1).
		BuildUrl("servicePrincipals", resourceId, "appRoleAssignedTo")

	body := map[string]string{
		"appRoleId":   appRoleId,
		"principalId": principalID,
		"resourceId":  resourceId,
	}

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodPost, url, body, nil)
	if err != nil {
		return fmt.Errorf("baton-azure-infrastrucure: failed to grant app role assignment to service principal: %w", err)
	}

	return nil
}

// ServicePrincipalDeleteOwner removes an owner from a service principal
// https://learn.microsoft.com/en-us/graph/api/serviceprincipal-delete-owners?view=graph-rest-1.0&tabs=http
func (a *AzureClient) ServicePrincipalDeleteOwner(ctx context.Context, principalId, ownerID string) error {
	url := a.QueryBuilder().
		Version(V1).
		BuildUrl("servicePrincipals", principalId, "owners", ownerID, "$ref")

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodDelete, url, nil, nil)
	if err != nil {
		return fmt.Errorf("baton-azure-infrastrucure: failed to delete owner from service principal: %w", err)
	}

	return nil
}

// ServicePrincipalDeleteAppRoleAssignedTo Deletes an appRoleAssignment that a user, group, or client service principal has been granted for a resource service principal.
// https://learn.microsoft.com/en-us/graph/api/serviceprincipal-delete-approleassignedto?view=graph-rest-1.0&tabs=http
func (a *AzureClient) ServicePrincipalDeleteAppRoleAssignedTo(ctx context.Context, principalId, appRoleAssignmentId string) error {
	url := a.QueryBuilder().
		Version(V1).
		BuildUrl("servicePrincipals", principalId, "appRoleAssignedTo", appRoleAssignmentId)

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodDelete, url, nil, nil)
	if err != nil {
		return fmt.Errorf("baton-azure-infrastrucure: failed to delete owner from service principal: %w", err)
	}

	return nil
}

func (a *AzureClient) ListServicePrincipalsManagedIdentity(ctx context.Context, nextLink string) (*ServicePrincipalsList, error) {
	nextLink = a.QueryBuilder().
		// TODO: Validate if is V1 or BETA
		Version(V1).
		Add("$select", strings.Join(servicePrincipalSelect, ",")).
		Add("$filter", "servicePrincipalType eq 'ManagedIdentity'").
		Add("$top", "999").
		BuildUrlWithPagination("servicePrincipals", nextLink)

	resp := &ServicePrincipalsList{}

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, nextLink, nil, resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastrucure: failed to get service principals managed: %w", err)
	}

	return resp, nil
}
