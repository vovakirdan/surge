//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2SchedulerPlacementStealPathSourceGate(t *testing.T) {
	source := readRuntimeV2SchedulerStateSource(t)
	body, ok := cFunctionBody(source, "pop_task_from_deque")
	if !ok {
		t.Fatal("pop_task_from_deque not found")
	}
	guardIndex := strings.Index(body, "rt_task_can_steal_from_shard_or_trace_denied(task, stealer_shard_id)")
	traceIndex := strings.Index(body, "rt_trace_sched_record(source, id)")
	if guardIndex < 0 || traceIndex < 0 || guardIndex > traceIndex {
		t.Fatal("steal path must validate owner shard before tracing a pop")
	}
	conditionIndex := strings.LastIndex(body[:guardIndex], "source == RT_TRACE_SCHED_SRC_STEAL")
	if conditionIndex < 0 {
		t.Fatal("steal validation is not gated by RT_TRACE_SCHED_SRC_STEAL")
	}
	deniedSegment := body[conditionIndex:traceIndex]
	if !strings.Contains(deniedSegment, "deque_push_") || !strings.Contains(deniedSegment, "return 0;") {
		t.Fatalf("steal denial branch must restore the task and return before tracing:\n%s", deniedSegment)
	}
	placementSource := readRuntimeV2SchedulerPlacementSource(t)
	helperBody, ok := cFunctionBody(placementSource, "rt_task_can_steal_from_shard_or_trace_denied")
	if !ok {
		t.Fatal("rt_task_can_steal_from_shard_or_trace_denied not found")
	}
	if !strings.Contains(helperBody, "rt_task_can_steal_from_shard(task, shard_id)") ||
		!strings.Contains(helperBody, "rt_trace_sched_tier1_steal_denied()") {
		t.Fatalf("steal denial helper must preserve the no-steal check and expose Task 12 evidence:\n%s",
			helperBody)
	}
	for offset := 0; ; {
		relative := strings.Index(source[offset:], "RT_TRACE_SCHED_SRC_STEAL")
		if relative < 0 {
			break
		}
		index := offset + relative
		beforeStart := index - 80
		if beforeStart < 0 {
			beforeStart = 0
		}
		afterEnd := index + 220
		if afterEnd > len(source) {
			afterEnd = len(source)
		}
		isGuard := strings.Contains(source[beforeStart:index], "source ==")
		if !isGuard && !strings.Contains(source[index:afterEnd], "shard_id") {
			t.Fatalf("steal source at byte %d is not passed the stealing shard id", index)
		}
		offset = index + len("RT_TRACE_SCHED_SRC_STEAL")
	}
}

func TestRuntimeV2SchedulerPlacementParkedWithWorkSourceGate(t *testing.T) {
	source := readRuntimeV2SchedulerStateSource(t)
	assertIndex := strings.Index(source, "rt_debug_assert_no_parked_with_work(ex, ctx->shard_id)")
	if assertIndex < 0 {
		t.Fatal("worker sleep path must assert no shard-local queued work before cond wait")
	}
	waitIndex := strings.Index(source[assertIndex:], "pthread_cond_wait(&ex->ready_cv, &ex->lock)")
	if waitIndex < 0 {
		t.Fatal("worker sleep path must assert no shard-local queued work before cond wait")
	}
	loopStart := strings.LastIndex(source[:assertIndex], "worker_next_ready(ctx, &id)")
	if loopStart < 0 {
		t.Fatal("parked-with-work assertion is not in the worker no-ready loop")
	}
}

func readRuntimeV2SchedulerStateSource(t *testing.T) string {
	t.Helper()
	// Ready-queue cluster extracted to rt_ready_queue.c (Epic 10 Task 2).
	sourceBytes, err := os.ReadFile(filepath.Join(repoRoot(t), "runtime", "native", "rt_ready_queue.c"))
	if err != nil {
		t.Fatalf("read rt_ready_queue.c: %v", err)
	}
	return string(sourceBytes)
}

func readRuntimeV2SchedulerPlacementSource(t *testing.T) string {
	t.Helper()
	sourceBytes, err := os.ReadFile(filepath.Join(repoRoot(t), "runtime", "native", "rt_scheduler_placement.c"))
	if err != nil {
		t.Fatalf("read rt_scheduler_placement.c: %v", err)
	}
	return string(sourceBytes)
}
