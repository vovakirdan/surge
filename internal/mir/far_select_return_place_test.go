package mir_test

import (
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/sema"
)

const farSelectReturnPlaceSource = crossingMIRPrelude + `
fn consume(v: string) -> nothing {}

async fn straight_line(ch: far Channel<string>, stop: far Channel<int>) -> nothing {
    let mut job = "job-";
    job = job + "payload";
    let v = select {
        ch.send(own job) => 1;
        stop.recv() => {
            consume(job);
            ret 2;
        };
    };
}
`

func TestFarSelectOwnedBindingUsesExplicitReturnPlace(t *testing.T) {
	compiled := compileCrossingMIR(t, farSelectReturnPlaceSource,
		crossingForms(sema.CrossingLoweringChannelSelect))
	crossing := findCrossingInstr(t, compiled.mod)
	if len(crossing.RemoteOps) != 2 {
		t.Fatalf("remote ops = %d, want 2", len(crossing.RemoteOps))
	}
	send := &crossing.RemoteOps[0]
	if send.Method != "send" || send.ReturnPlace == nil {
		t.Fatalf("owned send missing return-place protocol: %+v", send)
	}
	if send.Value.Kind != mir.OperandMove || !sameBareLocal(send.Value.Place, *send.ReturnPlace) {
		t.Fatalf("conditional transfer = %s -> %v, want MOVE of exact return place",
			send.Value.Kind, send.ReturnPlace)
	}
	if send.Value.Type != compiled.mod.Funcs[crossingFuncID(t, compiled.mod, crossing)].Locals[send.ReturnPlace.Local].Type {
		t.Fatal("conditional transfer type must equal its return-place local type")
	}

	for _, fn := range compiled.mod.Funcs {
		mir.SimplifyCFG(fn)
	}
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}
	for _, fn := range compiled.mod.Funcs {
		mir.SimplifyCFG(fn)
	}
	if err := mir.ValidateWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
		CrossingForms: crossingForms(sema.CrossingLoweringChannelSelect),
	}); err != nil {
		t.Fatalf("validate conditional-transfer MIR: %v", err)
	}

	poll := findNamedMIRFunc(t, compiled.mod, "straight_line$poll")
	for bi := range poll.Blocks {
		for ii := range poll.Blocks[bi].Instrs {
			ins := &poll.Blocks[bi].Instrs[ii]
			if ins.Kind != mir.InstrCall || !strings.HasPrefix(ins.Call.Callee.Name, "Pc") {
				continue
			}
			for _, arg := range ins.Call.Args {
				if arg.Place.Kind == mir.PlaceLocal && arg.Place.Local == send.ReturnPlace.Local {
					t.Fatalf("pending state must not retain conditional-transfer source L%d", arg.Place.Local)
				}
			}
		}
	}
	if got := findingsIn(mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema),
		"straight_line$poll"); len(got) != 0 {
		t.Fatalf("conditional-transfer lowering must be verifier-clean:\n%s", strings.Join(got, "\n"))
	}
}

func TestFarSelectReturnPlaceValidationFailsClosed(t *testing.T) {
	compiled := compileCrossingMIR(t, farSelectReturnPlaceSource,
		crossingForms(sema.CrossingLoweringChannelSelect))
	crossing := findCrossingInstr(t, compiled.mod)
	send := &crossing.RemoteOps[0]
	if send.ReturnPlace == nil {
		t.Fatal("fixture missing return place")
	}

	t.Run("copy", func(t *testing.T) {
		old := send.Value.Kind
		send.Value.Kind = mir.OperandCopy
		defer func() { send.Value.Kind = old }()
		err := mir.ValidateWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
			CrossingForms: crossingForms(sema.CrossingLoweringChannelSelect),
		})
		if err == nil || !strings.Contains(err.Error(), "return place requires MOVE") {
			t.Fatalf("expected exact-MOVE validation error, got %v", err)
		}
	})

	t.Run("projection", func(t *testing.T) {
		old := append([]mir.PlaceProj(nil), send.ReturnPlace.Proj...)
		send.ReturnPlace.Proj = []mir.PlaceProj{{Kind: mir.PlaceProjField, FieldName: "x"}}
		defer func() { send.ReturnPlace.Proj = old }()
		err := mir.ValidateWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
			CrossingForms: crossingForms(sema.CrossingLoweringChannelSelect),
		})
		if err == nil || !strings.Contains(err.Error(), "return place must be a bare local") {
			t.Fatalf("expected bare-local validation error, got %v", err)
		}
	})
}

func TestFarSelectDuplicateOwnedRootDoesNotEnterReturnProtocol(t *testing.T) {
	compiled := compileCrossingMIR(t, crossingMIRPrelude+`
async fn duplicate_root(a: far Channel<string>, b: far Channel<string>) -> nothing {
    let job = "payload";
    let v = select {
        a.send(own job) => 1;
        b.send(own job) => 2;
    };
}
`, crossingForms(sema.CrossingLoweringChannelSelect))
	crossing := findCrossingInstr(t, compiled.mod)
	for i := range crossing.RemoteOps {
		if crossing.RemoteOps[i].ReturnPlace != nil {
			t.Fatalf("duplicate ownership root must stay outside conditional-transfer protocol: op %d", i)
		}
	}
	for _, fn := range compiled.mod.Funcs {
		mir.SimplifyCFG(fn)
	}
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("lower duplicate-root async state machine: %v", err)
	}
	got := findingsIn(mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema), "duplicate_root$poll")
	if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "crossing_remote_value") {
		t.Fatalf("unsupported duplicate root must remain a verifier finding, got:\n%s", strings.Join(got, "\n"))
	}
}

func findCrossingInstr(t *testing.T, mod *mir.Module) *mir.CrossingInstr {
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
	t.Fatal("missing channel-select crossing")
	return nil
}

func crossingFuncID(t *testing.T, mod *mir.Module, crossing *mir.CrossingInstr) mir.FuncID {
	t.Helper()
	for id, fn := range mod.Funcs {
		if fn == nil {
			continue
		}
		for bi := range fn.Blocks {
			for ii := range fn.Blocks[bi].Instrs {
				if &fn.Blocks[bi].Instrs[ii].Crossing == crossing {
					return id
				}
			}
		}
	}
	t.Fatal("crossing owner function missing")
	return mir.NoFuncID
}

func findNamedMIRFunc(t *testing.T, mod *mir.Module, suffix string) *mir.Func {
	t.Helper()
	for _, fn := range mod.Funcs {
		if fn != nil && strings.HasSuffix(fn.Name, suffix) {
			return fn
		}
	}
	t.Fatalf("missing MIR function %q", suffix)
	return nil
}

func sameBareLocal(a, b mir.Place) bool {
	return a.Kind == mir.PlaceLocal && b.Kind == mir.PlaceLocal &&
		len(a.Proj) == 0 && len(b.Proj) == 0 && a.Local == b.Local
}
