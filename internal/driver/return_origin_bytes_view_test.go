package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// A bytes view borrows its string: only the exact core rt_string_bytes_view gives its result
// formal 0's loan, which every later holder keeps; look-alikes and broken identities stay refused.
const (
	bytesViewRefuted = "opaque result type may carry borrowed state"
	bytesViewEffect  = "opaque call may change reference-bearing or callable contents"
	bytesViewStore   = "store through a place needs reference-content transfer"
	bytesViewEscape  = "borrow of 's' outlives its owner when this scope exits"
	bytesViewOwner   = "'s' owns storage that ends in this scope"
)

type bytesViewSpan struct {
	start, end int
	snippet    string
}

type bytesViewCase struct {
	name, text, digest string
	clean              bool            // no Pending anywhere in the dependency source
	cleared            []bytesViewSpan // no Pending of any reason at exactly these spans
	pending            []bytesViewSpan // a Pending with reason stays at exactly these spans
	reason             string
	escape             *bytesViewSpan // SEM3139 for the local 's' here, or inside it when within
	within             bool
	refusedIn          *bytesViewSpan      // some Pending stays inside this span
	stored             *bytesViewSpan      // the store keeps bytesViewStore here, or an analysis SEM3139 stands
	slots              map[string][]uint32 // these bodies return normally from exactly these formals
}

func bytesViewCases() []bytesViewCase {
	return []bytesViewCase{
		{name: "param_view_returned", clean: true, digest: "0e173cb811a09bfe16755d7c32af65928b36f4c2b7c218de509eb5d10a84c176",
			cleared: []bytesViewSpan{{66, 75, "s.bytes()"}}, slots: map[string][]uint32{"keep": {0}},
			text: "pragma module::dep;\nfn keep(s: &string) -> BytesView {\n    return s.bytes();\n}\n"},
		{name: "reader_calls_clean", clean: true, digest: "4c8cbc87538afbde1e46b938af9224ec3c10bf67d78fb6a0f08c54c128fbc0ee",
			cleared: []bytesViewSpan{{69, 78, "s.bytes()"}, {91, 100, "v.__len()"}},
			text:    "pragma module::dep;\nfn count_bytes(s: &string) -> uint {\n    let v = s.bytes();\n    return v.__len();\n}\n"},
		{name: "local_view_escapes", digest: "8d93aea640435b8a709b7bd3878b00dfc5a634d525a7fe0f171f00718eeb2e1e", escape: &bytesViewSpan{90, 107, "return s.bytes();"},
			text: "pragma module::dep;\nfn view_escape() -> BytesView {\n    let s: string = \"hel\" + \"lo\";\n    return s.bytes();\n}\n"},
		{name: "laundered_by_value", digest: "1fbecc53c07cc4ee9da5141f8257143afad1ee3eddf017460db2c7416606e31b",
			escape: &bytesViewSpan{166, 186, "return pass_view(w);"}, slots: map[string][]uint32{"pass_view": {0}},
			text: "pragma module::dep;\nfn pass_view(v: BytesView) -> BytesView {\n    return v;\n}\nfn leak_view() -> BytesView {\n    let s: string = \"a\" + \"b\";\n    let w = s.bytes();\n    return pass_view(w);\n}\n"},
		{name: "holder_escapes", digest: "0b75afcdb55f9f9d9b62141274991c05c200ffaeecc56ae75b29918624b5365a",
			escape: &bytesViewSpan{142, 186, "return { view: s.bytes(), cursor: 0:int64 };"},
			text:   "pragma module::dep;\ntype ViewHolder = { view: BytesView, cursor: int64 };\nfn leak_holder() -> ViewHolder {\n    let s: string = \"a\" + \"b\";\n    return { view: s.bytes(), cursor: 0:int64 };\n}\n"},
		{name: "outer_binding_escapes", digest: "867027588148621236224475b74db6cabc46f1a11ded5eccb4370c0662f40ad3", within: true,
			escape: &bytesViewSpan{20, 176, "fn outer_view(p: &string) -> uint {\n    let mut v = p.bytes();\n    {\n        let s: string = \"a\" + \"b\";\n        v = s.bytes();\n    }\n    return v.__len();\n}"},
			text:   "pragma module::dep;\nfn outer_view(p: &string) -> uint {\n    let mut v = p.bytes();\n    {\n        let s: string = \"a\" + \"b\";\n        v = s.bytes();\n    }\n    return v.__len();\n}\n"},
		{name: "view_field_store_not_skipped", digest: "ed7f5dcfe3b52f24afd31d1b1077cb35a2721e97d46ad1253f9e3c250b1cc8c1", stored: &bytesViewSpan{227, 245, "h.view = s.bytes()"},
			text: "pragma module::dep;\ntype FieldHolder = { view: BytesView, cursor: int64 };\nfn store_view(p: &string) -> uint {\n    let mut h: FieldHolder = { view: p.bytes(), cursor: 0:int64 };\n    {\n        let s: string = \"a\" + \"b\";\n        h.view = s.bytes();\n    }\n    return h.view.__len();\n}\n"},
		{name: "namesake_constructor_control", digest: "54fcced25c13e080a8f40e14dc17de6d427194ec8b261c73bfd15f7986ac544b", reason: bytesViewRefuted,
			pending: []bytesViewSpan{{34, 43, "view_like"}, {120, 132, "view_like(s)"}},
			text:    "pragma module::dep;\n@intrinsic fn view_like(s: &string) -> BytesView;\nfn use_view(s: &string) -> BytesView {\n    return view_like(s);\n}\n"},
		{name: "namesake_reader_control", digest: "3c470898a3054c72376cdd7e9051e91a2d2eb5e11cf57a11148d808d53df46da", reason: bytesViewEffect,
			pending: []bytesViewSpan{{132, 140, "peek(&v)"}},
			text:    "pragma module::dep;\n@intrinsic fn peek(v: &BytesView) -> uint;\nfn peek_view(s: &string) -> uint {\n    let v = s.bytes();\n    return peek(&v);\n}\n"},
		{name: "callback_result_control", digest: "e8a24b47f8dcde501d348afdb45a4bf978e4072ec7f9c1c918db36d810c60dcf", reason: bytesViewRefuted,
			pending: []bytesViewSpan{{101, 105, "f(s)"}},
			text:    "pragma module::dep;\nfn apply_view(f: fn(&string) -> BytesView, s: &string) -> BytesView {\n    return f(s);\n}\n"},
		{name: "function_value_control", digest: "681f2af07ae662b2c87df19a253e4a8f08bd75c241f234ef2aa1c069523f5e63",
			refusedIn: &bytesViewSpan{20, 113, "fn value_view(s: &string) -> BytesView {\n    let f = rt_string_bytes_view;\n    return f(s);\n}"},
			text:      "pragma module::dep;\nfn value_view(s: &string) -> BytesView {\n    let f = rt_string_bytes_view;\n    return f(s);\n}\n"},
	}
}

func analyzeBytesView(t *testing.T, stage string, tc bytesViewCase, mutate func(originalGenericFixture)) (originalGenericFixture, *sema.ReturnOriginAnalysis, []sema.ReturnOriginPending) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))); got != tc.digest {
		t.Fatalf("PRECONDITION: frozen dependency source changed: %s", got)
	}
	spans := append(slices.Clone(tc.cleared), tc.pending...)
	for _, span := range []*bytesViewSpan{tc.escape, tc.refusedIn, tc.stored} {
		if span != nil {
			spans = append(spans, *span)
		}
	}
	if slices.ContainsFunc(spans, func(s bytesViewSpan) bool {
		return s.start < 0 || s.end > len(tc.text) || s.start >= s.end || tc.text[s.start:s.end] != s.snippet
	}) {
		t.Fatalf("PRECONDITION: a frozen span is not its snippet: %+v", spans)
	}
	f := originalGenericSignatureFixture(t, tc.text, false, true)
	if mutate != nil {
		mutate(f)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	local := slices.DeleteFunc(slices.Clone(analysis.Pending), func(p sema.ReturnOriginPending) bool {
		return p.SourceKey != f.unit.SourceKey && p.Span.File != f.owner.File.ID
	})
	logReturnOriginCallEvidence(t, map[string]any{"stage": stage, "case": tc.name, "source_key": f.unit.SourceKey, "pending": local, "diagnostics": analysis.Diagnostics})
	return f, analysis, local
}

func bytesViewSummary(t *testing.T, analysis *sema.ReturnOriginAnalysis, file source.FileID, name string) sema.ReturnOriginSummary {
	t.Helper()
	found := slices.DeleteFunc(slices.Clone(analysis.Summaries), func(s sema.ReturnOriginSummary) bool {
		return s.Name != name || s.Source.File != file
	})
	if len(found) != 1 {
		t.Fatalf("PRECONDITION: summary %s in file %d is not unique: %+v", name, file, found)
	}
	return found[0]
}

func checkBytesView(t *testing.T, tc bytesViewCase, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis, local []sema.ReturnOriginPending) {
	t.Helper()
	inside := func(p sema.ReturnOriginPending, span bytesViewSpan, exact bool) bool {
		start, end := int(p.Span.Start), int(p.Span.End)
		return p.SourceKey == f.unit.SourceKey && (start == span.start && end == span.end || !exact && start >= span.start && end <= span.end)
	}
	refused, stored := tc.refusedIn == nil, tc.stored == nil
	for _, p := range local {
		if tc.clean {
			t.Errorf("dependency obligation left unfinished: %s at %d:%d", p.Reason, p.Span.Start, p.Span.End)
		}
		for _, span := range tc.cleared {
			if inside(p, span, true) {
				t.Errorf("certified %q at %d:%d still has: %s", span.snippet, span.start, span.end, p.Reason)
			}
		}
		refused = refused || inside(p, *tc.refusedIn, false)
		stored = stored || inside(p, *tc.stored, true) && p.Reason == bytesViewStore
	}
	for _, span := range tc.pending {
		if !slices.ContainsFunc(local, func(p sema.ReturnOriginPending) bool { return inside(p, span, true) && p.Reason == tc.reason }) {
			t.Errorf("lost %q at %d:%d %q: %+v", tc.reason, span.start, span.end, span.snippet, local)
		}
	}
	for name, want := range tc.slots {
		if s := bytesViewSummary(t, analysis, f.owner.File.ID, name); s.NoNormalReturn || s.Unknown || !slices.Equal(s.ParamSlots, want) {
			t.Errorf("%s summary = %+v, want a normal result from formals %v", name, s, want)
		}
	}
	var owner *symbols.Symbol
	owners := 0
	for i := range f.unit.Symbols.Table.Symbols.Data() {
		c := f.unit.Symbols.Table.Symbols.Get(symbols.SymbolID(i + 1))
		if name, _ := f.unit.Builder.StringsInterner.Lookup(c.Name); c.Kind == symbols.SymbolLet && c.Span.File == f.owner.File.ID && name == "s" {
			owner, owners = c, owners+1
		}
	}
	if owners > 1 {
		t.Fatal("PRECONDITION: ambiguous local owner 's'")
	}
	escaped := false
	for _, d := range analysis.Diagnostics {
		if d.Primary.File != f.owner.File.ID {
			continue
		}
		if (tc.escape == nil && tc.stored == nil) || owner == nil || d.Code != diag.SemaBorrowEscapesReturn || d.Severity != diag.SevError || d.Message != bytesViewEscape {
			t.Errorf("unexpected dependency diagnostic: %+v", d)
			continue
		}
		stored = stored || tc.stored != nil
		start, end := int(d.Primary.Start), int(d.Primary.End)
		placed := tc.escape != nil && (start == tc.escape.start && end == tc.escape.end || tc.within && start >= tc.escape.start && end <= tc.escape.end && start < end)
		for _, note := range d.Notes {
			escaped = escaped || placed && note.Span == owner.Span && note.Msg == bytesViewOwner
		}
	}
	if !refused || !stored {
		t.Errorf("refusal lost (inside %+v, store %+v keeps %q or SEM3139): %+v", tc.refusedIn, tc.stored, bytesViewStore, local)
	}
	if tc.escape != nil && !escaped {
		t.Errorf("missing SEM3139 for 's' at %d:%d (within=%v) with the owner note: %+v", tc.escape.start, tc.escape.end, tc.within, analysis.Diagnostics)
	}
}

func TestAnalyzeBytesViewOrigins(t *testing.T) {
	for _, tc := range bytesViewCases() {
		t.Run(tc.name, func(t *testing.T) {
			f, analysis, local := analyzeBytesView(t, "bytes_view", tc, nil)
			checkBytesView(t, tc, f, analysis, local)
		})
	}
}

// bytesViewCore finds needle inside a declaration that occurs exactly once in a unique core unit.
func bytesViewCore(t *testing.T, f originalGenericFixture, key, pattern, needle string) (source.FileID, string, int, int) {
	t.Helper()
	var unit *sema.ReturnOriginUnit
	units := 0
	for i := range f.inputs.units {
		if f.inputs.units[i].SourceKey == key {
			unit, units = &f.inputs.units[i], units+1
		}
	}
	if units != 1 {
		t.Fatalf("PRECONDITION: core unit %s occurs %d times", key, units)
	}
	file := unit.Builder.Files.Get(unit.FileID).Span.File
	content := string(f.owner.FileSet.Get(file).Content)
	if strings.Count(content, pattern) != 1 || strings.Count(pattern, needle) != 1 {
		t.Fatalf("PRECONDITION: core declaration %q with %q is not unique", pattern, needle)
	}
	start := strings.Index(content, pattern) + strings.Index(pattern, needle)
	return file, content, start, start + len(needle)
}

const bytesViewStringBytes = "pub fn bytes(self: &string) -> BytesView { return rt_string_bytes_view(self); }"

// The core constructor, string.bytes and append_bytes_view's reader call carry no obligation.
func TestReturnOriginBytesViewCoreRows(t *testing.T) {
	f, analysis, _ := analyzeBytesView(t, "bytes_view_core_rows", bytesViewCases()[0], nil)
	for _, row := range []struct{ key, pattern, needle string }{
		{"core/intrinsics.sg", "@intrinsic fn rt_string_bytes_view(", "rt_string_bytes_view"},
		{"core/string.sg", bytesViewStringBytes, "-> BytesView"},
		{"core/string.sg", bytesViewStringBytes, "return rt_string_bytes_view(self);"},
		{"core/string.sg", bytesViewStringBytes, "rt_string_bytes_view(self)"},
	} {
		file, _, start, end := bytesViewCore(t, f, row.key, row.pattern, row.needle)
		for _, p := range analysis.Pending {
			if p.SourceKey == row.key && p.Span.File == file && int(p.Span.Start) == start && int(p.Span.End) == end {
				t.Errorf("core %s %q at %d:%d still has: %s", row.key, row.needle, start, end, p.Reason)
			}
		}
	}
	stringFile, _, _, _ := bytesViewCore(t, f, "core/string.sg", bytesViewStringBytes, "bytes(")
	if s := bytesViewSummary(t, analysis, stringFile, "bytes"); s.NoNormalReturn || s.Unknown || !slices.Equal(s.ParamSlots, []uint32{0}) {
		t.Errorf("core string.bytes summary = %+v, want a normal result from formal 0", s)
	}
	const appendView = "pub fn append_bytes_view(self: &mut Array<byte>, view: &BytesView) -> nothing {"
	arrayFile, content, bodyStart, _ := bytesViewCore(t, f, "core/array.sg", appendView, appendView)
	bodyEnd := bodyStart + strings.Index(content[bodyStart:], "\n    }\n") + len("\n    }")
	for _, p := range analysis.Pending {
		if p.SourceKey == "core/array.sg" && p.Span.File == arrayFile && int(p.Span.Start) >= bodyStart && int(p.Span.End) <= bodyEnd {
			t.Errorf("core append_bytes_view keeps %s at %d:%d", p.Reason, p.Span.Start, p.Span.End)
		}
	}
}

// A core constructor with a broken module identity is refuted like any other view result.
func TestReturnOriginBytesViewIdentityMutation(t *testing.T) {
	t.Run("mutated_bytes_view_constructor", func(t *testing.T) {
		tc := bytesViewCases()[0]
		tc.name, tc.clean, tc.cleared, tc.slots = "mutated_bytes_view_constructor", false, nil, nil
		f, analysis, local := analyzeBytesView(t, "bytes_view_mutation", tc, func(f originalGenericFixture) {
			matched := 0
			for i := range f.authority.CallableCandidates {
				if c := &f.authority.CallableCandidates[i]; c.Name == "rt_string_bytes_view" && c.SourceKey == "builtin" && c.ModulePath == "core/intrinsics" {
					c.ModulePath = "core/intrinsics_shadow"
					matched++
				}
			}
			if matched != 1 {
				t.Fatalf("PRECONDITION: core rt_string_bytes_view candidate is not unique: %d", matched)
			}
		})
		file, _, start, end := bytesViewCore(t, f, "core/intrinsics.sg", "@intrinsic fn rt_string_bytes_view(", "rt_string_bytes_view")
		if !slices.ContainsFunc(analysis.Pending, func(p sema.ReturnOriginPending) bool {
			return p.SourceKey == "core/intrinsics.sg" && p.Span.File == file && int(p.Span.Start) == start && int(p.Span.End) == end && p.Reason == bytesViewRefuted
		}) {
			t.Errorf("mutated core constructor at %d:%d lost its %q refusal", start, end, bytesViewRefuted)
		}
		stringFile, _, _, _ := bytesViewCore(t, f, "core/string.sg", bytesViewStringBytes, "bytes(")
		if s := bytesViewSummary(t, analysis, stringFile, "bytes"); !s.Unknown {
			t.Errorf("string.bytes over a mutated constructor = %+v, want an unproved source", s)
		}
		if !slices.ContainsFunc(local, func(p sema.ReturnOriginPending) bool {
			return p.SourceKey == f.unit.SourceKey && p.Span.Start == 66 && p.Span.End == 75
		}) {
			t.Errorf("dependency call s.bytes() at 66:75 lost its obligation: %+v", local)
		}
	})
}
