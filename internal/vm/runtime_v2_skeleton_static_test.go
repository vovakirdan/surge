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

#if defined(RT_RUNTIME_MAX_SHARDS)
#define RT_RUNTIME_STATIC_SHARD_LIMIT RT_RUNTIME_MAX_SHARDS
#elif defined(RT_RUNTIME_SHARD_COUNT)
#define RT_RUNTIME_STATIC_SHARD_LIMIT RT_RUNTIME_SHARD_COUNT
#else
#error "Runtime V2 must define a static shard storage limit"
#endif

#if RT_RUNTIME_STATIC_SHARD_LIMIT < 1
#error "Runtime V2 static shard storage limit must be positive"
#endif

static void runtime_v2_skeleton_compat_shape(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* shard0 = rt_runtime_shard0(runtime);
    size_t shard_count = rt_runtime_shard_count(runtime);

    _Static_assert(RT_RUNTIME_STATIC_SHARD_LIMIT >= 1, "runtime shard storage limit must be positive");
    _Static_assert(sizeof(rt_runtime) > 0, "rt_runtime must be complete");
    _Static_assert(sizeof(rt_shard) > 0, "rt_shard must be complete");

    if (shard_count < 1 || shard_count > RT_RUNTIME_STATIC_SHARD_LIMIT) {
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
