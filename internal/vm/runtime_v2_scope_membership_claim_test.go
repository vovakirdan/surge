//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// A wake is scheduling, never scope admission. The stand creates and starts a
// task before a fail-fast scope exists, then wakes it from inside that scope.
// The owner observes the empty scope drained before the foreign task is allowed
// to complete Cancelled. The fixed runtime keeps the answer unchanged and both
// defect counters at zero. The negative-control build restores only the two
// historical mistakes: wake rewrites provenance and an unregistered task may
// raise fail-fast. The same synchronization then produces both non-zero
// counters and a Cancelled owner, without relying on scheduler timing.
func TestRuntimeV2LifecycleScopeCreationProvenanceRejectsLateAdoption(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessScopeMembership(t, false)
	for _, threads := range []string{"2", "4", "8"} {
		t.Run("threads-"+threads, func(t *testing.T) {
			env := lifecycleEnv(
				"SURGE_TRACE_EXEC=1",
				"SURGE_SHARDS=1",
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "scope-membership-claim", env)
			if exitCode != 0 {
				t.Fatalf("scope-provenance proof failed at SURGE_THREADS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					threads, exitCode, stdout, stderr)
			}
			for _, want := range []string{
				"scope-provenance before: drained=1 failfast=0",
				"scope-provenance after: owner_kind=1 failfast=0 registered=0 creation_scope=0",
				"scope_identity_rewritten=0 scope_failfast_after_drained_answer=0",
			} {
				if !strings.Contains(stderr, want) {
					t.Fatalf("scope-provenance proof missing %q at SURGE_THREADS=%s\nstdout:\n%s\nstderr:\n%s",
						want, threads, stdout, stderr)
				}
			}
		})
	}
}

func TestRuntimeV2LifecycleScopeCreationProvenanceNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessScopeMembership(t, true)
	env := lifecycleEnv(
		"SURGE_TRACE_EXEC=1", "SURGE_SHARDS=1", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "scope-membership-claim", env)
	if exitCode == 0 {
		t.Fatalf("scope-provenance negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	for _, want := range []string{
		"scope-provenance before: drained=1 failfast=0",
		"scope-provenance after: owner_kind=2 failfast=1 registered=0",
		"scope_identity_rewritten=1 scope_failfast_after_drained_answer=1",
		"foreign task changed an already-drained scope",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("scope-provenance negative control missing %q (code=%d)\nstdout:\n%s\nstderr:\n%s",
				want, exitCode, stdout, stderr)
		}
	}
}

func buildRuntimeV2LifecycleHarnessScopeMembership(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_scope_provenance"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_SCOPE_PROVENANCE_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

// lifecycleHarnessScopeMembershipModes is concatenated into the shared native
// lifecycle harness. The legacy mode names remain only to avoid perturbing the
// shared dispatcher; both route to the creation-provenance proof.
const lifecycleHarnessScopeMembershipModes = `
#ifdef RT_TEST_SYNC_POINTS
#include "rt_scope_provenance_trace.h"

#define POLL_SCOPE_MEMBERSHIP_OWNER 4042
#define POLL_SCOPE_MEMBERSHIP_CHILD 4043

static _Atomic(void*) g_scope_provenance_foreign;
static _Atomic(void*) g_scope_provenance_handle;
static _Atomic uint32_t g_scope_provenance_foreign_running;
static _Atomic uint32_t g_scope_provenance_foreign_release;
static _Atomic uint32_t g_scope_provenance_first_join;
static _Atomic uint32_t g_scope_provenance_owner_finish;
static _Atomic uint32_t g_scope_provenance_first_failfast;
static _Atomic uint32_t g_scope_provenance_last_failfast;

static void poll_scope_membership_child(void) {
    atomic_store_explicit(&g_scope_provenance_foreign_running, 1, memory_order_release);
    while (atomic_load_explicit(&g_scope_provenance_foreign_release, memory_order_acquire) == 0) {
        sleep_us(1000);
    }
    rt_async_return_cancelled(NULL, 0);
}

static void poll_scope_membership_owner(void) {
    void* handle = rt_scope_enter(true);
    atomic_store_explicit(&g_scope_provenance_handle, handle, memory_order_release);
    rt_task_wake(atomic_load_explicit(&g_scope_provenance_foreign, memory_order_acquire));

    uint64_t pending = 0;
    bool failfast = true;
    bool drained = rt_scope_join_all(handle, &pending, &failfast);
    atomic_store_explicit(&g_scope_provenance_first_failfast,
                          failfast ? 1U : 0U,
                          memory_order_release);
    atomic_store_explicit(&g_scope_provenance_first_join, drained ? 1U : 2U, memory_order_release);

    while (atomic_load_explicit(&g_scope_provenance_owner_finish, memory_order_acquire) == 0) {
        sleep_us(1000);
    }
    pending = 0;
    failfast = false;
    drained = rt_scope_join_all(handle, &pending, &failfast);
    atomic_store_explicit(&g_scope_provenance_last_failfast,
                          failfast ? 1U : 0U,
                          memory_order_release);
    if (!drained) {
        rt_async_return_cancelled(NULL, 0);
        return;
    }
    rt_scope_exit(handle);
    if (failfast) {
        rt_async_return_cancelled(NULL, 0);
        return;
    }
    rt_async_return(NULL, &(uint64_t){0});
}

static int mode_scope_membership_claim(rt_executor* ex) {
    atomic_store_explicit(&g_scope_provenance_foreign, NULL, memory_order_release);
    atomic_store_explicit(&g_scope_provenance_handle, NULL, memory_order_release);
    atomic_store_explicit(&g_scope_provenance_foreign_running, 0, memory_order_release);
    atomic_store_explicit(&g_scope_provenance_foreign_release, 0, memory_order_release);
    atomic_store_explicit(&g_scope_provenance_first_join, 0, memory_order_release);
    atomic_store_explicit(&g_scope_provenance_owner_finish, 0, memory_order_release);
    atomic_store_explicit(&g_scope_provenance_first_failfast, 0, memory_order_release);
    atomic_store_explicit(&g_scope_provenance_last_failfast, 0, memory_order_release);
    rt_scope_provenance_trace_reset();

    rt_task* foreign = spawn_child_for_stand(ex, POLL_SCOPE_MEMBERSHIP_CHILD, 0);
    if (foreign == NULL) {
        return fail("scope-provenance stand: foreign allocation failed");
    }
    atomic_store_explicit(&g_scope_provenance_foreign, foreign, memory_order_release);
    if (!wait_u32_at_least(&g_scope_provenance_foreign_running, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-provenance stand: foreign task never started");
    }

    rt_task* owner = spawn_child_for_stand(ex, POLL_SCOPE_MEMBERSHIP_OWNER, 0);
    if (owner == NULL || !wait_u32_at_least(&g_scope_provenance_first_join, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-provenance stand: owner never observed its scope");
    }
    unsigned first_join =
        atomic_load_explicit(&g_scope_provenance_first_join, memory_order_acquire);
    unsigned first_failfast =
        atomic_load_explicit(&g_scope_provenance_first_failfast, memory_order_acquire);
    fprintf(stderr,
            "scope-provenance before: drained=%u failfast=%u\n",
            first_join == 1U ? 1U : 0U,
            first_failfast);
    if (first_join != 1U || first_failfast != 0U) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-provenance stand: empty scope did not first answer drained");
    }

    atomic_store_explicit(&g_scope_provenance_foreign_release, 1, memory_order_release);
    if (!wait_task_status(foreign, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-provenance stand: foreign task never completed");
    }
    atomic_store_explicit(&g_scope_provenance_owner_finish, 1, memory_order_release);
    if (!wait_task_status(owner, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("scope-provenance stand: owner never completed");
    }

    uint8_t owner_kind = 0;
    uint64_t bits = 0;
    rt_task_await(owner, &owner_kind, &bits);
    unsigned last_failfast =
        atomic_load_explicit(&g_scope_provenance_last_failfast, memory_order_acquire);
    uint64_t identity_rewritten = rt_scope_identity_rewritten_total();
    uint64_t failfast_after_drained = rt_scope_failfast_after_drained_answer_total();
    fprintf(stderr,
            "scope-provenance after: owner_kind=%u failfast=%u registered=%u creation_scope=%llu\n",
            (unsigned)owner_kind,
            last_failfast,
            (unsigned)foreign->scope_registered,
            (unsigned long long)foreign->creation_scope_key.id);
    fprintf(stderr,
            "scope_identity_rewritten=%llu scope_failfast_after_drained_answer=%llu\n",
            (unsigned long long)identity_rewritten,
            (unsigned long long)failfast_after_drained);
    (void)rt_executor_request_shutdown(ex);
    if (owner_kind != 1 || last_failfast != 0 || foreign->scope_registered != 0 ||
        waker_valid(foreign->creation_scope_key) || identity_rewritten != 0 ||
        failfast_after_drained != 0) {
        return fail("scope-provenance stand: foreign task changed an already-drained scope");
    }
    return 0;
}

static int mode_scope_membership_completed_before_registration(rt_executor* ex) {
    return mode_scope_membership_claim(ex);
}
#endif
`
