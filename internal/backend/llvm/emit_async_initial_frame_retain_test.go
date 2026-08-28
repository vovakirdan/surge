package llvm

import (
	"fmt"
	"strings"
	"testing"
)

// The reference an async task's INITIAL frame takes for a channel handle must
// be the runtime's atomic retain, not the inline count bump.
//
// The two retains are not interchangeable. A counted scalar's count is the
// first field of a block this backend lays out, so a copy bumps it inline and
// non-atomically. A channel's count is private to the runtime object and is
// atomic, because a copy of the handle may be retained from another shard's
// frame. `emitRetainValue` tells them apart by the operand's TYPE, so a retain
// operand that carries no type silently takes the scalar path -- which writes
// +1 into the first word of the runtime's channel header, leaves the handle
// count untouched, and lets the creating scope's release destroy the object
// under the task that was handed it. That is a use-after-free, not a leak, and
// it shows up only in a whole program under valgrind, which is why the shape is
// pinned here in the IR where it is cheap to see.
func TestEmitAsyncInitialFrameRetainsAChannelThroughTheRuntime(t *testing.T) {
	sourceCode := `async fn take_one(ch: Channel<int>) -> int {
    checkpoint().await();
    let got: Option<int> = ch.try_recv();
    return compare got { Some(v) => v; nothing => 0; };
}

@entrypoint
fn main() -> int {
    let ch = Channel::<int>::new(4:uint);
    ch.try_send(7);
    compare take_one(ch).await() {
        Success(v) => return v;
        Cancelled() => return 9;
    };
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	fn := findMIRFunc(t, mirMod, "take_one")
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", fn.ID))

	if !strings.Contains(body, "call void @rt_channel_handle_retain(") {
		t.Fatalf("the initial frame took no runtime retain for the channel it holds:\n%s", body)
	}
	if strings.Contains(body, retainScratchGlobal) {
		t.Fatalf("the initial frame bumped a channel's count inline; that word is the runtime's header, not a count:\n%s", body)
	}
}
