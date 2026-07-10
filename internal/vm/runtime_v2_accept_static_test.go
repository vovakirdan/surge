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
	netOwnedAccessors := []string{
		"rt_executor_net_poll_scratch",
		"rt_executor_fd_registry",
		"rt_executor_fd_registry_const",
	}
	var offenders []string
	for _, name := range netOwnedAccessors {
		body, ok := cFunctionBody(source, name)
		if !ok {
			t.Fatalf("net-owned accessor %s not found in rt_runtime.c", name)
		}
		if cBodyHasShard0Shortcut(body) {
			offenders = append(offenders, "runtime/native/rt_runtime.c:"+name)
		}
	}

	netOwnedSources := []struct {
		path  string
		names []string
	}{
		{
			path: "runtime/native/rt_net.c",
			names: []string{
				"rt_net_listen",
				"rt_net_connect",
				"rt_net_close_listener",
				"rt_net_close_conn",
				"rt_net_accept",
				"net_wait_current_task",
				"rt_net_wait_readable",
				"rt_net_wait_writable",
			},
		},
		{
			path:  "runtime/native/rt_net_poll_pass.c",
			names: []string{"poll_net_waiters_on_shard"},
		},
		{
			path: "runtime/native/rt_net_poller.c",
			names: []string{
				"rt_net_poll_wake_init",
				"rt_net_wake_poll_on_shard",
				"rt_net_wake_poll_all_shards",
				"rt_net_has_waiters_on_shard",
				"rt_net_begin_poll_on_shard",
				"rt_net_poll_waiters_owned_on_shard",
			},
		},
		{
			path: "runtime/native/rt_net_accept_group.c",
			names: []string{
				"rt_net_register_open_fd_on_owner",
				"rt_net_interest_present_for_key",
				"rt_net_fd_ready_now",
				"rt_net_wait_accept",
			},
		},
		{
			path: "runtime/native/rt_net_lifecycle.c",
			names: []string{
				"rt_net_owner_shard_or_compat",
				"rt_net_close_fd_on_owner",
				"rt_net_close_listener_members",
			},
		},
		{
			path: "runtime/native/rt_fd_registry.c",
			names: []string{
				"rt_fd_registry_complete_ready_net_waiters",
				"rt_fd_registry_drain_shutdown_net_waiters_locked_on_owner",
			},
		},
		{
			path: "runtime/native/rt_shutdown.c",
			names: []string{
				"rt_executor_drain_shutdown_net_waiters",
				"rt_executor_request_shutdown",
			},
		},
		{
			path: "runtime/native/rt_async_waiter.c",
			names: []string{
				"rt_net_owner_shard_probe_locked",
				"fd_registry_bridge_net_detach_if_last_on_owner",
			},
		},
		{
			path:  "runtime/native/rt_async_state.c",
			names: []string{"next_ready"},
		},
		{
			// park_current extracted to rt_task_park.c.
			path:  "runtime/native/rt_task_park.c",
			names: []string{"park_current"},
		},
		{
			path:  "runtime/native/rt_worker_turn.c",
			names: []string{"rt_io_main", "rt_worker_main"},
		},
	}
	for _, target := range netOwnedSources {
		targetBytes, err := os.ReadFile(filepath.Join(root, target.path))
		if err != nil {
			t.Fatalf("read %s: %v", target.path, err)
		}
		targetSource := string(targetBytes)
		for _, name := range target.names {
			body, ok := cFunctionBody(targetSource, name)
			if !ok {
				t.Fatalf("net-owned function %s:%s not found", target.path, name)
			}
			if cBodyHasShard0Shortcut(body) {
				offenders = append(offenders, target.path+":"+name)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("net-owned paths still route through shard 0: %s; pass explicit owner shards", strings.Join(offenders, ", "))
	}

	// Stays-global compatibility exemptions:
	// channel blocking compat, generic waiter-store compat, and scheduler
	// compatibility until owner placement is defined. These paths may keep
	// explicit shard-0/global accessors while net ownership migrates.
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

func TestRuntimeV2AcceptReadinessClearsSiblingWaitKeys(t *testing.T) {
	root := repoRoot(t)
	sourceBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_async_waiter.c"))
	if err != nil {
		t.Fatalf("read rt_async_waiter.c: %v", err)
	}
	source := string(sourceBytes)

	wakeBody, ok := cFunctionBody(source, "rt_executor_wake_net_waiters_for_key_on_owner")
	if !ok {
		t.Fatal("rt_executor_wake_net_waiters_for_key_on_owner not found")
	}
	if !strings.Contains(wakeBody, "clear_accept_winner_wait_keys") {
		t.Fatalf("accept readiness must clear sibling listener-member wait keys before closing")
	}

	clearBody, ok := cFunctionBody(source, "clear_accept_winner_wait_keys")
	if !ok {
		t.Fatal("clear_accept_winner_wait_keys not found")
	}
	for _, needle := range []string{
		"key.kind != WAKER_NET_ACCEPT",
		"net_ready_accept_valid",
		"net_ready_accept_fd",
		"net_ready_accept_owner_shard",
		"clear_wait_keys(ex, task)",
	} {
		if !strings.Contains(clearBody, needle) {
			t.Fatalf("clear_accept_winner_wait_keys is missing %q", needle)
		}
	}
}

func TestRuntimeV2AcceptListenerRegistryGrowsUnderLock(t *testing.T) {
	root := repoRoot(t)
	sourceBytes, err := os.ReadFile(filepath.Join(root, "runtime", "native", "rt_net_handles.c"))
	if err != nil {
		t.Fatalf("read rt_net_handles.c: %v", err)
	}
	body, ok := cFunctionBody(string(sourceBytes), "rt_net_listener_registry_add")
	if !ok {
		t.Fatal("rt_net_listener_registry_add not found")
	}
	lockAt := strings.Index(body, "pthread_mutex_lock(&net_listener_registry_lock)")
	wantAt := strings.Index(body, "size_t want = net_listener_registry_len")
	ensureAt := strings.Index(body, "net_listener_registry_ensure_cap(want)")
	if lockAt < 0 || wantAt < 0 || ensureAt < 0 {
		t.Fatalf("listener registry add must lock, size from current len, and ensure capacity")
	}
	if !(lockAt < wantAt && wantAt < ensureAt) {
		t.Fatalf("listener registry capacity must be computed under net_listener_registry_lock")
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
#error "fixed shard count storage must be replaced with RT_RUNTIME_MAX_SHARDS"
#else
#if RT_RUNTIME_MAX_SHARDS < 1
#error "RT_RUNTIME_MAX_SHARDS must be positive"
#endif

_Static_assert(RT_RUNTIME_MAX_SHARDS >= 1, "RT_RUNTIME_MAX_SHARDS must be positive");
_Static_assert(sizeof(((rt_runtime*)0)->shards) / sizeof(((rt_runtime*)0)->shards[0]) == RT_RUNTIME_MAX_SHARDS,
               "rt_runtime.shards must be sized by RT_RUNTIME_MAX_SHARDS");

int runtime_v2_accept_dynamic_shape(const rt_runtime* runtime) {
    if (rt_runtime_shard_count(runtime) < 1 ||
        rt_runtime_shard_count(runtime) > RT_RUNTIME_MAX_SHARDS) {
        return 0;
    }
    return 1;
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

func cBodyHasShard0Shortcut(body string) bool {
	return strings.Contains(body, "rt_runtime_shard0(") || strings.Contains(body, "shards[0]")
}
