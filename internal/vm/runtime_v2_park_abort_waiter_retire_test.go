//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RV2-DEBT-201. park_current has two abort branches. The SECOND one (the
// token re-check under the owner lock) captures the park generation and calls
// remove_waiter_generation after releasing that lock. The FIRST one (the token
// was already set when the park began) only requeued the task, so the channel
// registration channel_park_prepare_locked had already appended survived the
// task that owned it.
//
// The drive is the shape cancel_task's own comment names: hold a RUNNING task
// that has already passed its poll's cancelled check at SP_PARK_BEFORE_WAITING,
// cancel it there, then release. park_current therefore reads a set token on
// its very FIRST exchange, which is branch one by construction. The body's next
// poll observes cancelled and unwinds, so nothing re-prepares the key and
// mark_done finds a cleared park_key with nothing to remove.
//
// Both halves are measured, never inferred. The positive run names the park it
// caught by VALUE at the window (the body's own task id, a prepared
// WAKER_CHAN_RECV key, a nonzero generation, exactly one live entry) and
// asserts exactly one user park crossed the window all run. The negative
// control zeroes ONLY the first branch's generation, so its strand is proof
// that the first branch is the branch this drive takes.
func TestRuntimeV2LifecycleDebt201AbortedParkRetiresChannelEntry(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt201(t, false)
	for _, shards := range []string{"1", "2", "8"} {
		shards := shards
		t.Run("positive-shards-"+shards, func(t *testing.T) {
			threads := map[string]string{"1": "4", "2": "2", "8": "8"}[shards]
			env := lifecycleEnv(
				"SURGE_SHARDS="+shards,
				"SURGE_THREADS="+threads,
				"SURGE_BLOCKING_THREADS=1",
				"SURGE_SYNC_POINT=SP_PARK_BEFORE_WAITING:block")
			stdout, stderr, exitCode := runLifecycleHarness(
				t, binPath, "debt201-park-abort-retires-entry", env)
			if exitCode != 0 {
				t.Fatalf("DEBT-201 positive proof failed at SURGE_SHARDS=%s (code=%d)\nstdout:\n%s\nstderr:\n%s",
					shards, exitCode, stdout, stderr)
			}
			// Non-vacuity: the run must actually have caught a prepared
			// channel receive holding one live entry, not an empty window.
			if !strings.Contains(stderr, "kind=4 ") || !strings.Contains(stderr, "prepared=1 ") {
				t.Fatalf("DEBT-201 positive proof did not catch a prepared channel receive\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
			if !strings.Contains(stderr, "entries=1") {
				t.Fatalf("DEBT-201 positive proof never observed the registration it must retire\nstdout:\n%s\nstderr:\n%s",
					stdout, stderr)
			}
		})
	}
}

func TestRuntimeV2LifecycleDebt201AbortedParkRetiresChannelEntryNegativeControl(t *testing.T) {
	binPath := buildRuntimeV2LifecycleHarnessDebt201(t, true)
	env := lifecycleEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT=SP_PARK_BEFORE_WAITING:block")
	stdout, stderr, exitCode := runLifecycleHarness(
		t, binPath, "debt201-park-abort-retires-entry", env)
	if exitCode == 0 {
		t.Fatalf("DEBT-201 negative control unexpectedly passed\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	const want = "debt201 aborted park left its channel registration behind"
	if !strings.Contains(stderr, want) {
		t.Fatalf("DEBT-201 negative control failed for the wrong reason (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}

// The retirement must stay GENERATION-QUALIFIED and stay AFTER the owner
// unlock. remove_waiter_from_store_seq treats seq 0 as "match any", so calling
// it unqualified on this branch would sweep the prepare_park registrations
// (join / scope / remote reply keys, all at seq 0) that also reach it - the
// RV2-DEBT-046 lost-wake shape. And a removal taken before rt_shard_unlock
// would hold two shard locks.
func TestRuntimeV2LifecycleDebt201AbortRetirementStaticShape(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "runtime", "native", "rt_task_park.c"))
	if err != nil {
		t.Fatalf("read rt_task_park.c: %v", err)
	}
	source := string(raw)
	start := strings.Index(source, "void park_current(rt_executor* ex, waker_key key) {")
	if start < 0 {
		t.Fatal("could not find park_current")
	}
	body := source[start:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}
	firstBranch := strings.Index(body, "if (task_wake_token_exchange(task, 0) != 0) {")
	if firstBranch < 0 {
		t.Fatal("could not find park_current's first abort branch")
	}
	branch := body[firstBranch:]
	if end := strings.Index(branch, "// Register-then-commit"); end > 0 {
		branch = branch[:end]
	}
	seqAt := strings.Index(branch, "RT_DEBT201_ABORT_SEQ(task, key)")
	lockAt := strings.Index(branch, "rt_shard_lock(owner_shard);")
	unlockAt := strings.Index(branch, "rt_shard_unlock(owner_shard);")
	removeAt := strings.Index(branch, "remove_waiter_generation(ex, key, task->id, abort_seq);")
	guardAt := strings.Index(branch, "if (abort_seq != 0) {")
	if seqAt < 0 || lockAt < 0 || unlockAt < 0 || removeAt < 0 || guardAt < 0 {
		t.Fatalf("first abort branch lost a required step (seq=%d lock=%d unlock=%d remove=%d guard=%d)",
			seqAt, lockAt, unlockAt, removeAt, guardAt)
	}
	if !(seqAt < lockAt && lockAt < unlockAt && unlockAt < guardAt && guardAt < removeAt) {
		t.Fatalf("first abort branch order broken: generation must be read before the lock and the removal must follow the unlock (seq=%d lock=%d unlock=%d guard=%d remove=%d)",
			seqAt, lockAt, unlockAt, guardAt, removeAt)
	}
}

// lifecycleHarnessParkAbortModes is concatenated into the shared lifecycle
// harness translation unit by buildRuntimeV2LifecycleHarnessWithFlags.
const lifecycleHarnessParkAbortModes = `
#ifdef RT_TEST_SYNC_POINTS

// Counts the live registrations for one exact (key value, task id) pair in the
// store the key routes to. Counting by VALUE is what names the park: a count
// over "entries for this task" would also match an unrelated key.
static size_t debt201_entries_for(rt_executor* ex, waker_key key, uint64_t task_id) {
    rt_shard* shard = rt_waiter_key_shard(ex, key);
    if (shard == NULL) {
        return 0;
    }
    rt_shard_lock(shard);
    const rt_waiter_store* store = rt_shard_waiter_store_const(shard);
    size_t found = 0;
    for (size_t i = 0; store != NULL && i < store->len; i++) {
        const waiter* w = &store->entries[i];
        if (w->task_id == task_id && w->key.kind == key.kind && w->key.id == key.id) {
            found++;
        }
    }
    rt_shard_unlock(shard);
    return found;
}

static int mode_debt201_park_abort_retires_entry(rt_executor* ex) {
    atomic_store_explicit(&g_park_forever_chan, NULL, memory_order_release);
    rt_task* maker = spawn_pinned(ex, POLL_MAKE_PARK_FOREVER_CHAN, 0);
    if (maker == NULL || !wait_ptr(&g_park_forever_chan, 4000) ||
        !wait_task_status(maker, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 channel setup failed");
    }

    unsigned park_before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_PARK_BEFORE_WAITING);
    unsigned cancel_before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE);

    rt_task* body = spawn_pinned(ex, POLL_CANCEL_PARK_PROOF, 0);
    if (body == NULL) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 body allocation failed");
    }
    if (!wait_sync_point_count(RT_SYNC_POINT_SP_PARK_BEFORE_WAITING, park_before, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 body did not reach SP_PARK_BEFORE_WAITING");
    }

    // Name the park by VALUE while its poller is held here. The body is
    // RUNNING and blocked, and the wake path only touches a task's park fields
    // once it is provably parked, so these are stable single-writer reads.
    uint64_t body_id = body->id;
    waker_key key = body->park_key;
    uint32_t seq = body->park_seq;
    unsigned prepared = (unsigned)body->park_prepared;
    unsigned status = (unsigned)task_status_load(body);
    size_t at_window = debt201_entries_for(ex, key, body_id);
    fprintf(stderr,
            "debt201 window: task=%llu kind=%u id=%llu prepared=%u seq=%u status=%u entries=%zu\n",
            (unsigned long long)body_id, (unsigned)key.kind, (unsigned long long)key.id, prepared,
            seq, status, at_window);
    if (status != (unsigned)TASK_RUNNING) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 body was not RUNNING at the park window");
    }
    if (key.kind != WAKER_CHAN_RECV || prepared == 0 || seq == 0) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 caught the wrong park: want the body's own prepared channel receive");
    }
    if (at_window != 1) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 channel registration missing at the park window");
    }

    // Cancel while the poller is still held: the wake token is set BEFORE
    // park_current is entered, so its first exchange reads it and the first
    // abort branch is the branch taken.
    rt_task_cancel(body);
    if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE) <= cancel_before) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 cancel did not reach SP_CANCEL_BEFORE_WAKE");
    }
    rt_sync_point_open();
    if (!wait_task_status(body, TASK_DONE, 4000)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 body stranded after release");
    }

    // Exactly ONE user park crossed the window this whole run. A second
    // crossing would mean the body re-parked on the same key, and
    // channel_park_prepare_locked's dedupe would have re-armed the entry -
    // which would make the count below prove nothing.
    unsigned park_after = rt_sync_point_reached_count(RT_SYNC_POINT_SP_PARK_BEFORE_WAITING);
    size_t left = debt201_entries_for(ex, key, body_id);
    fprintf(stderr, "debt201 after: park_before=%u park_after=%u entries=%zu\n", park_before,
            park_after, left);
    if (park_after != park_before + 1) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 saw more than the one park it drove");
    }
    if (left != 0) {
        (void)rt_executor_request_shutdown(ex);
        return fail("debt201 aborted park left its channel registration behind");
    }
    if (!await_expect(ex, body, 2, 0, "debt201 cancelled body")) {
        return 1;
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
#endif
`
