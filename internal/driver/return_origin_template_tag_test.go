package driver

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// analyzeOriginRoot analyzes one root program with full core, the harness that
// finalizes concrete use sites. The prepare hook runs before the analysis.
func analyzeOriginRoot(t *testing.T, stage, text string, allowEscape bool, prepare func(originalGenericFixture)) (originalGenericFixture, *sema.ReturnOriginAnalysis) {
	t.Helper()
	res := returnOriginStdlibFixture(t, text, allowEscape)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: source closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	wantUnits := 11
	if strings.Contains(text, "stdlib/time") {
		wantUnits++
	}
	if err != nil || len(inputs.units) != wantUnits {
		t.Fatalf("PRECONDITION: full input missing: units=%d want=%d error=%v", len(inputs.units), wantUnits, err)
	}
	checkReturnOriginStdlibBags(t, res, allowEscape)
	f := originalGenericFixture{owner: res, authority: res.Sema, inputs: inputs}
	owners := 0
	for _, unit := range inputs.units {
		if unit.Builder.Files.Get(unit.FileID).Span.File == res.File.ID {
			f.unit, owners = unit, owners+1
		}
	}
	if owners != 1 {
		t.Fatalf("PRECONDITION: the test source has %d owning units", owners)
	}
	if prepare != nil {
		prepare(f)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": stage, "source_key": f.unit.SourceKey,
		"pending": originPendingWithin(analysis, f.unit.SourceKey, 0, len(text)), "diagnostics": analysis.Diagnostics})
	return f, analysis
}

// requireOriginEscape finds the exact SEM3139 for a local owner whose borrow
// leaves its scope, noted at the owner's declaration.
func requireOriginEscape(t *testing.T, analysis *sema.ReturnOriginAnalysis, resolved *symbols.Result, file source.FileID, primary originSpan, owner string) {
	t.Helper()
	var ownerSpan source.Span
	for _, symbol := range resolved.Table.Symbols.Data() {
		if name, _ := resolved.Table.Strings.Lookup(symbol.Name); name == owner && symbol.Kind == symbols.SymbolLet && symbol.Span.File == file {
			if !ownerSpan.Empty() {
				t.Fatalf("PRECONDITION: local owner %q is not unique", owner)
			}
			ownerSpan = symbol.Span
		}
	}
	if ownerSpan.Empty() {
		t.Fatalf("PRECONDITION: local owner %q is missing", owner)
	}
	want := source.Span{File: file, Start: uint32(primary.start), End: uint32(primary.end)}
	for _, d := range analysis.Diagnostics {
		if d.Code == diag.SemaBorrowEscapesReturn && d.Severity == diag.SevError && d.Primary == want &&
			d.Message == fmt.Sprintf("borrow of '%s' outlives its owner when this scope exits", owner) &&
			slices.ContainsFunc(d.Notes, func(note diag.Note) bool {
				return note.Span == ownerSpan && note.Msg == fmt.Sprintf("'%s' owns storage that ends in this scope", owner)
			}) {
			return
		}
	}
	t.Errorf("missing SEM3139 at %q for owner %q: %+v", primary.snippet, owner, analysis.Diagnostics)
}

// A tag built in its own template keeps every payload when the template's view
// says a payload slot may carry references, so the result is the template's
// input. A tuple literal and a callable payload keep their refusal.
const templateTagSource = `pragma module::dep;
fn wrap<T>(value: T) -> Option<T> {
    return Some(value);
}
fn failure<T, E: ErrorLike>(value: Erring<T, E>) -> Option<E> {
    return compare value {
        Success(_) => nothing;
        err => Some(err);
    };
}
fn pair<T>(value: T) -> (T, int) {
    return (value, 1);
}
fn hold<T>(f: fn(T) -> int) -> Option<fn(T) -> int> {
    return Some(f);
}
`

const templateTagDigest = "d36913894c4596e0dc268dd838db0c40806c35bd70ff0dee1afddc98306a2961"

func TestAnalyzeTemplateTagConstructorOrigins(t *testing.T) {
	checkOriginSource(t, templateTagSource, templateTagDigest)
	f, analysis := analyzeOriginDependency(t, "template_tag", templateTagSource, nil)
	checkOriginBodyLeaves(t, analysis, f, templateTagSource, templateTagDigest, []originBodyLeaf{
		{name: "wrap", body: "wrap", function: originSpan{20, 81, "fn wrap<T>(value: T) -> Option<T> {\n    return Some(value);\n}"}, clean: true, slots: []uint32{0}},
		{name: "failure", body: "failure", function: originSpan{82, 238, "fn failure<T, E: ErrorLike>(value: Erring<T, E>) -> Option<E> {\n    return compare value {\n        Success(_) => nothing;\n        err => Some(err);\n    };\n}"}, clean: true, slots: []uint32{0}},
		{name: "tuple_control", body: "pair", function: originSpan{239, 298, "fn pair<T>(value: T) -> (T, int) {\n    return (value, 1);\n}"}, stays: []originRefusal{{originSpan{285, 295, "(value, 1)"}, originConstructRefusal}}},
		{name: "callable_payload_control", body: "hold", function: originSpan{299, 375, "fn hold<T>(f: fn(T) -> int) -> Option<fn(T) -> int> {\n    return Some(f);\n}\n"}, stays: []originRefusal{{originSpan{364, 371, "Some(f)"}, originConstructRefusal}}},
	})
}

// A concrete program that instantiates such a template still refuses the tag
// use inside the instance: the per-instance payload transfer is not proven.
// Only a root program with full core finalizes that instance's use sites.
const instantiatedTagSource = `fn wrap<T>(value: T) -> Option<T> {
    return Some(value);
}
fn use_wrap(n: int64) -> Option<int64> {
    return wrap::<int64>(n);
}
`

func TestAnalyzeInstantiatedTagCallerStaysRefused(t *testing.T) {
	site := originSpan{47, 58, "Some(value)"}
	checkOriginSource(t, instantiatedTagSource, "a4b1e9bc6057100ef482e61014327cf98bb20af795e3e8781f4741260087b981", site)
	f, analysis := analyzeOriginRoot(t, "instantiated_tag_caller", instantiatedTagSource, false, func(f originalGenericFixture) {
		uses := 0
		for _, use := range f.authority.InstantiationClosure.UseSites {
			if use.Kind == sema.InstantiationTag && use.SourceKey == f.unit.SourceKey && use.Caller != (sema.InstanceKey{}) &&
				int(use.Site.Start) == site.start && int(use.Site.End) == site.end {
				uses++
			}
		}
		if uses != 1 {
			t.Fatalf("PRECONDITION: %d tag uses from an instantiated caller at %q, want 1", uses, site.snippet)
		}
	})
	if !originPendingAt(analysis, f.unit.SourceKey, site, originTagCallerRefusal) {
		t.Errorf("instantiated tag use lost its refusal at %q: %+v", site.snippet, originPendingWithin(analysis, f.unit.SourceKey, 0, len(instantiatedTagSource)))
	}
}

// A local borrow built into a template tag reaches the result, so its owner's
// escape is reported instead of an unproved constructor.
const templateTagEscapeSource = `pragma no_std;
tag Duo<A, B>(A, B);
type Both<A, B> = Duo(A, B) | nothing;
fn leak<T>(value: T) -> Both<T, &string> {
    let owned: string = "local";
    return Duo::<T, &string>(value, &owned);
}
`

func TestAnalyzeTemplateTagConstructorEscape(t *testing.T) {
	escape := originSpan{155, 195, "return Duo::<T, &string>(value, &owned);"}
	result := originSpan{96, 115, "-> Both<T, &string>"}
	checkOriginSource(t, templateTagEscapeSource, "32b4d82482e22533c22be56f4e808f56f98e1a6b3a1c47e73d205a4b5f3cc164", escape, result)
	f := originalGenericSignatureFixture(t, templateTagEscapeSource, true, false)
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	local := originPendingWithin(analysis, f.unit.SourceKey, 0, len(templateTagEscapeSource))
	logReturnOriginCallEvidence(t, map[string]any{"stage": "template_tag_escape", "pending": local, "diagnostics": analysis.Diagnostics})
	requireOriginEscape(t, analysis, f.owner.Symbols, f.owner.File.ID, escape, "owned")
	if len(local) != 0 {
		t.Errorf("escaped template tag pending = %+v, want none: the SEM3139 refusal completes the result at %q", local, result.snippet)
	}
	requireOriginSummary(t, analysis, f.owner.File.ID, "leak", true, []uint32{0})
}
