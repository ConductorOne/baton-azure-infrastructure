package client

import (
	"context"
	"fmt"
	"net/http"
)

func (a *AzureClient) GetOrganizations(ctx context.Context) ([]Organization, error) {
	resp := Organizations{}

	reqURL := NewAzureQueryBuilder().BuildUrl("organization")
	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, reqURL, nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("baton-azure-infrastructure: failed to get organization ID: %w", err)
	}

	return resp.Value, nil
}
