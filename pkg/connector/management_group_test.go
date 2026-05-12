package connector

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups"
)

// TestConnector_managementGroups_MemoizesResult pins the memoization
// contract: calling (*Connector).managementGroups twice returns the same
// cached slice without re-running the underlying load. We preseed the
// cache via mgmtGroupsOnce so the test doesn't need a live Azure client;
// the behavior being verified is that once mgmtGroupsOnce has fired, the
// method returns the cached value unchanged.
func TestConnector_managementGroups_MemoizesResult(t *testing.T) {
	seed := []*armmanagementgroups.ManagementGroupInfo{
		{ID: ptr("/providers/Microsoft.Management/managementGroups/tenant-root")},
		{ID: ptr("/providers/Microsoft.Management/managementGroups/c1connectors-root")},
	}

	c := &Connector{}
	// Fire the Once with our preseeded cache + no error. Subsequent
	// managementGroups(ctx) calls must return this exact slice.
	c.mgmtGroupsOnce.Do(func() {
		c.mgmtGroupsCache = seed
		c.mgmtGroupsErr = nil
	})

	ctx := context.Background()
	got1, err1 := c.managementGroups(ctx)
	if err1 != nil {
		t.Fatalf("first call returned err: %v", err1)
	}
	got2, err2 := c.managementGroups(ctx)
	if err2 != nil {
		t.Fatalf("second call returned err: %v", err2)
	}

	// Pointer identity: both calls must return the exact same slice
	// backing array (not a copy, not a fresh load).
	if len(got1) != len(got2) {
		t.Fatalf("memoized slice length diverged across calls: %d vs %d", len(got1), len(got2))
	}
	if &got1[0] != &got2[0] {
		t.Errorf("expected identical backing slice on repeated calls; second call returned a different pointer")
	}
	if len(got1) != len(seed) {
		t.Errorf("expected seeded length %d, got %d", len(seed), len(got1))
	}
}

// TestConnector_managementGroups_PropagatesError pins that once the Once
// fires with an error, every subsequent call returns that same error.
// This is how ensureHierarchy / listInit distinguish "fail the sync"
// from "degrade gracefully" — the propagated error is actionable.
func TestConnector_managementGroups_PropagatesError(t *testing.T) {
	sentinelErr := errors.New("simulated management-groups load failure")

	c := &Connector{}
	c.mgmtGroupsOnce.Do(func() {
		c.mgmtGroupsCache = nil
		c.mgmtGroupsErr = sentinelErr
	})

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		got, err := c.managementGroups(ctx)
		if !errors.Is(err, sentinelErr) {
			t.Errorf("call %d: expected propagated error %v, got %v", i, sentinelErr, err)
		}
		if got != nil {
			t.Errorf("call %d: expected nil slice on error, got len=%d", i, len(got))
		}
	}
}
