//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// The three deterministic rows for a task's canonical result and the
// entitlements that may ask for it. Each holds ONE window open and performs one
// racing action there; each has a negative-control build that removes the guard
// the row is about and must be seen failing at the same window rather than
// timing out.
//
// The window two of them share is SP_CLONE_READER_OUT_OF_LOCK: a take has been
// decided CLONE, the reader is counted into clone_readers, the owner shard lock
// is gone, and the duplication reads the canonical value where it lies. That is
// the only instant at which a claim is OUT, and everything the counts promise
// -- the value does not move, the value is not destroyed -- has to hold across
// exactly it.

func entitlementEnv(threads string, points string) []string {
	return lifecycleEnv(
		"SURGE_SHARDS=1",
		"SURGE_THREADS="+threads,
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT="+points)
}

const entitlementCloneReaderPoint = "SP_CLONE_READER_OUT_OF_LOCK:block"

// Shutdown does not drop a canonical result some entitlement can still consume.
//
// The reclaim that destroys a task's result runs when the LAST reference to a
// DONE task goes, and an asker inside a take is holding one of those references
// -- that reference is the canonical value's pin. Shutdown is not an exception:
// it stops new entitlements and lets claimed work finish before the slot goes.
// The drive holds a clone reader out of lock, then asks the executor to stop
// and lets a sibling entitlement go, and the reader must still be handed a
// whole value afterwards.
func TestRuntimeV2TaskEntitlementShutdownDoesNotDropAClaimedCanonical(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessEntitlement(t, "")
	for _, threads := range []string{"2", "4"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "entitlement-shutdown-vs-claimed-clone",
				entitlementEnv(threads, entitlementCloneReaderPoint))
			if exitCode != 0 {
				t.Fatalf("shutdown-vs-claimed-clone proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			// Non-vacuity: the window was reached (only a take decided CLONE
			// reaches it) and nothing destroyed the canonical value there.
			if !strings.Contains(stderr, "entitlement shutdown window: canonical_drops=0") {
				t.Fatalf("shutdown-vs-claimed-clone proof never observed the claim held across the shutdown\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			// And the cohort's cost is the one §10 asks for: one duplication
			// for the extra asker, one move for the last, two drops in all.
			if !strings.Contains(stderr, "duplications=1 drops=2 double_drops=0") {
				t.Fatalf("shutdown-vs-claimed-clone proof did not end with one duplication and one move\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2TaskEntitlementShutdownUnpinnedCanonicalNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessEntitlement(
		t, "RV2_SHUTDOWN_UNPINNED_CANONICAL_NEGATIVE_CONTROL")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "entitlement-shutdown-vs-claimed-clone",
		entitlementEnv("2", entitlementCloneReaderPoint))
	if exitCode == 0 {
		t.Fatalf("shutdown-unpinned-canonical negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	// The window must be built the same way and the destruction must land IN
	// it: one drop of the canonical value while the claim is still out.
	if !strings.Contains(stderr, "entitlement shutdown window: canonical_drops=1") {
		t.Fatalf("shutdown-unpinned-canonical negative control did not destroy the canonical at the window (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	const want = "the canonical result was destroyed while a claimed clone reader was still out"
	if !strings.Contains(stderr, want) {
		t.Fatalf("shutdown-unpinned-canonical negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// A cancel that arrives after the task's answer is committed does not revoke
// results that are already available.
//
// `cancel` through any live handle is task-global and idempotent, never
// entitlement-local. Once the task has published its value and gone DONE there
// is no per-handle answer left for a cancel to write, and emptying the slot
// would leave whichever sibling was already served holding what the others are
// told does not exist. The drive holds a clone reader out of lock and cancels
// through a sibling entitlement there; SP_CANCEL_AT_COMMITTED_RESULT is the
// witness that the cancel really did arrive at a task with a published result.
func TestRuntimeV2TaskEntitlementCancelDoesNotRevokeACommittedResult(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessEntitlement(t, "")
	for _, threads := range []string{"2", "4"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "entitlement-cancel-vs-committed-result",
				entitlementEnv(threads, entitlementCloneReaderPoint))
			if exitCode != 0 {
				t.Fatalf("cancel-vs-committed-result proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			if !strings.Contains(stderr,
				"entitlement cancel window: at_committed_result=1 canonical_drops=0") {
				t.Fatalf("cancel-vs-committed-result proof did not land a cancel on a committed result\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			// Three entitlements, three independent values, and no answer of
			// Cancelled anywhere: two duplications, one move, three drops.
			if !strings.Contains(stderr, "duplications=2 drops=3 double_drops=0") {
				t.Fatalf("cancel-vs-committed-result proof did not serve every entitlement its own value\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2TaskEntitlementCancelRevokesCommittedResultNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessEntitlement(
		t, "RV2_CANCEL_REVOKES_COMMITTED_RESULT_NEGATIVE_CONTROL")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "entitlement-cancel-vs-committed-result",
		entitlementEnv("2", entitlementCloneReaderPoint))
	if exitCode == 0 {
		t.Fatalf("cancel-revokes-committed-result negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if !strings.Contains(stderr,
		"entitlement cancel window: at_committed_result=1 canonical_drops=1") {
		t.Fatalf("cancel-revokes-committed-result negative control did not revoke at the window (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	const want = "a cancel revoked a result the task had already committed"
	if !strings.Contains(stderr, want) {
		t.Fatalf("cancel-revokes-committed-result negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// A result capability minted for one occupant of a slot cannot be spent on the
// next one in the same storage.
//
// The capability names a result by four integers, and the slot's own generation
// is the one that says WHICH occupant: bytes get rebound and refilled, so a
// holder that arrives late must be told "gone" rather than handed whoever moved
// in. The drive holds the late holder at the moment the slot is about to be
// asked, destroys the occupant the capability named, rebinds the same bytes and
// publishes a different value into them, and then lets the holder ask.
func TestRuntimeV2TaskEntitlementStaleCapabilityCannotReachReusedStorage(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessEntitlement(t, "")
	for _, threads := range []string{"2", "4"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "entitlement-stale-result-capability",
				entitlementEnv(threads, "SP_RESULT_CAPABILITY_BEFORE_MATCH:block"))
			if exitCode != 0 {
				t.Fatalf("stale-capability proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			// Non-vacuity: the slot really did move on under the held holder --
			// the first occupant was destroyed once and a second one is there.
			if !strings.Contains(stderr,
				"entitlement capability window: rebind_drops=1 late_taken=0 second_occupant_ready=1") {
				t.Fatalf("stale-capability proof did not rebind the slot under the held holder\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			if !strings.Contains(stderr, "drops=2 double_drops=0") {
				t.Fatalf("stale-capability proof did not destroy each occupant exactly once\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2TaskEntitlementStaleResultGenerationNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessEntitlement(
		t, "RV2_STALE_RESULT_GENERATION_NEGATIVE_CONTROL")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "entitlement-stale-result-capability",
		entitlementEnv("2", "SP_RESULT_CAPABILITY_BEFORE_MATCH:block"))
	if exitCode == 0 {
		t.Fatalf("stale-result-generation negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	// Same window, same rebind, and the late holder is handed the occupant it
	// never named.
	if !strings.Contains(stderr,
		"entitlement capability window: rebind_drops=1 late_taken=1 second_occupant_ready=0") {
		t.Fatalf("stale-result-generation negative control did not build the window (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	const want = "a capability minted for one occupant was spent on the next one in the same storage"
	if !strings.Contains(stderr, want) {
		t.Fatalf("stale-result-generation negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// buildRuntimeV2LifecycleHarnessEntitlement builds the shared lifecycle harness
// with the sync points armed and, for a negative control, with exactly one
// guard removed. An empty control builds the tree as it stands.
func buildRuntimeV2LifecycleHarnessEntitlement(t *testing.T, control string) string {
	t.Helper()
	name := "lifecycle_harness_entitlement"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if control != "" {
		name += "_" + strings.ToLower(control)
		flags = append(flags, "-D"+control)
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}
