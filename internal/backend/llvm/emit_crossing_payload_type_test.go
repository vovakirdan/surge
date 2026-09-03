package llvm

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"surge/internal/sema"
)

// Every far crossing names its payload's TYPE to the runtime, a scalar
// included, and the module defines the descriptor that id resolves to.
//
// The runtime sizes a far channel's cell, moves a far task's result and
// reclaims a reply nobody consumed through that descriptor. A scalar used to be
// handed over as id 0 ("no descriptor, treat as a machine word"), which is the
// wrong width for anything narrower than one -- a `bool` cell moved as eight
// bytes. The mutant is the old gate back in the emitter: only a heap-owning
// payload registers its type. The assertions on the initial calls go red the
// moment a scalar crosses as 0; the assertions on the retry calls pin that a
// retry still passes 0 there, because the runtime reads none of those
// arguments on a retry.
func TestEmitCrossingsNameEveryPayloadType(t *testing.T) {
	sourceCode := `
async fn mint(a: far Task<bool>) -> int {
    let ch: far Channel<bool> = channel_on::<bool>(shard(0:ShardId), 2);
    let sib: far Channel<bool> = ch.share();
    let r: TaskResult<bool> = a.await();
    let _ = sib;
    let _ = r;
    return 0;
}
`
	mirMod, result := lowerCrossingMIRFromSource(
		t,
		sourceCode,
		sema.CrossingLoweringChannelCreate,
		sema.CrossingLoweringChannelShare,
		sema.CrossingLoweringFarTaskAwait,
	)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	createInitial := regexp.MustCompile(`call i32 @rt_far_channel_create\(i64 %[^,]+, i64 %[^,]+, i64 (\d+), ptr`)
	awaitInitial := regexp.MustCompile(`call i32 @rt_far_task_await\(ptr %[^,]+, i64 (\d+), ptr`)
	lookup := findLLVMFuncBody(t, ir, "__surge_value_ops_for")

	for _, row := range []struct {
		name string
		re   *regexp.Regexp
	}{
		{name: "channel_on::<bool>", re: createInitial},
		{name: "far Task<bool>.await", re: awaitInitial},
	} {
		match := row.re.FindStringSubmatch(ir)
		if match == nil {
			t.Fatalf("%s: initial runtime call missing from the IR:\n%s", row.name, ir)
		}
		id, convErr := strconv.ParseUint(match[1], 10, 64)
		if convErr != nil || id == 0 {
			t.Fatalf("%s: payload type crossed as id %q, want a nonzero type id:\n%s", row.name, match[1], ir)
		}
		if !strings.Contains(lookup, "i64 "+match[1]+", label %value_ops."+match[1]) {
			t.Fatalf("%s: type id %d has no descriptor in __surge_value_ops_for:\n%s", row.name, id, lookup)
		}
	}

	// The share crossing takes no payload type: the sibling names the same
	// channel, and the channel already knows its element.
	if !strings.Contains(ir, "declare i32 @rt_far_channel_share(ptr, ptr, ptr, ptr)") {
		t.Fatalf("far Channel.share must cross with four pointer arguments and no payload word:\n%s", ir)
	}
	for _, want := range []string{
		"call i32 @rt_far_channel_create(i64 0, i64 0, i64 0, ptr",
		"call i32 @rt_far_channel_share(ptr null, ptr",
		"call i32 @rt_far_task_await(ptr null, i64 0,",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("retry call missing %q:\n%s", want, ir)
		}
	}
}
