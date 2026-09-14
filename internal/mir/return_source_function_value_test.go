package mir

import (
	"slices"
	"strings"
	"testing"

	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// These typed HIR/table fixtures exercise the real missing-Expr.Type path.
// They are metadata controls, not public source or lifetime-analysis proofs.
func TestFunctionValueFallbackReturnSources(t *testing.T) {
	for _, name := range []string{"explicit", "all", "empty", "concrete_substitution", "typed_destination", "bodyless", "missing_original", "arity_mismatch"} {
		t.Run(name, func(t *testing.T) {
			in := types.NewInterner()
			strs := source.NewInterner()
			table := symbols.NewTable(symbols.Hints{}, strs)
			original := table.Symbols.New(&symbols.Symbol{Name: strs.Intern("source"), Kind: symbols.SymbolFunction})
			// ensureFunc allocates instance IDs outside the original symbol arena.
			instance := symbols.SymbolID(0x90000001)
			ref := in.Intern(types.MakeReference(in.Builtins().Int64, false))
			params, result := []types.TypeID{ref, ref}, ref
			originalParams, originalResult := slices.Clone(params), result
			sources := types.ExplicitReturnSources(1)
			switch name {
			case "all":
				sources = types.ReturnSources{}
			case "empty":
				sources = types.ExplicitReturnSources()
			case "concrete_substitution":
				generic := in.RegisterTypeParam(strs.Intern("T"), uint32(original), 0, false, types.NoTypeID)
				originalParams, originalResult = []types.TypeID{generic, generic}, generic
			}
			originalSources := sources
			originalType := in.RegisterFnWithReturnSources(originalParams, originalResult, originalSources)
			table.Symbols.Get(original).Type = originalType
			mf := &mono.MonoFunc{Key: mono.MonoKey{Sym: original}, OrigSym: original, InstanceSym: instance,
				Func: &hir.Func{Name: "source::<fixture>", SymbolID: instance,
					Params: []hir.Param{{Name: "a", Type: ref}, {Name: "b", Type: ref}}, Result: result}}
			if name == "concrete_substitution" {
				mf.TypeArgs = []types.TypeID{ref}
			}
			resolved := &symbols.Result{Table: table}
			mm := &mono.MonoModule{Source: &hir.Module{Symbols: resolved, TypeInterner: in},
				Funcs: map[mono.MonoKey]*mono.MonoFunc{mf.Key: mf}, FuncBySym: map[symbols.SymbolID]*mono.MonoFunc{instance: mf}}
			l := &funcLowerer{types: in, symbols: resolved, mono: mm, out: &Module{},
				f:          &Func{Name: "function_value_witness", ScopeLocal: NoLocalID},
				scopeLocal: NoLocalID, pendingReleaseGuard: NoLocalID}
			expr := &hir.Expr{Kind: hir.ExprVarRef, Span: source.Span{File: 1, Start: 1, End: 2},
				Data: hir.VarRefData{Name: "source::<fixture>", SymbolID: instance}}
			var destination types.TypeID
			wantError := ""
			switch name {
			case "bodyless":
				mf.Func = nil // Supported imported/intrinsic instance without HIR.
			case "typed_destination":
				mf.OrigSym = symbols.NoSymbolID
				sources = types.ExplicitReturnSources(0)
				destination = in.RegisterFnWithReturnSources(params, result, sources)
			case "missing_original":
				mf.OrigSym = symbols.NoSymbolID
				wantError = "original"
			case "arity_mismatch":
				mf.Func.Params = mf.Func.Params[:1]
				wantError = "arity"
			}
			if expr.Type != types.NoTypeID || table.Symbols.Get(instance) != nil || mm.FuncBySym[instance] != mf {
				t.Fatal("fixture does not reach the missing-type mono lookup")
			}
			var monoParams []types.TypeID
			monoResult := types.NoTypeID
			if mf.Func != nil {
				for _, param := range mf.Func.Params {
					monoParams = append(monoParams, param.Type)
				}
				monoResult = mf.Func.Result
			}
			t.Logf("METADATA case=%s original=%d mono_original=%d instance=%d original_type=%d original_params=%v original_result=%d mono_params=%v mono_result=%d original_sources=%q hir_func_present=%t destination=%d",
				name, original, mf.OrigSym, instance, originalType, originalParams, originalResult, monoParams, monoResult, originalSources.CanonicalKey(), mf.Func != nil, destination)
			var op Operand
			var err error
			if destination != types.NoTypeID {
				op, err = l.lowerExprForType(expr, destination)
			} else {
				op, err = l.lowerExpr(expr, false)
			}
			if wantError != "" {
				if err == nil || !strings.Contains(err.Error(), "mir:") || !strings.Contains(err.Error(), "function") || !strings.Contains(err.Error(), wantError) {
					t.Fatalf("missing %s contract refusal: operand=%+v error=%v", wantError, op, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			requireFunctionValueContract(t, in, op, instance, params, result, sources)
			if expr.Type != types.NoTypeID {
				t.Fatal("fallback mutated the source HIR type")
			}
			originalInfo, _ := in.FnInfo(originalType)
			if originalInfo == nil || !slices.Equal(originalInfo.Params, originalParams) || originalInfo.Result != originalResult || !originalInfo.ReturnSources().Equal(originalSources) {
				t.Fatal("concrete reconstruction changed the original generic signature")
			}
			if name == "typed_destination" {
				t.Log("CONTROL typed_synthetic_expr_type_priority: missing original is legal when Expr.Type is already known")
				typed := *expr
				typed.Type = originalType
				op, err = l.lowerExprForType(&typed, destination)
				if err != nil {
					t.Fatal(err)
				}
				requireFunctionValueContract(t, in, op, instance, params, result, types.ExplicitReturnSources(1))
				if op.Type != originalType {
					t.Fatal("expected destination overrode the existing expression type")
				}
			}
			if name == "bodyless" {
				t.Log("CONTROL typed_tag_value_priority: tag values retain their expression's function type")
				tag := table.Symbols.New(&symbols.Symbol{Name: strs.Intern("Tag"), Kind: symbols.SymbolTag, Type: in.Builtins().Int64})
				typed := &hir.Expr{Kind: hir.ExprVarRef, Type: originalType, Data: hir.VarRefData{Name: "Tag", SymbolID: tag}}
				op, err = l.lowerExpr(typed, false)
				if err != nil {
					t.Fatal(err)
				}
				requireFunctionValueContract(t, in, op, tag, params, result, sources)
			}
		})
	}
}

func requireFunctionValueContract(t *testing.T, in *types.Interner, op Operand, symbol symbols.SymbolID, params []types.TypeID, result types.TypeID, sources types.ReturnSources) {
	t.Helper()
	if op.Kind != OperandConst || op.Const.Kind != ConstFn || op.Type == types.NoTypeID || op.Const.Type != op.Type || op.Const.Sym != symbol {
		t.Fatalf("function value representation changed: %+v", op)
	}
	info, ok := in.FnInfo(op.Type)
	if !ok || info == nil || !slices.Equal(info.Params, params) || info.Result != result {
		t.Fatalf("function value concrete signature changed: %+v", info)
	}
	if !info.ReturnSources().Equal(sources) {
		t.Fatalf("function value return sources=%q, want %q", info.ReturnSources().CanonicalKey(), sources.CanonicalKey())
	}
}
