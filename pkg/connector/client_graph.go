package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
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

func (c *Connector) buildBetaURL(reqPath string, v url.Values) string {
	ux := url.URL{
		Scheme:   "https",
		Host:     apiDomain,
		Path:     path.Join(betaVersion, reqPath),
		RawQuery: v.Encode(),
	}
	return ux.String()
}

func (c *Connector) query(ctx context.Context, scopes []string, method string, requestURL string, body io.Reader, res interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return err
	}

	token, err := c.token.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: scopes,
	})
	if err != nil {
		return err
	}

	req.Header["Authorization"] = []string{fmt.Sprintf("Bearer %s", token.Token)}
	if body != nil {
		req.Header["Content-Type"] = []string{"application/json"}
	}
	// Needed to get certain filters working
	req.Header["ConsistencyLevel"] = []string{"eventual"}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("microsoft-entra: failed to send request: %w", err)
	}
	defer resp.Body.Close()

	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("microsoft-entra: failed to read response body: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("microsoft-entra: %s '%s' %w", method, requestURL, ErrNotFound)
	}

	if res != nil && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusCreated {
		return newHTTPError(resp, string(rawResp), ErrNoResponse)
	}

	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusNoContent &&
		resp.StatusCode != http.StatusCreated {
		return newHTTPError(resp, string(rawResp), ErrRequestFailed)
	}

	if res != nil {
		if err := json.Unmarshal(rawResp, res); err != nil {
			return err
		}
	}

	return nil
}
