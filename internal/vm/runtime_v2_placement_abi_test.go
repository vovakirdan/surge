//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeV2PlacementABIStaticShape(t *testing.T) {
	source := `
#include <stdint.h>

#include "rt_placement.h"

rt_placement (*runtime_v2_check_placement_pool)(void) = rt_placement_pool;
rt_placement (*runtime_v2_check_placement_distributed)(void) = rt_placement_distributed;
rt_placement (*runtime_v2_check_placement_shard)(uint32_t) = rt_placement_shard;
rt_placement_resolution (*runtime_v2_check_placement_resolve)(
    rt_runtime*, rt_placement, uint32_t) = rt_placement_resolve;
struct rt_placement_debug_snapshot (*runtime_v2_check_placement_snapshot)(
    const rt_runtime*) = rt_placement_debug_snapshot;

_Static_assert(sizeof(rt_placement) == sizeof(uint64_t),
               "Placement ABI must be one pointer-free tagged word");
_Static_assert(RT_PLACEMENT_KIND_BITS == 8u, "Placement kind must occupy low 8 bits");
_Static_assert(RT_PLACEMENT_KIND_MASK == UINT64_C(0xff), "Placement kind mask must be 8 bits");
_Static_assert(RT_PLACEMENT_KIND_POOL != RT_PLACEMENT_KIND_DISTRIBUTED,
               "pool and distributed must be distinct");
_Static_assert(RT_PLACEMENT_KIND_DISTRIBUTED != RT_PLACEMENT_KIND_SHARD,
               "distributed and shard must be distinct");
_Static_assert(RT_PLACEMENT_STATUS_OK == 0, "OK status must stay zero");
_Static_assert(RT_PLACEMENT_STATUS_UNSUPPORTED != RT_PLACEMENT_STATUS_OK,
               "unsupported must not look successful");
_Static_assert(RT_PLACEMENT_STATUS_INVALID_SHARD != RT_PLACEMENT_STATUS_OK,
               "invalid shard must not look successful");
_Static_assert(sizeof(rt_placement_resolution) >= sizeof(uint64_t),
               "resolution must carry tagged payload evidence");
_Static_assert(sizeof(((rt_placement_resolution*)0)->shard_id) == sizeof(uint32_t),
               "resolved shard id must be uint32_t");
_Static_assert(sizeof(((struct rt_placement_debug_snapshot*)0)->resolve_attempts) ==
                   sizeof(uint64_t),
               "debug counters must be 64-bit");
`

	runFDRegistryStaticCheck(t, "Runtime V2 placement ABI static shape", source)
}

func TestRuntimeV2PlacementResolverRows(t *testing.T) {
	source := `
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "rt_async_internal.h"
#include "rt_placement.h"

static int fail(const char* message) {
    fprintf(stderr, "placement-resolver-check: %s\n", message);
    return 1;
}

static int require_int(int condition, const char* message) {
    if (!condition) {
        return fail(message);
    }
    return 0;
}

static int check_count(size_t shard_count) {
    rt_runtime runtime;
    memset(&runtime, 0, sizeof(runtime));
    runtime.shard_count = shard_count;
    rt_placement_debug_reset(&runtime);

    uint32_t last = (uint32_t)(shard_count - 1u);
    rt_placement_resolution exact = rt_placement_resolve(&runtime, rt_placement_shard(last), 0);
    if (require_int(exact.status == RT_PLACEMENT_STATUS_OK, "exact shard did not resolve")) return 1;
    if (require_int(exact.shard_id == last, "exact shard id changed")) return 2;
    if (require_int(exact.kind == RT_PLACEMENT_KIND_SHARD, "exact kind not recorded")) return 3;
    if (require_int(exact.payload == last, "exact payload not recorded")) return 4;

    rt_placement_resolution invalid =
        rt_placement_resolve(&runtime, rt_placement_shard((uint32_t)shard_count), 0);
    if (require_int(invalid.status == RT_PLACEMENT_STATUS_INVALID_SHARD,
                    "out-of-range shard did not fail closed")) return 5;
    if (require_int(invalid.shard_id == UINT32_MAX,
                    "out-of-range shard produced a destination")) return 6;

    rt_placement_resolution pool = rt_placement_resolve(&runtime, rt_placement_pool(), 0);
    if (require_int(pool.status == RT_PLACEMENT_STATUS_UNSUPPORTED,
                    "pool did not return unsupported")) return 7;
    if (require_int(pool.shard_id == UINT32_MAX, "pool produced a destination")) return 8;

    rt_placement_resolution unknown = rt_placement_resolve(&runtime, UINT64_C(0xfe), 0);
    if (require_int(unknown.status == RT_PLACEMENT_STATUS_UNSUPPORTED,
                    "unknown kind did not return unsupported")) return 9;

    rt_placement_debug_reset(&runtime);
    rt_placement_resolution distributed =
        rt_placement_resolve(&runtime, rt_placement_distributed(), 0);
    if (require_int(distributed.status == RT_PLACEMENT_STATUS_OK,
                    "distributed did not resolve")) return 10;
    if (shard_count == 1) {
        if (require_int(distributed.shard_id == 0, "single-shard distributed changed shard")) return 11;
    } else {
        if (require_int(distributed.shard_id != 0,
                        "first distributed attempt did not prefer non-caller")) return 12;
    }

    uint8_t seen[RT_RUNTIME_MAX_SHARDS] = {0};
    seen[distributed.shard_id] = 1;
    for (size_t i = 0; i < shard_count * 2u; i++) {
        rt_placement_resolution row =
            rt_placement_resolve(&runtime, rt_placement_distributed(), 0);
        if (require_int(row.status == RT_PLACEMENT_STATUS_OK,
                        "distributed round-robin row failed")) return 13;
        if (require_int(row.shard_id < shard_count,
                        "distributed selected shard out of range")) return 14;
        seen[row.shard_id] = 1;
    }
    if (shard_count > 1) {
        int saw_non_caller = 0;
        for (size_t i = 1; i < shard_count; i++) {
            if (seen[i] != 0) {
                saw_non_caller = 1;
            }
        }
        if (require_int(saw_non_caller, "distributed never selected a non-caller")) return 15;
    }

    struct rt_placement_debug_snapshot snapshot = rt_placement_debug_snapshot(&runtime);
    if (require_int(snapshot.distributed_resolutions == 1u + shard_count * 2u,
                    "distributed counter mismatch")) return 16;
    if (shard_count > 1) {
        if (require_int(snapshot.distributed_non_caller_resolutions > 0,
                        "non-caller counter did not record distributed proof")) return 17;
    }
    if (shard_count == 2) {
        rt_placement_debug_reset(&runtime);
        rt_placement_resolution from_one =
            rt_placement_resolve(&runtime, rt_placement_distributed(), 1);
        if (require_int(from_one.status == RT_PLACEMENT_STATUS_OK,
                        "distributed from caller 1 did not resolve")) return 18;
        if (require_int(from_one.shard_id == 0,
                        "distributed from caller 1 did not prefer non-caller")) return 19;
    }
    return 0;
}

int main(void) {
    if (check_count(1) != 0) return 1;
    if (check_count(2) != 0) return 2;
    if (check_count(8) != 0) return 3;
    return 0;
}
`

	runPlacementCProgram(t, "Runtime V2 placement resolver rows", source)
}

func runPlacementCProgram(t *testing.T, label, source string) {
	t.Helper()
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 placement check: %v", err)
	}
	exe := filepath.Join(t.TempDir(), "placement-abi")
	cmd := exec.Command(
		clang,
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-I"+filepath.Join(root, "runtime", "native"),
		"-x",
		"c",
		"-",
		filepath.Join(root, "runtime", "native", "rt_placement.c"),
		"-pthread",
		"-o",
		exe,
	)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s compile failed:\n%s", label, output)
	}

	runCmd := exec.Command(exe)
	runOutput, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s run failed: %v\n%s", label, err, runOutput)
	}
}
