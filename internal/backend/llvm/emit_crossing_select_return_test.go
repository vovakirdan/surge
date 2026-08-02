package llvm

import (
	"regexp"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/sema"
)

func TestEmitFarSelectOwnedBindingReturnsLosingPayloadBeforeReady(t *testing.T) {
	sourceCode := `
async fn straight_line(ch: far Channel<string>, stop: far Channel<int>) -> nothing {
    let mut job = "job-";
    job = job + "payload";
    let v = select {
        ch.send(own job) => 1;
        stop.recv() => { print(job, ""); ret 2; };
    };
    print("done\n", "");
}
`

	mod, result := lowerCrossingMIRFromSource(t, sourceCode, sema.CrossingLoweringChannelSelect)
	crossing := findLLVMSelectCrossing(t, mod)
	if len(crossing.RemoteOps) != 2 || crossing.RemoteOps[0].ReturnPlace == nil {
		t.Fatalf("missing conditional-transfer MIR shape: %+v", crossing.RemoteOps)
	}
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "straight_line$poll").ID))

	// A resumed invocation owns no source local yet. Its retry therefore passes
	// the fresh arm-bits array as an OUT buffer while every input arm descriptor
	// except the statically-known count stays inert.
	retryCall := regexp.MustCompile(`call i32 @rt_far_channel_select\(ptr null, ptr null, ptr %[^,]+, ptr null, i64 2, i64 0, i64 0, ptr null,`)
	if !retryCall.MatchString(body) {
		t.Fatalf("select retry must pass the arm-bits handback buffer and exact count:\n%s", body)
	}

	// The committed SEND index is skipped; every other outcome loads the
	// returned raw bits and stores the reconstructed pointer before the final
	// branch to a MIR ready block.
	committedCheck := regexp.MustCompile(`icmp eq i64 %[^,]+, 0`)
	if !committedCheck.MatchString(body) {
		t.Fatalf("missing committed SEND guard around return-place restore:\n%s", body)
	}
	guardAt := committedCheck.FindStringIndex(body)[0]
	restoreTail := body[guardAt:]
	storeAt := strings.Index(restoreTail, "store ptr ")
	readyAt := strings.Index(restoreTail, "br label %bb")
	if storeAt < 0 || readyAt < 0 || storeAt > readyAt {
		t.Fatalf("returned losing payload must store before ReadyBB dispatch:\n%s", restoreTail)
	}
}

func findLLVMSelectCrossing(t *testing.T, mod *mir.Module) *mir.CrossingInstr {
	t.Helper()
	for _, fn := range mod.Funcs {
		if fn == nil {
			continue
		}
		for bi := range fn.Blocks {
			for ii := range fn.Blocks[bi].Instrs {
				ins := &fn.Blocks[bi].Instrs[ii]
				if ins.Kind == mir.InstrCrossing && ins.Crossing.Kind == sema.CrossingLoweringChannelSelect {
					return &ins.Crossing
				}
			}
		}
	}
	t.Fatal("missing ChannelSelect crossing")
	return nil
}
