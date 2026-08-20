//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// buildRuntimeV2LifecycleHarness compiles the lifecycle behavior harness
// against the full native runtime (rt_entry.c excluded), mirroring
// buildRuntimeV2LockSplitHarness (runtime_v2_lock_split_harness_test.go) and
// buildRuntimeV2SchedulerPlacementHarness (runtime_v2_scheduler_placement_test.go).
func buildRuntimeV2LifecycleHarness(t *testing.T) string {
	t.Helper()
	return buildRuntimeV2LifecycleHarnessWithFlags(t, "lifecycle_harness", nil)
}

// buildRuntimeV2LifecycleHarnessTSan builds the same harness with
// ThreadSanitizer enabled, mirroring the spike's proof methodology
// (docs/runtime-v2-epics/08-lifecycle-lane-proving-spike.md: "a standalone C
// model built clang -O1 -g -fsanitize=thread") but against the real tree
// instead of the scratchpad model. Skips (not fails) if clang lacks TSan
// support, recorded via t.Skip so the skip is never a silent pass.
func buildRuntimeV2LifecycleHarnessTSan(t *testing.T) string {
	t.Helper()
	return buildRuntimeV2LifecycleHarnessWithFlags(
		t, "lifecycle_harness_tsan", []string{"-fsanitize=thread", "-g", "-O1"})
}

func buildRuntimeV2LifecycleHarnessSyncPoints(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_syncpoints"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_023_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

func buildRuntimeV2LifecycleHarnessDebt020(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_debt020"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_020_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

func buildRuntimeV2LifecycleHarnessDebt046(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_debt046"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_046_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

func buildRuntimeV2LifecycleHarnessDebt201(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_debt201"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_201_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

func buildRuntimeV2LifecycleHarnessDebt022(t *testing.T, negativeControl bool) string {
	t.Helper()
	name := "lifecycle_harness_debt022"
	flags := []string{"-DRT_TEST_SYNC_POINTS"}
	if negativeControl {
		name += "_negative"
		flags = append(flags, "-DRV2_DEBT_022_NEGATIVE_CONTROL")
	}
	return buildRuntimeV2LifecycleHarnessWithFlags(t, name, flags)
}

func buildRuntimeV2LifecycleHarnessWithFlags(t *testing.T, name string, extraFlags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping native lifecycle behavior test")
	}

	root := repoRoot(t)
	tmpDir := t.TempDir()
	harnessPath := filepath.Join(tmpDir, name+".c")
	binPath := filepath.Join(tmpDir, name)
	source := lifecycleHarnessCommon + lifecycleHarnessCreateJoinModes + lifecycleHarnessHandleLifetimeModes +
		lifecycleHarnessScopeAndShutdown + lifecycleHarnessPlacementAdoption + lifecycleHarnessScopeCrossOwner +
		lifecycleHarnessSyncPointModes + lifecycleHarnessReadyRequeueModes +
		lifecycleHarnessSleepPublishModes + lifecycleHarnessParkAbortModes + lifecycleHarnessMain
	if writeErr := os.WriteFile(harnessPath, []byte(source), 0o600); writeErr != nil {
		t.Fatalf("write harness: %v", writeErr)
	}

	sources, globErr := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if globErr != nil {
		t.Fatalf("glob runtime sources: %v", globErr)
	}
	sort.Strings(sources)

	args := []string{
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-pthread",
	}
	args = append(args, extraFlags...)
	args = append(args, "-I"+filepath.Join(root, "runtime", "native"), "-o", binPath, harnessPath)
	for _, src := range sources {
		if filepath.Base(src) == "rt_entry.c" {
			continue
		}
		args = append(args, src)
	}

	buildCmd := exec.Command(clang, args...)
	buildCmd.Dir = root
	buildOut, buildErr, buildCode := runCommand(t, buildCmd, "")
	if buildCode != 0 {
		if strings.Contains(buildErr, "unsupported option") || strings.Contains(buildErr, "-fsanitize=thread") {
			t.Skipf("clang build does not support the requested flags; skipping\nstderr:\n%s", buildErr)
		}
		t.Fatalf("build lifecycle harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			buildCode, buildOut, buildErr)
	}
	return binPath
}

func runLifecycleHarness(t *testing.T, binPath string, mode string, env []string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, mode)
	cmd.Env = env
	return runCommand(t, cmd, "")
}

func lifecycleEnv(values ...string) []string {
	env := os.Environ()
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 {
			continue
		}
		env = overrideEnvVar(env, parts[0], parts[1])
	}
	return env
}

// runLifecycleModeAcrossShards runs mode at SURGE_SHARDS=1,2,8 (the epic's
// required matrix for create/join probes, 08-task-lifecycle-lane-and-net-
// fairness.md "Proof And Quality Contract": "join polling and result
// observation across SURGE_SHARDS=1, 2, and 8").
func runLifecycleModeAcrossShards(t *testing.T, binPath string, mode string) {
	t.Helper()
	// SURGE_THREADS must equal SURGE_SHARDS whenever SURGE_SHARDS>1 (the
	// runtime enforces this at startup); SURGE_SHARDS=1 tolerates extra
	// worker threads, matching the convention already used by
	// runtime_v2_lock_split_behavior_test.go (shards=1,threads=2).
	shardThreads := map[string]string{"1": "4", "2": "2", "8": "8"}
	for _, shards := range []string{"1", "2", "8"} {
		t.Run("shards-"+shards, func(t *testing.T) {
			env := lifecycleEnv("SURGE_SHARDS="+shards, "SURGE_THREADS="+shardThreads[shards], "SURGE_BLOCKING_THREADS=1")
			stdout, stderr, exitCode := runLifecycleHarness(t, binPath, mode, env)
			if exitCode != 0 {
				t.Fatalf("lifecycle mode %q failed at SURGE_SHARDS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					mode, shards, exitCode, stdout, stderr)
			}
		})
	}
}

// lifecycleHarnessCommon holds includes, shared globals, spawn/wait helpers,
// and the create/join/handle-lifetime poll functions. Split across five Go
// source files purely to keep each one under the project's 500-line cap:
// lifecycleHarnessCreateJoinModes and lifecycleHarnessHandleLifetimeModes
// (this package, in runtime_v2_lifecycle_behavior_create_join_test.go and
// runtime_v2_lifecycle_behavior_handle_lifetime_test.go), then
// lifecycleHarnessScopeAndShutdown (runtime_v2_lifecycle_behavior_scope_test.go)
// and lifecycleHarnessMain (runtime_v2_lifecycle_behavior_await_shutdown_test.go).
// All five constants are concatenated into one compiled translation unit by
// buildRuntimeV2LifecycleHarnessWithFlags below.
const lifecycleHarnessCommon = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"
#include "rt_sync_point.h"

#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

enum {
    POLL_OWNER_PROBE = 4001,
    POLL_JOIN_TARGET_SPIN = 4002,
    POLL_JOIN_TARGET_QUICK = 4003,
    POLL_JOINER = 4004,
    POLL_CLONE_TARGET = 4005,
    POLL_CLONE_RACER = 4006,
    POLL_PIN_TARGET = 4007,
    POLL_PIN_JOINER = 4008,
    POLL_SCOPE_CHILD_QUICK = 4009,
    POLL_SCOPE_CHILD_SPIN = 4010,
    POLL_SCOPE_OWNER = 4011,
    POLL_SCOPE_CANCEL_OWNER = 4012,
    POLL_SPIN_FOREVER = 4013,
    POLL_TIMER_PARK = 4014,
    POLL_CHANNEL_PARK = 4015,
    POLL_BLOCKING_PARK = 4016,
    POLL_EXTERNAL_AWAIT_TARGET = 4017,
    POLL_MAKE_CHAN = 4018,
    POLL_SCOPE_OWNER_FOREVER = 4019,
    POLL_JOIN_TARGET_GATED = 4020,
    POLL_PARK_FOREVER = 4021,
    POLL_MAKE_PARK_FOREVER_CHAN = 4022,
    POLL_SCOPE_OWNER_FAILFAST = 4023,
    POLL_ADOPT_TARGET = 4024,
    POLL_ADOPT_JOINER = 4025,
    POLL_XOWNER_GRANDCHILD = 4026,
    POLL_XOWNER_SCOPE_CHILD = 4027,
    POLL_XOWNER_OWNER = 4028,
    POLL_DEBT020_ADOPT_JOINER = 4029,
    POLL_DEBT020_GAP_JOINER = 4030,
    POLL_DEBT022_GATED_TARGET = 4032,
    // 4033 is POLL_READY_REQUEUE_PROBE (defined in the ready-requeue modes).
    POLL_DEBT046_JOINER = 4034
	};

enum { BLOCKING_FN_SLOW = 5001 };

static _Atomic uint32_t g_owner_probe_shard;
static _Atomic uint32_t g_owner_probe_ran;
static void* g_join_target;
static _Atomic uint32_t g_clone_racer_iters;
static _Atomic uint32_t g_clone_uaf_detected;
static _Atomic(void*) g_clone_shared_target;
static _Atomic(void*) g_chan_a;

static void sleep_us(unsigned long micros) {
    struct timespec ts;
    ts.tv_sec = (time_t)(micros / 1000000UL);
    ts.tv_nsec = (long)((micros % 1000000UL) * 1000UL);
    while (nanosleep(&ts, &ts) != 0) {
    }
}

static int fail(const char* msg) {
    if (msg != NULL) {
        fputs(msg, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

static int wait_task_status(const rt_task* task, uint8_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (task_status_load(task) == want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static int wait_ptr(_Atomic(void*)* slot, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(slot, memory_order_acquire) != NULL) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static int wait_u32_at_least(_Atomic uint32_t* slot, uint32_t want, uint32_t attempts) {
    for (uint32_t i = 0; i < attempts; i++) {
        if (atomic_load_explicit(slot, memory_order_acquire) >= want) {
            return 1;
        }
        sleep_us(1000);
    }
    return 0;
}

static rt_task* alloc_ready_task(rt_executor* ex, int64_t poll_fn_id) {
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        return NULL;
    }
    memset(task, 0, sizeof(*task));
    task->id = id;
    task->poll_fn_id = poll_fn_id;
    task->kind = TASK_KIND_USER;
    task_status_store(task, TASK_READY);
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_slot_store(ex, id, task);
    return task;
}

static uint32_t pin_shard(rt_executor* ex, uint32_t wanted) {
    size_t count = rt_runtime_shard_count(rt_executor_runtime(ex));
    if (count < 1) {
        return 0;
    }
    return (uint32_t)(wanted % (uint32_t)count);
}

static rt_task* spawn_pinned(rt_executor* ex, int64_t poll_fn_id, uint32_t wanted_shard) {
    rt_control_lock(ex);
    rt_task* task = alloc_ready_task(ex, poll_fn_id);
    if (task != NULL) {
        rt_task_set_placement(task, pin_shard(ex, wanted_shard), TASK_PLACEMENT_CONNECTION);
        ready_push(ex, task->id);
    }
    rt_control_unlock(ex);
    return task;
}

// spawn_pinned_with_state sets the task's initial __task_state() payload
// directly, so many concurrent instances of the same poll function can each
// carry their own designated target instead of racing on one shared global
// (needed by the completion-pin stress, which runs many (target, joiner)
// pairs concurrently).
static rt_task* spawn_pinned_with_state(rt_executor* ex,
                                        int64_t poll_fn_id,
                                        uint32_t wanted_shard,
                                        void* state) {
    rt_control_lock(ex);
    rt_task* task = alloc_ready_task(ex, poll_fn_id);
    if (task != NULL) {
        task->state = state;
        rt_task_set_placement(task, pin_shard(ex, wanted_shard), TASK_PLACEMENT_CONNECTION);
        ready_push(ex, task->id);
    }
    rt_control_unlock(ex);
    return task;
}

static int await_expect(rt_executor* ex, void* handle, uint8_t want_kind, uint64_t want_bits,
                         const char* what) {
    rt_task* task = (rt_task*)handle;
    if (!wait_task_status(task, TASK_DONE, 8000)) {
        (void)rt_executor_request_shutdown(ex);
        fprintf(stderr, "timeout waiting for %s\n", what);
        return 0;
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(handle, &kind, &bits);
    if (kind != want_kind || bits != want_bits) {
        fprintf(stderr, "%s: kind=%u bits=%llu want kind=%u bits=%llu\n", what, (unsigned)kind,
                (unsigned long long)bits, (unsigned)want_kind, (unsigned long long)want_bits);
        return 0;
    }
    return 1;
}

// --- Owner-local create + ready publication (S5-Q1 / rule 1 regression guard) ---
// rt_debug_current_worker_shard_id (rt_async_state.c:352, already the proof
// mechanism for TestRuntimeV2SchedulerPlacementNoStealWorkerPath) tells us
// which shard's worker actually ran this poll; compared against the task's
// own owner_shard_id (set at spawn by rt_task_set_placement), this proves
// the create->ready-push->run path stayed on the assigned owner shard.
static void poll_owner_probe(void) {
    const rt_task* task = rt_current_task();
    uint32_t worker_shard = rt_debug_current_worker_shard_id();
    if (task != NULL && worker_shard == task->owner_shard_id) {
        atomic_store_explicit(&g_owner_probe_shard, worker_shard, memory_order_release);
    } else {
        atomic_store_explicit(&g_owner_probe_shard, UINT32_MAX, memory_order_release);
    }
    atomic_store_explicit(&g_owner_probe_ran, 1, memory_order_release);
    rt_async_return(NULL, 0);
}

static _Atomic uint32_t g_join_spin_steps;

static void poll_join_target_spin(void) {
    uint32_t step = atomic_fetch_add_explicit(&g_join_spin_steps, 1, memory_order_acq_rel);
    if (step < 3) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_async_return(NULL, 42);
}

static void poll_join_target_quick(void) {
    rt_async_return(NULL, 7);
}

static _Atomic uint32_t g_join_target_release;

// Spins until the harness driver flips the release flag, giving a
// deterministic "joiner parks first, then the target completes" ordering
// (the yield-count-based poll_join_target_spin cannot guarantee this: with
// enough worker concurrency the target can finish all its yields before the
// joiner is ever scheduled once).
static void poll_join_target_gated(void) {
    if (atomic_load_explicit(&g_join_target_release, memory_order_acquire) == 0) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_async_return(NULL, 42);
}

static _Atomic(void*) g_park_forever_chan;

// Creates the channel poll_park_forever recvs on; the mode driver must spawn
// this and wait for g_park_forever_chan to be set before spawning anything
// that uses POLL_PARK_FOREVER, so there is no create-time race between
// concurrent park_forever instances.
static void poll_make_park_forever_chan(void) {
    atomic_store_explicit(&g_park_forever_chan, rt_channel_new(0, 0), memory_order_release);
    rt_async_return(NULL, 0);
}

// Genuinely parks forever (never sits in the ready queue), unlike
// poll_spin_forever which busy-yields (POLL_YIELDED -> re-enqueued every
// cycle) -- needed wherever a shard's "no parked-with-work" invariant will be
// checked (rt_debug_assert_no_parked_with_work only inspects whether the
// shard's ready queue is non-empty, rt_scheduler_placement.c:126-135). A long
// rt_sleep does NOT work for this: tick_virtual/advance_time_to_next_timer
// (rt_async_state.c:1199-1257) fast-forwards the virtual clock straight to
// the next timer deadline once workers go idle, so a "long" sleep fires
// almost immediately once nothing else is ready -- confirmed by direct
// observation while developing this harness. A channel recv on a
// never-sent-to channel has no such auto-fire mechanism.
static void poll_park_forever(void) {
    void* ch = atomic_load_explicit(&g_park_forever_chan, memory_order_acquire);
    uint64_t bits = 0;
    uint8_t st = rt_channel_recv(ch, &bits);
    if (st == 0) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_async_return(NULL, 99);
}

static void poll_joiner(void) {
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(g_join_target, &bits);
    if (st == 0) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_async_return(NULL, st == 1 ? bits : 0);
}

// --- Handle clone/release/last-reference-free (rule 1, S5-Q6) ---

static void poll_clone_target(void) {
    rt_async_return(NULL, 1);
}

// Repeatedly clones and joins (via rt_task_poll, the actual worker join
// lane -- not rt_task_await's external/done_cv compat path, rule 5) a shared
// target handle while it may be completing concurrently on another
// shard/worker; rt_task_clone (rt_async_task.c:234-247) only bumps the
// atomic handle_refs, and task_release (rt_async_state.c:1414-1427) only
// frees at refs 1->0 && DONE, so this racer must never observe a
// use-after-free. Each racer clones once then polls its own clone to
// completion, mirroring poll_joiner's register/yield/repeat convention.
// __task_state (rt_async_task.c:46-55) is the per-task-instance persisted
// pointer the scheduler round-trips through rt_async_yield/rt_async_return's
// state argument -- required here since many racer instances run
// concurrently and cannot share one global for their own cloned handle.
static void poll_clone_racer(void) {
    void* cloned = __task_state();
    if (cloned == NULL) {
        void* target = atomic_load_explicit(&g_clone_shared_target, memory_order_acquire);
        if (target == NULL) {
            rt_async_return(NULL, 0);
            return;
        }
        cloned = rt_task_clone(target, NULL, 0);
        if (cloned == NULL) {
            atomic_store_explicit(&g_clone_uaf_detected, 1, memory_order_release);
            rt_async_return(NULL, 0);
            return;
        }
    }
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(cloned, &bits);
    if (st == 0) {
        rt_async_yield(cloned, 0);
        return;
    }
    if (st != 1 || bits != 1) {
        atomic_store_explicit(&g_clone_uaf_detected, 1, memory_order_release);
    }
    atomic_fetch_add_explicit(&g_clone_racer_iters, 1, memory_order_acq_rel);
    rt_async_return(NULL, 0);
}

// --- Completion-pin interleaving (rule 1: task_add_ref at mark_done entry,
// rt_async_state.c:1515; task_release_lane_aware at exit, :1574). Target
// completes with no yield so mark_done runs almost immediately; the joiner
// busy-polls with no sleep so its last-handle release has the best chance of
// landing while another worker thread is still inside mark_done's body. This
// is a probabilistic regression stress, not a forced deterministic
// reproduction (does not touch rt_async_state.c to inject a delay);
// TSan is the oracle that turns "no crash" into "no data race, no UAF".
static void poll_pin_target(void) {
    rt_async_return(NULL, 99);
}

static void poll_pin_joiner(void) {
    // Same register/yield/repeat convention as poll_joiner: rt_task_poll must
    // be followed by rt_async_yield when it reports "not yet", never called
    // again directly, so prepare_park's registration is properly handed to
    // the scheduler's park path instead of re-registering underneath it. The
    // target handle is this instance's own __task_state payload (set at
    // spawn via spawn_pinned_with_state), not a shared global, since many
    // pin-stress pairs run concurrently.
    void* target = __task_state();
    uint64_t bits = 0;
    uint8_t st = rt_task_poll(target, &bits);
    if (st == 0) {
        rt_async_yield(target, 0);
        return;
    }
    rt_async_return(NULL, st == 1 && bits == 99 ? 1 : 0);
}
`
