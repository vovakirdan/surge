//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"strings"
	"testing"
)

// Carrier affinity (RV2 D4.6): a task that borrows its creator's frame runs
// only on the worker that carried the creator, and the scheduler keeps it
// there by ROUTE, not by refusal -- publication goes to the carrier's deque
// with a credit addressed to the carrier, so no other sleeper can spend that
// credit and leave the carrier asleep (RUNTIME_V2.md, "The ROUTING half of
// affinity"). The refusal on pop is a defence and is counted, never relied on.
//
// The stand runs at SURGE_SHARDS=1, the one topology with several carriers on
// a shard, and so the only one where affinity is observable at all.
//
// Two modes, three mutants:
//
//   - carrier-affine-publish: an owner creates one affine child and three
//     affine spinners. The child parks on a channel; the DRIVER thread, which
//     is no worker, sends into it CARRIER_WAKE_CYCLES times. Each send publishes
//     the child from outside the carrier: the route must reach the carrier's
//     deque and the credit the carrier's token. The child and the spinners
//     record every poll made off the carrier; the count must read zero, and
//     the child must complete. Between cycles the driver broadcasts a shard
//     credit so every idle worker scans the carrier's deque, meets the
//     spinners and is refused -- the positive control for the refusal counter.
//   - carrier-affine-shutdown: an owner creates an affine child and requests
//     shutdown from inside its own poll, so the child sits in the carrier's
//     deque unpolled when the carrier exits. The exiting carrier must cancel
//     it (Section 10's shutdown sentence): the child reads DONE+CANCELLED.
//
// RV2_D46_ROUTE_NEGATIVE_CONTROL restores the pusher's route (a wake from the
// driver lands on the inject queue, where any worker meets it, is refused,
// and the carrier -- credited on the shard, or not at all -- may never come);
// RV2_D46_NEGATIVE_CONTROL keeps the route and credits the SHARD instead of
// the carrier, the P0 the model names: with eight workers the signal reaches
// the wrong sleeper with probability 6/7 per cycle, and a run of 24 cycles
// that never loses one has probability (1/7)^24. RV2_D46_SHUTDOWN_NEGATIVE_
// CONTROL leaves the pinned child in the deque at exit.

func runRuntimeV2CarrierAffineProofs(t *testing.T) {
	t.Helper()
	positive := buildRuntimeV2LifecycleHarnessWithFlags(t, "carrier_affine", nil)
	t.Run("publish-addressed-to-the-carrier", func(t *testing.T) {
		for _, threads := range []int{2, 4, 8} {
			t.Run(fmt.Sprintf("threads-%d", threads), func(t *testing.T) {
				env := lifecycleEnv(
					"SURGE_SHARDS=1",
					fmt.Sprintf("SURGE_THREADS=%d", threads),
					"SURGE_BLOCKING_THREADS=1",
					"SURGE_SCHED_TRACE=1")
				stdout, stderr, code := runLifecycleHarness(t, positive, "carrier-affine-publish", env)
				if code != 0 {
					t.Fatalf("carrier-affine publish failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
						code, stdout, stderr)
				}
				trace := parseSchedTrace(t, stderr)
				// One child and three spinners pinned; every driver send credited
				// the carrier; at least one idle worker met a spinner and was
				// refused -- the defence counter with its positive control.
				if trace.carrierPinned < 4 {
					t.Fatalf("carrier_pinned=%d, want >= 4 (one child, three spinners)\n%s",
						trace.carrierPinned, stderr)
				}
				if trace.carrierAddressedWakes < 24 {
					t.Fatalf("carrier_addressed_wakes=%d, want >= 24 (one per driver send)\n%s",
						trace.carrierAddressedWakes, stderr)
				}
				if trace.carrierStealDenied < 1 {
					t.Fatalf("carrier_steal_denied=%d, want >= 1 (idle workers scanned the spinners)\n%s",
						trace.carrierStealDenied, stderr)
				}
				if trace.carrierShutdownCancelled != 0 {
					t.Fatalf("carrier_shutdown_cancelled=%d, want 0 (every pinned task completed)\n%s",
						trace.carrierShutdownCancelled, stderr)
				}
			})
		}
	})
	t.Run("shutdown-cancels-the-unpolled-pinned-child", func(t *testing.T) {
		for _, threads := range []int{2, 4, 8} {
			t.Run(fmt.Sprintf("threads-%d", threads), func(t *testing.T) {
				env := lifecycleEnv(
					"SURGE_SHARDS=1",
					fmt.Sprintf("SURGE_THREADS=%d", threads),
					"SURGE_BLOCKING_THREADS=1",
					"SURGE_SCHED_TRACE=1")
				stdout, stderr, code := runLifecycleHarness(t, positive, "carrier-affine-shutdown", env)
				if code != 0 {
					t.Fatalf("carrier-affine shutdown failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
						code, stdout, stderr)
				}
				trace := parseSchedTrace(t, stderr)
				if trace.carrierShutdownCancelled != 1 {
					t.Fatalf("carrier_shutdown_cancelled=%d, want 1\n%s", trace.carrierShutdownCancelled, stderr)
				}
			})
		}
	})
}

func TestRuntimeV2CarrierAffinePublicationProof(t *testing.T) {
	skipTimeoutTests(t)
	runRuntimeV2CarrierAffineProofs(t)
}

// Each mutant is built once and must fail the mode it breaks at eight
// workers, the width where the wrong sleeper is the likely one.
func TestRuntimeV2CarrierAffinePublicationNegativeControls(t *testing.T) {
	skipTimeoutTests(t)
	env := lifecycleEnv(
		"SURGE_SHARDS=1", "SURGE_THREADS=8", "SURGE_BLOCKING_THREADS=1", "SURGE_SCHED_TRACE=1")
	mutants := []struct {
		name, flag, mode, want string
	}{
		{"pushers-route", "-DRV2_D46_ROUTE_NEGATIVE_CONTROL", "carrier-affine-publish", "carrier"},
		{"shard-wide-credit", "-DRV2_D46_NEGATIVE_CONTROL", "carrier-affine-publish", "carrier"},
		{"deque-left-at-exit", "-DRV2_D46_SHUTDOWN_NEGATIVE_CONTROL", "carrier-affine-shutdown", "carrier"},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			bin := buildRuntimeV2LifecycleHarnessWithFlags(
				t, "carrier_affine_"+strings.ReplaceAll(m.name, "-", "_"), []string{m.flag})
			stdout, stderr, code := runLifecycleHarness(t, bin, m.mode, env)
			if code == 0 {
				t.Fatalf("mutant %s passed %s; the stand cannot see it\nstdout:\n%s\nstderr:\n%s",
					m.name, m.mode, stdout, stderr)
			}
			// The pusher's route is caught one step earlier than the bound:
			// the wake lands on the inject queue, a worker that is not the
			// carrier meets it there, refuses it, and must then park beside
			// work it cannot take -- the parked-with-work invariant names
			// that before the driver's bound does.
			if !strings.Contains(stderr, m.want) && !strings.Contains(stderr, "parked-with-work") {
				t.Fatalf("mutant %s failed for another reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
					m.name, code, stdout, stderr)
			}
		})
	}
}

const lifecycleHarnessCarrierModes = `
#define POLL_CARRIER_OWNER 4048
#define POLL_CARRIER_CHILD 4049
#define POLL_CARRIER_SPINNER 4050
#define POLL_CARRIER_SHUTDOWN_OWNER 4051
#define CARRIER_WAKE_CYCLES 24
#define CARRIER_SPINNERS 3

static _Atomic int g_carrier_owner_worker;
static _Atomic(void*) g_carrier_child;
static _Atomic(void*) g_carrier_spinners[CARRIER_SPINNERS];
static _Atomic(void*) g_carrier_chan;
static _Atomic uint32_t g_carrier_owner_done;
static _Atomic uint32_t g_carrier_child_polls;
static _Atomic uint32_t g_carrier_off_carrier_polls;
static _Atomic uint32_t g_carrier_stop;
static _Atomic uint32_t g_carrier_spinners_stop;

static int carrier_current_worker(void) {
    return tls_worker_ctx != NULL ? (int)tls_worker_ctx->worker_id : -1;
}

// Every poll of a pinned task records whether it ran where it was pinned.
static void carrier_note_poll(void) {
    if (carrier_current_worker() != atomic_load_explicit(&g_carrier_owner_worker, memory_order_acquire)) {
        atomic_fetch_add_explicit(&g_carrier_off_carrier_polls, 1, memory_order_acq_rel);
    }
}

// The owner runs once, on some worker: that worker is the carrier. What it
// creates through __task_create_affine is pinned to it before publication.
// It creates the child alone, so that during the wake cycles the carrier has
// nothing else to do and is ASLEEP when each wake arrives -- the case the
// addressed credit exists for. The spinners come later, from the child.
static void poll_carrier_owner(void) {
    atomic_store_explicit(&g_carrier_owner_worker, carrier_current_worker(), memory_order_release);
    atomic_store_explicit(
        &g_carrier_chan, rt_channel_new(0, rt_channel_opaque_word_ops(), 0), memory_order_release);
    atomic_store_explicit(&g_carrier_child,
                          __task_create_affine(POLL_CARRIER_CHILD, NULL, rt_channel_opaque_word_ops()),
                          memory_order_release);
    atomic_store_explicit(&g_carrier_owner_done, 1, memory_order_release);
    rt_async_return(NULL, &(uint64_t){0});
}

// Parks on a channel nobody ever sends into; every poll after the first is
// one wake from the driver, a thread that is no worker. Each wake publishes
// the child from outside the carrier and must land on the carrier. Told to
// stop, it completes.
static void poll_carrier_child(void) {
    carrier_note_poll();
    atomic_fetch_add_explicit(&g_carrier_child_polls, 1, memory_order_acq_rel);
    if (atomic_load_explicit(&g_carrier_stop, memory_order_acquire) != 0) {
        // Told to stop, and running on the carrier: the spinners it creates
        // here are pinned to the carrier too. They are what an idle worker's
        // steal scan meets and must refuse, once the wake cycles are over.
        for (int i = 0; i < CARRIER_SPINNERS; i++) {
            atomic_store_explicit(
                &g_carrier_spinners[i],
                __task_create_affine(POLL_CARRIER_SPINNER, NULL, rt_channel_opaque_word_ops()),
                memory_order_release);
        }
        rt_async_return(NULL, &(uint64_t){0});
        return;
    }
    void* ch = atomic_load_explicit(&g_carrier_chan, memory_order_acquire);
    uint64_t bits = 0;
    (void)rt_channel_recv(ch, &bits);
    rt_async_yield(NULL, 0);
}

// Always ready, always in the carrier's deque: what an idle worker's steal
// scan meets and must refuse.
static void poll_carrier_spinner(void) {
    carrier_note_poll();
    if (atomic_load_explicit(&g_carrier_spinners_stop, memory_order_acquire) != 0) {
        rt_async_return(NULL, &(uint64_t){0});
        return;
    }
    rt_async_yield(NULL, 0);
}

static int mode_carrier_affine_publish(rt_executor* ex) {
    atomic_store_explicit(&g_carrier_owner_worker, -1, memory_order_release);
    atomic_store_explicit(&g_carrier_child, NULL, memory_order_release);
    atomic_store_explicit(&g_carrier_chan, NULL, memory_order_release);
    atomic_store_explicit(&g_carrier_owner_done, 0, memory_order_release);
    atomic_store_explicit(&g_carrier_child_polls, 0, memory_order_release);
    atomic_store_explicit(&g_carrier_off_carrier_polls, 0, memory_order_release);
    atomic_store_explicit(&g_carrier_stop, 0, memory_order_release);
    atomic_store_explicit(&g_carrier_spinners_stop, 0, memory_order_release);
    rt_task* owner = spawn_pinned(ex, POLL_CARRIER_OWNER, 0);
    if (owner == NULL) {
        return fail("carrier owner allocation failed");
    }
    if (!wait_u32_at_least(&g_carrier_owner_done, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("carrier owner never ran");
    }
    rt_task* child = task_from_handle(atomic_load_explicit(&g_carrier_child, memory_order_acquire));
    void* ch = atomic_load_explicit(&g_carrier_chan, memory_order_acquire);
    if (child == NULL || ch == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("carrier owner left no child or no channel");
    }
    void* child_handle = atomic_load_explicit(&g_carrier_child, memory_order_acquire);
    for (uint32_t cycle = 1; cycle <= CARRIER_WAKE_CYCLES; cycle++) {
        if (!wait_task_status(child, TASK_WAITING, 4000)) {
            fprintf(stderr, "carrier: child never parked before cycle %u (status=%u)\n",
                    cycle, (unsigned)task_status_load(child));
            (void)rt_executor_request_shutdown(ex);
            return fail("carrier child did not park");
        }
        // A wake from a thread that is no worker: the publication it makes
        // has to find the carrier on its own -- its deque and its credit.
        rt_task_wake(child_handle);
        if (!wait_u32_at_least(&g_carrier_child_polls, cycle + 1, 4000)) {
            fprintf(stderr,
                    "carrier: cycle %u lost -- the wake did not reach the carrier "
                    "(child status=%u enqueued=%u carrier=%d)\n",
                    cycle,
                    (unsigned)task_status_load(child),
                    (unsigned)task_enqueued_load(child),
                    atomic_load_explicit(&g_carrier_owner_worker, memory_order_acquire));
            (void)rt_executor_request_shutdown(ex);
            return fail("carrier wake lost");
        }
        // Every idle worker scans the carrier's deque once, meets a spinner,
        // and is refused: the positive control for the refusal counter.
        rt_control_lock(ex);
        rt_sched_wake_broadcast_all(ex);
        rt_control_unlock(ex);
    }
    atomic_store_explicit(&g_carrier_stop, 1, memory_order_release);
    if (!wait_task_status(child, TASK_WAITING, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("carrier child did not park before the stop");
    }
    rt_task_wake(child_handle);
    if (!wait_task_status(child, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("carrier child did not complete after the stop");
    }
    // The spinners now sit in the carrier's deque, always ready. Every idle
    // worker is credited on the shard a few times so that it scans that
    // deque, meets a spinner, and is refused -- the positive control for the
    // refusal counter, which the addressed route keeps at zero otherwise.
    for (int round = 0; round < 8; round++) {
        rt_control_lock(ex);
        rt_sched_wake_broadcast_all(ex);
        rt_control_unlock(ex);
        sleep_us(2000);
    }
    atomic_store_explicit(&g_carrier_spinners_stop, 1, memory_order_release);
    for (int i = 0; i < CARRIER_SPINNERS; i++) {
        rt_task* spinner = task_from_handle(atomic_load_explicit(&g_carrier_spinners[i], memory_order_acquire));
        if (spinner == NULL || !wait_task_status(spinner, TASK_DONE, 4000)) {
            (void)rt_executor_request_shutdown(ex);
            return fail("carrier spinner did not stop");
        }
    }
    uint32_t off = atomic_load_explicit(&g_carrier_off_carrier_polls, memory_order_acquire);
    if (off != 0) {
        fprintf(stderr, "carrier: %u polls of pinned tasks ran off the carrier\n", off);
        return fail("carrier-affine task polled on another worker");
    }
    // The four counters are read by the driver from this dump; the harness
    // exits without the executor's own.
    rt_sched_trace_dump();
    return 0;
}

// The owner pins a child and, still inside its own poll, asks for shutdown:
// the child is in the carrier's deque and nobody else may take it, so at the
// carrier's exit it is unpolled, and the carrier must cancel it.
static void poll_carrier_shutdown_owner(void) {
    atomic_store_explicit(&g_carrier_owner_worker, carrier_current_worker(), memory_order_release);
    atomic_store_explicit(&g_carrier_child,
                          __task_create_affine(POLL_CARRIER_SPINNER, NULL, rt_channel_opaque_word_ops()),
                          memory_order_release);
    atomic_store_explicit(&g_carrier_owner_done, 1, memory_order_release);
    (void)rt_executor_request_shutdown(ensure_exec());
    rt_async_return(NULL, &(uint64_t){0});
}

static int mode_carrier_affine_shutdown(rt_executor* ex) {
    atomic_store_explicit(&g_carrier_owner_worker, -1, memory_order_release);
    atomic_store_explicit(&g_carrier_child, NULL, memory_order_release);
    atomic_store_explicit(&g_carrier_owner_done, 0, memory_order_release);
    atomic_store_explicit(&g_carrier_off_carrier_polls, 0, memory_order_release);
    atomic_store_explicit(&g_carrier_stop, 0, memory_order_release);
    atomic_store_explicit(&g_carrier_spinners_stop, 0, memory_order_release);
    rt_task* owner = spawn_pinned(ex, POLL_CARRIER_SHUTDOWN_OWNER, 0);
    if (owner == NULL) {
        return fail("carrier shutdown owner allocation failed");
    }
    if (!wait_u32_at_least(&g_carrier_owner_done, 1, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("carrier shutdown owner never ran");
    }
    rt_task* child = task_from_handle(atomic_load_explicit(&g_carrier_child, memory_order_acquire));
    if (child == NULL) {
        return fail("carrier shutdown owner left no child");
    }
    if (!wait_task_status(child, TASK_DONE, 4000)) {
        fprintf(stderr, "carrier: pinned child left unpolled at shutdown (status=%u enqueued=%u)\n",
                (unsigned)task_status_load(child), (unsigned)task_enqueued_load(child));
        return fail("carrier did not cancel its unpolled pinned child at exit");
    }
    if (child->result_kind != TASK_RESULT_CANCELLED) {
        return fail("carrier's unpolled pinned child completed other than cancelled");
    }
    if (atomic_load_explicit(&g_carrier_off_carrier_polls, memory_order_acquire) != 0) {
        return fail("carrier-affine child polled on another worker before shutdown");
    }
    rt_sched_trace_dump();
    return 0;
}
`
