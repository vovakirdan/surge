package llvm

import (
	"regexp"
	"strings"
	"testing"
)

// RV2-DEBT-265. The ready block loads the fail-fast flag whatever
// `rt_scope_join_all` answered, and the runtime has two early exits that say
// "drained" about a scope that is gone. If the slot is not written before the
// call, those exits leave the block branching on whatever the frame held --
// undefined behaviour whose wrong-default reading is `Success`, the exact
// answer RV2-DEBT-261 and RV2-DEBT-263 exist to prevent.
func TestEmitJoinAllWritesItsFlagSlotBeforeTheCall(t *testing.T) {
	sourceCode := `async fn child() -> int {
    checkpoint().await();
    return 1;
}

async fn body() -> int {
    let r = (@failfast async {
        let t = spawn child();
        let _ = t.await();
        ret 0;
    }).await();
    let code = compare r {
        Success(v) => v;
        Cancelled() => 1;
    };
    return code;
}

@entrypoint
fn main() -> int {
    let r = body().await();
    return compare r {
        Success(v) => v;
        Cancelled() => 1;
    };
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	if !strings.Contains(ir, "@rt_scope_join_all") {
		t.Fatal("the program emitted no join_all call; the fixture no longer exercises a @failfast block")
	}

	callPattern := regexp.MustCompile(`call i1 @rt_scope_join_all\(ptr %\w+, ptr (%\w+), ptr (%\w+)\)`)
	calls := callPattern.FindAllStringSubmatch(ir, -1)
	if len(calls) == 0 {
		t.Fatal("no join_all call matched the expected shape")
	}
	for _, call := range calls {
		pendingSlot, failfastSlot := call[1], call[2]
		callIndex := strings.Index(ir, call[0])
		before := ir[:callIndex]
		for slot, store := range map[string]string{
			pendingSlot:  "store i64 0, ptr " + pendingSlot,
			failfastSlot: "store i1 false, ptr " + failfastSlot,
		} {
			if !strings.Contains(before, store) {
				t.Fatalf("%s is read by the ready block but never written before the call to rt_scope_join_all", slot)
			}
		}
	}
}
