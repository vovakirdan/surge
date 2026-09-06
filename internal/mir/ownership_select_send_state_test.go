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

// A far-select SEND payload that may share a counted block is handed to the
// runtime as a PRIVATE transfer temp: retained out of the live binding,
// un-shared, and moved — never the binding's own address. The runtime consumes
// the staged reference on every path (winner, loser cell, failed submission),
// so a bare COPY of `value` would have it release the binding's reference; and
// the temp must be outside the drop frames, or a region flush would release it
// a second time.
//
// Red on the base tree: the payload was `copy value` (lowerExpr with consume
// false) and no un-share existed. Compiles here because this harness runs sema
// but not buildpipeline's channel-element gate.
func TestFarSelectCountedSendPayloadIsAPrivateTemp(t *testing.T) {
	compiled := compileCrossingMIR(t, crossingMIRPrelude+`
async fn counted_far_select(ch: far Channel<float>, stop: far Channel<int>) -> float {
    let value: float = 1.5;
    let winner = select {
        ch.send(value) => 1;
        stop.recv() => 2;
    };
    return value;
}
`, crossingForms(sema.CrossingLoweringChannelSelect))
	crossing := findCrossingInstr(t, compiled.mod)
	fn := compiled.mod.Funcs[crossingFuncID(t, compiled.mod, crossing)]
	value := namedLocal(t, fn, "value")
	if len(crossing.RemoteOps) != 2 || crossing.RemoteOps[0].Method != "send" {
		t.Fatalf("unexpected far select shape: %+v", crossing.RemoteOps)
	}
	payload := crossing.RemoteOps[0].Value
	if payload.Kind != mir.OperandMove || payload.Place.Kind != mir.PlaceLocal || len(payload.Place.Proj) != 0 {
		t.Fatalf("SEND payload = %+v, want MOVE out of a bare local", payload)
	}
	temp := payload.Place.Local
	if temp == value {
		t.Fatalf("SEND payload moves the live binding L%d(value) itself", value)
	}
	if crossing.RemoteOps[0].ReturnPlace != nil {
		t.Fatalf("a private temp has no losing-arm handback; the runtime destroys it in its cell: %+v", crossing.RemoteOps[0])
	}
	assertPrivateTempBeforeCrossing(t, fn, temp, value)

	for _, f := range compiled.mod.Funcs {
		mir.SimplifyCFG(f)
	}
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}
	if err := mir.ValidateStructureWithOptions(compiled.mod, compiled.types,
		mir.ValidateOptions{CrossingForms: crossingForms(sema.CrossingLoweringChannelSelect)}); err != nil {
		t.Fatalf("the split shape must pass the boundary shape rule: %v", err)
	}
	poll := findNamedMIRFunc(t, compiled.mod, "counted_far_select$poll")
	split := channelSelectCrossingIn(t, poll)
	if n := pendingStateStores(t, poll, split.PendBB, temp); n != 0 {
		t.Fatalf("the private temp is packed into the pending state %d time(s); it is consumed by the "+
			"crossing and must never gain a second owner in the frame", n)
	}
	if got := findingsIn(mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema),
		"counted_far_select$poll"); len(got) != 0 {
		t.Fatalf("a private SEND payload must be verifier-clean:\n%s", joinLines(got))
	}
}

// assertPrivateTempBeforeCrossing pins the prelude the relinquish builds in
// the crossing's own block: `temp = retain value`, and after it `unshare temp`,
// both before the crossing. That the un-share is the temp's LAST touch is the
// lowering-time validator's rule, which this program already passed to get
// here.
func assertPrivateTempBeforeCrossing(t *testing.T, fn *mir.Func, temp, value mir.LocalID) {
	t.Helper()
	tempPlace := mir.Place{Kind: mir.PlaceLocal, Local: temp}
	for bi := range fn.Blocks {
		bb := &fn.Blocks[bi]
		crossingAt, retainAt, unshareAt := -1, -1, -1
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			switch {
			case ins.Kind == mir.InstrCrossing:
				crossingAt = ii
			case ins.Kind == mir.InstrUnshare && sameBareLocal(ins.Unshare.Place, tempPlace):
				unshareAt = ii
			case ins.Kind == mir.InstrAssign && sameBareLocal(ins.Assign.Dst, tempPlace):
				use := ins.Assign.Src
				if use.Kind != mir.RValueUse || use.Use.Kind != mir.OperandRetain ||
					!sameBareLocal(use.Use.Place, mir.Place{Kind: mir.PlaceLocal, Local: value}) {
					t.Fatalf("%s bb%d#%d defines the private temp as %+v, want `retain L%d(value)`",
						fn.Name, bi, ii, use, value)
				}
				retainAt = ii
			}
		}
		if crossingAt < 0 {
			continue
		}
		if retainAt < 0 || unshareAt < 0 || retainAt >= unshareAt || unshareAt >= crossingAt {
			t.Fatalf("%s bb%d: want `L%d = retain L%d` (#%d), then `unshare L%d` (#%d), then the crossing (#%d)",
				fn.Name, bi, temp, value, retainAt, temp, unshareAt, crossingAt)
		}
		return
	}
	t.Fatalf("%s has no crossing", fn.Name)
}

func channelSelectCrossingIn(t *testing.T, fn *mir.Func) *mir.CrossingInstr {
	t.Helper()
	for bi := range fn.Blocks {
		for ii := range fn.Blocks[bi].Instrs {
			ins := &fn.Blocks[bi].Instrs[ii]
			if ins.Kind == mir.InstrCrossing && ins.Crossing.Kind == sema.CrossingLoweringChannelSelect {
				return &ins.Crossing
			}
		}
	}
	t.Fatalf("%s has no channel-select crossing", fn.Name)
	return nil
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
	if count := pendingStateStores(t, fn, pending, job); count != 1 {
		t.Fatalf("%s pending state stores L%d(job) %d times, want exactly 1", fn.Name, job, count)
	}
}

// pendingStateStores counts how many times the suspend block packs local into
// the state payload, and checks each such store is a MOVE under a STORE
// contract.
func pendingStateStores(t *testing.T, fn *mir.Func, pending mir.BlockID, local mir.LocalID) int {
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
			if arg.Place.Kind != mir.PlaceLocal || len(arg.Place.Proj) != 0 || arg.Place.Local != local {
				continue
			}
			count++
			if arg.Kind != mir.OperandMove || i >= len(ins.Call.ArgContracts) ||
				ins.Call.ArgContracts[i] != mir.ArgContractStore {
				t.Fatalf("%s pending state stores L%d as %+v/%v, want MOVE/STORE",
					fn.Name, local, arg, ins.Call.ArgContracts)
			}
		}
	}
	return count
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
