package connector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	uhttp "github.com/conductorone/baton-sdk/pkg/uhttp"
)

const (
	apiDomain                   = "graph.microsoft.com"
	apiVersion                  = "v1.0"
	betaVersion                 = "beta"
	microsoftBuiltinAppsOwnerID = "f8cdef31-a31e-4b4a-93e4-5f571e91255a"
)

var (
	ErrNotFound               = errors.New("microsoft-entra-client: 404 not found")
	ErrNoResponse             = errors.New("HTTP request failed to supply a value")
	ErrRequestFailed          = errors.New("HTTP request failed")
	ErrFailedToParseRateLimit = errors.New("failed to parse rate limit")

	// https://learn.microsoft.com/en-us/graph/api/resources/approleassignment?view=graph-rest-1.0
	//
	//  	The identifier (id) for the app role which is assigned to the principal. This app role must be
	//		exposed in the appRoles property on the resource application's service principal (resourceId).
	//		If the resource application has not declared any app roles, a default app role ID of
	//		00000000-0000-0000-0000-000000000000 can be specified to signal that the principal is assigned
	//		to the resource app without any specific app roles. Required on create
	//
	defaultAppRoleAssignmentID = "00000000-0000-0000-0000-000000000000"
)

type HTTPError struct {
	StatusCode  int
	RawResponse string
	RetryAfter  int
	RateLimited bool
	Err         error
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %d %s", e.Err, e.StatusCode, e.RawResponse)
}

func newHTTPError(resp *http.Response, rawResponse string, err error) *HTTPError {
	retryAfter := 0
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter, err = strconv.Atoi(resp.Header.Get("Retry-After"))
		if err != nil {
			err = fmt.Errorf("%w: %w", err, ErrFailedToParseRateLimit)
		}
	}

	return &HTTPError{
		StatusCode:  resp.StatusCode,
		RawResponse: rawResponse,
		Err:         err,
		RetryAfter:  retryAfter,
		RateLimited: resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusGatewayTimeout,
	}
}

func (c *Connector) buildURL(reqPath string, v url.Values) string {
	ux := url.URL{
		Scheme:   "https",
		Host:     apiDomain,
		Path:     path.Join(apiVersion, reqPath),
		RawQuery: v.Encode(),
	}
	return ux.String()
}

func WithBearerToken(token string) uhttp.RequestOption {
	return uhttp.WithHeader("Authorization", "Bearer "+token)
}

func (c *Connector) buildBetaURL(reqPath string, v url.Values) string {
	ux := url.URL{
		Scheme:   "https",
		Host:     apiDomain,
		Path:     path.Join(betaVersion, reqPath),
		RawQuery: v.Encode(),
	}
	return ux.String()
}

func (c *Connector) doRequest(ctx context.Context,
	method,
	endpointUrl string,
	token string,
	res interface{},
	body interface{},
) (annotations.Annotations, error) {

	urlAddress, err := url.Parse(endpointUrl)
	if err != nil {
		return nil, err
	}
	req, err := c.httpClient.NewRequest(ctx,
		method,
		urlAddress,
		WithBearerToken(token),
		uhttp.WithHeader("ConsistencyLevel", "eventual"),
		uhttp.WithContentTypeJSONHeader(),
		uhttp.WithJSONBody(body),
	)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req, uhttp.WithResponse(res))
	if resp != nil {
		defer resp.Body.Close()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resMessage, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("microsoft-azure-infrastructure: failed to read response body: %w", err)
		}
		return nil, fmt.Errorf("microsoft-azure-infrastructure: %s, '%s', %w: %s", method, urlAddress.String(), ErrRequestFailed, string(resMessage))
	}

	if err != nil {
		return nil, err
	}

	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("microsoft-azure-infrastructure: failed to read response body: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("microsoft-azure-infrastructure: %s '%s' %w", method, urlAddress.String(), ErrNotFound)
	}

	if res != nil && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusCreated {
		return nil, newHTTPError(resp, string(rawResp), ErrNoResponse)
	}

	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusNoContent &&
		resp.StatusCode != http.StatusCreated {
		return nil, newHTTPError(resp, string(rawResp), ErrRequestFailed)
	}

	return nil, nil
}

func (c *Connector) query(ctx context.Context, scopes []string, method, requestURL string, body interface{}, res interface{}) error {
	token, err := c.token.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: scopes,
	})
	if err != nil {
		return err
	}

	_, err = c.doRequest(ctx, method, requestURL, token.Token, res, body)
	if err != nil {
		return err
	}

	return nil
}
