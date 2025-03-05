package client

import (
	"net/url"
	"path"
)

type AzureApiVersion string

const (
	V1   = "v1.0"
	Beta = "beta"
)

type AzureQueryBuilder struct {
	params     map[string][]string
	apiVersion AzureApiVersion
}

func NewAzureQueryBuilder() *AzureQueryBuilder {
	return &AzureQueryBuilder{
		params:     map[string][]string{},
		apiVersion: V1,
	}
}

func (q *AzureQueryBuilder) Version(version AzureApiVersion) *AzureQueryBuilder {
	q.apiVersion = version

	return q
}

func (q *AzureQueryBuilder) Add(key string, value string) *AzureQueryBuilder {
	if _, ok := q.params[key]; !ok {
		q.params[key] = []string{}
	}
	q.params[key] = append(q.params[key], value)

	return q
}

func (q *AzureQueryBuilder) BuildUrl(reqPaths ...string) string {
	values := url.Values{}
	for key, value := range q.params {
		values[key] = value
	}

	urls := []string{string(q.apiVersion)}
	urls = append(urls, reqPaths...)

	ux := url.URL{
		Scheme:   "https",
		Host:     apiDomain,
		Path:     path.Join(urls...),
		RawQuery: values.Encode(),
	}
	return ux.String()
}

func (q *AzureQueryBuilder) BuildUrlWithPagination(reqPath string, nextLink string) string {
	if nextLink != "" {
		return nextLink
	}

	values := url.Values{}
	for key, value := range q.params {
		values[key] = value
	}

	ux := url.URL{
		Scheme:   "https",
		Host:     apiDomain,
		Path:     path.Join(string(q.apiVersion), reqPath),
		RawQuery: values.Encode(),
	}
	return ux.String()
}
