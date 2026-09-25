package driver

import (
	"strings"
	"testing"
)

// Compare patterns (return_origin_compare_patterns.go): a constant pattern (a literal, a negated literal, an enum
// variant) is compared at run time, binds nothing and can miss; a tag pattern whose payload patterns can miss, and a
// tuple pattern with a refutable element, are partial and keep their alternative on the unmatched continuation; a
// binding inside a pattern keeps every root of the subject unless its type can hold nothing. The unmatched
// continuation itself is unchanged: it keeps "compare has an unproved unmatched continuation" (owner question 4).
// Every source is a ROOT program against the real core, and each row reads one body.

const originCompareContinuationRefusal = "compare has an unproved unmatched continuation"

// The shapes of hir/compare.sg, mir/erring_option_nested_tag.sg, vm_compare/compare_enum_variants.sg,
// vm_compare/compare_patterns.sg and spec_audit/s03_compare.sg, and the forms around them.
const originComparePatternFinishSource = `enum Color = {
    Red,
    Green,
    Blue
}

fn classify(x: int) -> int {
    return compare x {
        0 => 0;
        n if n > 0 => 1;
        _ => -1;
    };
}

fn nested_tag(v: Erring<Option<string>, Error>) -> int {
    return compare v {
        Success(Some(s)) => 1;
        Success(nothing) => 2;
        err => 3;
    };
}

fn enum_label(c: int) -> string {
    return compare c {
        Color::Red => "red";
        Color::Green => "green";
        _ => "other";
    };
}

fn literal_kinds(n: int, u: uint, f: float) -> int {
    let a = compare n {
        -1 => 1;
        12345678901234567890 => 2;
        _ => 0;
    };
    let b = compare u {
        3 => 1;
        _ => 0;
    };
    let c = compare f {
        1.5 => 1;
        _ => 0;
    };
    return a + b + c;
}

fn tuple_patterns(t: (int, bool)) -> int {
    return compare t {
        (0, true) => 1;
        (x, false) => x;
        (x, y) => 0;
    };
}

fn nested_compare(o: int?) -> int {
    return compare o {
        Some(n) => compare n {
            0 => 10;
            _ if n > 1 => 20;
            _ => 30;
        };
        nothing => 0;
    };
}

fn bool_arms(b: bool) -> int {
    return compare b {
        true => 1;
        false => 0;
    };
}

fn literal_keeps_both(n: int, a: &int, b: &int) -> &int {
    return compare n {
        0 => a;
        _ => b;
    };
}

fn enum_keeps_both(n: int, a: &int, b: &int) -> &int {
    return compare n {
        Color::Blue => a;
        _ => b;
    };
}

fn nested_payload_keeps_origin(a: &int) -> &int {
    let v: Erring<Option<&int>, Error> = Success(Some::<&int>(a));
    return compare v {
        Success(Some(p)) => p;
        Success(nothing) => a;
        err => a;
    };
}

fn tuple_binding_keeps_origin(t: (Option<&int>, int)) -> Option<&int> {
    return compare t {
        (o, 0) => o;
        (o, n) => o;
    };
}

fn later_arm_after_partial_tag(v: int?, a: &int, b: &int) -> &int {
    return compare v {
        Some(1) => a;
        nothing => a;
        _ => b;
    };
}

fn later_arm_after_partial_nested(v: Option<Option<int>>, a: &int, b: &int) -> &int {
    return compare v {
        Some(nothing) => a;
        nothing => a;
        _ => b;
    };
}
`

// The unmatched continuation keeps its row (owner question 4), whether a guard or a partial pattern leaves it.
const originComparePatternRefusedSource = `fn guard_falls_through(v: int?) -> int {
    return compare v {
        Some(x) if x > 5 => x;
        nothing => 0;
    };
}

fn partial_tag_only(v: int?) -> int {
    return compare v {
        Some(1) => 1;
        nothing => 0;
    };
}

fn partial_nested_only(v: Option<Option<int>>) -> int {
    return compare v {
        Some(Some(n)) => n;
        nothing => 0;
    };
}

fn string_literal_pattern(s: string) -> int {
    return compare s {
        "a" => 1;
        _ => 0;
    };
}
`

// Soundness canaries: a reference to a dying local leaves its frame through an arm.
const originComparePatternEscapeSource = `fn literal_arm_local(n: int, a: &int) -> &int {
    let local: int = 7;
    return compare n {
        0 => a;
        1 => &local;
        _ => a;
    };
}

fn nested_payload_local(a: &int) -> &int {
    let x: int = 1;
    let v: Erring<Option<&int>, Error> = Success(Some::<&int>(&x));
    return compare v {
        Success(Some(p)) => p;
        Success(nothing) => a;
        err => a;
    };
}

fn later_arm_local(v: int?, a: &int) -> &int {
    let local: int = 7;
    return compare v {
        Some(1) => a;
        nothing => a;
        _ => &local;
    };
}

fn later_nested_arm_local(v: Option<Option<int>>, a: &int) -> &int {
    let local: int = 7;
    return compare v {
        Some(nothing) => a;
        nothing => a;
        _ => &local;
    };
}

fn tuple_binding_local() -> Option<&int> {
    let x: int = 1;
    let t = (Some::<&int>(&x), 1);
    return compare t {
        (o, 0) => o;
        (o, n) => o;
    };
}
`

const (
	originComparePatternFinishSourceDigest  = "dc620f55a4d3cee05db8ea32b10206a0b39383e7ba5fc7f393eea72e316d7a86"
	originComparePatternRefusedSourceDigest = "fb6235b360416396b49719f752dd35bba5ba61193e18e90cf45d067a072c66b0"
	originComparePatternEscapeSourceDigest  = "5259e0cfbe1bdd8693c55b5da2fbb2545d0c86d4228fcadeac3d46a4ac5a8d2a"
)

// originPatternDerived are the rows that only carry a refused source on to a result or an outgoing reference.
var originPatternDerived = []string{"function result contains an unproved source", "outgoing reference has unresolved or captured provenance"}

// patternFn is the frozen span of the function whose text starts with header and runs to its closing brace.
func patternFn(t *testing.T, text, header string) originSpan {
	t.Helper()
	start := strings.Index(text, header)
	if start < 0 || strings.Count(text, header) != 1 {
		t.Fatalf("PRECONDITION: %q is not one function header", header)
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("PRECONDITION: %q has no closing brace", header)
	}
	end += start + len("\n}")
	return originSpan{start, end, text[start:end]}
}

// patternIn is the frozen span of the one occurrence of snippet inside fn.
func patternIn(t *testing.T, text string, fn originSpan, snippet string) originSpan {
	t.Helper()
	body := text[fn.start:fn.end]
	at := strings.Index(body, snippet)
	if at < 0 || strings.Count(body, snippet) != 1 {
		t.Fatalf("PRECONDITION: %q is not one occurrence inside %q", snippet, fn.snippet)
	}
	return originSpan{fn.start + at, fn.start + at + len(snippet), snippet}
}

// originPatternRow is one t.Run leaf: the Pending rows inside the function must be exactly want (plus allow), no
// diagnostic may point inside it, and a row with slots set must publish a normal summary with those sources.
type originPatternRow struct {
	name, header, body string
	want               []struct{ snippet, reason string }
	allow              []string
	slots              []uint32
}

func runOriginPatternRows(t *testing.T, stage, text, digest string, rows []originPatternRow) {
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			fn := patternFn(t, text, row.header)
			var want []originRefusal
			spans := []originSpan{fn}
			for _, refusal := range row.want {
				span := patternIn(t, text, fn, refusal.snippet)
				want = append(want, originRefusal{span: span, reason: refusal.reason})
				spans = append(spans, span)
			}
			checkOriginSource(t, text, digest, spans...)
			f, analysis := analyzeOriginRoot(t, stage+"_"+row.name, text, false, nil)
			originExactPending(t, analysis, f.unit.SourceKey, fn, want, row.allow...)
			originNoEscape(t, analysis, f.owner.File.ID, fn)
			if row.slots != nil {
				requireOriginSummary(t, analysis, f.owner.File.ID, row.body, false, row.slots)
			}
		})
	}
}

func originComparePatternFinishRows() []originPatternRow {
	return []originPatternRow{
		{name: "literal_guard_and_wildcard", header: "fn classify(", body: "classify", slots: []uint32{}},
		{name: "nested_tag_patterns", header: "fn nested_tag(", body: "nested_tag", slots: []uint32{}},
		{name: "enum_variant_patterns", header: "fn enum_label(", body: "enum_label", slots: []uint32{}},
		{name: "literal_kinds", header: "fn literal_kinds(", body: "literal_kinds", slots: []uint32{}},
		{name: "tuple_patterns", header: "fn tuple_patterns(", body: "tuple_patterns", slots: []uint32{}},
		{name: "nested_compare_in_an_arm", header: "fn nested_compare(", body: "nested_compare", slots: []uint32{}},
		{name: "bool_arms_cover", header: "fn bool_arms(", body: "bool_arms", slots: []uint32{}},
		{name: "literal_arm_keeps_both_results", header: "fn literal_keeps_both(", body: "literal_keeps_both", slots: []uint32{1, 2}},
		{name: "enum_arm_keeps_both_results", header: "fn enum_keeps_both(", body: "enum_keeps_both", slots: []uint32{1, 2}},
		{name: "nested_payload_binding_keeps_its_origin", header: "fn nested_payload_keeps_origin(", body: "nested_payload_keeps_origin", slots: []uint32{0}},
		{name: "tuple_binding_keeps_its_origin", header: "fn tuple_binding_keeps_origin(", body: "tuple_binding_keeps_origin", slots: []uint32{0}},
		// A partial tag pattern must not consume its tag: the later arm for the same tag is reached.
		{name: "later_arm_after_a_partial_tag_is_reached", header: "fn later_arm_after_partial_tag(", body: "later_arm_after_partial_tag", slots: []uint32{1, 2}},
		{name: "later_arm_after_a_partial_nested_tag_is_reached", header: "fn later_arm_after_partial_nested(", body: "later_arm_after_partial_nested", slots: []uint32{1, 2}},
	}
}

func originComparePatternRefusedRows() []originPatternRow {
	type w = struct{ snippet, reason string }
	return []originPatternRow{
		{name: "guard_fallthrough_keeps_the_continuation_row", header: "fn guard_falls_through(", body: "guard_falls_through",
			want: []w{{"compare v {\n        Some(x) if x > 5 => x;\n        nothing => 0;\n    }", originCompareContinuationRefusal}}, allow: originPatternDerived},
		{name: "partial_tag_keeps_the_continuation_row", header: "fn partial_tag_only(", body: "partial_tag_only",
			want: []w{{"compare v {\n        Some(1) => 1;\n        nothing => 0;\n    }", originCompareContinuationRefusal}}, allow: originPatternDerived},
		{name: "partial_nested_tag_keeps_the_continuation_row", header: "fn partial_nested_only(", body: "partial_nested_only",
			want: []w{{"compare v {\n        Some(Some(n)) => n;\n        nothing => 0;\n    }", originCompareContinuationRefusal}}, allow: originPatternDerived},
		// The checker leaves a string literal pattern untyped; it keeps the pattern row instead of aborting.
		{name: "untyped_string_pattern_keeps_its_row", header: "fn string_literal_pattern(", body: "string_literal_pattern",
			want: []w{{"\"a\"", "compare pattern needs a precise matching transfer"}}, allow: originPatternDerived},
	}
}

// 14 RUN: 1 parent, 13 leaves.
func TestAnalyzeComparePatterns(t *testing.T) {
	rows := originComparePatternFinishRows()
	if len(rows) != 13 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	runOriginPatternRows(t, "compare_pattern", originComparePatternFinishSource, originComparePatternFinishSourceDigest, rows)
}

// 5 RUN: 1 parent, 4 leaves.
func TestAnalyzeComparePatternContinuations(t *testing.T) {
	rows := originComparePatternRefusedRows()
	if len(rows) != 4 {
		t.Fatalf("PRECONDITION: frozen roster changed: rows=%d", len(rows))
	}
	runOriginPatternRows(t, "compare_pattern_refused", originComparePatternRefusedSource, originComparePatternRefusedSourceDigest, rows)
}

// 6 RUN: 1 parent, 5 leaves. Each leak is SEM3139 for its local, never a clean body.
func TestComparePatternEscapeIsReported(t *testing.T) {
	rows := []struct{ name, header, owner string }{
		{"literal_arm_returns_a_local", "fn literal_arm_local(", "local"},
		{"nested_payload_binding_of_a_local", "fn nested_payload_local(", "x"},
		{"later_arm_after_a_partial_tag_returns_a_local", "fn later_arm_local(", "local"},
		{"later_arm_after_a_partial_nested_tag_returns_a_local", "fn later_nested_arm_local(", "local"},
		{"tuple_pattern_binding_of_a_local", "fn tuple_binding_local(", "x"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			fn := patternFn(t, originComparePatternEscapeSource, row.header)
			checkOriginSource(t, originComparePatternEscapeSource, originComparePatternEscapeSourceDigest, fn)
			f, analysis := analyzeOriginRoot(t, "compare_pattern_escape_"+row.name, originComparePatternEscapeSource, true, nil)
			requireSelectEscape(t, analysis, f.owner.File.ID, fn, row.owner)
		})
	}
}
