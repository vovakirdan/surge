package mir

import (
	"testing"

	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// These synthetic identity controls exercise real argument/call lowering. The
// buildpipeline observer separately proves the public core declaration's origin.
func TestNoResultCallTerminationRequiresCoreIntrinsicExit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		terminate bool
	}{
		{"core_exit", true},
		{"renamed_display", true},
		{"ordinary_nothing", false},
		{"other_core_intrinsic", false},
		{"not_builtin", false},
		{"wrong_module", false},
		{"not_function", false},
		{"missing_mono", false},
		{"missing_origin_symbol", false},
		{"missing_original_hir", false},
		{"template_not_intrinsic", false},
		{"concrete_not_intrinsic", false},
		{"result_not_nothing", false},
		{"unresolved_name_only", false},
		{"wrong_arity", false},
		{"mismatched_instance", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ti := types.NewInterner()
			strings := source.NewInterner()
			table := symbols.NewTable(symbols.Hints{}, strings)
			originalID := table.Symbols.New(&symbols.Symbol{
				Name: strings.Intern("exit"), Kind: symbols.SymbolFunction,
				Flags:      symbols.SymbolFlagPublic | symbols.SymbolFlagImported | symbols.SymbolFlagBuiltin,
				ModulePath: "core",
			})
			instanceID := table.Symbols.New(&symbols.Symbol{
				Name: strings.Intern("exit::<fixture>"), Kind: symbols.SymbolFunction,
			})
			otherID := table.Symbols.New(&symbols.Symbol{Kind: symbols.SymbolFunction})
			original := &hir.Func{
				Name: "exit", SymbolID: originalID, Flags: hir.FuncPublic | hir.FuncIntrinsic,
				Result: ti.Builtins().Nothing, Params: []hir.Param{{Name: "value", Type: ti.Builtins().Int64}},
			}
			concrete := *original
			concrete.Name, concrete.SymbolID = "exit::<fixture>", instanceID
			mf := &mono.MonoFunc{OrigSym: originalID, InstanceSym: instanceID, Func: &concrete}
			symResult := &symbols.Result{Table: table}
			mm := &mono.MonoModule{
				Source:    &hir.Module{Funcs: []*hir.Func{original}, Symbols: symResult, TypeInterner: ti},
				FuncBySym: map[symbols.SymbolID]*mono.MonoFunc{instanceID: mf},
			}
			l := &funcLowerer{
				f:   &Func{Result: ti.Builtins().Nothing, Entry: 0, Blocks: []Block{{ID: 0}}, ScopeLocal: NoLocalID},
				cur: 0, types: ti, symbols: symResult, mono: mm,
				scopeLocal: NoLocalID, pendingReleaseGuard: NoLocalID,
			}
			callID, display := instanceID, "exit::<fixture>"
			sym := table.Symbols.Get(originalID)
			switch tc.name {
			case "renamed_display":
				display = "unrelated_display"
			case "ordinary_nothing":
				sym.Name = strings.Intern("ordinary_nothing")
				sym.Flags &^= symbols.SymbolFlagBuiltin
				original.Flags &^= hir.FuncIntrinsic
				concrete.Flags &^= hir.FuncIntrinsic
			case "other_core_intrinsic":
				sym.Name = strings.Intern("another_intrinsic")
			case "not_builtin":
				sym.Flags &^= symbols.SymbolFlagBuiltin
			case "wrong_module":
				sym.ModulePath = "application"
			case "not_function":
				sym.Kind = symbols.SymbolLet
			case "missing_mono":
				l.mono = nil
			case "missing_origin_symbol":
				mf.OrigSym = otherID + 1 // Valid ID encoding, absent from the arena.
			case "missing_original_hir":
				mm.Source.Funcs = nil
			case "template_not_intrinsic":
				original.Flags &^= hir.FuncIntrinsic
			case "concrete_not_intrinsic":
				concrete.Flags &^= hir.FuncIntrinsic
			case "result_not_nothing":
				concrete.Result = ti.Builtins().Int64
			case "unresolved_name_only":
				callID, display = symbols.NoSymbolID, "exit"
			case "wrong_arity":
				original.Params, concrete.Params = nil, nil
			case "mismatched_instance":
				mf.InstanceSym = otherID
			}
			// Even malformed authority retains one real, heap-free argument. It
			// must reach the call; a fixture error cannot satisfy a negative row.
			data := hir.CallData{
				SymbolID: callID,
				Callee:   &hir.Expr{Kind: hir.ExprVarRef, Data: hir.VarRefData{Name: display, SymbolID: callID}},
				Args: []*hir.Expr{{Kind: hir.ExprLiteral, Type: ti.Builtins().Int64,
					Data: hir.LiteralData{Kind: hir.LiteralInt, Text: "7", IntValue: 7}}},
			}
			value, err := l.lowerCallExpr(&hir.Expr{Kind: hir.ExprCall, Type: ti.Builtins().Nothing, Data: data}, false)
			if err != nil {
				t.Fatalf("real lowerCallExpr failed: %v", err)
			}
			if value.Kind != OperandConst || value.Const.Kind != ConstNothing {
				t.Errorf("no-result placeholder changed: %+v", value)
			}
			l.emit(&Instr{Kind: InstrNop})
			l.setTerm(&Terminator{Kind: TermReturn})
			bb := &l.f.Blocks[0]
			if len(bb.Instrs) == 0 || bb.Instrs[0].Kind != InstrCall {
				t.Fatalf("real call missing: %+v", bb)
			}
			call := bb.Instrs[0].Call
			if call.HasDst || call.Callee.Kind != CalleeSym || call.Callee.Sym != callID || call.Callee.Name != display {
				t.Errorf("call identity/emission changed: %+v", call)
			}
			if len(call.Args) != 1 || len(call.ArgContracts) != 1 {
				t.Fatalf("real argument/contract missing: %+v", call)
			}
			arg := call.Args[0]
			if arg.Kind != OperandConst || arg.Type != ti.Builtins().Int64 || arg.Const.Kind != ConstInt || arg.Const.IntValue != 7 || call.ArgContracts[0] != ArgContractBorrow {
				t.Errorf("lowered argument/contract changed: %+v / %v", arg, call.ArgContracts)
			}
			if tc.terminate {
				if bb.Term.Kind != TermUnreachable || len(bb.Instrs) != 1 {
					t.Errorf("intrinsic exit must terminate after the call: term=%v instructions=%d", bb.Term.Kind, len(bb.Instrs))
				}
			} else if bb.Term.Kind != TermReturn || len(bb.Instrs) != 2 || bb.Instrs[1].Kind != InstrNop {
				t.Errorf("non-authoritative call must keep its continuation: %+v", bb)
			}
		})
	}
}
