package client

import (
	"net/url"
	"path"
)

type AzureQueryBuilder map[string][]string

func NewAzureQueryBuilder() AzureQueryBuilder {
	return AzureQueryBuilder{}
}

func (q AzureQueryBuilder) Add(key string, value string) AzureQueryBuilder {
	if _, ok := q[key]; !ok {
		q[key] = []string{}
	}
	q[key] = append(q[key], value)

	return q
}

func (q AzureQueryBuilder) BuildBetaUrl(reqPath string) string {
	values := url.Values{}
	for key, value := range q {
		values[key] = value
	}

	ux := url.URL{
		Scheme:   "https",
		Host:     apiDomain,
		Path:     path.Join(betaVersion, reqPath),
		RawQuery: values.Encode(),
	}
	return ux.String()
}
