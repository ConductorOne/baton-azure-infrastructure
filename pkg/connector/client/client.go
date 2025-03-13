package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
)

var ValidHosts = []string{
	"graph.microsoft.com",
	"graph.microsoft.us",
	"dod-graph.microsoft.us",
	"graph.microsoft.de",
	"microsoftgraph.chinacloudapi.cn",
	"canary.graph.microsoft.com",
}

var graphReadScopes = []string{
	"https://graph.microsoft.com/.default",
}

type AzureClient struct {
	token         azcore.TokenCredential
	httpClient    *uhttp.BaseHttpClient
	clientFactory *armsubscription.ClientFactory
	skipAdGroups  bool
	graphDomain   string
}

func NewAzureClient(
	ctx context.Context,
	httpClient *http.Client,
	token azcore.TokenCredential,
	skipAdGroups bool,
	graphDomain string,
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
		graphDomain:   graphDomain,
		skipAdGroups:  skipAdGroups,
	}, nil
}

func (a *AzureClient) doRequest(ctx context.Context,
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

	req, err := a.httpClient.NewRequest(ctx,
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

	resp, err := a.httpClient.Do(req, opts...)
	if err != nil {
		return err
	}

	resp.Body.Close()

	return nil
}

func (a *AzureClient) requestWithToken(
	ctx context.Context,
	scopes []string,
	method,
	requestURL string,
	body interface{},
	res interface{},
) error {
	token, err := a.token.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: scopes,
	})
	if err != nil {
		return err
	}

	err = a.doRequest(ctx, method, requestURL, token.Token, res, body)
	if err != nil {
		return err
	}

	return nil
}

func (a *AzureClient) FromPath(
	ctx context.Context,
	path string,
	res interface{},
) error {
	err := a.requestWithToken(ctx, graphReadScopes, http.MethodGet, path, nil, res)
	if err != nil {
		return err
	}

	return nil
}

func (a *AzureClient) QueryBuilder() *AzureQueryBuilder {
	return NewAzureQueryBuilder(a.graphDomain)
}
