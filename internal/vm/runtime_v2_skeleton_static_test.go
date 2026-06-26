//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2SkeletonStaticShape(t *testing.T) {
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 pending static check: %v", err)
	}

	source := `
#include "rt_async_internal.h"

#ifndef RT_RUNTIME_SHARD_COUNT
#error "RT_RUNTIME_SHARD_COUNT must define the N=1 runtime shape"
#endif

#if RT_RUNTIME_SHARD_COUNT != 1
#error "Runtime V2 skeleton must expose exactly one shard"
#endif

static void runtime_v2_skeleton_shape(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* shard0 = rt_runtime_shard0(runtime);

    _Static_assert(RT_RUNTIME_SHARD_COUNT == 1, "runtime skeleton must be N=1");
    _Static_assert(sizeof(rt_runtime) > 0, "rt_runtime must be complete");
    _Static_assert(sizeof(rt_shard) > 0, "rt_shard must be complete");

    if (rt_runtime_shard_count(runtime) != 1) {
        __builtin_trap();
    }

    (void)shard0;
}
`

	cmd := exec.Command(
		clang,
		"-std=c11",
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
		t.Fatalf("Runtime V2 skeleton static shape check failed:\n%s", output)
	}
}
