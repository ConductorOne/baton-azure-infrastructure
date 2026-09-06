package client

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// graphAppRoleAssignedToPage is the shape Graph returns for a paged
// appRoleAssignedTo collection: a continuation alongside the values.
const graphAppRoleAssignedToPage = `{
  "@odata.context": "https://graph.microsoft.com/beta/$metadata#servicePrincipals('sp1')/appRoleAssignedTo",
  "@odata.nextLink": "https://graph.microsoft.com/beta/servicePrincipals/sp1/appRoleAssignedTo?$top=999&$skiptoken=OPAQUE",
  "value": [
    {"id": "a1", "appRoleId": "role-1", "principalId": "p1", "principalType": "User"},
    {"id": "a2", "appRoleId": "role-1", "principalId": "p2", "principalType": "Group"}
  ]
}`

// TestAppRoleAssignmentListCapturesNextLink is the regression guard for the bug
// this type exists to fix. ServicePrincipal has no field for the expanded
// collection's continuation, so a truncated page of assignments was
// indistinguishable from a complete one and grants silently halved. If the
// NextLink field or its tag is dropped, paging stops after page one and the
// truncation returns.
func TestAppRoleAssignmentListCapturesNextLink(t *testing.T) {
	var page AppRoleAssignmentList
	require.NoError(t, json.Unmarshal([]byte(graphAppRoleAssignedToPage), &page))

	assert.NotEmpty(t, page.NextLink, "continuation must survive unmarshal or paging stops after the first page")
	assert.Contains(t, page.NextLink, "$skiptoken=OPAQUE")

	require.Len(t, page.Assignments, 2)
	assert.Equal(t, "a1", page.Assignments[0].Id)
	assert.Equal(t, "role-1", page.Assignments[0].AppRoleId)
	assert.Equal(t, "p2", page.Assignments[1].PrincipalId)
}

// TestAppRoleAssignmentListLastPageHasNoNextLink pins the loop's termination
// condition: the final page must leave NextLink empty, or callers page forever.
func TestAppRoleAssignmentListLastPageHasNoNextLink(t *testing.T) {
	var page AppRoleAssignmentList
	require.NoError(t, json.Unmarshal([]byte(`{"value": [{"id": "a3", "appRoleId": "role-2"}]}`), &page))

	assert.Empty(t, page.NextLink)
	assert.Len(t, page.Assignments, 1)
}

func TestServicePrincipalAppRoleAssignedToURL(t *testing.T) {
	const host = "graph.microsoft.com"

	t.Run("first page targets the dedicated collection endpoint", func(t *testing.T) {
		got := servicePrincipalAppRoleAssignedToURL(NewAzureQueryBuilder(host), "sp1", "")

		assert.Contains(t, got, "/beta/servicePrincipals/sp1/appRoleAssignedTo",
			"grants must come from the collection endpoint, not an $expand on the principal")
		assert.NotContains(t, got, "$expand",
			"$expand is what truncated the collection; it must not reappear here")
		assert.Contains(t, got, "top=999")
	})

	t.Run("continuation is used verbatim so paging advances", func(t *testing.T) {
		next := "https://graph.microsoft.com/beta/servicePrincipals/sp1/appRoleAssignedTo?$top=999&$skiptoken=OPAQUE"

		got := servicePrincipalAppRoleAssignedToURL(NewAzureQueryBuilder(host), "sp1", next)

		assert.Equal(t, next, got,
			"rebuilding the URL from parameters would drop the skip token and restart at page one")
		assert.True(t, strings.Contains(got, "$skiptoken=OPAQUE"))
	})
}
