package driver

import (
	"crypto/sha256"
	"maps"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const stringRangeFormsSource = `fn probe(value: &string, start: int, end: int, bounds: Range<int>) -> string {
    let a = value[[0..1]];
    let b = value[[start..end]];
    let c = value[[start..]];
    let d = value[[..end]];
    let e = value[[..]];
    let f = value[[..=end]];
    let g = value[bounds];
    return a + b + c + d + e + f + g;
}
fn local_slice() -> string {
    let owned: string = "abcd";
    return owned[[1..]];
}
`

const stringRangeEffectSource = `fn probe(value: &string, a: &string, b: &string) -> &string {
    let mut chosen: &string = a;
    let mut observed: &string = a;
    let part = value[[{ let _ = nothing; chosen = b; ret 0; }..{ let _ = nothing; observed = chosen; chosen = a; ret 1; }]];
    return observed;
}
`

const stringRangeEscapeSource = `fn probe(value: &string, outside: &string) -> int {
    let mut escaped: &string = outside;
    let part = value[[0..{ let owned: string = "local"; escaped = &owned; ret 1; }]];
    return 1;
}
`

const stringRangeForeignSource = `type Foreign = { marker: int64 };
extern<Foreign> {
    fn __index(self: &Foreign, index: Range<int>) -> string {
        let _ = self;
        let _ = index;
        return "foreign";
    }
}
fn probe(value: &Foreign, bounds: Range<int>) -> string {
    let observed: bool = value[bounds] == "foreign";
    return "foreign";
}
`

// Exact component sites are reviewed while all eleven original units and
// unrelated Pending remain visible. This does not assert public Complete.
func TestAnalyzeTypedStringRangeOrigins(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"string_range_forms", stringRangeFormsSource}, {"bound_effect_order", stringRangeEffectSource},
		{"bound_inner_escape", stringRangeEscapeSource}, {"foreign_selected_range", stringRangeForeignSource},
		{"wrong_range_constructor", stringRangeEffectSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_STRING_RANGE_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.src)), tc.src)
			res := returnOriginStdlibFixture(t, tc.src, tc.name == "bound_inner_escape")
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			inputs, unitsErr := collectReturnOriginUnits(res)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "closure_error": errorReturnOriginCallText(closureErr),
				"unit_error": errorReturnOriginCallText(unitsErr), "closure": res.Sema.InstantiationClosure, "candidates": res.Sema.CallableCandidates})
			if closureErr != nil || unitsErr != nil || len(inputs.units) != 11 {
				t.Fatal("PRECONDITION: missing full source input or finalized authority")
			}
			checkReturnOriginStdlibBags(t, res, tc.name == "bound_inner_escape")
			checkReturnOriginCloneUnits(t, res, inputs.units)
			facts := checkReturnOriginRangeSites(t, res, inputs.units, tc.name)
			root := &inputs.units[facts.root]
			if string(res.File.Content) != tc.src {
				t.Fatal("PRECONDITION: loader changed the frozen root source")
			}
			var allowed sema.ReturnOriginPending
			if tc.name == "foreign_selected_range" {
				equalities := 0
				for raw := uint32(1); raw <= root.Builder.Exprs.Arena.Len(); raw++ {
					id := ast.ExprID(raw)
					data, ok := root.Builder.Exprs.Binary(id)
					if !ok || data == nil || data.Op != ast.ExprBinaryEq || data.Left != facts.indexes[0] {
						continue
					}
					c := checkReturnOriginRangeCallable(t, res, inputs.units, *root, root.Sema.MagicBinarySymbols[id])
					if len(c.ParamTypes) != 2 {
						t.Fatal("PRECONDITION: string equality does not have two borrowed operands")
					}
					self, typed := res.Sema.TypeInterner.Lookup(c.ParamTypes[0])
					if !c.Builtin || !c.Intrinsic || c.HasBody || c.Name != "__eq" || !c.HasSelf || len(c.TemplateParams) != 0 || !typed || self.Kind != types.KindReference || self.Mutable ||
						self.Elem != res.Sema.TypeInterner.Builtins().String || c.ParamTypes[1] != c.ParamTypes[0] || c.ResultType != res.Sema.TypeInterner.Builtins().Bool || root.Sema.ExprTypes[id] != c.ResultType || root.Sema.ExprTypes[data.Right] != self.Elem {
						t.Fatal("PRECONDITION: foreign result consumer is not typed borrowed string equality")
					}
					logReturnOriginCallEvidence(t, map[string]any{"foreign_equality": id, "ast": data, "candidate": c, "type": root.Sema.ExprTypes[id]})
					equalities++
				}
				if equalities != 1 {
					t.Fatal("PRECONDITION: foreign index lost its single nonconsuming equality use")
				}
				allowed = sema.ReturnOriginPending{SourceKey: root.SourceKey, Span: root.Builder.Exprs.Get(facts.indexes[0]).Span, Reason: "index requires a non-scalar index transfer"}
			}
			var owner, primary source.Span
			if tc.name == "bound_inner_escape" {
				for _, sym := range res.Symbols.Table.Symbols.Data() {
					name, _ := res.Builder.StringsInterner.Lookup(sym.Name)
					if name == "owned" && sym.Span.File == res.File.ID {
						if !owner.Empty() {
							t.Fatal("PRECONDITION: bound owner is not unique")
						}
						owner = sym.Span
					}
				}
				start := strings.Index(tc.src, "ret 1;")
				if owner.Empty() || start < 0 {
					t.Fatal("PRECONDITION: frozen bound owner or ret is missing")
				}
				primary = source.Span{File: res.File.ID, Start: uint32(start), End: uint32(start + len("ret 1;"))}
				logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "expected_owner": owner, "expected_primary": primary})
			}
			original := root.Symbols
			before := maps.Clone(original.ExprSymbols)
			if tc.name == "wrong_range_constructor" {
				var replacement symbols.SymbolID
				matches := 0
				for _, c := range res.Sema.CallableCandidates {
					if c.Name != "rt_range_int_from_start" || !c.Builtin || !c.Intrinsic || c.HasBody || c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" {
						continue
					}
					matches++
					aliases := root.Publication.LocalSymbols(c.Symbol)
					// An absent publication map means the symbol table is shared.
					if len(root.Publication.RootToLocalSymbols) == 0 {
						aliases = []symbols.SymbolID{c.Symbol}
					}
					if len(aliases) != 1 || !aliases[0].IsValid() {
						t.Fatal("PRECONDITION: replacement has missing or ambiguous published local symbols")
					}
					replacement = aliases[0]
				}
				if matches != 1 {
					t.Fatal("PRECONDITION: replacement lacks one exact canonical constructor")
				}
				c := checkReturnOriginRangeCallable(t, res, inputs.units, *root, replacement)
				if c.Name != "rt_range_int_from_start" || !slices.Equal(c.ParamTypes, []types.TypeID{res.Sema.TypeInterner.Builtins().Int, res.Sema.TypeInterner.Builtins().Bool}) {
					t.Fatal("PRECONDITION: replacement is not the exact original shorter constructor")
				}
				copy := *original
				copy.ExprSymbols = maps.Clone(original.ExprSymbols)
				copy.ExprSymbols[facts.literals[0]] = replacement
				root.Symbols = &copy
				allowed = sema.ReturnOriginPending{SourceKey: root.SourceKey, Span: root.Builder.Exprs.Get(facts.literals[0]).Span, Reason: "range literal disagrees with its original constructor"}
			}
			effective := maps.Clone(root.Symbols.ExprSymbols)
			changed := 0
			for id, sym := range before {
				if effective[id] != sym {
					changed++
				}
			}
			if len(effective) != len(before) || (tc.name == "wrong_range_constructor" && changed != 1) || (tc.name != "wrong_range_constructor" && changed != 0) {
				t.Fatal("PRECONDITION: mutation did not change exactly its one original expression symbol")
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "analysis": analysis, "analysis_error": errorReturnOriginCallText(err),
				"original_expr_symbols": before, "effective_expr_symbols": effective, "allowed_pending": allowed})
			if !maps.Equal(before, original.ExprSymbols) || !maps.Equal(effective, root.Symbols.ExprSymbols) {
				t.Fatal("analysis mutated original or detached expression-symbol evidence")
			}
			if err != nil || analysis == nil {
				t.Fatalf("range analysis failed: %v", err)
			}
			for _, pending := range analysis.Pending {
				if (pending.SourceKey == root.SourceKey || facts.sites[pending.Span] == pending.SourceKey) && pending != allowed {
					t.Errorf("reviewed range transfer remains unproved: %+v", pending)
				}
			}
			if allowed.Reason != "" && !slices.Contains(analysis.Pending, allowed) {
				t.Errorf("missing exact selected-operation refusal: %+v", allowed)
			}
			if !slices.Contains(analysis.Pending, facts.array) {
				t.Errorf("separate Array<T> range obligation disappeared: %+v", facts.array)
			}
			if tc.name != "foreign_selected_range" {
				var slots []uint32
				if tc.name == "bound_effect_order" || tc.name == "wrong_range_constructor" {
					slots = []uint32{2}
				}
				summary := requireReturnOriginSummary(t, analysis, "probe")
				if summary.Source.File != res.File.ID || summary.NoNormalReturn || summary.Unknown || !slices.Equal(summary.ParamSlots, slots) {
					t.Errorf("range effects/result lost exact probe sources %v: %+v", slots, summary)
				}
			}
			if tc.name == "string_range_forms" {
				s := requireReturnOriginSummary(t, analysis, "local_slice")
				if s.Source.File != res.File.ID || s.NoNormalReturn || s.Unknown || len(s.ParamSlots) != 0 {
					t.Errorf("owning local string slice acquired borrow roots: %+v", s)
				}
			}
			if tc.name != "bound_inner_escape" {
				if len(analysis.Diagnostics) != 0 {
					t.Errorf("valid range source acquired diagnostics: %+v", analysis.Diagnostics)
				}
				return
			}
			found := false
			for _, d := range analysis.Diagnostics {
				if d.Code == diag.SemaBorrowEscapesReturn && d.Severity == diag.SevError && d.Primary == primary && len(d.Help) > 0 &&
					d.Message == "borrow of 'owned' outlives its owner when this scope exits" && slices.ContainsFunc(d.Notes, func(n diag.Note) bool {
					return !owner.Empty() && n.Span == owner && n.Msg == "'owned' owns storage that ends in this scope"
				}) {
					found = true
				}
			}
			if !found {
				t.Errorf("range result erased exact bound-owner escape at %v, owner %v: %+v", primary, owner, analysis.Diagnostics)
			}
		})
	}
}

type returnOriginRangeFacts struct {
	root              int
	sites             map[source.Span]string
	indexes, literals []ast.ExprID
	array             sema.ReturnOriginPending
}

func checkReturnOriginRangeCallable(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit, u sema.ReturnOriginUnit, local symbols.SymbolID) sema.CallableCandidate {
	t.Helper()
	var found []sema.CallableCandidate
	for _, c := range res.Sema.CallableCandidates {
		mapped := c.Symbol == local
		if len(u.Publication.RootToLocalSymbols) > 0 {
			mapped = slices.Contains(u.Publication.LocalSymbols(c.Symbol), local)
		}
		if mapped {
			found = append(found, c)
		}
	}
	sym := u.Symbols.Table.Symbols.Get(local)
	if !local.IsValid() || len(found) != 1 || sym == nil || sym.Kind != symbols.SymbolFunction {
		t.Fatal("PRECONDITION: selected range callable lacks unique canonical authority")
	}
	c := found[0]
	info, ok := u.Sema.TypeInterner.FnInfo(sym.Type)
	if !ok || info == nil || sym.Span != c.Source || !slices.Equal(info.Params, c.ParamTypes) || info.Result != c.ResultType || !info.ReturnSources().Equal(c.ReturnSources) {
		t.Fatal("PRECONDITION: selected range callable lost its typed declaration")
	}
	owners := 0
	for _, owner := range units {
		if owner.Builder.Files.Get(owner.FileID).Span.File != c.Source.File {
			continue
		}
		for _, identity := range owner.Publication.LocalCallables {
			mapped := identity.Symbol == c.Symbol
			if len(owner.Publication.RootToLocalSymbols) > 0 {
				mapped = slices.Contains(owner.Publication.LocalSymbols(c.Symbol), identity.Symbol)
			}
			if !mapped || identity.BodyKey != c.BodyKey || identity.SourceKey != c.SourceKey {
				continue
			}
			original := owner.Symbols.Table.Symbols.Get(identity.Symbol)
			if original == nil || original.Span != c.Source || original.Decl.SourceFile != c.Source.File || original.Decl.ASTFile != owner.FileID {
				t.Fatal("PRECONDITION: range callable lost original physical owner")
			}
			var fn *ast.FnItem
			if original.Decl.Item.IsValid() {
				fn, _ = owner.Builder.Items.Fn(original.Decl.Item)
			}
			for member, id := range owner.Symbols.ExternSyms {
				if id == identity.Symbol {
					m := owner.Builder.Items.ExternMember(member)
					if m != nil && m.Kind == ast.ExternMemberFn {
						fn = owner.Builder.Items.FnByPayload(m.Fn)
					}
				}
			}
			originalInfo, typed := owner.Sema.TypeInterner.FnInfo(original.Type)
			if !typed || originalInfo == nil || !slices.Equal(originalInfo.Params, c.ParamTypes) || originalInfo.Result != c.ResultType || !originalInfo.ReturnSources().Equal(c.ReturnSources) || fn == nil || fn.NameSpan != c.Source || fn.FnKeywordSpan != c.DeclKeyword || fn.Body.IsValid() != c.HasBody {
				t.Fatal("PRECONDITION: range callable lost exact original AST")
			}
			if c.Builtin && (owner.SourceKey != "core/intrinsics.sg" || c.SourceKey != "builtin" || c.ModulePath != "core/intrinsics" || original.Flags&symbols.SymbolFlagBuiltin == 0) {
				t.Fatal("PRECONDITION: builtin range callable lacks original core certificate")
			}
			owners++
			logReturnOriginCallEvidence(t, map[string]any{"selected_range_symbol": local, "selected_range_candidate": c, "original_owner": owner.SourceKey, "original_identity": identity, "original_symbol": original, "original_function": fn})
		}
	}
	if owners != 1 {
		t.Fatal("PRECONDITION: range callable does not have one original physical declaration")
	}
	return c
}
