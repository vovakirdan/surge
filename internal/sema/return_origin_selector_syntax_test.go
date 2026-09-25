package sema

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// An `is`/`heir` right operand and an Enum::Variant target are syntax the checker
// resolves into a record and never types. Each row asks the analysis to read the
// record instead of the operand, and to keep refusing when the record is gone.
const selectorSyntaxKey = "selector-syntax.sg"

const selectorIsHeirSource = `pragma no_std;
tag Some<T>(T); type Option<T> = Some(T) | nothing;
type Base = { x: int };
type Child = Base : { y: int };
fn probe(o: Option<int>, c: Child, a: &string, b: &string) -> &string {
    if o is Some && c heir Base && !(o is nothing) {
        return a;
    }
    return b;
}
`

const selectorLoanOperandSource = `pragma no_std;
fn probe(r: &string, a: &string) -> &string {
    if r is &string {
        return a;
    }
    return a;
}
`

const selectorEnumSource = `pragma no_std;
enum Color: int = { Red = 1, Green = 2 }
enum Token: string = { L = "{" }
fn probe(c: int, a: &string, b: &string) -> &string {
    let red: int = Color::Red;
    let t: string = Token::L;
    let _ = red;
    let _ = t;
    return compare c { Color::Green => a; _ => b; };
}
`

// selectorEnumValueSource names variants only as values: no compare pattern, so the analysis can finish.
const selectorEnumValueSource = `pragma no_std;
enum Color: int = { Red = 1, Green = 2 }
enum Token: string = { L = "{" }
fn probe(a: &string) -> &string {
    let red: int = Color::Red;
    let t: string = Token::L;
    let _ = red;
    let _ = t;
    return a;
}
`

// selectorHeirFirstSource makes `heir` the first type test, so its right operand is the first untyped node.
const selectorHeirFirstSource = `pragma no_std;
type Base = { x: int };
type Child = Base : { y: int };
fn probe(c: Child, a: &string, b: &string) -> &string {
    if c heir Base {
        return a;
    }
    return b;
}
`

type selectorSyntaxUnit struct {
	builder  *ast.Builder
	checked  *Result
	unit     ReturnOriginUnit
	probeKey string
}

// selectorSyntaxFixture is the single-file sema harness of return_origin_compare_test.go:
// parse, resolve, check, canonical source keys, a publication made from the candidates.
func selectorSyntaxFixture(t *testing.T, text, digest string) selectorSyntaxUnit {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	b, file, parseBag := parseSnippet(t, text)
	if parseBag.HasErrors() {
		t.Fatalf("PRECONDITION: source parse failed: %s", diagnosticsSummary(parseBag))
	}
	resolveBag := diag.NewBag(64)
	resolved := symbols.ResolveFile(b, file, &symbols.ResolveOptions{Reporter: &diag.BagReporter{Bag: resolveBag}})
	if resolveBag.HasErrors() {
		t.Fatalf("PRECONDITION: symbol resolution failed: %s", diagnosticsSummary(resolveBag))
	}
	bag := diag.NewBag(64)
	checked := Check(t.Context(), b, file, Options{Symbols: &resolved, Reporter: &diag.BagReporter{Bag: bag}})
	if bag.HasErrors() {
		t.Fatalf("PRECONDITION: the checker refuses the source: %s", diagnosticsSummary(bag))
	}
	if err := CanonicalizeInstantiationGraphSources(&checked, func(source.FileID) (string, error) { return selectorSyntaxKey, nil }); err != nil {
		t.Fatal(err)
	}
	publication := FinalizationPublication{SourceKey: selectorSyntaxKey}
	for _, candidate := range checked.CallableCandidates {
		publication.LocalCallables = append(publication.LocalCallables, FinalizationCallableIdentity{Symbol: candidate.Symbol, BodyKey: candidate.BodyKey, SourceKey: candidate.SourceKey})
	}
	unit := ReturnOriginUnit{Builder: b, FileID: file, Sema: &checked, Symbols: &resolved, SourceKey: selectorSyntaxKey, Publication: publication}
	index, err := indexReturnOriginUnit(unit, &checked)
	if err != nil {
		t.Fatalf("PRECONDITION: original unit index: %v", err)
	}
	fn := index.functions[lookupSymbolByName(&resolved, b.StringsInterner.Intern("probe"))]
	if fn == nil || !fn.item.Body.IsValid() {
		t.Fatal("PRECONDITION: probe has no typed body")
	}
	return selectorSyntaxUnit{builder: b, checked: &checked, unit: unit, probeKey: fn.key}
}

// selectorExprAt finds the one expression of a kind whose span is exactly text[start:end].
func selectorExprAt(t *testing.T, f selectorSyntaxUnit, text string, kind ast.ExprKind, start, end int, snippet string) ast.ExprID {
	t.Helper()
	if end > len(text) || text[start:end] != snippet {
		t.Fatalf("PRECONDITION: frozen span %d:%d is not %q", start, end, snippet)
	}
	var found ast.ExprID
	for raw := uint32(1); raw <= f.builder.Exprs.Arena.Len(); raw++ {
		node := f.builder.Exprs.Get(ast.ExprID(raw))
		if node != nil && node.Kind == kind && int(node.Span.Start) == start && int(node.Span.End) == end {
			if found.IsValid() {
				t.Fatalf("PRECONDITION: two %v expressions at %d:%d", kind, start, end)
			}
			found = ast.ExprID(raw)
		}
	}
	if !found.IsValid() {
		t.Fatalf("PRECONDITION: no %v expression at %d:%d %q", kind, start, end, snippet)
	}
	return found
}

// selectorClean requires a finished analysis whose probe returns exactly the given slots.
func selectorClean(t *testing.T, f selectorSyntaxUnit, slots []uint32) {
	t.Helper()
	analysis, err := AnalyzeReturnOrigins(t.Context(), f.checked, []ReturnOriginUnit{f.unit})
	if err != nil || analysis == nil {
		t.Fatalf("origin analysis aborted: %v", err)
	}
	if !analysis.Complete() || len(analysis.Pending) != 0 || len(analysis.Diagnostics) != 0 {
		t.Errorf("analysis is not clean: pending=%+v diagnostics=%+v", analysis.Pending, analysis.Diagnostics)
	}
	for i := range analysis.Summaries {
		if summary := &analysis.Summaries[i]; summary.BodyKey == f.probeKey {
			if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, slots) {
				t.Errorf("probe summary = %+v, want exactly slots %v", *summary, slots)
			}
			return
		}
	}
	t.Error("probe summary is absent")
}

// selectorAbort requires the exact untyped-expression abort naming one expression.
func selectorAbort(t *testing.T, f selectorSyntaxUnit, id ast.ExprID) {
	t.Helper()
	want := fmt.Sprintf("return origins: expression %d is not typed in %s", id, selectorSyntaxKey)
	analysis, err := AnalyzeReturnOrigins(t.Context(), f.checked, []ReturnOriginUnit{f.unit})
	if err == nil || err.Error() != want || analysis != nil {
		t.Errorf("want exact refusal %q and nil analysis; got %v, %+v", want, err, analysis)
	}
}

const selectorIsHeirDigest = "e80b4b8642d868f78c4313cee44c6e95e4f7ccb8d20d27d16eedb500c687842c"

// 6 RUN: 1 parent, 5 leaves.
func TestReturnOriginTypeTestOperands(t *testing.T) {
	t.Run("is_heir_operands", func(t *testing.T) {
		f := selectorSyntaxFixture(t, selectorIsHeirSource, selectorIsHeirDigest)
		for _, row := range []struct {
			start, end int
			snippet    string
			heir       bool
		}{{202, 211, "o is Some", false}, {215, 226, "c heir Base", true}, {232, 244, "o is nothing", false}} {
			id := selectorExprAt(t, f, selectorIsHeirSource, ast.ExprBinary, row.start, row.end, row.snippet)
			data, _ := f.builder.Exprs.Binary(id)
			_, isRecorded := f.checked.IsOperands[id]
			_, heirRecorded := f.checked.HeirOperands[id]
			if _, typed := f.checked.ExprTypes[data.Right]; typed || isRecorded == row.heir || heirRecorded != row.heir {
				t.Fatalf("PRECONDITION: %q lost its operand record or its untyped right side", row.snippet)
			}
		}
		selectorClean(t, f, []uint32{2, 3})
	})
	t.Run("loan_bearing_operand_keeps_g6", func(t *testing.T) {
		f := selectorSyntaxFixture(t, selectorLoanOperandSource, "0fe395fdba29066ce7a2df8f68f348f0c0b82862d3a979384826700976c4af3f")
		id := selectorExprAt(t, f, selectorLoanOperandSource, ast.ExprBinary, 68, 80, "r is &string")
		if _, recorded := f.checked.IsOperands[id]; !recorded {
			t.Fatal("PRECONDITION: the type test lost its operand record")
		}
		analysis, err := AnalyzeReturnOrigins(t.Context(), f.checked, []ReturnOriginUnit{f.unit})
		if err != nil || analysis == nil {
			t.Fatalf("origin analysis aborted: %v", err)
		}
		span := f.builder.Exprs.Get(id).Span
		if analysis.Complete() || len(analysis.Pending) != 1 || analysis.Pending[0].Span != span ||
			analysis.Pending[0].Reason != "storage loan would be discarded by a payload-free value" {
			t.Errorf("want exactly the loan-discard row at 68:80; got %+v", analysis.Pending)
		}
	})
	t.Run("missing_is_record_stays_refused", func(t *testing.T) {
		f := selectorSyntaxFixture(t, selectorIsHeirSource, selectorIsHeirDigest)
		id := selectorExprAt(t, f, selectorIsHeirSource, ast.ExprBinary, 202, 211, "o is Some")
		data, _ := f.builder.Exprs.Binary(id)
		delete(f.checked.IsOperands, id)
		selectorAbort(t, f, data.Right)
	})
	t.Run("missing_heir_record_stays_refused", func(t *testing.T) {
		f := selectorSyntaxFixture(t, selectorHeirFirstSource, "82c8b21d1e69e4220c7023517e94cc2204edcbc7cd9fc785ed8c87b4f1eaf5ab")
		id := selectorExprAt(t, f, selectorHeirFirstSource, ast.ExprBinary, 134, 145, "c heir Base")
		data, _ := f.builder.Exprs.Binary(id)
		if selectorHeirFirstSource[141:145] != "Base" || int(f.builder.Exprs.Get(data.Right).Span.Start) != 141 {
			t.Fatal("PRECONDITION: the heir right operand is not Base at 141:145")
		}
		if _, recorded := f.checked.HeirOperands[id]; !recorded {
			t.Fatal("PRECONDITION: the heir test lost its operand record")
		}
		delete(f.checked.HeirOperands, id)
		selectorAbort(t, f, data.Right)
	})
	// The compare fixture guard_is_type_operand (once unsupported_guard_is) is admitted with its
	// record; the same guard without the record still stops the analysis at its right operand.
	t.Run("guard_is_without_record_stays_refused", func(t *testing.T) {
		f := selectorSyntaxFixture(t, selectorGuardIsSource, "ada5b603c73d545690377000bdf51739af4bb939958776944431c01429e59044")
		id := selectorExprAt(t, f, selectorGuardIsSource, ast.ExprBinary, 105, 117, "value is int")
		data, _ := f.builder.Exprs.Binary(id)
		if _, recorded := f.checked.IsOperands[id]; !recorded {
			t.Fatal("PRECONDITION: the guard lost its operand record")
		}
		delete(f.checked.IsOperands, id)
		selectorAbort(t, f, data.Right)
	})
}

const selectorGuardIsSource = `pragma no_std;
fn probe(value: int, a: &string, b: &string) -> &string {
    return compare value { _ if value is int => a; _ => b; };
}
`

const selectorEnumDigest = "98f4df68cb8f9d478ece6f432fd7bb5c326bc3d8158e680acad3d6e91681e781"

// selectorVariant finds one Enum::Variant member and checks what the checker kept for it.
func selectorVariant(t *testing.T, f selectorSyntaxUnit, text string, start, end int, snippet string, want types.TypeID) (ast.ExprID, *ast.ExprMemberData) {
	t.Helper()
	id := selectorExprAt(t, f, text, ast.ExprMember, start, end, snippet)
	data, _ := f.builder.Exprs.Member(id)
	if _, typed := f.checked.ExprTypes[data.Target]; typed || f.checked.ExprTypes[id] != want {
		t.Fatalf("PRECONDITION: %q is typed %d with a typed target=%t, want %d over an untyped target", snippet, f.checked.ExprTypes[id], typed, want)
	}
	return id, data
}

// 3 RUN: 1 parent, 2 leaves. The record rows are in return_origin_enum_variant_record_test.go.
func TestReturnOriginEnumVariantTargets(t *testing.T) {
	t.Run("enum_value", func(t *testing.T) {
		f := selectorSyntaxFixture(t, selectorEnumValueSource, "e1598514533aa84d6e16c7564b4782772fe9b10ebcf4eae91c3a704536e6f3f2")
		builtins := f.checked.TypeInterner.Builtins()
		selectorVariant(t, f, selectorEnumValueSource, 142, 152, "Color::Red", builtins.Int)
		selectorVariant(t, f, selectorEnumValueSource, 174, 182, "Token::L", builtins.String)
		selectorClean(t, f, []uint32{0})
	})
	// An enum variant in a pattern is a constant compared at run time (N-PATTERN,
	// return_origin_compare_patterns.go runtimeTestPattern): it binds nothing and can miss, so
	// the `_` arm stays reachable and the result is the join of both arms' parameters. It used
	// to keep the compare-pattern row; the abort at the pattern's untyped target stays removed.
	t.Run("enum_pattern", func(t *testing.T) {
		f := selectorSyntaxFixture(t, selectorEnumSource, selectorEnumDigest)
		selectorVariant(t, f, selectorEnumSource, 259, 271, "Color::Green", f.checked.TypeInterner.Builtins().Int)
		analysis, err := AnalyzeReturnOrigins(t.Context(), f.checked, []ReturnOriginUnit{f.unit})
		if err != nil || analysis == nil {
			t.Fatalf("origin analysis aborted: %v", err)
		}
		if !analysis.Complete() || len(analysis.Pending) != 0 || len(analysis.Diagnostics) != 0 {
			t.Errorf("want a complete analysis with no row and no diagnostic; got %+v %+v", analysis.Pending, analysis.Diagnostics)
		}
		found := 0
		for _, summary := range analysis.Summaries {
			if summary.Name != "probe" {
				continue
			}
			found++
			if summary.NoNormalReturn || summary.Unknown || !slices.Equal(summary.ParamSlots, []uint32{1, 2}) {
				t.Errorf("probe must return one of its reference parameters a or b: %+v", summary)
			}
		}
		if found != 1 {
			t.Fatalf("PRECONDITION: %d summaries for probe", found)
		}
	})
}
