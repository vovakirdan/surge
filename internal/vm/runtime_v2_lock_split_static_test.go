//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// static gates. These pin the post-split shape decided by
// 07-locking-model-proving-spike.md (decisions D1-D16). They are expected to
// stay red until the implementation lands and are wired into CI only once
// green. Until then no Makefile gate runs them.

func runLockSplitClangShapeCheck(t *testing.T, source string) {
	t.Helper()
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 pending static check: %v", err)
	}
	cmd := exec.Command(
		clang,
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-fsyntax-only",
		"-I"+filepath.Join(root, "runtime", "native"),
		"-x",
		"c",
		"-",
	)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lock-split static shape check failed:\n%s", output)
	}
}

// D1: each shard owns its lock and its two condition variables, and the
// waiter entry carries the owner hint (D3/D5).
func TestRuntimeV2LockSplitShardSyncShape(t *testing.T) {
	runLockSplitClangShapeCheck(t, `
#include "rt_async_internal.h"

_Static_assert(sizeof(((rt_shard*)0)->lock) == sizeof(pthread_mutex_t),
               "rt_shard.lock must be the shard mutex");
_Static_assert(sizeof(((rt_shard*)0)->worker_cv) == sizeof(pthread_cond_t),
               "rt_shard.worker_cv must be the shard worker condvar");
_Static_assert(sizeof(((rt_shard*)0)->poller_cv) == sizeof(pthread_cond_t),
               "rt_shard.poller_cv must be the shard poller condvar");
_Static_assert(sizeof(((waiter*)0)->owner_hint) == sizeof(uint32_t),
               "waiter entries must carry the task owner hint");
`)
}

// D2: the two lane entry points exist; rt_lock/rt_unlock are replaced by
// explicit lane choices at every call site.
func TestRuntimeV2LockSplitLaneAPIShape(t *testing.T) {
	runLockSplitClangShapeCheck(t, `
#include "rt_async_internal.h"

void lock_split_lane_api_shape(void);
void lock_split_lane_api_shape(void) {
    void (*control_lock)(rt_executor*) = rt_control_lock;
    void (*control_unlock)(rt_executor*) = rt_control_unlock;
    void (*shard_lock)(rt_shard*) = rt_shard_lock;
    void (*shard_unlock)(rt_shard*) = rt_shard_unlock;
    int (*lane_debug)(void) = rt_lane_debug_enabled;
    (void)control_lock;
    (void)control_unlock;
    (void)shard_lock;
    (void)shard_unlock;
    (void)lane_debug;
}
`)
}

// D7: the virtual clock is an atomic; sleepers live in a per-shard store.
func TestRuntimeV2LockSplitClockAndSleepStoreShape(t *testing.T) {
	runLockSplitClangShapeCheck(t, `
#include "rt_async_internal.h"

uint64_t lock_split_clock_shape(rt_executor* ex, rt_shard* shard);
uint64_t lock_split_clock_shape(rt_executor* ex, rt_shard* shard) {
    (void)sizeof(((rt_shard*)0)->sleep_store);
    (void)shard;
    return atomic_load_explicit(&ex->now_ms, memory_order_acquire);
}
`)
}

// After the split every locking call site names its lane: the ambiguous
// global helpers must be gone from the native runtime.
func TestRuntimeV2LockSplitNoAmbiguousGlobalLock(t *testing.T) {
	offenders := lockSplitScanNativeSources(t, func(path, source string) []string {
		var hits []string
		for _, needle := range []string{"rt_lock(", "rt_unlock("} {
			at := 0
			for {
				idx := strings.Index(source[at:], needle)
				if idx < 0 {
					break
				}
				abs := at + idx
				// skip declarations/definitions of the removed helpers
				lineStart := strings.LastIndexByte(source[:abs], '\n') + 1
				line := source[lineStart:]
				if end := strings.IndexByte(line, '\n'); end >= 0 {
					line = line[:end]
				}
				if !strings.Contains(line, "void rt_lock(") && !strings.Contains(line, "void rt_unlock(") {
					hits = append(hits, path+": "+strings.TrimSpace(line))
				}
				at = abs + len(needle)
			}
		}
		return hits
	})
	if len(offenders) > 0 {
		t.Fatalf("ambiguous global lock helpers still in use after the split:\n%s",
			strings.Join(offenders, "\n"))
	}
}

// D6: the worker scheduler turn runs on the shard lane and never takes the
// control lane on its steady path.
func TestRuntimeV2LockSplitWorkerLoopShardLane(t *testing.T) {
	body := lockSplitFindFunctionBody(t, "rt_worker_main")
	if !strings.Contains(body, "rt_shard_lock(") {
		t.Fatal("rt_worker_main must take its shard lock")
	}
	for _, banned := range []string{"rt_control_lock(", "rt_lock("} {
		if strings.Contains(body, banned) {
			t.Fatalf("rt_worker_main must not use %s on the worker turn", banned)
		}
	}
}

// D7: the whole-task-table sleep scans die with the sleep store.
func TestRuntimeV2LockSplitNoWholeTableSleepScan(t *testing.T) {
	for _, fn := range []string{"tick_virtual", "advance_time_to_next_timer"} {
		body := lockSplitFindFunctionBody(t, fn)
		if strings.Contains(body, "tasks_cap") {
			t.Fatalf("%s still scans the whole task table; sleepers must come from the sleep store", fn)
		}
	}
}

// D5/D10 (channel slice): channels are owner-shard state; the direct send
// path locks the channel owner's shard, not the control lane.
func TestRuntimeV2LockSplitChannelOwnerShape(t *testing.T) {
	root := repoRoot(t)
	sourceBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_async_channel.c"))
	if err != nil {
		t.Fatalf("read rt_async_channel.c: %v", err)
	}
	source := string(sourceBytes)
	if !strings.Contains(source, "owner_shard_id") {
		t.Fatal("struct rt_channel must record its owner shard")
	}
	body, ok := cFunctionBody(source, "rt_channel_send_inner")
	if !ok {
		t.Fatal("rt_channel_send_inner not found")
	}
	if !strings.Contains(body, "rt_shard_lock(") && !strings.Contains(body, "rt_channel_owner_lock(") {
		t.Fatal("rt_channel_send_inner must lock the channel owner's shard")
	}
	for _, banned := range []string{"rt_control_lock(", "rt_lock("} {
		if strings.Contains(body, banned) {
			t.Fatalf("rt_channel_send_inner must not use %s on the steady path", banned)
		}
	}
}

// D1: the global ready_cv and io_cv retire once their last waiters migrate.
func TestRuntimeV2LockSplitGlobalCondvarRetirement(t *testing.T) {
	offenders := lockSplitScanNativeSources(t, func(path, source string) []string {
		var hits []string
		for _, needle := range []string{"ready_cv", "io_cv"} {
			if strings.Contains(source, needle) {
				hits = append(hits, path+": still references "+needle)
			}
		}
		return hits
	})
	if len(offenders) > 0 {
		t.Fatalf("global condvars must retire with the lock split:\n%s",
			strings.Join(offenders, "\n"))
	}
}

func lockSplitScanNativeSources(t *testing.T, scan func(path, source string) []string) []string {
	t.Helper()
	root := repoRoot(t)
	patterns := []string{
		filepath.Join(root, "runtime", "native", "*.c"),
		filepath.Join(root, "runtime", "native", "*.h"),
	}
	var offenders []string
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		for _, path := range paths {
			sourceBytes, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read %s: %v", path, readErr)
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			offenders = append(offenders, scan(rel, string(sourceBytes))...)
		}
	}
	return offenders
}

func lockSplitFindFunctionBody(t *testing.T, name string) string {
	t.Helper()
	root := repoRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, "runtime", "native", "*.c"))
	if err != nil {
		t.Fatalf("glob native sources: %v", err)
	}
	for _, path := range paths {
		sourceBytes, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		if body, ok := lockSplitFunctionDefinitionBody(string(sourceBytes), name); ok {
			return body
		}
	}
	t.Fatalf("function %s not found in runtime/native", name)
	return ""
}

// lockSplitFunctionDefinitionBody finds the definition of name, skipping
// forward declarations and call sites: the parameter list must be followed
// by an opening brace, not a semicolon.
func lockSplitFunctionDefinitionBody(source, name string) (string, bool) {
	at := 0
	for {
		idx := strings.Index(source[at:], name+"(")
		if idx < 0 {
			return "", false
		}
		abs := at + idx
		at = abs + len(name)
		depth := 0
		end := -1
		for i := abs + len(name); i < len(source); i++ {
			ch := source[i]
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if end < 0 {
			return "", false
		}
		rest := strings.TrimLeft(source[end+1:], " \t\r\n")
		if !strings.HasPrefix(rest, "{") {
			continue
		}
		bodyStart := end + 1 + strings.Index(source[end+1:], "{") + 1
		braceDepth := 1
		for i := bodyStart; i < len(source); i++ {
			switch source[i] {
			case '{':
				braceDepth++
			case '}':
				braceDepth--
				if braceDepth == 0 {
					return source[bodyStart:i], true
				}
			}
		}
		return "", false
	}
}
