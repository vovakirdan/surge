package llvm

import (
	"strings"
	"testing"

	"surge/internal/sema"
)

// A crossing's one duplication is charged where it happens: the state block
// gets a copy of every Copy capture, and the initial block of the crossing
// reports those bytes to the runtime's resident-byte telemetry. An owned
// capture moves and is not charged; a retry poll ships nothing and is not
// charged. The mutant is a counter emitted before the retry branch: the
// same call would then appear on every poll of a pending crossing, and the
// "exactly once" assertion goes red.
func TestEmitCrossingChargesCopyCapturesOnce(t *testing.T) {
	sourceCode := `
@copy
type Block = { a: int, b: int, c: int, d: int, e: int, f: int, g: int, h: int };

fn weigh(block: Block) -> int {
    return block.a + block.h;
}

async fn ship(block: Block) -> int {
    let task: far Task<int> = spawn on shard(1:ShardId) { ret weigh(block); };
    return compare task.await() { Success(x) => x; Cancelled() => 0 - 1; };
}
`
	mirMod, result := lowerCrossingMIRFromSource(
		t, sourceCode, sema.CrossingLoweringSpawnOn, sema.CrossingLoweringFarTaskAwait)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	const want = "call void @rt_resident_bytes_record_crossing_clone(i64 64)"
	if got := strings.Count(ir, want); got != 1 {
		t.Fatalf("copy capture charged %d times, want exactly once in the crossing's initial block:\n%s", got, ir)
	}
	if !strings.Contains(ir, "declare void @rt_resident_bytes_record_crossing_clone(i64)") {
		t.Fatalf("crossing clone counter is not declared:\n%s", ir)
	}
}

func TestEmitCrossingDoesNotChargeMovedCaptures(t *testing.T) {
	sourceCode := `
@shard_movable
type Movable = { id: int };

async fn ship() -> int {
    let j: own Movable = own Movable{ id: 4 };
    let task: far Task<int> = spawn on shard(1:ShardId) { ret j.id; };
    return compare task.await() { Success(x) => x; Cancelled() => 0 - 1; };
}
`
	mirMod, result := lowerCrossingMIRFromSource(
		t, sourceCode, sema.CrossingLoweringSpawnOn, sema.CrossingLoweringFarTaskAwait)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	if strings.Contains(ir, "@rt_resident_bytes_record_crossing_clone(i64 ") {
		t.Fatalf("a moved capture was charged as a crossing clone:\n%s", ir)
	}
}
