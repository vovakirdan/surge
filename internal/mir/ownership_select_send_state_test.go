package mir_test

import (
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/sema"
)

const ownershipSelectSendStateSource = crossingMIRPrelude + `
fn consume(value: string) -> nothing {}

async fn local_select(ch: Channel<string>, stop: Channel<int>) -> nothing {
    let mut job = "job-";
    job = job + "payload";
    let winner = select {
        ch.send(own job) => 1;
        stop.recv() => {
            consume(job);
            ret 2;
        };
    };
}
`

const ownershipSelectSendCopyHeapSource = crossingMIRPrelude + `
async fn copy_heap_select(ch: own Channel<float>, stop: own Channel<int>) -> float {
    let value: float = 1.5;
    let winner = select {
        ch.send(own value) => 1;
        stop.recv() => 2;
    };
    return value;
}
`

// A local select conditionally transfers one owned send payload: rt_select_poll
// borrows it while Pending, the channel owns it only if this SEND wins, and a
// losing arm keeps the original binding. The exact bare-local MOVE prevents
// generic `own`/select temps from putting duplicate owners in the cancellation
// state.
func TestOwnershipLocalSelectSendPendingStateHasOneOwner(t *testing.T) {
	compiled := compileCrossingMIR(t, ownershipSelectSendStateSource, nil)
	before := findNamedMIRFunc(t, compiled.mod, "local_select")
	job := namedLocal(t, before, "job")
	ch := namedLocal(t, before, "ch")
	stop := namedLocal(t, before, "stop")
	selectInstr := onlySelectInstr(t, before)
	assertLocalSelectSendRoot(t, before, selectInstr, job)
	assertLocalSelectChannelRoots(t, before, selectInstr, ch, stop)
	assertNoLocalSelectPayloadAliases(t, before)
	assertLosingArmUsesRoot(t, before, job)

	for _, fn := range compiled.mod.Funcs {
		mir.SimplifyCFG(fn)
	}
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}
	for _, fn := range compiled.mod.Funcs {
		mir.SimplifyCFG(fn)
	}

	poll := findNamedMIRFunc(t, compiled.mod, "local_select$poll")
	selectInstr = onlySelectInstr(t, poll)
	assertLocalSelectSendRoot(t, poll, selectInstr, job)
	assertLocalSelectChannelRoots(t, poll, selectInstr, ch, stop)
	assertNoLocalSelectPayloadAliases(t, poll)
	assertPendingStateStoresRootOnce(t, poll, selectInstr.PendBB, job)
	assertPendingStateStoresRootOnce(t, poll, selectInstr.PendBB, ch)
	assertPendingStateStoresRootOnce(t, poll, selectInstr.PendBB, stop)
	assertLosingArmUsesRoot(t, poll, job)

	if got := findingsIn(mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema),
		"local_select$poll"); len(got) != 0 {
		t.Fatalf("local select conditional transfer must be verifier-clean:\n%s", joinLines(got))
	}
}

// Sema treats Copy payloads as non-moving even when they own heap storage
// (float is the reference-counted scalar witness): the binding remains legal
// after the select. The exact-MOVE fast path must therefore stay limited to
// non-Copy payloads for both local and far selects.
func TestOwnershipSelectSendCopyHeapDoesNotUseConditionalMove(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		compiled := compileCrossingMIR(t, ownershipSelectSendCopyHeapSource, nil)
		fn := findNamedMIRFunc(t, compiled.mod, "copy_heap_select")
		value := namedLocal(t, fn, "value")
		sel := onlySelectInstr(t, fn)
		if len(sel.Arms) != 2 || sel.Arms[0].Kind != mir.SelectArmChanSend {
			t.Fatalf("unexpected local select shape: %+v", sel.Arms)
		}
		assertNotExactSelectMove(t, "local", sel.Arms[0].Value, value)
	})

	t.Run("far", func(t *testing.T) {
		compiled := compileCrossingMIR(t, crossingMIRPrelude+`
async fn copy_heap_far_select(ch: far Channel<float>, stop: far Channel<int>) -> float {
    let value: float = 1.5;
    let winner = select {
        ch.send(own value) => 1;
        stop.recv() => 2;
    };
    return value;
}
`, crossingForms(sema.CrossingLoweringChannelSelect))
		crossing := findCrossingInstr(t, compiled.mod)
		if len(crossing.RemoteOps) != 2 || crossing.RemoteOps[0].Method != "send" {
			t.Fatalf("unexpected far select shape: %+v", crossing.RemoteOps)
		}
		if crossing.RemoteOps[0].ReturnPlace != nil {
			t.Fatalf("Copy+heap payload entered conditional-return protocol: %+v", crossing.RemoteOps[0])
		}
		fn := compiled.mod.Funcs[crossingFuncID(t, compiled.mod, crossing)]
		value := namedLocal(t, fn, "value")
		assertNotExactSelectMove(t, "far", crossing.RemoteOps[0].Value, value)
	})
}

func assertNotExactSelectMove(t *testing.T, label string, op mir.Operand, local mir.LocalID) {
	t.Helper()
	if op.Kind == mir.OperandMove && op.Place.Kind == mir.PlaceLocal &&
		len(op.Place.Proj) == 0 && op.Place.Local == local {
		t.Fatalf("%s Copy+heap payload became raw MOVE of still-live L%d", label, local)
	}
}

func namedLocal(t *testing.T, fn *mir.Func, name string) mir.LocalID {
	t.Helper()
	for i := range fn.Locals {
		if fn.Locals[i].Name == name {
			return mir.LocalID(i)
		}
	}
	t.Fatalf("%s missing local %q", fn.Name, name)
	return mir.NoLocalID
}

func onlySelectInstr(t *testing.T, fn *mir.Func) *mir.SelectInstr {
	t.Helper()
	var found *mir.SelectInstr
	for bi := range fn.Blocks {
		for ii := range fn.Blocks[bi].Instrs {
			ins := &fn.Blocks[bi].Instrs[ii]
			if ins.Kind != mir.InstrSelect {
				continue
			}
			if found != nil {
				t.Fatalf("%s emitted more than one select", fn.Name)
			}
			found = &ins.Select
		}
	}
	if found == nil {
		t.Fatalf("%s emitted no select", fn.Name)
	}
	return found
}

func assertLocalSelectSendRoot(t *testing.T, fn *mir.Func, sel *mir.SelectInstr, job mir.LocalID) {
	t.Helper()
	if len(sel.Arms) != 2 || sel.Arms[0].Kind != mir.SelectArmChanSend {
		t.Fatalf("%s select arms have unexpected shape: %+v", fn.Name, sel.Arms)
	}
	value := sel.Arms[0].Value
	if value.Kind != mir.OperandMove || value.Place.Kind != mir.PlaceLocal ||
		len(value.Place.Proj) != 0 || value.Place.Local != job {
		t.Fatalf("%s SEND payload = %+v, want exact MOVE of L%d(job)", fn.Name, value, job)
	}
	if value.Type != fn.Locals[job].Type {
		t.Fatalf("%s SEND payload type = %d, want local type %d", fn.Name, value.Type, fn.Locals[job].Type)
	}
}

func assertNoLocalSelectPayloadAliases(t *testing.T, fn *mir.Func) {
	t.Helper()
	for i := range fn.Locals {
		name := fn.Locals[i].Name
		if strings.HasPrefix(name, "tmp_select_val") || strings.HasPrefix(name, "tmp_un") ||
			strings.HasPrefix(name, "tmp_select_ch") {
			t.Fatalf("%s created duplicate select payload owner L%d(%s)", fn.Name, i, name)
		}
	}
}

func assertLocalSelectChannelRoots(
	t *testing.T,
	fn *mir.Func,
	sel *mir.SelectInstr,
	sendChannel, recvChannel mir.LocalID,
) {
	t.Helper()
	want := []mir.LocalID{sendChannel, recvChannel}
	for i, local := range want {
		channel := sel.Arms[i].Channel
		if channel.Kind != mir.OperandCopy || channel.Place.Kind != mir.PlaceLocal ||
			len(channel.Place.Proj) != 0 || channel.Place.Local != local {
			t.Fatalf("%s arm %d channel = %+v, want exact COPY of L%d",
				fn.Name, i, channel, local)
		}
	}
}

func assertPendingStateStoresRootOnce(t *testing.T, fn *mir.Func, pending mir.BlockID, job mir.LocalID) {
	t.Helper()
	if pending == mir.NoBlockID || int(pending) < 0 || int(pending) >= len(fn.Blocks) {
		t.Fatalf("%s select has invalid pending block bb%d", fn.Name, pending)
	}
	count := 0
	for ii := range fn.Blocks[pending].Instrs {
		ins := &fn.Blocks[pending].Instrs[ii]
		if ins.Kind != mir.InstrCall || !strings.HasPrefix(ins.Call.Callee.Name, "Pc") {
			continue
		}
		for i := range ins.Call.Args {
			arg := ins.Call.Args[i]
			if arg.Place.Kind != mir.PlaceLocal || len(arg.Place.Proj) != 0 || arg.Place.Local != job {
				continue
			}
			count++
			if arg.Kind != mir.OperandMove || i >= len(ins.Call.ArgContracts) ||
				ins.Call.ArgContracts[i] != mir.ArgContractStore {
				t.Fatalf("%s pending state stores L%d as %+v/%v, want MOVE/STORE",
					fn.Name, job, arg, ins.Call.ArgContracts)
			}
		}
	}
	if count != 1 {
		t.Fatalf("%s pending state stores L%d(job) %d times, want exactly 1", fn.Name, job, count)
	}
}

func assertLosingArmUsesRoot(t *testing.T, fn *mir.Func, job mir.LocalID) {
	t.Helper()
	for bi := range fn.Blocks {
		for ii := range fn.Blocks[bi].Instrs {
			ins := &fn.Blocks[bi].Instrs[ii]
			if ins.Kind != mir.InstrCall {
				continue
			}
			for i := range ins.Call.Args {
				arg := ins.Call.Args[i]
				if arg.Place.Kind == mir.PlaceLocal && len(arg.Place.Proj) == 0 &&
					arg.Place.Local == job && i < len(ins.Call.ArgContracts) &&
					ins.Call.ArgContracts[i] == mir.ArgContractTransferOwned {
					return
				}
			}
		}
	}
	t.Fatalf("%s losing recv arm no longer consumes original L%d(job)", fn.Name, job)
}
