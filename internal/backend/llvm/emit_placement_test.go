package llvm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPlacementIntrinsicsLowerAsTaggedI64(t *testing.T) {
	withRepoStdlib(t)

	sourceCode := `
fn pick_pool() -> Placement {
    return pool;
}

fn pick_distributed() -> Placement {
    return distributed;
}

fn pick_shard() -> Placement {
    return shard(3:ShardId);
}

@entrypoint
fn main() -> int {
    let a = pick_pool();
    let b = pick_distributed();
    let c = pick_shard();
    let _ = a;
    let _ = b;
    let _ = c;
    return 0;
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	poolBody := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", findMIRFunc(t, mirMod, "pick_pool").ID))
	distributedBody := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", findMIRFunc(t, mirMod, "pick_distributed").ID))
	shardBody := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", findMIRFunc(t, mirMod, "pick_shard").ID))

	if !regexp.MustCompile(`define i64 @fn\.\d+\(\)`).MatchString(poolBody) {
		t.Fatalf("Placement-returning function must lower as i64:\n%s", poolBody)
	}
	if !strings.Contains(poolBody, fmt.Sprintf("ret i64 %d", placementEncode(placementKindPool, 0))) {
		t.Fatalf("pool placement did not return tagged i64:\n%s", poolBody)
	}
	if !strings.Contains(distributedBody, fmt.Sprintf("ret i64 %d", placementEncode(placementKindDistributed, 0))) {
		t.Fatalf("distributed placement did not return tagged i64:\n%s", distributedBody)
	}
	if !regexp.MustCompile(`shl i64 %t\d+, 8`).MatchString(shardBody) ||
		!regexp.MustCompile(fmt.Sprintf(`or i64 %%t\d+, %d`, placementKindShard)).MatchString(shardBody) {
		t.Fatalf("shard placement did not encode payload in upper bits:\n%s", shardBody)
	}

	for _, bad := range []string{"@pool", "@distributed", "@shard"} {
		if strings.Contains(ir, bad) {
			t.Fatalf("placement intrinsic leaked unresolved call/global %s:\n%s", bad, ir)
		}
	}
	if strings.Contains(ir, "define ptr @fn.") && strings.Contains(ir, "pick_") {
		t.Fatalf("Placement functions must not lower as ptr:\n%s", ir)
	}
}

func TestUserPlacementAliasDoesNotLowerAsRuntimePlacement(t *testing.T) {
	sourceCode := `pragma no_std;

type Placement = bool;

fn make_user_placement() -> Placement {
    return true;
}

@entrypoint
fn main() -> int {
    let p = make_user_placement();
    if p {
        return 0;
    }
    return 1;
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", findMIRFunc(t, mirMod, "make_user_placement").ID))
	if !regexp.MustCompile(`define i1 @fn\.\d+\(\)`).MatchString(body) {
		t.Fatalf("user alias named Placement must lower as its target bool, not runtime i64:\n%s", body)
	}
}

func TestUserShardFunctionDoesNotBecomePlacementIntrinsic(t *testing.T) {
	sourceCode := `pragma no_std;

fn shard(id: int64) -> int64 {
    return id + 1;
}

fn call_user_shard() -> int64 {
    return shard(41:int64);
}

@entrypoint
fn main() -> int {
    return call_user_shard() as int;
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	_ = ir
}

func withRepoStdlib(t *testing.T) {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "core", "intrinsics.sg")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			t.Setenv("SURGE_STDLIB", dir)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo stdlib root from %s", dir)
		}
		dir = parent
	}
}
