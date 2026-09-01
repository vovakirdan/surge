//go:build runtime_v2_pending

package vm_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// These are the two process-terminal P3 controls.  They live in the existing
// entitlement stand rather than in production: the Go test is the bounded
// parent, and the compiled lifecycle harness is the child whose exit and
// diagnostics it observes.
const lifecycleHarnessTaskEntitlementTerminalModes = `
#ifdef RT_TEST_SYNC_POINTS
static _Atomic uint32_t g_ent_terminal_attempts;

static void entitlement_terminal_attempt(const char* label) {
    unsigned attempt =
        atomic_fetch_add_explicit(&g_ent_terminal_attempts, 1, memory_order_acq_rel) + 1;
    fprintf(stderr, "p3-terminal %s: attempt=%u\n", label, attempt);
    fflush(stderr);
}

static void entitlement_user_panic_clone(void* dst, const void* src) {
    entitlement_terminal_attempt("user-panic");
#ifdef RV2_DEBT_305_USER_PANIC_RETURNS_NEGATIVE_CONTROL
    entitlement_clone(dst, src);
#else
    static const uint8_t message[] = "P3 user clone panic";
    (void)dst;
    (void)src;
    rt_panic(message, sizeof(message) - 1);
#endif
}

static void entitlement_alloc_refusal_clone(void* dst, const void* src) {
    entitlement_terminal_attempt("alloc-null");
    void* refused = rt_alloc(UINT64_MAX, _Alignof(entitlement_value));
    if (refused != NULL) {
        panic_msg("P3 allocation-refusal control was unexpectedly served");
    }
#ifdef RV2_DEBT_305_ALLOC_NULL_RETURNS_NEGATIVE_CONTROL
    entitlement_clone(dst, src);
#else
    static const uint8_t message[] = "could not allocate entitlement clone";
    (void)dst;
    (void)src;
    rt_fatal_static(RT_OOM, message, sizeof(message) - 1);
#endif
}

static const rt_value_ops entitlement_user_panic_value_ops = {
    .layout = {.size = sizeof(entitlement_value),
               .align = _Alignof(entitlement_value),
               .stride = sizeof(entitlement_value),
               .flags = RT_VALUE_FLAG_DROPPABLE | RT_VALUE_FLAG_CLONABLE},
    .move_init = entitlement_move,
    .copy_init = NULL,
    .clone_init = entitlement_user_panic_clone,
    .drop_in_place = entitlement_drop,
    .trace = NULL,
    .plan_cross = entitlement_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static const rt_value_ops entitlement_alloc_refusal_value_ops = {
    .layout = {.size = sizeof(entitlement_value),
               .align = _Alignof(entitlement_value),
               .stride = sizeof(entitlement_value),
               .flags = RT_VALUE_FLAG_DROPPABLE | RT_VALUE_FLAG_CLONABLE},
    .move_init = entitlement_move,
    .copy_init = NULL,
    .clone_init = entitlement_alloc_refusal_clone,
    .drop_in_place = entitlement_drop,
    .trace = NULL,
    .plan_cross = entitlement_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static int mode_entitlement_terminal_clone(rt_executor* ex,
                                           const rt_value_ops* ops,
                                           const char* label) {
    entitlement_reset();
    atomic_store_explicit(&g_ent_terminal_attempts, 0, memory_order_release);
    rt_task* target = entitlement_spawn_owner(ex, ops);
    if (target == NULL || !wait_task_status(target, TASK_DONE, 8000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("P3 terminal clone control: owner never completed");
    }
    void* sibling = rt_task_clone(target, ops->clone_init);
    if (sibling == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("P3 terminal clone control: sibling entitlement was not created");
    }

    uint8_t kind = 0;
    entitlement_value served;
    memset(&served, 0, sizeof(served));
    rt_task_await(target, &kind, &served);

    const char* answer = kind == 1 ? "Success" : (kind == 2 ? "Cancelled" : "Unknown");
    fprintf(stderr, "p3-terminal %s: answer=%s attempts=%u\n",
            label, answer,
            atomic_load_explicit(&g_ent_terminal_attempts, memory_order_acquire));
    if (kind == 1) {
        entitlement_drop(&served);
    }
    rt_task_handle_drop(sibling);
    (void)rt_executor_request_shutdown(ex);
    return fail("P3 terminal clone control: terminal callback returned and published an answer");
}

static int mode_entitlement_user_clone_panic(rt_executor* ex) {
    // After rt_panic terminates the child, this row claims neither unwind nor
    // destructor execution, exact drops/heap balance, nor a particular state
    // for the canonical slot, claim, or pin left in the dead process.
    return mode_entitlement_terminal_clone(
        ex, &entitlement_user_panic_value_ops, "user-panic");
}

static int mode_entitlement_alloc_null_fatal(rt_executor* ex) {
    // UINT64_MAX makes rt_alloc return NULL deterministically; this is not real
    // OOM.  The canonical task result is already READY before an entitlement
    // can clone it: "no result publication" here means no clone destination or
    // awaiter Success is published after the refused duplication, not that the
    // canonical result never existed.  After termination the row makes no
    // cleanup, balance, exact-drop, canonical-slot, claim, or pin assertion.
    return mode_entitlement_terminal_clone(
        ex, &entitlement_alloc_refusal_value_ops, "alloc-null");
}
#endif
`

const entitlementTerminalChildLimit = 5 * time.Second

func runEntitlementTerminalChild(
	t *testing.T, binPath, mode string,
) (stdout, stderr string, exitCode int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), entitlementTerminalChildLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, mode)
	cmd.Env = entitlementEnv("2", "")
	stdout, stderr, exitCode = runCommand(t, cmd, "")
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("P3 terminal child %q exceeded %s (retry spin or hang)\nstdout:\n%s\nstderr:\n%s",
			mode, entitlementTerminalChildLimit, stdout, stderr)
	}
	return stdout, stderr, exitCode
}

func assertEntitlementTerminalChild(
	t *testing.T, binPath, mode, label, reportText string,
) {
	t.Helper()
	stdout, stderr, exitCode := runEntitlementTerminalChild(t, binPath, mode)
	t.Logf("bounded terminal child %q: exit=%d stderr=%q", mode, exitCode, stderr)
	if exitCode != 1 {
		t.Fatalf("P3 terminal child %q exited %d, want Error/1\nstdout:\n%s\nstderr:\n%s",
			mode, exitCode, stdout, stderr)
	}
	markerPrefix := "p3-terminal " + label + ": attempt="
	if strings.Count(stderr, markerPrefix) != 1 ||
		!strings.Contains(stderr, markerPrefix+"1\n") {
		t.Fatalf("P3 terminal child %q made other than one attempt\nstdout:\n%s\nstderr:\n%s",
			mode, stdout, stderr)
	}
	if !strings.Contains(stderr, reportText) {
		t.Fatalf("P3 terminal child %q missed the ordinary panic report\nstdout:\n%s\nstderr:\n%s",
			mode, stdout, stderr)
	}
	if strings.Contains(stderr, "answer=") || strings.Contains(stderr, "Cancelled") {
		t.Fatalf("P3 terminal child %q returned or answered Cancelled\nstdout:\n%s\nstderr:\n%s",
			mode, stdout, stderr)
	}
}

func TestRuntimeV2TaskEntitlementTerminalControls(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessEntitlement(t, "")
	t.Run("user_clone_panic", func(t *testing.T) {
		assertEntitlementTerminalChild(
			t, binPath, "entitlement-user-clone-panic", "user-panic",
			"P3 user clone panic")
	})
	t.Run("allocator_null", func(t *testing.T) {
		assertEntitlementTerminalChild(
			t, binPath, "entitlement-alloc-null-fatal", "alloc-null",
			"[RT_OOM]: could not allocate entitlement clone")
	})
}

func TestRuntimeV2TaskEntitlementTerminalControlsNegativeControls(t *testing.T) {
	for _, tc := range []struct {
		name, control, mode, label string
	}{
		{"user_clone_panic_returns", "RV2_DEBT_305_USER_PANIC_RETURNS_NEGATIVE_CONTROL",
			"entitlement-user-clone-panic", "user-panic"},
		{"allocator_null_returns", "RV2_DEBT_305_ALLOC_NULL_RETURNS_NEGATIVE_CONTROL",
			"entitlement-alloc-null-fatal", "alloc-null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binPath := buildRuntimeV2LifecycleHarnessEntitlement(t, tc.control)
			stdout, stderr, exitCode := runEntitlementTerminalChild(t, binPath, tc.mode)
			t.Logf("Rule13 child %q with %s: exit=%d stderr=%q",
				tc.mode, tc.control, exitCode, stderr)
			if exitCode == 0 {
				t.Fatalf("P3 terminal negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			want := "p3-terminal " + tc.label + ": answer=Success attempts=1"
			if !strings.Contains(stderr, want) ||
				!strings.Contains(stderr, "terminal callback returned and published an answer") {
				t.Fatalf("P3 terminal negative control failed for the wrong reason (exit=%d)\nstdout:\n%s\nstderr:\n%s",
					exitCode, stdout, stderr)
			}
		})
	}
}
