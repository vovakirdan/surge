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
	ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mod, "straight_line$poll").ID))

	// A resumed invocation ships no arm table again: every arm-describing
	// argument is inert on the retry EXCEPT the value array, which is where a
	// losing payload is moved back to and therefore has to name THIS poll's
	// storage.
	retryCall := regexp.MustCompile(`call i32 @rt_far_channel_select\(ptr null, ptr null, ptr %[^,]+, ptr null, i64 2, i64 0, i64 0, ptr null,`)
	if !retryCall.MatchString(body) {
		t.Fatalf("select retry must pass the arm value array and exact count:\n%s", body)
	}

	// The retry's value array names the SEND arm's own MIR place, stored into
	// the array BEFORE the call. That address is what makes the losing payload
	// arrive where sema's losing-arm drop already looks for it -- and it is
	// computed per poll rather than carried across one, because the frame it
	// belongs to does not survive a park.
	retryAt := retryCall.FindStringIndex(body)[0]
	retryHead := body[:retryAt]
	fillAt := strings.LastIndex(retryHead, "store ptr ")
	if fillAt < 0 {
		t.Fatalf("select retry must fill the arm value array before calling:\n%s", body)
	}

	// Nothing reconstructs a value from returned bits any more: the payload
	// travelled as a value, not as a word, so there is no word to widen back.
	if strings.Contains(body, "inttoptr i64") {
		widened := regexp.MustCompile(`inttoptr i64 %[^ ]+ to ptr`)
		if widened.MatchString(body[retryAt:]) {
			t.Fatalf("a returned select payload must not be rebuilt from a machine word:\n%s", body)
		}
	}

	// The winner index still reaches the select's own destination before the
	// ready dispatch.
	winnerStore := regexp.MustCompile(`(?s)load i64, ptr %[^\n]+\n.*?br label %bb`)
	if !winnerStore.MatchString(body[retryAt:]) {
		t.Fatalf("the winner index must be stored before ReadyBB dispatch:\n%s", body)
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
