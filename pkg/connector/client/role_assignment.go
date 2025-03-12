package client

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
)

func (a *AzureClient) GetRoleAssignments(
	ctx context.Context,
	subscriptionID,
	scope string,
) ([]*armauthorization.RoleAssignment, error) {
	roleClient, err := armauthorization.NewRoleAssignmentsClient(
		subscriptionID,
		a.token,
		nil,
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
