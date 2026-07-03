//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2AcceptNetOwnershipNoShard0Shortcut(t *testing.T) {
	root := repoRoot(t)
	sourcePath := filepath.Join(root, "runtime", "native", "rt_runtime.c")
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read rt_runtime.c: %v", err)
	}

	source := string(sourceBytes)
	netOwned := []string{
		"rt_executor_net_poll_scratch",
		"rt_executor_fd_registry",
		"rt_executor_fd_registry_const",
	}
	var offenders []string
	for _, name := range netOwned {
		body, ok := cFunctionBody(source, name)
		if !ok {
			t.Fatalf("net-owned accessor %s not found in rt_runtime.c", name)
		}
		if strings.Contains(body, "rt_runtime_shard0(") || strings.Contains(body, "shards[0]") {
			offenders = append(offenders, name)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("net-owned accessors still route through shard 0: %s; pass explicit owner shards before completing Epic 6", strings.Join(offenders, ", "))
	}

	// Stays-global compatibility exemptions from Task 2:
	// channel blocking compat, generic waiter-store compat, and scheduler
	// compatibility until Task 7 defines owner placement. These paths may keep
	// explicit shard-0/global accessors in Epic 6 while net ownership migrates.
	staysGlobal := []string{
		"rt_executor_scheduler",
		"rt_executor_scheduler_const",
		"rt_executor_channel_blocking_compat",
		"rt_executor_channel_blocking_compat_const",
		"rt_executor_waiter_store",
		"rt_executor_waiter_store_const",
	}
	for _, name := range staysGlobal {
		if _, ok := cFunctionBody(source, name); !ok {
			t.Fatalf("documented stays-global accessor %s not found in rt_runtime.c", name)
		}
	}
}

func TestRuntimeV2AcceptDynamicShardArrayShape(t *testing.T) {
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 pending static check: %v", err)
	}

	source := `
#include "rt_async_internal.h"

#ifndef RT_RUNTIME_MAX_SHARDS
#error "Task 6 must replace fixed shard count storage with RT_RUNTIME_MAX_SHARDS"
#else
#if RT_RUNTIME_MAX_SHARDS < 1
#error "RT_RUNTIME_MAX_SHARDS must be positive"
#endif

static void runtime_v2_accept_dynamic_shape(rt_runtime* runtime) {
    _Static_assert(RT_RUNTIME_MAX_SHARDS >= 1, "RT_RUNTIME_MAX_SHARDS must be positive");
    _Static_assert(sizeof(runtime->shards) / sizeof(runtime->shards[0]) == RT_RUNTIME_MAX_SHARDS,
                   "rt_runtime.shards must be sized by RT_RUNTIME_MAX_SHARDS");

    if (rt_runtime_shard_count(runtime) < 1 ||
        rt_runtime_shard_count(runtime) > RT_RUNTIME_MAX_SHARDS) {
        __builtin_trap();
    }
}
#endif
`

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
		t.Fatalf("Runtime V2 dynamic shard array shape check failed:\n%s", output)
	}
}

func cFunctionBody(source, name string) (string, bool) {
	start := strings.Index(source, name+"(")
	if start < 0 {
		return "", false
	}
	open := strings.Index(source[start:], "{")
	if open < 0 {
		return "", false
	}
	bodyStart := start + open + 1
	depth := 1
	for offset, ch := range source[bodyStart:] {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[bodyStart : bodyStart+offset], true
			}
		}
	}
	return "", false
}
