package driver

import (
	"crypto/sha256"
	"maps"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const returnOriginGenericBox = `type Box<T> = { marker: int64 };
extern<Box<T>> {
    fn keep(self: &Box<T>) -> &Box<T> { return self; }
}
`
const returnOriginGenericBoxLocal = returnOriginGenericBox + `fn bridge<T>(value: Box<T>, incompatible: Box<int64>) -> int64 {
    let borrowed = value.keep();
    return 1;
}
`
const returnOriginGenericBoxEscape = returnOriginGenericBox + `fn bridge<T>(unused: T) -> int64 {
    let escaped = {
        let owned: Box<T> = { marker: 1 };
        ret owned.keep();
    };
    return 1;
}
`
const returnOriginGenericUnusedRoot = `pragma module::dep, no_std;
fn identity<T>(value: T) -> T { return value; }
type Holder = { marker: int64 };
extern<Holder> {
    fn dormant(self: &Holder, value: &string) -> &string { return identity::<&string>(value); }
}
`
const returnOriginGenericOwnCopy = `fn sink<T>(value: own T) -> nothing;
fn bridge<T>(value: int64, unused: T) -> nothing { sink::<int64>(value); }
`

// These source transfers are independent of current concrete-use coverage.
// Corruption cases first retain and analyze the complete admitted source.
func TestAnalyzeOriginalGenericCallSignatures(t *testing.T) {
	for _, tc := range []struct {
		name, text, call, caller, callee, mutation, reason string
		escape, root                                       bool
		sources                                            []uint32
	}{
		{"nested_receiver_local", returnOriginGenericBoxLocal, "value.keep()", "bridge", "keep", "", "", false, false, nil},
		{"nested_receiver_escape", returnOriginGenericBoxEscape, "owned.keep()", "bridge", "keep", "", "", true, false, nil},
		{"unused_nongeneric_root", returnOriginGenericUnusedRoot, "identity::<&string>(value)", "dormant", "identity", "", "", false, true, []uint32{1}},
		{"own_copy_argument", returnOriginGenericOwnCopy, "sink::<int64>(value)", "bridge", "sink", "", "", false, false, nil},
		{"wrong_nested_argument", returnOriginGenericBoxLocal, "value.keep()", "bridge", "keep", "argument", "generic original call argument disagrees with its substituted source signature", false, false, nil},
		{"wrong_result_type", returnOriginGenericBoxLocal, "value.keep()", "bridge", "keep", "result", "generic original call result disagrees with its substituted source signature", false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_GENERIC_SIGNATURE_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.text)), tc.text)
			fixture := originalGenericSignatureFixture(t, tc.text, tc.escape, tc.root)
			res, inputs, unit, authority := fixture.owner, fixture.inputs, fixture.unit, fixture.authority
			if string(res.File.Content) != tc.text {
				t.Fatal("PRECONDITION: typed source bytes changed")
			}
			logReturnOriginGenericSignature(t, "admitted", tc.name, res, authority, unit, nil, nil)
			callee := originalGenericSignatureCandidate(t, res, authority, unit, tc.callee)
			caller := originalGenericSignatureCandidate(t, res, authority, unit, tc.caller)
			start := strings.Index(tc.text, tc.call)
			if start < 0 || strings.LastIndex(tc.text, tc.call) != start {
				t.Fatal("PRECONDITION: source call is not unique")
			}
			span := source.Span{File: res.File.ID, Start: uint32(start), End: uint32(start + len(tc.call))}
			var expression ast.ExprID
			for id, typ := range res.Sema.ExprTypes {
				if node := res.Builder.Exprs.Get(id); node != nil && node.Span == span {
					if expression.IsValid() || typ == types.NoTypeID || node.Kind != ast.ExprCall {
						t.Fatal("PRECONDITION: selected source call has no unique typed expression")
					}
					expression = id
				}
			}
			call, ok := res.Builder.Exprs.Call(expression)
			if !ok || call == nil || res.Symbols.ExprSymbols[expression] != originalGenericSignatureLocal(t, unit, callee) ||
				callee.SourceKey != unit.SourceKey || caller.SourceKey != callee.SourceKey || !caller.HasBody {
				t.Fatal("PRECONDITION: call lost exact source-owned selected identity")
			}
			closure := authority.InstantiationClosure
			if len(closure.Instances) != 0 || len(closure.UseSites) != 0 || len(closure.ResolvedDeferredCalls) != 0 || slices.Contains(closure.LiveCallables, caller.Symbol) {
				t.Fatal("PRECONDITION: original-only caller unexpectedly has live/concrete use authority")
			}
			roots, edges := authority.InstantiationGraph.Roots(), authority.InstantiationGraph.Edges()
			var arguments []types.TypeID
			if tc.root {
				if _, seeded := authority.InstantiationCallableSeeds[caller.Symbol]; seeded {
					t.Fatal("PRECONDITION: dependency caller became a root execution seed")
				}
				var original []sema.InstantiationRoot
				for _, root := range roots {
					if root.Witness.Caller == caller.Symbol || root.Witness.SourceKey == caller.SourceKey && root.Witness.Site == span {
						original = append(original, root)
					}
				}
				for _, edge := range edges {
					if edge.Caller == caller.Symbol {
						t.Fatal("PRECONDITION: nongeneric dependency caller unexpectedly owns a template edge")
					}
				}
				if len(original) != 1 || len(caller.TemplateParams) != 0 || original[0].Kind != sema.InstantiationFunction ||
					original[0].Template != callee.Symbol || original[0].Witness.Caller != caller.Symbol || original[0].Witness.Site != span || original[0].Witness.SourceKey != caller.SourceKey {
					t.Fatal("PRECONDITION: unused nongeneric method lacks its unique original root")
				}
				arguments = original[0].TemplateArgs
			} else {
				if len(roots) != 0 || len(edges) != 1 || len(caller.TemplateParams) != 1 || edges[0].Kind != sema.InstantiationFunction ||
					edges[0].Caller != caller.Symbol || edges[0].Callee != callee.Symbol || edges[0].Witness.Caller != caller.Symbol ||
					edges[0].Witness.Site != span || edges[0].Witness.SourceKey != caller.SourceKey || edges[0].CallerTemplateArity != 1 ||
					len(edges[0].CallerBindings) != 1 || edges[0].CallerBindings[0].Param != caller.TemplateParams[0] || edges[0].CallerBindings[0].ArgIndex != 0 {
					t.Fatal("PRECONDITION: generic body lacks its exact original edge and binding")
				}
				arguments = edges[0].CalleeTemplateArgs
			}
			if len(callee.TemplateParams) != 1 || len(arguments) != 1 {
				t.Fatal("PRECONDITION: selected source template arity changed")
			}
			var receiver ast.ExprID
			var incompatible types.TypeID
			if callee.HasSelf {
				member, ok := res.Builder.Exprs.Member(call.Target)
				if !ok || member == nil || len(call.Args) != 0 || len(callee.ParamTypes) != 1 || !callee.HasBody {
					t.Fatal("PRECONDITION: nested method lost physical self slot zero")
				}
				receiver = member.Target
				original, _ := res.Sema.TypeInterner.Lookup(callee.ParamTypes[0])
				actual := res.Sema.ExprTypes[receiver]
				formalBox, formalOK := res.Sema.TypeInterner.StructInfo(original.Elem)
				actualBox, actualOK := res.Sema.TypeInterner.StructInfo(actual)
				result, _ := res.Sema.TypeInterner.Lookup(res.Sema.ExprTypes[expression])
				if original.Kind != types.KindReference || original.Mutable || !formalOK || !actualOK || formalBox == nil || actualBox == nil ||
					formalBox.Decl != actualBox.Decl || formalBox.Name != actualBox.Name ||
					!slices.Equal(formalBox.TypeArgs, callee.TemplateParams) || !slices.Equal(actualBox.TypeArgs, arguments) ||
					arguments[0] != caller.TemplateParams[0] || result.Kind != types.KindReference || result.Mutable || result.Elem != actual {
					t.Fatal("PRECONDITION: nested receiver/result lost original nominal and substituted type arguments")
				}
				owner := res.Symbols.ExprSymbols[receiver]
				sym := res.Symbols.Table.Symbols.Get(owner)
				if sym == nil || sym.Decl.ASTFile != res.FileID || sym.Decl.SourceFile != res.File.ID || !sym.Scope.IsValid() || sym.Type != actual {
					t.Fatal("PRECONDITION: owned receiver lost physical storage owner")
				}
				matches := 0
				for _, borrow := range res.Sema.Borrows {
					if borrow.Life.FromExpr == receiver {
						matches++
						if borrow.ID == sema.NoBorrowID || borrow.Kind != sema.BorrowShared || borrow.Reserved || borrow.Place.Base != owner {
							t.Fatal("PRECONDITION: implicit receiver borrow has a wrong certificate")
						}
					}
				}
				if matches != 1 {
					t.Fatal("PRECONDITION: receiver requires one real FromExpr borrow certificate")
				}
				if tc.mutation == "argument" {
					incompatible = caller.ParamTypes[1]
					other, found := res.Sema.TypeInterner.StructInfo(incompatible)
					if !found || other == nil || incompatible == actual || other.Name != actualBox.Name || other.Decl != actualBox.Decl ||
						!slices.Equal(other.TypeArgs, []types.TypeID{res.Sema.TypeInterner.Builtins().Int64}) || slices.Equal(other.TypeArgs, actualBox.TypeArgs) {
						t.Fatal("PRECONDITION: mismatch lacks an existing incompatible argument of the same nominal declaration")
					}
				}
			} else if tc.root {
				if len(call.Args) != 1 || !slices.Equal(arguments, []types.TypeID{res.Sema.ExprTypes[call.Args[0].Value]}) ||
					res.Sema.ExprTypes[expression] != arguments[0] || !slices.Equal(callee.ParamTypes, callee.TemplateParams) || callee.ResultType != callee.TemplateParams[0] {
					t.Fatal("PRECONDITION: original root lost its actual direct borrowed substitution")
				}
			} else {
				formal, _ := res.Sema.TypeInterner.Lookup(callee.ParamTypes[0])
				if callee.HasBody || len(call.Args) != 1 || formal.Kind != types.KindOwn || formal.Elem != callee.TemplateParams[0] ||
					!slices.Equal(arguments, []types.TypeID{res.Sema.TypeInterner.Builtins().Int64}) ||
					res.Sema.ExprTypes[call.Args[0].Value] != arguments[0] || !res.Sema.IsCopyType(arguments[0]) || res.Sema.ExprTypes[expression] != callee.ResultType {
					t.Fatal("PRECONDITION: own formal does not receive the admitted plain Copy value")
				}
			}
			logReturnOriginCallEvidence(t, map[string]any{"stage": "signature_inputs", "case": tc.name, "expression": expression, "span": span,
				"call": call, "receiver": receiver, "caller": caller, "callee": callee, "template_arguments": arguments, "incompatible": incompatible})
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), authority, inputs.units)
			logReturnOriginGenericSignature(t, "original_analysis", tc.name, res, authority, unit, analysis, err)
			if err != nil || analysis == nil {
				t.Fatalf("PRECONDITION: original typed traversal failed: %v", err)
			}
			if callee.HasSelf {
				kept := requireReturnOriginSummary(t, analysis, tc.callee)
				if kept.BodyKey != callee.BodyKey || kept.Source != callee.Source || kept.Unknown || kept.NoNormalReturn || !slices.Equal(kept.ParamSlots, []uint32{0}) {
					t.Errorf("original keep body lost its incoming-reference result: %+v", kept)
				}
			}
			if tc.mutation != "" {
				want := sema.ReturnOriginPending{SourceKey: caller.SourceKey, Span: span, Reason: tc.reason}
				if slices.Contains(analysis.Pending, want) {
					t.Fatal("PRECONDITION: intact source already reported the proposed signature corruption")
				}
				changed := *res.Sema
				changed.ExprTypes = maps.Clone(res.Sema.ExprTypes)
				originalTypes := maps.Clone(res.Sema.ExprTypes)
				changedExpr := receiver
				if tc.mutation == "result" {
					changedExpr, incompatible = expression, res.Sema.TypeInterner.Builtins().Int64
					actual, _ := res.Sema.TypeInterner.Lookup(res.Sema.ExprTypes[expression])
					other, present := res.Sema.TypeInterner.Lookup(incompatible)
					if !present || actual.Kind != types.KindReference || other.Kind != types.KindInt || other.Width != types.Width64 {
						t.Fatal("PRECONDITION: result mismatch lacks an existing incompatible scalar descriptor")
					}
				}
				changed.ExprTypes[changedExpr] = incompatible
				changedUnits := slices.Clone(inputs.units)
				changedUnits[0].Sema = &changed
				after, afterErr := sema.AnalyzeReturnOrigins(t.Context(), &changed, changedUnits)
				copyRes := *res
				copyRes.Sema = &changed
				logReturnOriginGenericSignature(t, "mutated_analysis", tc.name, &copyRes, &changed, changedUnits[0], after, afterErr)
				logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "changed_expr": changedExpr, "old_type": originalTypes[changedExpr], "new_type": incompatible, "expected_pending": want})
				if !maps.Equal(originalTypes, res.Sema.ExprTypes) {
					t.Fatal("original typed source was mutated")
				}
				if afterErr != nil || after == nil || after.Complete() || !slices.Contains(after.Pending, want) {
					t.Fatalf("missing exact signature-mismatch refusal: want=%+v analysis=%+v error=%v", want, after, afterErr)
				}
				return
			}
			if tc.escape {
				checkReturnOriginGenericEscape(t, res, analysis, tc.text, "owned", "ret owned.keep();")
				return
			}
			got := requireReturnOriginSummary(t, analysis, tc.caller)
			if got.BodyKey != caller.BodyKey || got.Source != caller.Source || got.NoNormalReturn || got.Unknown || !slices.Equal(got.ParamSlots, tc.sources) ||
				!analysis.Complete() || len(analysis.Diagnostics) != 0 {
				t.Fatalf("original signature transfer is incomplete: summary=%+v analysis=%+v", got, analysis)
			}
		})
	}
}

func originalGenericSignatureCandidate(t *testing.T, res *DiagnoseResult, authority *sema.Result, unit sema.ReturnOriginUnit, name string) sema.CallableCandidate {
	t.Helper()
	var found *sema.CallableCandidate
	for i := range authority.CallableCandidates {
		candidate := &authority.CallableCandidates[i]
		if candidate.Name == name && candidate.Source.File == res.File.ID {
			if found != nil || candidate.Source.File != res.File.ID || !candidate.Symbol.IsValid() {
				t.Fatal("PRECONDITION: original callable name is not a unique source declaration")
			}
			found = candidate
		}
	}
	if found == nil {
		t.Fatal("PRECONDITION: missing original callable declaration")
	}
	sym := res.Symbols.Table.Symbols.Get(originalGenericSignatureLocal(t, unit, *found))
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Decl.ASTFile != res.FileID || sym.Decl.SourceFile != res.File.ID || sym.Span != found.Source {
		t.Fatal("PRECONDITION: canonical candidate lost its exact original symbol")
	}
	info, ok := res.Sema.TypeInterner.FnInfo(sym.Type)
	if !ok || info == nil || !slices.Equal(info.Params, found.ParamTypes) || info.Result != found.ResultType ||
		!info.ReturnSources().Equal(found.ReturnSources) || sym.Signature == nil || sym.Signature.HasBody != found.HasBody || sym.Signature.HasSelf != found.HasSelf {
		t.Fatal("PRECONDITION: original candidate disagrees with its source signature")
	}
	return *found
}

func logReturnOriginGenericSignature(t *testing.T, stage, name string, res *DiagnoseResult, authority *sema.Result, unit sema.ReturnOriginUnit, analysis *sema.ReturnOriginAnalysis, err error) {
	t.Helper()
	var descriptors []map[string]any
	for id := types.TypeID(1); ; id++ {
		typ, present := res.Sema.TypeInterner.Lookup(id)
		if !present {
			break
		}
		param, _ := res.Sema.TypeInterner.TypeParamInfo(id)
		nominal, _ := res.Sema.TypeInterner.StructInfo(id)
		fn, _ := res.Sema.TypeInterner.FnInfo(id)
		descriptors = append(descriptors, map[string]any{"id": id, "type": typ, "param": param, "struct": nominal, "fn": fn})
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": stage, "case": name, "source": string(res.File.Content),
		"diagnostics": res.Bag.Items(), "analysis": analysis, "error": errorReturnOriginCallText(err), "types": descriptors,
		"expr_types": res.Sema.ExprTypes, "expr_symbols": res.Symbols.ExprSymbols, "binding_types": res.Sema.BindingTypes,
		"symbols": res.Symbols.Table.Symbols.Data(), "scopes": res.Symbols.Table.Scopes.Data(),
		"borrows": res.Sema.Borrows, "candidates": authority.CallableCandidates, "roots": authority.InstantiationGraph.Roots(),
		"edges": authority.InstantiationGraph.Edges(), "closure": authority.InstantiationClosure, "publication": unit.Publication,
		"seeds": authority.InstantiationCallableSeeds, "calls": authority.FunctionCallEdges})
}
