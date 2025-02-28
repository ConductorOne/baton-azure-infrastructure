package client

import (
	"context"
	"fmt"
	"net/http"
)

func (d *AzureClient) GetOrganizations(ctx context.Context) ([]Organization, error) {
	resp := Organizations{}
	reqURL := d.buildBetaURL("organization", nil)
	err := d.query(ctx, graphReadScopes, http.MethodGet, reqURL, nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("baton-microsoft-entra: failed to get organization ID: %w", err)
	}

	return resp.Value, nil
}
