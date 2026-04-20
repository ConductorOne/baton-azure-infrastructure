//go:build live

package connector

// TestLiveGrantRevoke exercises the roleAssignmentBuilder's Grant / Revoke
// against a live Azure tenant. The test is disabled in CI (`//go:build live`)
// and only runs when explicitly enabled — a caller that can clean up after
// itself and has the right permissions should wire it up. Configured via env:
//
//	BATON_AZURE_TENANT_ID
//	BATON_AZURE_CLIENT_ID
//	BATON_AZURE_CLIENT_SECRET
//	AZURE_SUBSCRIPTION_ID
//	AZURE_LIVE_TEST_RG        — resource group name (scope target)
//	AZURE_LIVE_TEST_PRINCIPAL — principal object ID (will be granted/revoked)
//	AZURE_LIVE_TEST_ROLE_UUID — role UUID (e.g. Reader = acdd72a7-3385-48ef-bd42-f606fba81ae7)
//
// The test: Grant → expect no error, no annotation. Grant again → expect
// GrantAlreadyExists. Revoke → expect no error, no annotation. Revoke again
// → expect GrantAlreadyRevoked. Idempotency contract verified end-to-end.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

func TestLiveGrantRevoke(t *testing.T) {
	tenantID := mustEnv(t, "BATON_AZURE_TENANT_ID")
	clientID := mustEnv(t, "BATON_AZURE_CLIENT_ID")
	clientSecret := mustEnv(t, "BATON_AZURE_CLIENT_SECRET")
	subID := mustEnv(t, "AZURE_SUBSCRIPTION_ID")
	rgName := mustEnv(t, "AZURE_LIVE_TEST_RG")
	principalID := mustEnv(t, "AZURE_LIVE_TEST_PRINCIPAL")
	roleUUID := mustEnv(t, "AZURE_LIVE_TEST_ROLE_UUID")

	ctx := context.Background()
	cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
	if err != nil {
		t.Fatalf("build credential: %v", err)
	}
	conn, err := NewConnectorFromToken(
		ctx,
		http.DefaultClient,
		cred,
		false,                 // mailboxSettings
		false,                 // skipAdGroups
		"graph.microsoft.com", // graphDomain
		false,                 // skipUnusedRoles
		true,                  // skipStorageContainerSync
		true,                  // skipEntraIDP2LicenseFeatures
	)
	if err != nil {
		t.Fatalf("build connector: %v", err)
	}
	b := newRoleAssignmentBuilder(conn)

	// Synthesize a binding resource with a ScopeBindingTrait the way a real
	// sync would emit it. Scope ID = bare RG name (matches resource_group
	// builder). Role ID = composite "<uuid>:<sub>" (matches role builder).
	roleResID, err := rs.NewResourceID(roleResourceType, fmt.Sprintf("%s:%s", roleUUID, subID))
	if err != nil {
		t.Fatalf("build role resource id: %v", err)
	}
	scopeResID, err := rs.NewResourceID(resourceGroupResourceType, rgName)
	if err != nil {
		t.Fatalf("build scope resource id: %v", err)
	}
	bindingID, err := rs.NewResourceID(roleAssignmentResourceType, fmt.Sprintf("test-binding@%s", principalID))
	if err != nil {
		t.Fatalf("build binding id: %v", err)
	}
	binding, err := rs.NewScopeBindingResource(
		"Test binding",
		roleAssignmentResourceType,
		bindingID.Resource,
		[]rs.ScopeBindingTraitOption{
			rs.WithRoleScopeRoleId(roleResID),
			rs.WithRoleScopeResourceId(scopeResID),
		},
	)
	if err != nil {
		t.Fatalf("NewScopeBindingResource: %v", err)
	}

	principal := &v2.Resource{
		Id: &v2.ResourceId{
			ResourceType: enterpriseApplicationResourceType.Id,
			Resource:     principalID,
		},
	}
	ent := &v2.Entitlement{
		Id:       fmt.Sprintf("%s:%s", binding.Id.Resource, roleAssignmentAssignedEntitlementSlug),
		Resource: binding,
		Slug:     roleAssignmentAssignedEntitlementSlug,
	}

	// 1. Grant.
	anns, err := b.Grant(ctx, principal, ent)
	if err != nil {
		t.Fatalf("Grant#1 returned error: %v", err)
	}
	if hasAlreadyExists(anns) {
		t.Fatalf("Grant#1 unexpectedly returned GrantAlreadyExists — assignment should not have pre-existed; check test env state")
	}
	t.Logf("Grant#1: success")

	// 2. Grant again — idempotent, expect GrantAlreadyExists annotation.
	anns, err = b.Grant(ctx, principal, ent)
	if err != nil {
		t.Fatalf("Grant#2 returned error (expected nil + annotation): %v", err)
	}
	if !hasAlreadyExists(anns) {
		t.Fatalf("Grant#2 did not return GrantAlreadyExists annotation")
	}
	t.Logf("Grant#2 (duplicate): GrantAlreadyExists ✓")

	// 3. Build a grant to pass to Revoke. The binding resource IS the
	// entitlement's resource; the grant connects principal -> binding.
	g := &v2.Grant{
		Id:          fmt.Sprintf("%s:%s:%s", binding.Id.Resource, roleAssignmentAssignedEntitlementSlug, principalID),
		Entitlement: ent,
		Principal:   principal,
	}

	// 4. Revoke — expect no error, no annotation (assignment was there, now gone).
	anns, err = b.Revoke(ctx, g)
	if err != nil {
		t.Fatalf("Revoke#1 returned error: %v", err)
	}
	if hasAlreadyRevoked(anns) {
		t.Fatalf("Revoke#1 unexpectedly returned GrantAlreadyRevoked — assignment should have existed")
	}
	t.Logf("Revoke#1: success")

	// 5. Revoke again — idempotent, expect GrantAlreadyRevoked annotation.
	anns, err = b.Revoke(ctx, g)
	if err != nil {
		t.Fatalf("Revoke#2 returned error (expected nil + annotation): %v", err)
	}
	if !hasAlreadyRevoked(anns) {
		t.Fatalf("Revoke#2 did not return GrantAlreadyRevoked annotation")
	}
	t.Logf("Revoke#2 (missing): GrantAlreadyRevoked ✓")
}

func mustEnv(t *testing.T, key string) string {
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("skipping live test: %s not set", key)
	}
	return v
}

func hasAlreadyExists(anns annotations.Annotations) bool {
	return anns.Contains(&v2.GrantAlreadyExists{})
}
func hasAlreadyRevoked(anns annotations.Annotations) bool {
	return anns.Contains(&v2.GrantAlreadyRevoked{})
}
