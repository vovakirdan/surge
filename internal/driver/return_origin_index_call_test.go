package driver

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// An index over a user type that SEMA resolved to an `__index` declaration is
// answered as the call HIR lowers it to when the declaration agrees with the
// typed operation and its result is erased. A result that can hold a reference
// or a loan, a converted result, a generic or `&mut` receiver, a formal the
// effect rule refuses, and a loan-carrying cursor argument stay refused.
const indexCallSource = `type Box = { value: int };
extern<Box> {
    fn __index(self: Box, r: Range<int>) -> int;
}

type Other = { value: int };
extern<Other> {
    fn __index(self: Other, r: Range<int>) -> int;
}

type Named = { text: string };
extern<Named> {
    fn __index(self: &Named, r: Range<int>) -> string {
        let _ = r;
        return "named";
    }
}

type Gen<T> = { value: T };
extern<Gen<T>> {
    fn __index(self: &Gen<T>, r: Range<int>) -> int {
        let _ = r;
        return 1;
    }
}

type SelfRef = { value: int };
extern<SelfRef> {
    fn __index(self: &SelfRef, r: Range<int>) -> &int {
        let _ = r;
        return &self.value;
    }
}

type LocalRef = { value: int };
extern<LocalRef> {
    fn __index(self: &LocalRef, r: Range<int>) -> &int {
        let _ = r;
        let local: int = 1;
        return &local;
    }
}

type OpaqueRef = { value: int };
extern<OpaqueRef> {
    fn __index(self: &OpaqueRef, r: Range<int>) -> &int;
}

type ArrBox = { value: int };
extern<ArrBox> {
    fn __index(self: ArrBox, r: Range<int>) -> int[];
}

type Tag<T> = { value: int };
extern<Tag<T>> {
    fn __index(self: &Tag<T>, r: Range<int>) -> int;
}

type FnBox = { value: int };
extern<FnBox> {
    fn __index(self: FnBox, f: fn(int) -> int) -> int;
}

type MutBox = { value: int };
extern<MutBox> {
    fn __index(self: &mut MutBox, r: Range<int>) -> int;
}

fn opaque_owned(b: Box) -> int {
    return b[[1..2]];
}

fn body_owned(n: &Named, r: Range<int>) -> bool {
    return n[r] == "named";
}

fn borrowed_local_receiver() -> bool {
    let n: Named = Named { text = "x" };
    return n[[0..1]] == "named";
}

fn generic_index(g: &Gen<int>) -> int {
    return g[[0..1]];
}

fn self_reference(s: &SelfRef) -> &int {
    return s[[0..1]];
}

fn local_reference(l: &LocalRef) -> &int {
    return l[[0..1]];
}

fn opaque_reference(o: &OpaqueRef) -> &int {
    return o[[0..1]];
}

fn mut_receiver(b: &mut MutBox) -> int {
    return b[[1..2]];
}

fn loan_result(b: ArrBox) -> uint {
    let _ = b[[0..1]];
    return 0:uint;
}

fn converted(b: Box) -> Option<int> {
    return b[[1..2]];
}

fn tag_index(t: &Tag<int>) -> int {
    return t[[0..1]];
}

fn add1(x: int) -> int {
    return x + 1;
}

fn fn_index(b: FnBox) -> int {
    return b[add1];
}

fn cursor_index(b: Box, xs: &int[]) -> int {
    let r = xs.__range();
    return b[r];
}

fn cursor_call(xs: &int[]) -> int {
    let r = xs.__range();
    return take(r);
}

fn take(r: Range<int>) -> int {
    let _ = r;
    return 0;
}
`

const (
	indexCallUnanswered = "index requires a non-scalar index transfer"
	indexCallDisagrees  = "selected index call disagrees with its original typed operation"
)

func TestAnalyzeSelectedIndexCall(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "selected_index_call", indexCallSource, true, nil)
	key, file := f.unit.SourceKey, f.owner.File.ID
	for _, row := range []struct{ name, header string }{
		{"opaque_owned_result_finishes", "fn opaque_owned("},
		{"body_owned_result_finishes", "fn body_owned("},
		{"implicitly_borrowed_local_receiver_finishes", "fn borrowed_local_receiver("},
	} {
		t.Run(row.name, func(t *testing.T) {
			fn := patternFn(t, indexCallSource, row.header)
			originExactPending(t, analysis, key, fn, nil)
			originNoEscape(t, analysis, file, fn)
			name := strings.TrimSuffix(strings.TrimPrefix(row.header, "fn "), "(")
			requireOriginSummary(t, analysis, file, name, false, nil)
		})
	}
	for _, row := range []struct{ name, header, index, reason string }{
		{"generic_index_stays_refused", "fn generic_index(", "g[[0..1]]", indexCallUnanswered},
		{"reference_into_self_stays_refused", "fn self_reference(", "s[[0..1]]", indexCallUnanswered},
		{"reference_into_local_stays_refused", "fn local_reference(", "l[[0..1]]", indexCallUnanswered},
		{"opaque_reference_result_stays_refused", "fn opaque_reference(", "o[[0..1]]", indexCallUnanswered},
		{"mutable_receiver_stays_refused", "fn mut_receiver(", "b[[1..2]]", indexCallUnanswered},
		{"loan_carrier_result_stays_refused", "fn loan_result(", "b[[0..1]]", indexCallUnanswered},
		{"converted_index_result_stays_refused", "fn converted(", "b[[1..2]]", "index requires its implicit conversion effect transfer"},
		{"generic_receiver_refused_by_template_check", "fn tag_index(", "t[[0..1]]", indexCallUnanswered},
		{"effect_rule_refuses_function_index", "fn fn_index(", "b[add1]", indexCallUnanswered},
		{"loan_carrying_cursor_stays_refused", "fn cursor_index(", "b[r]", backingLoanDiscard},
		{"loan_carrying_cursor_call_control", "fn cursor_call(", "take(r)", backingLoanDiscard},
	} {
		t.Run(row.name, func(t *testing.T) {
			fn := patternFn(t, indexCallSource, row.header)
			at := patternIn(t, indexCallSource, fn, row.index)
			if !originPendingAt(analysis, key, at, row.reason) {
				t.Errorf("%q lost its refusal %q: %+v", row.index, row.reason, originPendingWithin(analysis, key, fn.start, fn.end))
			}
			if originPendingAt(analysis, key, at, indexCallDisagrees) {
				t.Errorf("%q reads as a disagreeing selection: %+v", row.index, originPendingWithin(analysis, key, fn.start, fn.end))
			}
		})
	}
	t.Run("reference_into_local_body_is_diagnosed", func(t *testing.T) {
		block := patternFn(t, indexCallSource, "extern<LocalRef> {")
		at := patternIn(t, indexCallSource, block, "return &local;")
		found := slices.ContainsFunc(analysis.Diagnostics, func(d diag.Diagnostic) bool {
			return d.Code == diag.SemaBorrowEscapesReturn && d.Primary.File == file && int(d.Primary.Start) == at.start && int(d.Primary.End) == at.end
		})
		if !found {
			t.Errorf("the escaping __index body lost its SEM3139: %+v", analysis.Diagnostics)
		}
	})
}

// A detached mutation replaces the selection on one index node. The analysis
// must compare the selection with the typed operation, not trust it.
func TestAnalyzeSelectedIndexCallAuthority(t *testing.T) {
	for _, row := range []struct {
		name  string
		match func(f originalGenericFixture, c sema.CallableCandidate) bool
	}{
		{"sibling_receiver_declaration", func(f originalGenericFixture, c sema.CallableCandidate) bool {
			block := patternFn(t, indexCallSource, "extern<Other> {")
			return c.Name == "__index" && c.Source.File == f.owner.File.ID && int(c.Source.Start) >= block.start && int(c.Source.End) <= block.end
		}},
		{"core_scalar_string_index", func(f originalGenericFixture, c sema.CallableCandidate) bool {
			in := f.authority.TypeInterner
			return c.Name == "__index" && c.Builtin && c.Intrinsic && c.SourceKey == "builtin" && c.ModulePath == "core/intrinsics" &&
				c.ReceiverType == in.Builtins().String && len(c.ParamTypes) == 2 && c.ParamTypes[1] == in.Builtins().Int &&
				c.ResultType == in.Builtins().Uint32 && len(c.TemplateParams) == 0
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			fn := patternFn(t, indexCallSource, "fn opaque_owned(")
			at := patternIn(t, indexCallSource, fn, "b[[1..2]]")
			var before, effective map[ast.ExprID]symbols.SymbolID
			f, analysis := analyzeOriginRoot(t, "selected_index_call_"+row.name, indexCallSource, true, func(f originalGenericFixture) {
				replacement := indexCallReplacement(t, f, func(c sema.CallableCandidate) bool { return row.match(f, c) })
				id := originExprAt(t, f.unit, f.owner.File.ID, at, ast.ExprIndex)
				for i := range f.inputs.units {
					unit := &f.inputs.units[i]
					if unit.Builder.Files.Get(unit.FileID).Span.File != f.owner.File.ID {
						continue
					}
					original := unit.Sema
					before = maps.Clone(original.IndexSymbols)
					if !before[id].IsValid() || before[id] == replacement {
						t.Fatal("PRECONDITION: the original selection is not distinct from the replacement")
					}
					detached := *original
					detached.IndexSymbols = maps.Clone(original.IndexSymbols)
					detached.IndexSymbols[id] = replacement
					unit.Sema = &detached
					effective = detached.IndexSymbols
				}
			})
			changed := 0
			for expr, sym := range before {
				if effective[expr] != sym {
					changed++
				}
			}
			if len(before) == 0 || len(before) != len(effective) || changed != 1 {
				t.Fatal("PRECONDITION: the detached mutation changed more than its one selected operation")
			}
			if !originPendingAt(analysis, f.unit.SourceKey, at, indexCallDisagrees) {
				t.Errorf("a swapped selection was answered: %+v", originPendingWithin(analysis, f.unit.SourceKey, fn.start, fn.end))
			}
			if slices.ContainsFunc(analysis.Diagnostics, func(d diag.Diagnostic) bool {
				return d.Primary.File == f.owner.File.ID && int(d.Primary.Start) >= fn.start && int(d.Primary.End) <= fn.end
			}) {
				t.Errorf("detached metadata corruption became a source diagnostic: %+v", analysis.Diagnostics)
			}
		})
	}
}

// indexCallReplacement is the one candidate a row names, as the root unit
// publishes it.
func indexCallReplacement(t *testing.T, f originalGenericFixture, match func(sema.CallableCandidate) bool) symbols.SymbolID {
	t.Helper()
	var out symbols.SymbolID
	matches := 0
	for _, c := range f.authority.CallableCandidates {
		if !match(c) {
			continue
		}
		matches++
		aliases := f.unit.Publication.LocalSymbols(c.Symbol)
		if len(f.unit.Publication.RootToLocalSymbols) == 0 {
			aliases = []symbols.SymbolID{c.Symbol}
		}
		if len(aliases) != 1 || !aliases[0].IsValid() {
			t.Fatal("PRECONDITION: the replacement has missing or ambiguous published local symbols")
		}
		out = aliases[0]
	}
	if matches != 1 {
		t.Fatalf("PRECONDITION: the replacement matches %d candidates", matches)
	}
	if sym := f.unit.Symbols.Table.Symbols.Get(out); sym == nil || sym.Kind != symbols.SymbolFunction ||
		sym.Type == types.NoTypeID {
		t.Fatal("PRECONDITION: the replacement is not a typed function symbol")
	}
	return out
}
