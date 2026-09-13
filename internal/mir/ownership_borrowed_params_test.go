package mir_test

import (
	"crypto/sha256"
	"testing"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestBorrowedParameterWorkingLocalMIR(t *testing.T) {
	cases := []struct {
		name, source, call string
		params, drops      int
		working, returns   bool
	}{
		{"readonly_counted", "fn subject(p: int) -> int { return p; }", "subject(caller)", 1, 0, false, true},
		{"readonly_generic", "fn subject<T>(p: T) {}", "subject::<int>(caller)", 1, 0, false, false},
		{"direct_rebind_return", "fn subject(p: int) -> int { p = p + 1; return p; }", "subject(caller)", 1, 2, true, true},
		{"early_return", `fn subject(p: int, stop: bool) -> int {
    if stop { return p; }
    p = p + 1;
    return p;
}`, "subject(caller, false)", 2, 3, true, true},
		{"conditional_rebind", `fn subject(p: int, change: bool) -> int {
    if change { p = p + 1; }
    return p;
}`, "subject(caller, false)", 2, 2, true, true},
		{"loop_rebind", `fn subject(p: int, rounds: uint64) -> int {
    let mut i: uint64 = 0:uint64;
    while i < rounds {
        p = p + 1;
        i = i + 1:uint64;
    }
    return p;
}`, "subject(caller, 3:uint64)", 2, 2, true, true},
		{"explicit_drop", "fn subject(p: int) { @drop p; }", "subject(caller)", 1, 1, true, false},
		{"drop_then_rebind", "fn subject(p: int) -> int { @drop p; p = 2; return p; }", "subject(caller)", 1, 2, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := tc.call + ";"
			if tc.returns {
				call = "let _ = " + call
			}
			src := tc.source + "\nfn main() { let caller: int = 1; " + call + " }\n"
			t.Logf("source_sha256=%x", sha256.Sum256([]byte(src)))
			compiled := compileCrossingMIR(t, src, nil)
			fn := borrowedMIRFunction(t, compiled, tc.params)
			if fn.IsAsync || fn.Locals[0].Type != compiled.types.Builtins().Int ||
				fn.Locals[0].Flags != mir.LocalFlagCopy|mir.LocalFlagOwnsHeap {
				t.Fatalf("counted ABI parameter changed: %+v", fn.Locals[0])
			}
			if tc.params == 2 {
				want := compiled.types.Builtins().Bool
				if tc.name == "loop_rebind" {
					want = compiled.types.Builtins().Uint64
				}
				if fn.Locals[1].Type != want || fn.Locals[1].Flags != mir.LocalFlagCopy {
					t.Fatalf("second ABI position changed: %+v", fn.Locals[1])
				}
			}
			requireBorrowedMIRCall(t, compiled.mod, fn, mir.ArgContractBorrow)
			work := borrowedMIRWork(t, fn, tc.working)
			reachable := borrowedMIRBlocks(t, fn)
			drops, writes, returns := 0, 0, 0
			for _, bi := range reachable {
				block := &fn.Blocks[bi]
				for ii := range block.Instrs {
					ins := &block.Instrs[ii]
					if ins.Kind == mir.InstrDrop && borrowedMIRLocal(ins.Drop.Place, 0) ||
						ins.Kind == mir.InstrAssign && borrowedMIRLocal(ins.Assign.Dst, 0) {
						t.Fatal("counted incoming ABI slot was consumed or overwritten")
					}
					if !tc.working {
						continue
					}
					if ins.Kind == mir.InstrDrop && borrowedMIRLocal(ins.Drop.Place, work) {
						drops++
					}
					if ins.Kind == mir.InstrAssign && borrowedMIRLocal(ins.Assign.Dst, work) {
						writes++
						if bi != int(fn.Entry) || ii != 0 {
							if tc.name != "drop_then_rebind" {
								requireBorrowedOverwriteOrder(t, block, ii, work)
							}
							if tc.name == "loop_rebind" {
								requireBorrowedLoopBackedge(t, fn, block)
							}
						}
					}
				}
				if block.Term.Kind == mir.TermReturn {
					returns++
					if block.Term.Return.HasValue != tc.returns {
						t.Fatal("fixture return shape changed")
					}
					if tc.working && tc.returns {
						requireBorrowedReturnOrder(t, block, work)
					}
				}
			}
			wantReturns := 1
			if tc.name == "early_return" {
				wantReturns = 2
			}
			wantWrites := 0
			if tc.working {
				wantWrites = 2 // initial owner, then the one source rebind
				if tc.name == "explicit_drop" {
					wantWrites = 1
				}
			}
			if drops != tc.drops || writes != wantWrites || returns != wantReturns {
				t.Fatalf("work lifecycle: drops/writes/returns=%d/%d/%d, want %d/%d/%d", drops, writes, returns, tc.drops, wantWrites, wantReturns)
			}
			requireBorrowedMIRValid(t, compiled)
		})
	}
}

func TestBorrowedParameterExclusionsMIR(t *testing.T) {
	cases := []struct {
		name, source string
		owned, async bool
	}{
		{"reference", `fn subject(p: &mut int) { *p = 2; }
fn main() { let mut value: int = 1; subject(&mut value); }`, false, false},
		{"fixed_width", `fn subject(p: int64) { p = 2:int64; }
fn main() { subject(1:int64); }`, false, false},
		{"owned_composite", `@copy type Cell = { value: int };
fn subject(p: Cell) {}
fn main() { let value = Cell { value: 1 }; subject(value); }`, true, false},
		{"async", crossingMIRPrelude + "\nasync fn subject(p: int) -> int { return 0; }", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("source_sha256=%x", sha256.Sum256([]byte(tc.source)))
			compiled := compileCrossingMIR(t, tc.source, nil)
			fn := borrowedMIRFunction(t, compiled, 1)
			borrowedMIRWork(t, fn, false)
			if fn.IsAsync != tc.async || compiled.types.IsRefCounted(fn.Locals[0].Type) != tc.async {
				t.Fatalf("fixture exclusion type/activation changed: %+v", fn.Locals[0])
			}
			if tc.async {
				if fn.ScopeLocal != mir.LocalID(fn.ParamCount) || int(fn.ScopeLocal) >= len(fn.Locals) || fn.Locals[fn.ScopeLocal].Type != compiled.types.Builtins().Uint64 {
					t.Fatal("existing async scope-local position/type changed")
				}
			} else {
				contract := mir.ArgContractBorrow
				if tc.owned {
					contract = mir.ArgContractTransferOwned
				}
				requireBorrowedMIRCall(t, compiled.mod, fn, contract)
			}
			drops := 0
			for _, bi := range borrowedMIRBlocks(t, fn) {
				for _, ins := range fn.Blocks[bi].Instrs {
					if ins.Kind == mir.InstrDrop && borrowedMIRLocal(ins.Drop.Place, 0) {
						drops++
					}
				}
			}
			want := 0
			if tc.owned {
				want = 1
			}
			if drops != want {
				t.Fatalf("existing parameter release count = %d, want %d", drops, want)
			}
			if tc.async {
				// Raw async parameters become frame-owned fields. Verify that
				// ownership after the existing lowering establishes those fields.
				for _, f := range compiled.mod.Funcs {
					mir.SimplifyCFG(f)
				}
				if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
					t.Fatalf("existing async frame lowering: %v", err)
				}
				for _, f := range compiled.mod.Funcs {
					mir.SimplifyCFG(f)
				}
			}
			requireBorrowedMIRValid(t, compiled)
		})
	}
}

func borrowedMIRFunction(t *testing.T, compiled crossingMIRCompileResult, params int) *mir.Func {
	t.Helper()
	var found *mir.Func
	for _, fn := range compiled.mod.Funcs {
		if fn != nil && baseName(fn.Name) == "subject" {
			if found != nil {
				t.Fatal("more than one concrete subject function")
			}
			found = fn
		}
	}
	if found == nil || found.ParamCount != params || len(found.Locals) < params {
		t.Fatalf("missing exact %d-position subject ABI", params)
	}
	for i := range params {
		local := found.Locals[i]
		decl := compiled.symbols.Table.Symbols.Get(local.Sym)
		if !local.Sym.IsValid() || decl == nil || decl.Kind != symbols.SymbolParam || local.Type == types.NoTypeID {
			t.Fatalf("ABI position %d lost original parameter identity/type: %+v", i, local)
		}
		if !types.ContainsGenericParam(compiled.types, decl.Type) && decl.Type != local.Type {
			t.Fatalf("ABI position %d changed original concrete parameter type", i)
		}
	}
	return found
}

func requireBorrowedMIRCall(t *testing.T, mod *mir.Module, fn *mir.Func, first mir.ArgContract) {
	t.Helper()
	calls := 0
	for _, caller := range mod.Funcs {
		if caller == nil {
			continue
		}
		for _, block := range caller.Blocks {
			for _, ins := range block.Instrs {
				if ins.Kind != mir.InstrCall || ins.Call.Callee.Sym != fn.Sym {
					continue
				}
				calls++
				if len(ins.Call.Args) != fn.ParamCount || len(ins.Call.ArgContracts) != fn.ParamCount || ins.Call.ArgContracts[0] != first {
					t.Fatal("caller no longer matches the original parameter ABI contract")
				}
				for i, arg := range ins.Call.Args {
					if arg.Type != fn.Locals[i].Type || arg.Kind == mir.OperandRetain {
						t.Fatalf("argument %d changed type or acquired a caller-side retain", i)
					}
				}
			}
		}
	}
	if calls != 1 {
		t.Fatalf("fixture must reach one actual call to subject, got %d", calls)
	}
}

func borrowedMIRWork(t *testing.T, fn *mir.Func, want bool) mir.LocalID {
	t.Helper()
	work := mir.NoLocalID
	for bi, block := range fn.Blocks {
		for ii, ins := range block.Instrs {
			if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueUse ||
				ins.Assign.Src.Use.Kind != mir.OperandRetain || !borrowedMIRLocal(ins.Assign.Src.Use.Place, 0) {
				continue
			}
			if !want || work != mir.NoLocalID || bi != int(fn.Entry) || ii != 0 ||
				ins.Assign.Dst.Kind != mir.PlaceLocal || len(ins.Assign.Dst.Proj) != 0 || int(ins.Assign.Dst.Local) < fn.ParamCount || int(ins.Assign.Dst.Local) >= len(fn.Locals) {
				t.Fatal("working owner must be one first entry retain into a separate ordinary local")
			}
			work = ins.Assign.Dst.Local
			local := fn.Locals[work]
			if local.Sym.IsValid() || local.Type != fn.Locals[0].Type || local.Flags != fn.Locals[0].Flags || ins.Assign.Src.Use.Type != local.Type {
				t.Fatal("working owner changed parameter type/flags or retained its ABI symbol")
			}
		}
	}
	if (work != mir.NoLocalID) != want {
		t.Fatalf("working owner presence = %t, want %t", work != mir.NoLocalID, want)
	}
	return work
}

func borrowedMIRLocal(place mir.Place, local mir.LocalID) bool {
	return place.Kind == mir.PlaceLocal && len(place.Proj) == 0 && place.Local == local
}

func borrowedMIRBlocks(t *testing.T, fn *mir.Func) []int {
	t.Helper()
	queue, seen := []mir.BlockID{fn.Entry}, make(map[mir.BlockID]bool)
	var blocks []int
	for len(queue) != 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		if id < 0 || int(id) >= len(fn.Blocks) || fn.Blocks[id].ID != id {
			t.Fatalf("invalid reachable block %d", id)
		}
		seen[id] = true
		blocks = append(blocks, int(id))
		term := fn.Blocks[id].Term
		switch term.Kind {
		case mir.TermGoto:
			queue = append(queue, term.Goto.Target)
		case mir.TermIf:
			queue = append(queue, term.If.Then, term.If.Else)
		case mir.TermReturn, mir.TermUnreachable:
		default:
			t.Fatalf("unexpected fixture terminator %s", term.Kind)
		}
	}
	return blocks
}

func requireBorrowedReturnOrder(t *testing.T, block *mir.Block, work mir.LocalID) {
	t.Helper()
	value := block.Term.Return.Value
	if value.Kind != mir.OperandMove || value.Place.Kind != mir.PlaceLocal || len(value.Place.Proj) != 0 || value.Place.Local == work {
		t.Fatal("returned owner was not detached from working storage")
	}
	materialized, finalDrops := false, 0
	for _, ins := range block.Instrs {
		if ins.Kind == mir.InstrAssign && borrowedMIRLocal(ins.Assign.Dst, value.Place.Local) {
			if materialized || ins.Assign.Src.Kind != mir.RValueUse || ins.Assign.Src.Use.Kind != mir.OperandRetain || !borrowedMIRLocal(ins.Assign.Src.Use.Place, work) {
				t.Fatal("return owner was not independently retained from exact work")
			}
			materialized = true
		}
		if materialized && ins.Kind == mir.InstrDrop && borrowedMIRLocal(ins.Drop.Place, work) {
			finalDrops++
		}
	}
	if !materialized || finalDrops != 1 {
		t.Fatalf("return requires materialization before exactly one final work release: %t/%d", materialized, finalDrops)
	}
}

func requireBorrowedOverwriteOrder(t *testing.T, block *mir.Block, assignment int, work mir.LocalID) {
	t.Helper()
	write := block.Instrs[assignment].Assign
	if write.Src.Kind != mir.RValueUse || write.Src.Use.Place.Kind != mir.PlaceLocal || write.Src.Use.Place.Local == work {
		t.Fatal("rebind RHS was not prepared independently before releasing work")
	}
	prepared, released := false, false
	for _, ins := range block.Instrs[:assignment] {
		if ins.Kind == mir.InstrAssign && borrowedMIRLocal(ins.Assign.Dst, write.Src.Use.Place.Local) ||
			ins.Kind == mir.InstrCall && ins.Call.HasDst && borrowedMIRLocal(ins.Call.Dst, write.Src.Use.Place.Local) {
			prepared = true
		}
		if ins.Kind == mir.InstrDrop && borrowedMIRLocal(ins.Drop.Place, work) {
			if !prepared || released {
				t.Fatal("work was released before its RHS, or released twice before rebind")
			}
			released = true
		}
	}
	if !prepared || !released {
		t.Fatal("rebind lacks prepared RHS and displaced-owner release")
	}
}

func requireBorrowedLoopBackedge(t *testing.T, fn *mir.Func, body *mir.Block) {
	t.Helper()
	if body.Term.Kind != mir.TermGoto {
		t.Fatal("loop assignment lost its backedge")
	}
	header := &fn.Blocks[body.Term.Goto.Target]
	if header.Term.Kind != mir.TermIf || header.Term.If.Then != body.ID || header.Term.If.Else == body.ID || fn.Entry == body.ID {
		t.Fatal("loop assignment is not guarded by the zero-iteration exit")
	}
}

func requireBorrowedMIRValid(t *testing.T, compiled crossingMIRCompileResult) {
	t.Helper()
	if err := mir.ValidateStructure(compiled.mod, compiled.types); err != nil {
		t.Fatalf("MIR structure: %v", err)
	}
	if findings := mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema); len(findings) != 0 {
		t.Fatalf("strict ownership findings: %v", findings)
	}
}
