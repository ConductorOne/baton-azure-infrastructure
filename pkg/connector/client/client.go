package client

import (
	"context"
	"net/http"
	"net/url"
	"path"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
)

const (
	apiDomain   = "graph.microsoft.com"
	apiVersion  = "v1.0"
	betaVersion = "beta"
)

const (
	managerIDProfileKey          = "managerId"
	employeeNumberProfileKey     = "employeeNumber"
	managerEmailProfileKey       = "managerEmail"
	supervisorIDProfileKey       = "supervisorEId"
	supervisorEmailProfileKey    = "supervisorEmail"
	supervisorFullNameProfileKey = "supervisor"
)

var graphReadScopes = []string{
	"https://graph.microsoft.com/.default",
}

type AzureClient struct {
	token         azcore.TokenCredential
	httpClient    *uhttp.BaseHttpClient
	clientFactory *armsubscription.ClientFactory
}

func NewAzureClient(
	ctx context.Context,
	httpClient *http.Client,
	token azcore.TokenCredential,
) (*AzureClient, error) {
	client, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return nil, err
	}

	clientFactory, err := armsubscription.NewClientFactory(token, nil)
	if err != nil {
		return nil, err
	}

	return &AzureClient{
		token:         token,
		httpClient:    client,
		clientFactory: clientFactory,
	}, nil
}

func (c *AzureClient) buildBetaURL(reqPath string, v url.Values) string {
	ux := url.URL{
		Scheme:   "https",
		Host:     apiDomain,
		Path:     path.Join(betaVersion, reqPath),
		RawQuery: v.Encode(),
	}
	return ux.String()
}

func (c *AzureClient) doRequest(ctx context.Context,
	method,
	endpointUrl string,
	token string,
	res interface{},
	body interface{},
) error {
	urlAddress, err := url.Parse(endpointUrl)
	if err != nil {
		return err
	}

	reqOpts := []uhttp.RequestOption{
		uhttp.WithBearerToken(token),
		uhttp.WithHeader("ConsistencyLevel", "eventual"),
		uhttp.WithContentTypeJSONHeader(),
	}

	if body != nil {
		reqOpts = append(reqOpts, uhttp.WithJSONBody(body))
	}

	req, err := c.httpClient.NewRequest(ctx,
		method,
		urlAddress,
		reqOpts...,
	)
	if err != nil {
		return err
	}

	var opts []uhttp.DoOption
	if res != nil {
		opts = append(opts, uhttp.WithResponse(res))
	}

	resp, err := c.httpClient.Do(req, opts...)
	if err != nil {
		return err
	}

	resp.Body.Close()

	return nil
}

func (c *AzureClient) query(
	ctx context.Context,
	scopes []string,
	method,
	requestURL string,
	body interface{},
	res interface{},
) error {
	token, err := c.token.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: scopes,
	})
	if err != nil {
		return err
	}

	err = c.doRequest(ctx, method, requestURL, token.Token, res, body)
	if err != nil {
		return err
	}

	return nil
}

func (c *AzureClient) FromQueryBuilder(
	ctx context.Context,
	builder AzureQueryBuilder,
	path string,
	res interface{},
) error {
	err := c.query(ctx, graphReadScopes, http.MethodGet, builder.BuildBetaUrl(path), nil, res)
	if err != nil {
		return err
	}

	return nil
}
