//go:build runtime_v2_pending

package vm_test

import (
	"strings"
	"testing"
)

// The cancellation rows of the blocking-job ownership matrix
// (docs/runtime-v2-epics/23b-inline-storage-and-typed-carriers.md, section 7),
// driven deterministically against the blocking pool.
//
// A blocking job's captured state is one `rt_value_cell` that the worker marks
// SPENT immediately before the body runs and that the last release destroys
// through the state's own descriptor: initialized, it is walked and freed;
// spent, only freed. Every row here submits a state whose drop COUNTS -- and
// reports a second visit rather than freeing twice -- so what the release did
// is a number, and holds a worker at the window the row is about so the cancel
// lands where the row says it does. That is the gap these close: the two
// negative-control toggles of the release path compiled, and nothing observed
// them.
//
// Stale generation has no row, deliberately. The job is reference counted --
// one reference for the awaiting task, one for the pool -- and nothing frees it
// until both have released, so no late event can meet reused storage under
// the job's address; there is no generation to check because there is no
// window for one to guard.

func buildRuntimeV2LifecycleHarnessDebt080(t *testing.T, control string) string {
	t.Helper()
	name := "lifecycle_harness_debt080"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if control != "" {
		name += "_" + strings.ToLower(control)
		flags = append(flags, "-D"+control)
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

func debt080Env(syncPoint string) []string {
	values := []string{"SURGE_SHARDS=1", "SURGE_THREADS=2", "SURGE_BLOCKING_THREADS=1"}
	if syncPoint != "" {
		values = append(values, "SURGE_SYNC_POINT="+syncPoint+":block")
	}
	return lifecycleEnv(values...)
}

func runDebt080Row(t *testing.T, binPath, mode, syncPoint string) (string, string, int) {
	t.Helper()
	return runLifecycleHarness(t, binPath, mode, debt080Env(syncPoint))
}

// Cancel before claim: the worker has popped the job and is held before it
// reads the status. The cancel lands, the worker observes CANCELLED, and the
// state it never claimed is walked once and freed.
func TestRuntimeV2LifecycleDebt080CancelBeforeClaimProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt080(t, "")
	stdout, stderr, exitCode := runDebt080Row(t, binPath, "debt080-cancel-before-claim", "SP_BLOCKING_POP_BEFORE_STATUS")
	if exitCode != 0 {
		t.Fatalf("cancel-before-claim proof failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

// Non-vacuity for the row above: with the state spent before the release
// (RV2_DEBT_080_NEGATIVE_CONTROL, the pre-fix shallow free), the captures are
// never walked and the row must read zero drops.
func TestRuntimeV2LifecycleDebt080CancelBeforeClaimNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt080(t, "RV2_DEBT_080_NEGATIVE_CONTROL")
	stdout, stderr, exitCode := runDebt080Row(t, binPath, "debt080-cancel-before-claim", "SP_BLOCKING_POP_BEFORE_STATUS")
	if exitCode == 0 {
		t.Fatalf("cancel-before-claim negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "blocking captures were abandoned") {
		t.Fatalf("cancel-before-claim negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

// Cancel after claim: the worker has spent the state cell and is held before
// the body runs. The cancel wins its CAS, the body runs anyway and consumes
// the captures, and the release that follows frees only the block.
func TestRuntimeV2LifecycleDebt080CancelAfterClaimProof(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt080(t, "")
	stdout, stderr, exitCode := runDebt080Row(t, binPath, "debt080-cancel-after-claim", "SP_BLOCKING_STATE_BEFORE_BODY")
	if exitCode != 0 {
		t.Fatalf("cancel-after-claim proof failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

// The other negative control, the one the release path asserted without
// showing: with
// the claim removed (RV2_DEBT_080_WALK_ALWAYS_NEGATIVE_CONTROL) the release
// walks a state the body already consumed, and the captures are destroyed a
// second time.
func TestRuntimeV2LifecycleDebt080CancelAfterClaimNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt080(t, "RV2_DEBT_080_WALK_ALWAYS_NEGATIVE_CONTROL")
	stdout, stderr, exitCode := runDebt080Row(t, binPath, "debt080-cancel-after-claim", "SP_BLOCKING_STATE_BEFORE_BODY")
	if exitCode == 0 {
		t.Fatalf("cancel-after-claim negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "blocking captures destroyed twice") {
		t.Fatalf("cancel-after-claim negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

// Poll-cancelled: the job is still QUEUED behind a body occupying the single
// worker when its awaiter is cancelled. The awaiter's poll settles the task and
// releases its reference while the pool still holds the job; the pool pops a
// CANCELLED job later and releases the state exactly once. No window to hold:
// the occupied worker is what keeps the job queued.
func TestRuntimeV2LifecycleDebt080PollCancelledQueuedJob(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt080(t, "")
	stdout, stderr, exitCode := runDebt080Row(t, binPath, "debt080-poll-cancelled-queued", "")
	if exitCode != 0 {
		t.Fatalf("poll-cancelled queued job row failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

// Shutdown/drain: a job queued when the pool's shutdown is published is
// cancelled by the worker that pops it, never run. The worker is held at the
// drain window with the job in hand, which is what proves it took that branch
// rather than finishing the body before the flag was seen.
func TestRuntimeV2LifecycleDebt080ShutdownCancelsQueuedJob(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt080(t, "")
	stdout, stderr, exitCode := runDebt080Row(t, binPath, "debt080-shutdown-drains-queued", "SP_BLOCKING_SHUTDOWN_BEFORE_DRAIN")
	if exitCode != 0 {
		t.Fatalf("shutdown-drains-queued row failed (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

// Callback reentry: the last release of a job whose state is still
// initialized dispatches the state's drop, and a drop is generated code. The
// row performs that release under the control lock and expects the refusal
// `rt_value_refuse_if_locked` promises -- an abort naming the operation --
// rather than a drop running under the lock. Every other row here runs with
// the same guard armed, which is what makes their green mean "released
// detached".
func TestRuntimeV2LifecycleDebt080ReleaseRefusesUnderLock(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt080(t, "")
	stdout, stderr, exitCode := runDebt080Row(t, binPath, "debt080-release-refuses-under-lock", "SP_BLOCKING_POP_BEFORE_STATUS")
	if exitCode == 0 {
		t.Fatalf("a blocking release ran under the control lock and nothing refused it\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "rt_value_drop_in_place_detached was dispatched while a runtime lock is held") {
		t.Fatalf("release-under-lock row failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

// lifecycleHarnessBlockingCancelModes is concatenated into the shared lifecycle
// harness (buildRuntimeV2LifecycleHarnessWithFlags). The descriptor, the bodies
// and `__surge_value_ops_for` build in every configuration -- the dispatcher in
// lifecycleHarnessScopeAndShutdown reaches them -- while the mode drivers need
// the armed sync points.
const lifecycleHarnessBlockingCancelModes = `
// ---- A blocking job's captured state across cancellation ----

enum {
    BLOCKING_FN_CONSUME = 5081,  // takes the captures and releases them itself
    BLOCKING_FN_READ = 5082,     // reads the captures and leaves them to the release
    BLOCKING_FN_GATED = 5083,    // occupies the worker until the driver opens the gate
    BLOCKING_FN_FLUSH = 5084     // no state; completes at once
};
enum { COUNTED_STATE_TYPE = 7081 };
enum { COUNTED_STATE_LIVE = 0x600DF00Du, COUNTED_STATE_DEAD = 0xDEADBEEFu };

// The captured state: a marker that says whether it is still whole, and one
// heap block so a release that never runs is a leak and one that runs twice
// would be a double free -- except that the drop below reports the second
// visit instead of performing it, so the row reads a number.
typedef struct {
    uint64_t marker;
    char* text;
} counted_state;

static _Atomic uint32_t g_counted_drops;
static _Atomic uint32_t g_counted_double_drops;
static _Atomic uint32_t g_consume_runs;
static _Atomic uint32_t g_read_runs;
static _Atomic uint32_t g_gate_open;
static _Atomic uint32_t g_gate_entered;

static void counted_move(void* dst, void* src) {
    memcpy(dst, src, sizeof(counted_state));
    memset(src, 0, sizeof(counted_state));
}

static void counted_drop(void* value) {
    counted_state* state = (counted_state*)value;
    if (state->marker != COUNTED_STATE_LIVE) {
        fputs("blocking captures destroyed twice\n", stderr);
        atomic_fetch_add_explicit(&g_counted_double_drops, 1, memory_order_acq_rel);
        return;
    }
    state->marker = COUNTED_STATE_DEAD;
    free(state->text);
    state->text = NULL;
    atomic_fetch_add_explicit(&g_counted_drops, 1, memory_order_acq_rel);
}

static rt_carrier_status counted_plan_cross(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops counted_state_ops = {
    .layout = {.size = sizeof(counted_state),
               .align = _Alignof(counted_state),
               .stride = sizeof(counted_state),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = counted_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = counted_drop,
    .trace = NULL,
    .plan_cross = counted_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

// The compiler's descriptor lookup, which the runtime declares weak so a C
// stand can supply it. rt_blocking_submit resolves the state's type id here.
const rt_value_ops* __surge_value_ops_for(uint64_t type_id);
const rt_value_ops* __surge_value_ops_for(uint64_t type_id) {
    return type_id == COUNTED_STATE_TYPE ? &counted_state_ops : NULL;
}

static int blocking_cancel_call(uint64_t id, void* state, void* out_dst) {
    switch (id) {
        case BLOCKING_FN_CONSUME:
            // What a compiled body does with a capture it moved out: releases
            // it itself and leaves the state's bytes behind as residue nobody
            // may visit again.
            counted_drop(state);
            atomic_fetch_add_explicit(&g_consume_runs, 1, memory_order_acq_rel);
            if (out_dst != NULL) {
                *(uint64_t*)out_dst = 1;
            }
            return 1;
        case BLOCKING_FN_READ:
            atomic_fetch_add_explicit(&g_read_runs, 1, memory_order_acq_rel);
            if (out_dst != NULL) {
                *(uint64_t*)out_dst = ((counted_state*)state)->marker == COUNTED_STATE_LIVE ? 2 : 0;
            }
            return 1;
        case BLOCKING_FN_GATED:
            atomic_store_explicit(&g_gate_entered, 1, memory_order_release);
            while (atomic_load_explicit(&g_gate_open, memory_order_acquire) == 0) {
                sleep_us(200);
            }
            if (out_dst != NULL) {
                *(uint64_t*)out_dst = 3;
            }
            return 1;
        case BLOCKING_FN_FLUSH:
            if (out_dst != NULL) {
                *(uint64_t*)out_dst = 4;
            }
            return 1;
        default:
            return 0;
    }
}

#ifdef RT_TEST_SYNC_POINTS
static void* submit_counted_job(uint64_t fn_id) {
    counted_state* state = (counted_state*)rt_alloc(sizeof(counted_state), _Alignof(counted_state));
    if (state == NULL) {
        return NULL;
    }
    state->marker = COUNTED_STATE_LIVE;
    state->text = (char*)malloc(32);
    if (state->text == NULL) {
        rt_free((uint8_t*)state, sizeof(counted_state), _Alignof(counted_state));
        return NULL;
    }
    memset(state->text, 'x', 31);
    state->text[31] = 0;
    return rt_blocking_submit(fn_id, state, COUNTED_STATE_TYPE, 0);
}

// A state-less job through the same single worker. When it has completed,
// every release the worker owed for the jobs before it has run, so the counts
// can be read as final rather than waited on. An armed window on the pop path
// holds this job too, so it is walked through the window as well.
static int flush_pool(rt_executor* ex, rt_sync_point_id armed) {
    unsigned before = armed == RT_SYNC_POINT_NONE ? 0 : rt_sync_point_reached_count(armed);
    void* flush = rt_blocking_submit(BLOCKING_FN_FLUSH, NULL, 0, 0);
    if (flush == NULL) {
        return fail("flush job submission failed");
    }
    if (armed != RT_SYNC_POINT_NONE) {
        if (!wait_sync_point_count(armed, before, 4000)) {
            return fail("flush job never reached the armed window");
        }
        rt_sync_point_open();
    }
    return await_expect(ex, flush, 1, 4, "flush job") ? 0 : 1;
}

static int expect_counts(rt_executor* ex, uint32_t want_drops) {
    uint32_t drops = atomic_load_explicit(&g_counted_drops, memory_order_acquire);
    uint32_t twice = atomic_load_explicit(&g_counted_double_drops, memory_order_acquire);
    if (twice != 0) {
        (void)rt_executor_request_shutdown(ex);
        fprintf(stderr, "blocking captures destroyed twice: %u drops, %u repeated\n", (unsigned)drops, (unsigned)twice);
        return 1;
    }
    if (drops != want_drops) {
        (void)rt_executor_request_shutdown(ex);
        fprintf(stderr, "blocking captures were abandoned: %u drops, want %u\n", (unsigned)drops, (unsigned)want_drops);
        return 1;
    }
    return 0;
}

static int mode_debt080_cancel_before_claim(rt_executor* ex) {
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS);
    void* job = submit_counted_job(BLOCKING_FN_READ);
    if (job == NULL) {
        return fail("counted job submission failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS, before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("no worker reached the pop-before-status window");
    }
    // The worker holds the job before reading its status: this cancel is
    // observed by that read, and the awaiter's poll settles the task.
    rt_task_cancel(job);
    if (!await_expect(ex, job, 2, 0, "cancel-before-claim job")) {
        rt_sync_point_open();
        return 1;
    }
    rt_sync_point_open();
    if (flush_pool(ex, RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS) != 0) {
        return 1;
    }
    if (atomic_load_explicit(&g_read_runs, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("a job cancelled before its claim ran its body");
    }
    if (expect_counts(ex, 1) != 0) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_debt080_cancel_after_claim(rt_executor* ex) {
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_BLOCKING_STATE_BEFORE_BODY);
    void* job = submit_counted_job(BLOCKING_FN_CONSUME);
    if (job == NULL) {
        return fail("counted job submission failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_BLOCKING_STATE_BEFORE_BODY, before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("no worker reached the state-before-body window");
    }
    // The state is the body's now. The cancel wins its CAS regardless; the
    // body runs, consumes the captures, and the release must not come back.
    rt_task_cancel(job);
    if (!await_expect(ex, job, 2, 0, "cancel-after-claim job")) {
        rt_sync_point_open();
        return 1;
    }
    rt_sync_point_open();
    if (flush_pool(ex, RT_SYNC_POINT_SP_BLOCKING_STATE_BEFORE_BODY) != 0) {
        return 1;
    }
    if (atomic_load_explicit(&g_consume_runs, memory_order_acquire) != 1) {
        (void)rt_executor_request_shutdown(ex);
        return fail("the claimed body did not run");
    }
    if (expect_counts(ex, 1) != 0) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Occupies the single worker with a gated body and queues a counted job
// behind it; returns the counted job's handle, or NULL after reporting.
static void* queue_behind_gate(rt_executor* ex, uint64_t fn_id, void** gated_out) {
    atomic_store_explicit(&g_gate_open, 0, memory_order_release);
    atomic_store_explicit(&g_gate_entered, 0, memory_order_release);
    void* gated = rt_blocking_submit(BLOCKING_FN_GATED, NULL, 0, 0);
    if (gated == NULL) {
        (void)fail("gated job submission failed");
        return NULL;
    }
    if (!wait_u32_at_least(&g_gate_entered, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        (void)fail("gated body never started");
        return NULL;
    }
    void* job = submit_counted_job(fn_id);
    if (job == NULL) {
        (void)fail("counted job submission failed");
        return NULL;
    }
    if (!wait_task_status((rt_task*)job, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        (void)fail("queued job's awaiter did not park");
        return NULL;
    }
    *gated_out = gated;
    return job;
}

static int mode_debt080_poll_cancelled_queued(rt_executor* ex) {
    void* gated = NULL;
    void* job = queue_behind_gate(ex, BLOCKING_FN_READ, &gated);
    if (job == NULL) {
        return 1;
    }
    rt_task_cancel(job);
    if (!await_expect(ex, job, 2, 0, "poll-cancelled queued job")) {
        atomic_store_explicit(&g_gate_open, 1, memory_order_release);
        return 1;
    }
    // The task is settled; the pool still holds the job, so nothing has been
    // released yet -- a drop now would be a release of a state the pool owns.
    if (atomic_load_explicit(&g_counted_drops, memory_order_acquire) != 0) {
        atomic_store_explicit(&g_gate_open, 1, memory_order_release);
        (void)rt_executor_request_shutdown(ex);
        return fail("the pool released a queued job it still held");
    }
    atomic_store_explicit(&g_gate_open, 1, memory_order_release);
    if (!await_expect(ex, gated, 1, 3, "gated job")) {
        return 1;
    }
    if (flush_pool(ex, RT_SYNC_POINT_NONE) != 0) {
        return 1;
    }
    if (atomic_load_explicit(&g_read_runs, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("a job cancelled while queued ran its body");
    }
    if (expect_counts(ex, 1) != 0) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_debt080_shutdown_drains_queued(rt_executor* ex) {
    void* gated = NULL;
    void* job = queue_behind_gate(ex, BLOCKING_FN_CONSUME, &gated);
    if (job == NULL) {
        return 1;
    }
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_BLOCKING_SHUTDOWN_BEFORE_DRAIN);
    // Publish the pool's shutdown exactly as rt_executor_request_shutdown
    // does, without stopping the scheduler: the awaiter has to be polled for
    // the answer to be observed at all.
    pthread_mutex_lock(&ex->blocking_lock);
    ex->blocking_shutdown = 1;
    pthread_cond_broadcast(&ex->blocking_cv);
    pthread_mutex_unlock(&ex->blocking_lock);
    atomic_store_explicit(&g_gate_open, 1, memory_order_release);
    for (uint32_t i = 0;; i++) {
        if (atomic_load_explicit(&g_consume_runs, memory_order_acquire) != 0) {
            (void)rt_executor_request_shutdown(ex);
            return fail("a queued job ran after the pool's shutdown was published");
        }
        if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_BLOCKING_SHUTDOWN_BEFORE_DRAIN) > before) {
            break;
        }
        if (i >= 4000) {
            (void)rt_executor_request_shutdown(ex);
            return fail("queued job was not drained under shutdown");
        }
        sleep_us(1000);
    }
    rt_sync_point_open();
    if (!await_expect(ex, job, 2, 0, "shutdown-drained job")) {
        return 1;
    }
    if (!await_expect(ex, gated, 1, 3, "gated job")) {
        return 1;
    }
    if (atomic_load_explicit(&g_consume_runs, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("a queued job ran after the pool's shutdown was published");
    }
    if (expect_counts(ex, 1) != 0) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int mode_debt080_release_refuses_under_lock(rt_executor* ex) {
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS);
    void* job = submit_counted_job(BLOCKING_FN_READ);
    if (job == NULL) {
        return fail("counted job submission failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS, before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("no worker reached the pop-before-status window");
    }
    if (!wait_task_status((rt_task*)job, TASK_WAITING, 4000)) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("awaiter did not park before the pool-side cancel");
    }
    // Settle the job on the pool side only -- no wake, so the parked awaiter
    // keeps its reference and stays parked. The worker sees CANCELLED and
    // releases the pool's reference; the flush proves that release is done.
    rt_control_lock(ex);
    rt_blocking_request_cancel(ex, (rt_task*)job);
    rt_control_unlock(ex);
    rt_sync_point_open();
    if (flush_pool(ex, RT_SYNC_POINT_SP_BLOCKING_POP_BEFORE_STATUS) != 0) {
        return 1;
    }
    if (atomic_load_explicit(&g_counted_drops, memory_order_acquire) != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("the pool released a job the awaiter still held");
    }
    // The awaiter's reference is the last one and its state is initialized:
    // this poll releases it, and the release dispatches the state's drop. Done
    // under the control lock, that dispatch must be refused, not performed.
    rt_control_lock(ex);
    (void)poll_blocking_task(ex, (rt_task*)job);
    rt_control_unlock(ex);
    (void)rt_executor_request_shutdown(ex);
    return fail("a blocking release ran generated drop code under the control lock and nothing refused it");
}
#endif
`
