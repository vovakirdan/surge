//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2OwnerLocalWaiterSkeletonStaticShape(t *testing.T) {
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skipf("clang not installed; skipping Runtime V2 owner-local waiter static shape check: %v", err)
	}

	source := `
	#include "rt_async_internal.h"

	rt_waiter_store* (*runtime_v2_check_shard_waiter_store)(rt_shard*) = rt_shard_waiter_store;
	const rt_waiter_store* (*runtime_v2_check_shard_waiter_store_const)(const rt_shard*) = rt_shard_waiter_store_const;
	rt_waiter_store* (*runtime_v2_check_executor_waiter_store)(rt_executor*) = rt_executor_waiter_store;
	const rt_waiter_store* (*runtime_v2_check_executor_waiter_store_const)(const rt_executor*) = rt_executor_waiter_store_const;

	_Static_assert(sizeof(rt_waiter_store) > 0, "rt_waiter_store must be complete");
	_Static_assert(sizeof(((rt_waiter_store*)0)->entries) == sizeof(waiter*), "rt_waiter_store.entries must store waiter entries");
	_Static_assert(sizeof(((rt_waiter_store*)0)->len) == sizeof(size_t), "rt_waiter_store.len must stay size_t");
	_Static_assert(sizeof(((rt_waiter_store*)0)->cap) == sizeof(size_t), "rt_waiter_store.cap must stay size_t");
	_Static_assert(sizeof(((rt_waiter_store*)0)->net_len) == sizeof(size_t), "rt_waiter_store.net_len must stay size_t");
	_Static_assert(sizeof(((rt_shard*)0)->waiter_store) == sizeof(rt_waiter_store), "rt_shard.waiter_store must own waiter storage");
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
		t.Fatalf("Runtime V2 owner-local waiter static shape check failed:\n%s", output)
	}
}
