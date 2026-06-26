package vm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2WaiterHelperStaticBoundary(t *testing.T) {
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skipf("clang not installed; skipping Runtime V2 waiter static boundary check: %v", err)
	}

	source := `
#include "rt_async_internal.h"

waker_key (*runtime_v2_check_waker_none)(void) = waker_none;
int (*runtime_v2_check_waker_valid)(waker_key) = waker_valid;
waker_key (*runtime_v2_check_join_key)(uint64_t) = join_key;
waker_key (*runtime_v2_check_timer_key)(uint64_t) = timer_key;
waker_key (*runtime_v2_check_scope_key)(uint64_t) = scope_key;
waker_key (*runtime_v2_check_channel_send_key)(const rt_channel*) = channel_send_key;
waker_key (*runtime_v2_check_channel_recv_key)(const rt_channel*) = channel_recv_key;
waker_key (*runtime_v2_check_net_accept_key)(int) = net_accept_key;
waker_key (*runtime_v2_check_net_read_key)(int) = net_read_key;
waker_key (*runtime_v2_check_net_write_key)(int) = net_write_key;
waker_key (*runtime_v2_check_blocking_key)(uint64_t) = blocking_key;

rt_runtime_status (*runtime_v2_check_waiter_store_ensure_cap)(rt_waiter_store*) = rt_waiter_store_ensure_cap;
void (*runtime_v2_check_ensure_waiter_cap)(rt_executor*) = ensure_waiter_cap;
void (*runtime_v2_check_remove_waiter)(rt_executor*, waker_key, uint64_t) = remove_waiter;
void (*runtime_v2_check_add_waiter)(rt_executor*, waker_key, uint64_t) = add_waiter;
void (*runtime_v2_check_clear_wait_keys)(rt_executor*, rt_task*) = clear_wait_keys;
void (*runtime_v2_check_add_wait_key)(rt_executor*, rt_task*, waker_key) = add_wait_key;
void (*runtime_v2_check_prepare_park)(rt_executor*, rt_task*, waker_key, int) = prepare_park;
int (*runtime_v2_check_pop_waiter)(rt_executor*, waker_key, uint64_t*) = pop_waiter;
rt_waiter_store* (*runtime_v2_check_shard_waiter_store)(rt_shard*) = rt_shard_waiter_store;
const rt_waiter_store* (*runtime_v2_check_shard_waiter_store_const)(const rt_shard*) = rt_shard_waiter_store_const;
rt_waiter_store* (*runtime_v2_check_executor_waiter_store)(rt_executor*) = rt_executor_waiter_store;
const rt_waiter_store* (*runtime_v2_check_executor_waiter_store_const)(const rt_executor*) = rt_executor_waiter_store_const;

_Static_assert(sizeof(((waker_key*)0)->kind) == sizeof(uint8_t), "waker_key.kind must stay byte-sized");
_Static_assert(sizeof(((waker_key*)0)->id) == sizeof(uint64_t), "waker_key.id must stay uint64_t");
_Static_assert(sizeof(((waiter*)0)->key) == sizeof(waker_key), "waiter.key must store a waker_key");
_Static_assert(sizeof(((waiter*)0)->task_id) == sizeof(uint64_t), "waiter.task_id must stay uint64_t");

_Static_assert(sizeof(rt_waiter_store) > 0, "rt_waiter_store must be complete");
_Static_assert(sizeof(((rt_waiter_store*)0)->entries) == sizeof(waiter*), "rt_waiter_store.entries must store waiter entries");
_Static_assert(sizeof(((rt_waiter_store*)0)->len) == sizeof(size_t), "rt_waiter_store.len must stay size_t");
_Static_assert(sizeof(((rt_waiter_store*)0)->cap) == sizeof(size_t), "rt_waiter_store.cap must stay size_t");
_Static_assert(sizeof(((rt_waiter_store*)0)->net_len) == sizeof(size_t), "rt_waiter_store.net_len must stay size_t");
_Static_assert(sizeof(((rt_shard*)0)->waiter_store) == sizeof(rt_waiter_store), "rt_shard.waiter_store must own waiter storage");

_Static_assert(sizeof(((rt_task*)0)->wait_keys) == sizeof(waker_key*), "rt_task.wait_keys must track prepared keys");
_Static_assert(sizeof(((rt_task*)0)->wait_keys_len) == sizeof(size_t), "rt_task.wait_keys_len must stay size_t");
_Static_assert(sizeof(((rt_task*)0)->wait_keys_cap) == sizeof(size_t), "rt_task.wait_keys_cap must stay size_t");
_Static_assert(sizeof(((rt_task*)0)->park_key) == sizeof(waker_key), "rt_task.park_key must stay a waker_key");
_Static_assert(sizeof(((rt_task*)0)->park_prepared) == sizeof(uint8_t), "rt_task.park_prepared must stay byte-sized");
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
		t.Fatalf("Runtime V2 waiter static boundary check failed:\n%s", output)
	}
}
