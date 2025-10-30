package client

import (
	"context"
	"net/http"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"google.golang.org/grpc/codes"
)

// ServicePrincipalAddOwner adds an owner to a service principal
// https://learn.microsoft.com/en-us/graph/api/serviceprincipal-post-owners?view=graph-rest-1.0&tabs=http
func (a *AzureClient) ServicePrincipalAddOwner(ctx context.Context, servicePrincipalId string, objectId string) error {
	url := a.QueryBuilder().
		Version(V1).
		BuildUrl("servicePrincipals", servicePrincipalId, "owners", "$ref")

	body := &Assignment{
		ObjectRef: a.QueryBuilder().
			Version(V1).
			BuildUrl("directoryObjects", objectId),
	}

	err := a.requestWithToken(ctx, graphReadScopes, http.MethodPost, url, body, nil)
	if err != nil {
		return uhttp.WrapErrors(codes.Unavailable, "baton-azure-infrastructure: failed to add owner to service principal", err)
	}

	return nil
}
