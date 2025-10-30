package client

import (
	"context"
	"net/http"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"google.golang.org/grpc/codes"
)

func (a *AzureClient) GetOrganizations(ctx context.Context) ([]Organization, error) {
	resp := Organizations{}

	reqURL := a.QueryBuilder().BuildUrl("organization")
	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, reqURL, nil, &resp)
	if err != nil {
		return nil, uhttp.WrapErrors(codes.Unavailable, "baton-azure-infrastructure: failed to get organization", err)
	}

	return resp.Value, nil
}
