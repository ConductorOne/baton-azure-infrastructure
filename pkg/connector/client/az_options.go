package client

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"net/http"
)

type uhttpTransporterWrapper struct {
	client *uhttp.BaseHttpClient
}

func (c *uhttpTransporterWrapper) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

func (a *AzureClient) Options() azcore.ClientOptions {
	return policy.ClientOptions{
		Transport: &uhttpTransporterWrapper{client: a.httpClient},
	}
}

func (a *AzureClient) AzBlobClient(serviceUrl string) (*azblob.Client, error) {
	return azblob.NewClient(serviceUrl, a.token, &azblob.ClientOptions{
		ClientOptions: a.Options(),
	})
}
