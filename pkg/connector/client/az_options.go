package client

import (
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
)

type uhttpTransporterWrapper struct {
	client *uhttp.BaseHttpClient
}

type uhttpErrorResponseWrapper map[string]interface{}

func (u uhttpErrorResponseWrapper) Message() string {
	return fmt.Sprintf("response map %v", u)
}

func (c *uhttpTransporterWrapper) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req, uhttp.WithErrorResponse(&uhttpErrorResponseWrapper{}))
}

func (a *AzureClient) Options() azcore.ClientOptions {
	return policy.ClientOptions{
		Transport: &uhttpTransporterWrapper{client: a.httpClient},
	}
}

func (a *AzureClient) ArmOptions() *arm.ClientOptions {
	return &arm.ClientOptions{
		ClientOptions: a.Options(),
	}
}

func (a *AzureClient) AzBlobClient(serviceUrl string) (*azblob.Client, error) {
	return azblob.NewClient(serviceUrl, a.token, &azblob.ClientOptions{
		ClientOptions: a.Options(),
	})
}
