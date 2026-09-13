package buildpipeline

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/hir"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/ownershipgate"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

type exitCallAuthority struct {
	origin, instance, concrete             symbols.SymbolID
	symbol                                 symbols.Symbol
	name                                   string
	originalPresent, originalIntrinsic     bool
	concreteIntrinsic                      bool
	originalResult, concreteResult         types.TypeID
	originalParameters, concreteParameters int
}

// Snapshot only call identity at the existing mono boundary. Compile receives
// the original result/error unchanged; no temporary diagnostic observer is used.
func compileExitObserver(t *testing.T, path string) (CompileResult, map[symbols.SymbolID]exitCallAuthority) {
	t.Helper()
	repo := testRepoRoot(t)
	t.Setenv("SURGE_STDLIB", repo)
	original, entries := monomorphizeModule, 0
	authority := make(map[symbols.SymbolID]exitCallAuthority)
	monomorphizeModule = func(module *hir.Module, inst *mono.InstantiationMap, s *sema.Result, opts mono.Options) (*mono.MonoModule, error) {
		entries++
		mm, err := original(module, inst, s, opts)
		if err == nil && mm != nil && mm.Source != nil && mm.Source.Symbols != nil && mm.Source.Symbols.Table != nil {
			table := mm.Source.Symbols.Table
			for id, mf := range mm.FuncBySym {
				if mf == nil || mf.Func == nil || table.Symbols == nil || table.Strings == nil {
					continue
				}
				sym := table.Symbols.Get(mf.OrigSym)
				if sym == nil {
					continue
				}
				name, ok := table.Strings.Lookup(sym.Name)
				if !ok || name != "exit" {
					continue
				}
				a := exitCallAuthority{origin: mf.OrigSym, instance: mf.InstanceSym, concrete: mf.Func.SymbolID,
					symbol: *sym, name: name, concreteIntrinsic: mf.Func.IsIntrinsic(),
					concreteResult: mf.Func.Result, concreteParameters: len(mf.Func.Params)}
				for _, fn := range mm.Source.Funcs {
					if fn != nil && fn.SymbolID == mf.OrigSym {
						a.originalPresent, a.originalIntrinsic = true, fn.IsIntrinsic()
						a.originalResult, a.originalParameters = fn.Result, len(fn.Params)
					}
				}
				authority[id] = a
			}
		}
		return mm, err
	}
	t.Cleanup(func() { monomorphizeModule = original })
	result, err := Compile(context.Background(), &CompileRequest{
		TargetPath: path, BaseDir: repo, Analysis: true, Backend: BackendLLVM, MaxDiagnostics: 500,
	})
	if err != nil {
		if result.Diagnose != nil && result.Diagnose.Bag != nil {
			for _, d := range result.Diagnose.Bag.Items() {
				t.Logf("diagnostic %s: %s", d.Code, d.Message)
			}
		}
		t.Fatalf("Compile: %v", err)
	}
	if entries != 1 || result.MIR == nil || result.Diagnose == nil || result.Diagnose.Sema == nil || result.Diagnose.Sema.TypeInterner == nil {
		t.Fatalf("incomplete compile/mono evidence: entries=%d result=%+v", entries, result)
	}
	return result, authority
}

func exitObserverFunction(t *testing.T, module *mir.Module, name string) *mir.Func {
	t.Helper()
	var found *mir.Func
	for _, fn := range module.Funcs {
		if fn != nil && fn.Name == name {
			if found != nil {
				t.Fatalf("duplicate MIR function %q", name)
			}
			found = fn
		}
	}
	if found == nil {
		t.Fatalf("missing MIR function %q", name)
	}
	return found
}

func requireExitAuthority(t *testing.T, result CompileResult, a exitCallAuthority, callee symbols.SymbolID, path string, intrinsic bool) {
	t.Helper()
	if !a.origin.IsValid() || a.instance != callee || a.concrete != callee || !a.originalPresent || a.name != "exit" || a.symbol.Kind != symbols.SymbolFunction || a.symbol.ModulePath != "core" {
		t.Fatalf("missing actual original/concrete exit authority: %+v", a)
	}
	if a.originalIntrinsic != intrinsic || a.concreteIntrinsic != intrinsic || (a.symbol.Flags&symbols.SymbolFlagBuiltin != 0) != intrinsic {
		t.Errorf("intrinsic/builtin authority differs: %+v", a)
	}
	files := result.Diagnose.FileSet
	if files == nil || !files.HasFile(a.symbol.Span.File) {
		t.Fatal("original declaration has no source file")
	}
	actual := files.Get(a.symbol.Span.File).Path
	if !filepath.IsAbs(actual) {
		actual = filepath.Join(files.BaseDir(), actual)
	}
	rel, err := filepath.Rel(testRepoRoot(t), actual)
	if err != nil || filepath.ToSlash(rel) != path {
		t.Errorf("original declaration path=%q err=%v, want %s", rel, err, path)
	}
}

func TestIntrinsicExitTerminatesErringFailure(t *testing.T) {
	repo := testRepoRoot(t)
	path := filepath.Join(repo, "testdata/golden/sema/valid/erring_option.sg")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if digest := fmt.Sprintf("%x", sha256.Sum256(src)); digest != "21025e1339ab6a53e7adeb97430fcce3d9c269ebc66bc90ad8e4d9ce22a61203" {
		t.Fatalf("erring source changed: %s", digest)
	}
	result, authority := compileExitObserver(t, path)
	fn := exitObserverFunction(t, result.MIR, "exit::<int, Error>")
	intType := result.Diagnose.Sema.TypeInterner.Builtins().Int
	var errorBlock *mir.Block
	callIndex, calls := -1, 0
	for i := range fn.Blocks {
		for j := range fn.Blocks[i].Instrs {
			ins := &fn.Blocks[i].Instrs[j]
			if ins.Kind == mir.InstrCall && authority[ins.Call.Callee.Sym].originalIntrinsic {
				errorBlock, callIndex, calls = &fn.Blocks[i], j, calls+1
			}
		}
	}
	if calls != 1 {
		t.Fatalf("expected one actual intrinsic call, got %d", calls)
	}
	call := errorBlock.Instrs[callIndex].Call
	t.Run("intrinsic_call", func(t *testing.T) {
		a := authority[call.Callee.Sym]
		requireExitAuthority(t, result, a, call.Callee.Sym, "core/intrinsics.sg", true)
		nothing := result.Diagnose.Sema.TypeInterner.Builtins().Nothing
		if a.originalResult != nothing || a.concreteResult != nothing || a.originalParameters != 1 || a.concreteParameters != 1 || call.Callee.Kind != mir.CalleeSym || call.HasDst || len(call.Args) != 1 || len(call.ArgContracts) != 1 {
			t.Fatalf("intrinsic call signature/emission changed: authority=%+v call=%+v", a, call)
		}
		if call.Args[0].Kind != mir.OperandMove || types.Label(result.Diagnose.Sema.TypeInterner, call.Args[0].Type) != "Error" || call.ArgContracts[0] != mir.ArgContractTransferOwned {
			t.Errorf("Error must still transfer to the intrinsic: %+v", call)
		}
	})
	t.Run("error_arm", func(t *testing.T) {
		if errorBlock.Term.Kind != mir.TermUnreachable || callIndex != len(errorBlock.Instrs)-1 {
			t.Errorf("error call must end its block without a fabricated value or return edge: bb%d call#%d instructions=%d term=%+v", errorBlock.ID, callIndex, len(errorBlock.Instrs), errorBlock.Term)
		}
	})
	t.Run("success_arm", func(t *testing.T) {
		var success, returned *mir.Block
		payloadCount, returnCount := 0, 0
		payload := mir.NoLocalID
		for i := range fn.Blocks {
			bb := &fn.Blocks[i]
			if bb.Term.Kind == mir.TermReturn && bb.Term.Return.HasValue {
				returned = bb
				returnCount++
			}
			for _, ins := range bb.Instrs {
				if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueTagPayload && ins.Assign.Src.TagPayload.TagName == "Success" {
					success, payload = bb, ins.Assign.Dst.Local
					payloadCount++
					if !ins.Assign.Src.TagPayload.MoveOut {
						t.Error("Success payload lost its owning transfer")
					}
				}
			}
		}
		if payloadCount != 1 || returnCount != 1 || success == nil || returned == nil || payload < 0 || int(payload) >= len(fn.Locals) {
			t.Fatal("missing Success payload/return path")
		}
		selected := false
		for _, bb := range fn.Blocks {
			if bb.Term.Kind != mir.TermIf || bb.Term.If.Then != success.ID || bb.Term.If.Else != errorBlock.ID {
				continue
			}
			for _, ins := range bb.Instrs {
				if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueTagTest && ins.Assign.Src.TagTest.TagName == "Success" && ins.Assign.Dst.Local == bb.Term.If.Cond.Place.Local {
					selected = true
				}
			}
		}
		if !selected {
			t.Error("Success branch is not selected by the actual tag test")
		}
		ret := returned.Term.Return.Value
		if fn.Result != intType || ret.Kind != mir.OperandMove || ret.Type != intType || success.Term.Kind != mir.TermGoto || success.Term.Goto.Target != returned.ID {
			t.Fatalf("Success no longer transfers int along its normal return path: %+v", ret)
		}
		owners := map[mir.LocalID]bool{payload: true}
		for _, ins := range success.Instrs {
			if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueUse {
				op := ins.Assign.Src.Use
				if (op.Kind == mir.OperandRetain || op.Kind == mir.OperandMove) && owners[op.Place.Local] {
					owners[ins.Assign.Dst.Local] = true
				}
			}
		}
		for _, id := range []mir.LocalID{payload, ret.Place.Local} {
			if id < 0 || int(id) >= len(fn.Locals) || fn.Locals[id].Type != intType || fn.Locals[id].Flags&(mir.LocalFlagCopy|mir.LocalFlagOwnsHeap) != mir.LocalFlagCopy|mir.LocalFlagOwnsHeap || !owners[id] {
				t.Errorf("Success int owner/definition missing at L%d", id)
			}
		}
	})
	t.Run("ordinary_erring_exit", func(t *testing.T) {
		caller := exitObserverFunction(t, result.MIR, "test_erring_exit_method")
		found := 0
		for _, bb := range caller.Blocks {
			for i, ins := range bb.Instrs {
				if ins.Kind != mir.InstrCall || ins.Call.Callee.Sym != fn.Sym {
					continue
				}
				found++
				a := authority[ins.Call.Callee.Sym]
				requireExitAuthority(t, result, a, ins.Call.Callee.Sym, "core/result.sg", false)
				dst := ins.Call.Dst.Local
				if a.concreteResult != intType || !ins.Call.HasDst || dst < 0 || int(dst) >= len(caller.Locals) || caller.Locals[dst].Type != intType || i+1 >= len(bb.Instrs) || bb.Term.Kind != mir.TermReturn {
					t.Error("ordinary Erring.exit lost its result or caller continuation")
				}
			}
		}
		if found != 1 {
			t.Errorf("ordinary Erring.exit call count=%d, want 1", found)
		}
	})
	t.Run("ownership", func(t *testing.T) {
		findings := mir.VerifyOwnership(result.MIR, result.Diagnose.Sema.TypeInterner, result.Diagnose.Sema)
		for i := range findings {
			key, err := ownershipgate.NormalizeFinding(repo, result.Diagnose.FileSet, &findings[i])
			t.Logf("ownership finding=%+v normalized=%+v error=%v", findings[i], key, err)
		}
		if len(findings) != 0 {
			t.Errorf("whole-module ownership findings=%d, want zero", len(findings))
		}
	})
}

func TestOrdinaryNothingCallContinuesAfterTheCall(t *testing.T) {
	const source = `fn ordinary_nothing() -> nothing { return nothing; }
fn after_nothing() -> int { ordinary_nothing(); return 7; }
`
	result, _ := compileExitObserver(t, writeAnalysisSource(t, source))
	ordinary := exitObserverFunction(t, result.MIR, "ordinary_nothing")
	caller := exitObserverFunction(t, result.MIR, "after_nothing")
	found := 0
	for _, bb := range caller.Blocks {
		for _, ins := range bb.Instrs {
			if ins.Kind == mir.InstrCall && ins.Call.Callee.Sym == ordinary.Sym {
				found++
				ret := bb.Term.Return
				if ins.Call.HasDst || bb.Term.Kind != mir.TermReturn || !ret.HasValue || ret.Value.Kind != mir.OperandConst || ret.Value.Const.Kind != mir.ConstInt || ret.Value.Const.IntValue != 7 {
					t.Errorf("ordinary nothing call must continue to return 7: call=%+v term=%+v", ins.Call, bb.Term)
				}
			}
		}
	}
	if found != 1 {
		t.Errorf("ordinary nothing call count=%d, want 1", found)
	}
}
