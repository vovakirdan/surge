//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// What this row asks: when a task awaits a CHECKPOINT, does the poll leave a
// join waiter behind?
//
// It is asked because for months the answer was no. rt_task_poll registered a
// join waiter for every target kind except this one, and an unregistered
// awaiter does not wait: the caller branches to PendBB on a 0 return, the yield
// finds no valid park key and reports POLL_YIELDED rather than POLL_PARKED, and
// apply_poll_outcome pushes the awaiter straight back onto the inject queue. So
// `checkpoint().await()` re-entered the ready queue every turn and asked again.
// Usually the checkpoint finishes within a few of those turns and nothing shows;
// in the tail it does not, and one core burns at 100% while the carriers behind
// it sit unstarted.
//
// The rate that defect produces is not a stand -- a rate needs hundreds of runs
// to read and still answers with a probability. This asks the question directly
// and gets one answer every time.
const checkpointAwaitRegistrationSyncPoint = "SP_TASK_POLL_AFTER_JOIN_REGISTER"

func buildRuntimeV2CheckpointAwaitRegistrationHarness(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not installed; skipping checkpoint await registration proof")
	}

	root := repoRoot(t)
	harnessPath := filepath.Join(t.TempDir(), "checkpoint_await_registration.c")
	binPath := filepath.Join(filepath.Dir(harnessPath), "checkpoint_await_registration")
	if err := os.WriteFile(harnessPath, []byte(checkpointAwaitRegistrationHarnessSource), 0o600); err != nil {
		t.Fatalf("write checkpoint await registration harness: %v", err)
	}

	sources, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob runtime sources: %v", err)
	}
	sort.Strings(sources)
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-pthread", "-DRT_TEST_SYNC_POINTS",
		"-I" + filepath.Join(root, "runtime", "native"), "-o", binPath, harnessPath,
	}
	for _, source := range sources {
		if filepath.Base(source) != "rt_entry.c" {
			args = append(args, source)
		}
	}

	cmd := exec.Command(clang, args...)
	cmd.Dir = root
	stdout, stderr, exitCode := runCommand(t, cmd, "")
	if exitCode != 0 {
		t.Fatalf("build checkpoint await registration harness failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	return binPath
}

func runRuntimeV2CheckpointAwaitRegistrationHarness(t *testing.T, binPath string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath)
	cmd.Env = lifecycleEnv(
		"SURGE_SHARDS=2",
		"SURGE_THREADS=2",
		"SURGE_BLOCKING_THREADS=1",
		"SURGE_SYNC_POINT="+checkpointAwaitRegistrationSyncPoint+":block",
	)
	return runCommand(t, cmd, "")
}

// The behavioural row above is defeated by re-introducing a kind test in front
// of the registration, which is exactly the shape that was there. This reads
// rt_task_poll and refuses one: between the cancellation check and the park
// preparation there must be no branch on the target's kind, for a checkpoint or
// for anything else. Kinds are added to this runtime from time to time, and the
// next one must not be able to acquire the exemption by being written the same
// way this one was.
func checkRuntimeV2CheckpointAwaitRegistrationHasNoKindExemption(t *testing.T) {
	path := filepath.Join(repoRoot(t), "runtime", "native", "rt_async_task.c")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rt_async_task.c: %v", err)
	}
	text := string(source)
	start := strings.Index(text, "uint8_t rt_task_poll(void* task, void* out_dst) {")
	if start < 0 {
		t.Fatal("could not find rt_task_poll for the kind-exemption guard")
	}
	body := text[start:]
	if end := strings.Index(body, "\nstatic void rt_task_poll_adopt_placement("); end > 0 {
		body = body[:end]
	}
	gate := strings.Index(body, "if (current_task_cancelled(ex)) {")
	if gate < 0 {
		t.Fatal("could not find the cancel gate that opens the registration window")
	}
	// From the cancel gate forward there is exactly one prepare_park left, the
	// registration one; the DONE branch's park sits above the gate. Matched
	// without its indentation on purpose: a kind test around the block indents
	// it, and the guard must read that shape rather than fail to parse it.
	window := body[gate:]
	register := strings.Index(window, "prepare_park(ex, current, key, 0);")
	if register < 0 {
		t.Fatal("could not find the join registration in rt_task_poll")
	}
	window = window[:register]
	kindTest := regexp.MustCompile(`(?m)^\s*(if|switch)\s*\(.*(target->kind|task_kind)`)
	if kindTest.MatchString(stripCheckpointRegistrationComments(window)) {
		t.Fatal("rt_task_poll must not gate join registration on the target's kind: " +
			"an unregistered awaiter yields instead of parking and busy-loops on the inject queue")
	}
}

// Only code carries the rule; the comment block in this window explains the
// defect and necessarily names both `target->kind` and TASK_KIND_CHECKPOINT.
func stripCheckpointRegistrationComments(source string) string {
	var out strings.Builder
	for _, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

const checkpointAwaitRegistrationHarnessSource = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"
#include "rt_sync_point.h"

#include <sched.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

enum { POLL_CHECKPOINT_JOINER = 9411 };

static int fail(const char* message) {
    fputs(message, stderr);
    fputc('\n', stderr);
    return 1;
}

static uint64_t monotonic_millis(void) {
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    return (uint64_t)now.tv_sec * 1000U + (uint64_t)now.tv_nsec / 1000000U;
}

static int wait_task_status_until(const rt_task* task, uint8_t want) {
    uint64_t deadline = monotonic_millis() + 4000U;
    while (monotonic_millis() < deadline) {
        if (task_status_load(task) == want) {
            return 1;
        }
        sched_yield();
    }
    return 0;
}

static int join_waiter_registered(
    rt_executor* ex, const rt_task* target, const rt_task* joiner) {
    waker_key key = join_key(target->id);
    rt_runtime* runtime = rt_executor_runtime(ex);
    for (;;) {
        uint32_t route = rt_task_join_owner_shard_id_load(target);
        rt_shard* shard = rt_runtime_shard(runtime, route);
        if (shard == NULL) {
            return 0;
        }
        rt_shard_lock(shard);
        if (route != rt_task_join_owner_shard_id_load(target)) {
            rt_shard_unlock(shard);
            continue;
        }
        const rt_waiter_store* store = rt_executor_waiter_store_const_for_shard(ex, route);
        int found = 0;
        for (size_t i = 0; store != NULL && i < store->len; i++) {
            const waiter* entry = &store->entries[i];
            if (entry->key.kind == key.kind && entry->key.id == key.id &&
                entry->task_id == joiner->id) {
                found = 1;
                break;
            }
        }
        rt_shard_unlock(shard);
        return found;
    }
}

// A checkpoint the scheduler cannot finish underneath the question.
//
// A live one is two turns from done -- poll_checkpoint_task yields once and
// then succeeds -- so a target spawned the ordinary way may well be DONE by the
// time the awaiter polls it, and rt_task_poll would answer from its DONE fast
// path having registered nothing. That is not the defect and not its absence;
// it is a race, and a stand that races reports whichever it drew. So the target
// is pinned WAITING and enqueued nowhere: it is a checkpoint that has not
// completed, which is the only state in which "does the awaiter register?" is a
// question with an answer.
static rt_task* make_pinned_checkpoint(rt_executor* ex, uint32_t shard_id) {
    rt_control_lock(ex);
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(*task), _Alignof(rt_task));
    if (task != NULL) {
        memset(task, 0, sizeof(*task));
        task->id = id;
        task->generation = id;
        task->kind = TASK_KIND_CHECKPOINT;
        task_status_store(task, TASK_WAITING);
        task_cancel_gate_init(task);
        task_enqueued_store(task, 0);
        (void)task_wake_token_exchange(task, 0);
        atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
        rt_task_entitlements_init(&task->entitlements);
        rt_task_slot_store(ex, id, task);
        rt_task_set_placement(task, shard_id, TASK_PLACEMENT_CONNECTION);
    }
    rt_control_unlock(ex);
    return task;
}

static void poll_checkpoint_joiner(void) {
    void* target = __task_state();
    uint64_t bits = 0;
    uint8_t status = rt_task_poll(target, &bits);
    if (status == 0) {
        // The state is the target HANDLE, borrowed, not a box this task owns.
        rt_async_yield(target, 0);
    }
    rt_async_return(NULL, &(uint64_t){status == 1 ? bits : 0});
}

void __surge_poll_call(uint64_t id) {
    if (id == POLL_CHECKPOINT_JOINER) {
        poll_checkpoint_joiner();
    }
    rt_async_return(NULL, &(uint64_t){0});
}

void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    if (id == POLL_CHECKPOINT_JOINER && state != NULL) {
        task_release_lane_aware(ensure_exec(), (rt_task*)state);
    }
}

void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
}

int main(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return fail("missing executor");
    }
    rt_task* target = make_pinned_checkpoint(ex, 1);
    if (target == NULL) {
        return fail("checkpoint target allocation failed");
    }

    unsigned before = rt_sync_point_reached_count(
        RT_SYNC_POINT_SP_TASK_POLL_AFTER_JOIN_REGISTER);
    void* joiner_state = rt_task_clone(target, NULL);
    if (joiner_state == NULL) {
        return fail("checkpoint target clone failed");
    }
    rt_task* joiner = (rt_task*)__task_create(
        POLL_CHECKPOINT_JOINER, joiner_state, rt_channel_opaque_word_ops());
    if (joiner == NULL) {
        task_release_lane_aware(ex, (rt_task*)joiner_state);
        return fail("joiner allocation failed");
    }

    // The claim, stated as the wait that answers it: awaiting a checkpoint
    // reaches the registration window. Before the fix this wait always expired
    // -- the checkpoint branch returned 0 above the registration and the sync
    // point was never reached at all.
    if (!rt_sync_point_wait_until_after(
            RT_SYNC_POINT_SP_TASK_POLL_AFTER_JOIN_REGISTER, before)) {
        fprintf(stderr, "joiner status=%u target status=%u\n",
                (unsigned)task_status_load(joiner), (unsigned)task_status_load(target));
        (void)rt_executor_request_shutdown(ex);
        return fail("awaiting a checkpoint did not register a join waiter: "
                    "the awaiter yields instead of parking and re-asks every turn");
    }
    if (!join_waiter_registered(ex, target, joiner)) {
        rt_sync_point_open();
        (void)rt_executor_request_shutdown(ex);
        return fail("checkpoint join waiter was not in the target's waiter store");
    }

    // Released through cancellation rather than through the target: this target
    // is pinned by construction and will never complete, and what is being
    // proved is already proved.
    rt_task_cancel(joiner);
    rt_sync_point_open();
    if (!wait_task_status_until(joiner, TASK_DONE)) {
        (void)rt_executor_request_shutdown(ex);
        return fail("cancelled joiner did not complete");
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(joiner, &kind, &bits);
    if (kind != 2) {
        fprintf(stderr, "joiner completed kind=%u, want 2 (cancelled)\n", (unsigned)kind);
        (void)rt_executor_request_shutdown(ex);
        return fail("joiner did not complete as cancelled");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
`

func TestRuntimeV2AwaitingACheckpointRegistersAJoinWaiter(t *testing.T) {
	t.Run("registers", func(t *testing.T) {
		binPath := buildRuntimeV2CheckpointAwaitRegistrationHarness(t)
		stdout, stderr, exitCode := runRuntimeV2CheckpointAwaitRegistrationHarness(t, binPath)
		if exitCode != 0 {
			t.Fatalf("checkpoint await registration proof failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
				exitCode, stdout, stderr)
		}
	})
	t.Run("no-kind-exemption", checkRuntimeV2CheckpointAwaitRegistrationHasNoKindExemption)
}
