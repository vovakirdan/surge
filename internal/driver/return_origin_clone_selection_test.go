package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// originSpan is a frozen byte range of a test source together with the text it holds.
type originSpan struct {
	start, end int
	snippet    string
}

// originRefusal is one Pending a leaf expects, or forbids, at a frozen span.
type originRefusal struct {
	span   originSpan
	reason string
}

// originBodyLeaf judges one body of an analyzed test source.
type originBodyLeaf struct {
	name, body     string
	function       originSpan
	clean          bool
	slots          []uint32
	stays, cleared []originRefusal
}

const originCallRefusal = "call needs an exact body, canonical core contract, or opaque declaration promise"

const originProjectionRefusal = "projected borrowed payload needs precise origin facts"

const originConstructRefusal = "constructed result needs concrete borrowed-content facts"

const originOutgoingRefusal = "outgoing reference has unresolved or captured provenance"

const originResultRefusal = "function result contains an unproved source"

const originTagCallerRefusal = "generic tag caller requires its exact type-dependent payload transfer"

// checkOriginSource freezes one test source and every span a test reads from it.
func checkOriginSource(t *testing.T, text, digest string, spans ...originSpan) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	for _, span := range spans {
		if span.start < 0 || span.end > len(text) || span.start >= span.end || text[span.start:span.end] != span.snippet {
			t.Fatalf("PRECONDITION: frozen span %d:%d is not %q", span.start, span.end, span.snippet)
		}
	}
}

// originExprAt finds the unique expression of one kind at a frozen span.
func originExprAt(t *testing.T, unit sema.ReturnOriginUnit, file source.FileID, span originSpan, kind ast.ExprKind) ast.ExprID {
	t.Helper()
	var found ast.ExprID
	for raw := uint32(1); raw <= unit.Builder.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		node := unit.Builder.Exprs.Get(id)
		if node == nil || node.Kind != kind || node.Span.File != file || int(node.Span.Start) != span.start || int(node.Span.End) != span.end {
			continue
		}
		if found.IsValid() {
			t.Fatalf("PRECONDITION: %q has more than one expression", span.snippet)
		}
		found = id
	}
	if !found.IsValid() {
		t.Fatalf("PRECONDITION: %q has no expression", span.snippet)
	}
	return found
}

// originPendingAt reports a Pending of this reason, or of any reason when the
// reason is empty, at exactly this span of one source unit.
func originPendingAt(analysis *sema.ReturnOriginAnalysis, sourceKey string, span originSpan, reason string) bool {
	for _, pending := range analysis.Pending {
		if pending.SourceKey == sourceKey && int(pending.Span.Start) == span.start && int(pending.Span.End) == span.end &&
			(reason == "" || pending.Reason == reason) {
			return true
		}
	}
	return false
}

// originPendingWithin lists the Pending rows of one source unit inside a byte range.
func originPendingWithin(analysis *sema.ReturnOriginAnalysis, sourceKey string, start, end int) []sema.ReturnOriginPending {
	var out []sema.ReturnOriginPending
	for _, pending := range analysis.Pending {
		if pending.SourceKey == sourceKey && int(pending.Span.Start) >= start && int(pending.Span.End) <= end {
			out = append(out, pending)
		}
	}
	return out
}

// requireOriginSummary reads the one summary of a body declared in the test source.
func requireOriginSummary(t *testing.T, analysis *sema.ReturnOriginAnalysis, file source.FileID, name string, unknown bool, slots []uint32) {
	t.Helper()
	found := 0
	for _, s := range analysis.Summaries {
		if s.Name != name || s.Source.File != file {
			continue
		}
		found++
		if s.NoNormalReturn || s.Unknown != unknown || !slices.Equal(s.ParamSlots, slots) {
			t.Errorf("%s summary = %+v, want a normal result with unknown=%v and sources %v", name, s, unknown, slots)
		}
	}
	if found != 1 {
		t.Fatalf("PRECONDITION: %d summaries for %s in the test source", found, name)
	}
}

// analyzeOriginDependency analyzes one dependency source with core. The
// prepare hook runs after the fixture exists and before the analysis.
func analyzeOriginDependency(t *testing.T, stage, text string, prepare func(originalGenericFixture)) (originalGenericFixture, *sema.ReturnOriginAnalysis) {
	t.Helper()
	f := originalGenericSignatureFixture(t, text, false, true)
	if prepare != nil {
		prepare(f)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": stage, "source_key": f.unit.SourceKey,
		"pending": originPendingWithin(analysis, f.unit.SourceKey, 0, len(text)), "diagnostics": analysis.Diagnostics})
	for _, d := range analysis.Diagnostics {
		if d.Primary.File == f.owner.File.ID {
			t.Errorf("unexpected dependency diagnostic: %+v", d)
		}
	}
	return f, analysis
}

// checkOriginBodyLeaves judges each body leaf against one completed analysis.
func checkOriginBodyLeaves(t *testing.T, analysis *sema.ReturnOriginAnalysis, f originalGenericFixture, text, digest string, leaves []originBodyLeaf) {
	t.Helper()
	for _, leaf := range leaves {
		t.Run(leaf.name, func(t *testing.T) {
			spans := []originSpan{leaf.function}
			for _, refusal := range append(slices.Clone(leaf.stays), leaf.cleared...) {
				spans = append(spans, refusal.span)
			}
			checkOriginSource(t, text, digest, spans...)
			within := originPendingWithin(analysis, f.unit.SourceKey, leaf.function.start, leaf.function.end)
			for _, want := range leaf.stays {
				if !originPendingAt(analysis, f.unit.SourceKey, want.span, want.reason) {
					t.Errorf("lost %q at %d:%d %q: %+v", want.reason, want.span.start, want.span.end, want.span.snippet, within)
				}
			}
			for _, gone := range leaf.cleared {
				if originPendingAt(analysis, f.unit.SourceKey, gone.span, gone.reason) {
					t.Errorf("still refuses %q at %d:%d %q", gone.reason, gone.span.start, gone.span.end, gone.span.snippet)
				}
			}
			if !leaf.clean {
				return
			}
			for _, pending := range within {
				t.Errorf("%s left unfinished: %s at %d:%d", leaf.body, pending.Reason, pending.Span.Start, pending.Span.End)
			}
			requireOriginSummary(t, analysis, f.owner.File.ID, leaf.body, false, leaf.slots)
		})
	}
}

// A direct clone of a concrete non-Copy value is the call to its program-wide
// __clone. When that selection is the certified body-less core intrinsic, the
// owned result keeps no source and the `clone` name is never read as a value.
// A Copy clone, a __clone with a body and every broken selection keep refusing.
const directCloneSource = `pragma module::dep;
fn first_text(parts: string[]) -> string {
    return clone(parts[0]);
}
fn copy_text(text: &string) -> string {
    return clone(text);
}
fn count_text(text: &string) -> uint {
    return text.__len();
}
fn copy_number(n: &int64) -> int64 {
    return clone(n);
}
type Note = { label: string };
extern<Note> {
    pub fn __clone(self: &Note) -> Note {
        return Note { label = "note" };
    }
}
fn copy_note(n: &Note) -> Note {
    return clone(n);
}
`

const directCloneDigest = "35fe4eb9d8f669c419a0f53deba685eb2729d4c85b7e351fb69d1a1108e03304"

type directCloneLeaf struct {
	name     string
	function originSpan
	call     originSpan
	cleared  bool
	mutate   func(f originalGenericFixture, calls map[string]ast.ExprID)
}

// originSelectedCandidate maps a unit-local selection to its unique canonical
// candidate, the way the analysis reads a selection.
func originSelectedCandidate(t *testing.T, f originalGenericFixture, selected symbols.SymbolID) sema.CallableCandidate {
	t.Helper()
	var found []sema.CallableCandidate
	for _, candidate := range f.authority.CallableCandidates {
		mapped := candidate.Symbol == selected
		if len(f.unit.Publication.RootToLocalSymbols) != 0 {
			mapped = slices.Contains(f.unit.Publication.RootToLocalSymbols[candidate.Symbol], selected)
		}
		if mapped {
			found = append(found, candidate)
		}
	}
	if !selected.IsValid() || len(found) != 1 {
		t.Fatalf("PRECONDITION: selection %d has %d canonical candidates", selected, len(found))
	}
	return found[0]
}

func directCloneCalls(t *testing.T, f originalGenericFixture) map[string]ast.ExprID {
	t.Helper()
	calls := make(map[string]ast.ExprID)
	for _, site := range []struct {
		key  string
		span originSpan
		kind ast.ExprKind
	}{
		{"first", originSpan{74, 89, "clone(parts[0])"}, ast.ExprCall},
		{"index", originSpan{80, 88, "parts[0]"}, ast.ExprIndex},
		{"text", originSpan{144, 155, "clone(text)"}, ast.ExprCall},
		{"length", originSpan{209, 221, "text.__len()"}, ast.ExprCall},
		{"number", originSpan{273, 281, "clone(n)"}, ast.ExprCall},
		{"note", originSpan{465, 473, "clone(n)"}, ast.ExprCall},
	} {
		checkOriginSource(t, directCloneSource, directCloneDigest, site.span)
		calls[site.key] = originExprAt(t, f.unit, f.owner.File.ID, site.span, site.kind)
	}
	clones := f.unit.Sema.CloneSymbols
	for _, key := range []string{"first", "text", "note"} {
		if !clones[calls[key]].IsValid() {
			t.Fatalf("PRECONDITION: direct clone %s has no published selection", key)
		}
	}
	if _, selected := clones[calls["number"]]; selected {
		t.Fatal("PRECONDITION: a Copy clone gained a selection")
	}
	if c := originSelectedCandidate(t, f, clones[calls["first"]]); c.Name != "__clone" || !c.Intrinsic || c.HasBody || len(c.TemplateParams) != 0 {
		t.Fatalf("PRECONDITION: the string clone is not the body-less core intrinsic: %+v", c)
	}
	if c := originSelectedCandidate(t, f, clones[calls["note"]]); c.Name != "__clone" || !c.HasBody || c.Source.File != f.owner.File.ID {
		t.Fatalf("PRECONDITION: the Note clone is not its own body: %+v", c)
	}
	if c := originSelectedCandidate(t, f, f.unit.Symbols.ExprSymbols[calls["length"]]); c.Name != "__len" || c.HasBody || len(c.ParamTypes) != 1 {
		t.Fatalf("PRECONDITION: the __len selection is not a body-less one-formal method: %+v", c)
	}
	return calls
}

// The dependency harness finalizes no generic element read, so a clone of an
// array element is judged cleared in the root program below; its selection
// controls stay here, where only the call refusal is read.
func TestAnalyzeSelectedDirectCloneOrigins(t *testing.T) {
	firstText := originSpan{20, 92, "fn first_text(parts: string[]) -> string {\n    return clone(parts[0]);\n}"}
	copyText := originSpan{93, 158, "fn copy_text(text: &string) -> string {\n    return clone(text);\n}"}
	copyNumber := originSpan{225, 284, "fn copy_number(n: &int64) -> int64 {\n    return clone(n);\n}"}
	copyNote := originSpan{421, 477, "fn copy_note(n: &Note) -> Note {\n    return clone(n);\n}\n"}
	firstCall := originSpan{74, 89, "clone(parts[0])"}
	numberCall := originSpan{273, 281, "clone(n)"}
	for _, tc := range []directCloneLeaf{
		{name: "copy_text", function: copyText, call: originSpan{144, 155, "clone(text)"}, cleared: true},
		{name: "copy_number_control", function: copyNumber, call: numberCall},
		{name: "copy_note_control", function: copyNote, call: originSpan{465, 473, "clone(n)"}},
		{name: "absent_selection_control", function: firstText, call: firstCall, mutate: func(f originalGenericFixture, calls map[string]ast.ExprID) {
			delete(f.unit.Sema.CloneSymbols, calls["first"])
		}},
		{name: "invalid_selection_control", function: firstText, call: firstCall, mutate: func(f originalGenericFixture, calls map[string]ast.ExprID) {
			f.unit.Sema.CloneSymbols[calls["first"]] = symbols.NoSymbolID
		}},
		{name: "wrong_name_control", function: firstText, call: firstCall, mutate: func(f originalGenericFixture, calls map[string]ast.ExprID) {
			f.unit.Sema.CloneSymbols[calls["first"]] = f.unit.Symbols.ExprSymbols[calls["length"]]
		}},
		// Synthetic: typing never records a conversion on a clone argument.
		{name: "argument_conversion_control", function: firstText, call: firstCall, mutate: func(f originalGenericFixture, calls map[string]ast.ExprID) {
			if f.unit.Sema.ImplicitConversions == nil {
				f.unit.Sema.ImplicitConversions = make(map[ast.ExprID]sema.ImplicitConversion)
			}
			f.unit.Sema.ImplicitConversions[calls["index"]] = sema.ImplicitConversion{Kind: sema.ImplicitConversionTo}
		}},
		// Synthetic: typing never selects a __clone for a Copy result.
		{name: "copy_result_control", function: copyNumber, call: numberCall, mutate: func(f originalGenericFixture, calls map[string]ast.ExprID) {
			f.unit.Sema.CloneSymbols[calls["number"]] = f.unit.Sema.CloneSymbols[calls["first"]]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkOriginSource(t, directCloneSource, directCloneDigest, tc.function, tc.call)
			f, analysis := analyzeOriginDependency(t, "direct_clone_"+tc.name, directCloneSource, func(f originalGenericFixture) {
				calls := directCloneCalls(t, f)
				if tc.mutate != nil {
					tc.mutate(f, calls)
				}
			})
			if !tc.cleared {
				if !originPendingAt(analysis, f.unit.SourceKey, tc.call, originCallRefusal) {
					t.Errorf("%q lost its call refusal: %+v", tc.call.snippet, originPendingWithin(analysis, f.unit.SourceKey, tc.function.start, tc.function.end))
				}
				return
			}
			for _, pending := range originPendingWithin(analysis, f.unit.SourceKey, tc.function.start, tc.function.end) {
				t.Errorf("%s left unfinished: %s at %d:%d", tc.name, pending.Reason, pending.Span.Start, pending.Span.End)
			}
			requireOriginSummary(t, analysis, f.owner.File.ID, tc.name, false, nil)
		})
	}
}

const directCloneIndexSource = `fn first_text(parts: string[]) -> string {
    return clone(parts[0]);
}
`

// A clone of an array element in a root program with full core: the element
// read has its finalized concrete use, so the body is clean and has no source.
func TestAnalyzeSelectedDirectCloneOfElement(t *testing.T) {
	function := originSpan{0, 73, "fn first_text(parts: string[]) -> string {\n    return clone(parts[0]);\n}\n"}
	call := originSpan{54, 69, "clone(parts[0])"}
	checkOriginSource(t, directCloneIndexSource, "1e7fc759c49f7048048ee4cd7ec1fe8f2a3eac4fa89ad6566499a50e95575cb1", function, call)
	f, analysis := analyzeOriginRoot(t, "direct_clone_element", directCloneIndexSource, false, func(f originalGenericFixture) {
		id := originExprAt(t, f.unit, f.owner.File.ID, call, ast.ExprCall)
		if c := originSelectedCandidate(t, f, f.unit.Sema.CloneSymbols[id]); c.Name != "__clone" || !c.Intrinsic || c.HasBody || len(c.TemplateParams) != 0 {
			t.Fatalf("PRECONDITION: the string clone is not the body-less core intrinsic: %+v", c)
		}
	})
	for _, d := range analysis.Diagnostics {
		if d.Primary.File == f.owner.File.ID {
			t.Errorf("unexpected source diagnostic: %+v", d)
		}
	}
	for _, pending := range originPendingWithin(analysis, f.unit.SourceKey, function.start, function.end) {
		t.Errorf("first_text left unfinished: %s at %d:%d", pending.Reason, pending.Span.Start, pending.Span.End)
	}
	requireOriginSummary(t, analysis, f.owner.File.ID, "first_text", false, nil)
}

const directCloneAddressSource = `fn keep_first(parts: string[]) -> &string {
    let saved: string = clone(parts[0]);
    return &saved;
}
`

const directCloneArgumentSource = `fn probe(parts: string[], outside: &int64) -> int64 {
    let mut saved: &int64 = outside;
    let copied: string = clone(parts[{ let owned: int64 = 1; saved = &owned; ret 0; }]);
    return 1;
}
`

// Certifying a clone keeps every escape: the address of its owned result, and
// a borrow made while evaluating its argument.
func TestAnalyzeSelectedDirectCloneEscapes(t *testing.T) {
	for _, tc := range []struct {
		name, text, digest, owner string
		escape, clone, call       originSpan
	}{
		{"local_address_escape", directCloneAddressSource, "98701fc3b8dd46f828a7fe4956d94859373368eebaaf00a906b3d231cbb75f13", "saved",
			originSpan{89, 103, "return &saved;"}, originSpan{68, 73, "clone"}, originSpan{68, 83, "clone(parts[0])"}},
		{"argument_effect_escape", directCloneArgumentSource, "2785788cb9f3fff6fc7eb922e24eabb63d413f549487ef7bfe1d22c4a5b209fb", "owned",
			originSpan{168, 174, "ret 0;"}, originSpan{116, 121, "clone"}, originSpan{116, 178, "clone(parts[{ let owned: int64 = 1; saved = &owned; ret 0; }])"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkOriginSource(t, tc.text, tc.digest, tc.escape, tc.clone, tc.call)
			f, analysis := analyzeOriginRoot(t, "direct_clone_escape_"+tc.name, tc.text, true, nil)
			requireOriginEscape(t, analysis, f.owner.Symbols, f.owner.File.ID, tc.escape, tc.owner)
			for _, span := range []originSpan{tc.clone, tc.call} {
				if originPendingAt(analysis, f.unit.SourceKey, span, "") {
					t.Errorf("certified clone %q still refuses at %d:%d", span.snippet, span.start, span.end)
				}
			}
		})
	}
}
