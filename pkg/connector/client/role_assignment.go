package client

import (
	"context"
	"fmt"
	"net/http"
	"path"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (a *AzureClient) GetRoleAssignments(
	ctx context.Context,
	subscriptionID,
	scope string,
) ([]*armauthorization.RoleAssignment, error) {
	roleClient, err := armauthorization.NewRoleAssignmentsClient(
		subscriptionID,
		a.token,
		a.ArmOptions(),
	)
	if err != nil {
		return nil, err
	}

	page := roleClient.NewListForScopePager(scope, nil)

	var result []*armauthorization.RoleAssignment

	for page.More() {
		nextPage, err := page.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		result = append(result, nextPage.Value...)
	}

	return result, nil
}

func (a *AzureClient) GetPrivilegedAccessFromAzure(
	ctx context.Context,
	scope string,
) (*PMIPrivilegedAccessResource, error) {
	builder := a.QueryBuilder().
		Version(Beta).
		Add("$filter", fmt.Sprintf("externalId eq '%s'", scope))

	reqURL := builder.BuildUrl("privilegedAccess", "azureResources", "resources")

	var response GraphResponse[[]*PMIPrivilegedAccessResource]

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, reqURL, nil, &response)
	if err != nil {
		return nil, err
	}

	if len(response.Value) == 0 {
		return nil, status.New(codes.NotFound, fmt.Sprintf("resource not found for scope %s", scope)).Err()
	}

	return response.Value[0], nil
}

func (a *AzureClient) GetPrivilegedAccessRoleAssignments(ctx context.Context, pmiId string, nextLink string) ([]PMIRoleAssigment, string, error) {
	builder := a.QueryBuilder().
		Version(Beta).
		Add("$expand", "roleDefinition,subject").
		Add("$filter", fmt.Sprintf("(roleDefinition/resource/id eq '%s') and (assignmentState eq 'Eligible')", pmiId))

	reqURL := builder.BuildUrlWithPagination(path.Join("privilegedAccess", "azureResources", "roleAssignments"), nextLink)

	var response GraphResponse[[]PMIRoleAssigment]

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, reqURL, nil, &response)
	if err != nil {
		return nil, "", err
	}

	if len(response.Value) == 0 {
		return nil, "", status.New(codes.NotFound, fmt.Sprintf("resource not found for pmi %s", pmiId)).Err()
	}

	return response.Value, response.NextLink, nil
}
