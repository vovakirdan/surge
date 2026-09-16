package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

const freeSelfObligation = "generic original method call lacks its receiver expression"

type freeSelfSpan struct {
	start, end uint32
	text       string
}

type freeSelfSummary struct {
	name  string
	slots []uint32
}

type freeSelfLeaf struct {
	name, digest, text string
	allowEscape        bool
	// calls are the free-form generic call spans that must lose their obligation.
	calls []freeSelfSpan
	// escape is the span that must carry exactly one analysis SEM3139 after.
	escape *freeSelfSpan
	// summaries are the published input slots a proven passthrough must keep.
	summaries []freeSelfSummary
}

const freeSelfItself = "fn itself<T>(self: &T) -> &T {\n    return self;\n}\n"

func freeSelfLeaves() []freeSelfLeaf {
	at := func(start, end uint32, text string) freeSelfSpan { return freeSelfSpan{start, end, text} }
	return []freeSelfLeaf{
		{name: "len_array_value", digest: "30def423f4783180cdb57de0f57675c7042921b01365f3013ddb4075d60ab289",
			text:  "fn count() -> uint {\n    let arr: int[] = [1, 2, 3];\n    return len(arr);\n}\n",
			calls: []freeSelfSpan{at(64, 72, "len(arr)")}},
		{name: "len_view_ref", digest: "2c0d7ef3fa8f0bdeb39117d90650dbf0a86aaaca27911c44a53e4abb45f78bd3",
			text: "fn count_view() -> uint {\n    let mut base: int[] = [10, 20, 30, 40, 50];\n    let view = base[[1..4]];\n" +
				"    return len(&view);\n}\n",
			calls: []freeSelfSpan{at(114, 124, "len(&view)")}},
		{name: "free_self_escape", digest: "6650d8801d0cab44f7023a8ef02e188c5e2afcc7786a633227ff4fa753ad3c4e",
			text: freeSelfItself + "fn leak() -> &int {\n    let x: int = 1;\n    return itself(&x);\n}\n" +
				"fn keep(x: &int) -> &int {\n    return itself(x);\n}\n",
			allowEscape: true,
			calls:       []freeSelfSpan{at(101, 111, "itself(&x)"), at(153, 162, "itself(x)")},
			escape:      &freeSelfSpan{94, 112, "return itself(&x);"},
			summaries:   []freeSelfSummary{{"keep", []uint32{0}}}},
		{name: "free_self_second_slot", digest: "460768de9d4bcc5f7fb2d9c84bc5dedde59fea9296772da03967807ce66e270b",
			text: "fn second<T>(self: &T, other: &T) -> &T {\n    return other;\n}\nfn keep2(a: &int, b: &int) -> &int {\n" +
				"    return second(a, b);\n}\n",
			calls:     []freeSelfSpan{at(110, 122, "second(a, b)")},
			summaries: []freeSelfSummary{{"keep2", []uint32{1}}}},
		{name: "free_self_implicit_borrow_escape", digest: "de5a719d9407acf04fb1591c99cabe2bb2857148af9897ef7de14fd0746abd2a",
			text:        freeSelfItself + "fn leak_implicit() -> &int {\n    let x: int = 1;\n    return itself(x);\n}\n",
			allowEscape: true,
			calls:       []freeSelfSpan{at(110, 119, "itself(x)")},
			escape:      &freeSelfSpan{103, 120, "return itself(x);"}},
	}
}

// A generic function whose first parameter is only spelled `self` is called in
// its ordinary positional form, so the analysis reads its arguments instead of
// demanding a receiver expression. A returned input slot survives that reading,
// and a borrow of a local still escapes.
func TestAnalyzeFreeSelfGenericCall(t *testing.T) {
	for _, leaf := range freeSelfLeaves() {
		t.Run(leaf.name, func(t *testing.T) { checkFreeSelfLeaf(t, leaf) })
	}
}

func checkFreeSelfLeaf(t *testing.T, leaf freeSelfLeaf) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(leaf.text))); got != leaf.digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	spans := slices.Clone(leaf.calls)
	if leaf.escape != nil {
		spans = append(spans, *leaf.escape)
	}
	for _, span := range spans {
		if int(span.end) > len(leaf.text) || leaf.text[span.start:span.end] != span.text {
			t.Fatalf("PRECONDITION: span [%d,%d) does not hold %q", span.start, span.end, span.text)
		}
	}
	res := returnOriginStdlibFixture(t, leaf.text, leaf.allowEscape)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		t.Fatalf("PRECONDITION: owning units: %v", err)
	}
	rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
	at := func(span freeSelfSpan) source.Span {
		return source.Span{File: res.File.ID, Start: span.start, End: span.end}
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	logReturnOriginCallEvidence(t, map[string]any{"case": leaf.name, "analysis": analysis,
		"error": errorReturnOriginCallText(err), "typed_diagnostics": res.Bag.Items()})
	if err != nil || analysis == nil {
		t.Fatalf("analysis did not run: %v", err)
	}
	var root []sema.ReturnOriginPending
	for _, pending := range analysis.Pending {
		if pending.SourceKey == rootKey {
			root = append(root, pending)
		}
	}
	// S-B1 records the whole root set, so a weakened assertion can never be silent.
	t.Logf("S-B1 %s: root=%+v", leaf.name, root)
	for _, span := range leaf.calls {
		for _, pending := range root {
			if pending.Span != at(span) {
				continue
			}
			if pending.Reason == freeSelfObligation {
				t.Errorf("the free-form call at %v kept its obligation", at(span))
				continue
			}
			t.Errorf("the free-form call at %v kept a root row %q; if S-B1 attributes it to P1p-G B1, "+
				"weaken this leaf to the generic-call family per R2-5 and record it", at(span), pending.Reason)
		}
	}
	if leaf.escape != nil {
		escapes := 0
		for _, d := range analysis.Diagnostics {
			if d.Code == diag.SemaBorrowEscapesReturn && d.Primary == at(*leaf.escape) {
				escapes++
			}
		}
		if escapes != 1 {
			t.Errorf("%d escape refusals at %v, want exactly one: %+v", escapes, at(*leaf.escape), analysis.Diagnostics)
		}
	}
	for _, want := range leaf.summaries {
		summary := requireReturnOriginSummary(t, analysis, want.name)
		if summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, want.slots) {
			t.Errorf("summary %s = %+v, want slots %v", want.name, summary, want.slots)
		}
	}
}

// S-B0. The production discriminator is unexported in `sema`, so this test
// asserts the same four raw fields on the declarations it must answer for. The
// duplication is deliberate: CF-B5 covers production, and this row proves the
// fields the rule reads are the ones core `len` and the leaf functions carry.
// `ReceiverType` is NOT among them — the catalog synthesizes it from parameter
// 0 for exactly these declarations (callable_catalog.go:144-147).
func TestFreeSelfGenericCallPreconditions(t *testing.T) {
	text := freeSelfItself + "fn second<T>(self: &T, other: &T) -> &T {\n    return other;\n}\n" +
		"fn count() -> uint {\n    let arr: int[] = [1, 2, 3];\n    return len(arr);\n}\n"
	res := returnOriginStdlibFixture(t, text, false)
	// The owning units carry a callable identity snapshot only after the closure is
	// finalized (return_origin_finalization.go:192), exactly as every leaf path does
	// before reading them.
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		t.Fatalf("PRECONDITION: owning units: %v", err)
	}
	// `len` is core's declaration, which the root unit re-publishes under its own
	// key, so it is pinned by its module and accepted through either publication —
	// the four raw fields are then asserted on both, which is the pair production
	// compares. `itself` and `second` are the root's own, so they are pinned to the
	// root unit. Neither is pinned by a root ModulePath: the catalog back-fills that
	// from the checker's own module path (callable_catalog.go:127-130), which here
	// is the fixture's temp directory.
	type declPin struct{ unit, modulePath string }
	rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
	want := map[string]declPin{
		"len":    {modulePath: "core/base"},
		"itself": {unit: rootKey},
		"second": {unit: rootKey},
	}
	seen := map[string]int{}
	inventory := map[string]any{}
	for _, unit := range inputs.units {
		for _, c := range unit.Sema.CallableCandidates {
			pin, named := want[c.Name]
			if !named {
				continue
			}
			inventory[c.Name+"@"+unit.SourceKey] = map[string]any{"module_path": c.ModulePath, "has_self": c.HasSelf,
				"receiver_key": string(c.ReceiverKey), "want_unit": pin.unit, "want_module": pin.modulePath}
			if (pin.unit != "" && unit.SourceKey != pin.unit) || (pin.modulePath != "" && c.ModulePath != pin.modulePath) || !c.HasSelf {
				continue
			}
			logReturnOriginCallEvidence(t, map[string]any{"free_self_candidate": c, "unit": unit.SourceKey})
			for _, local := range unit.Publication.LocalSymbols(c.Symbol) {
				if requireFreeSelfDeclaration(t, c.Name, unit.Symbols.Table.Symbols.Get(local)) {
					seen[c.Name]++
				}
			}
			if len(unit.Publication.RootToLocalSymbols) == 0 {
				if requireFreeSelfDeclaration(t, c.Name, unit.Symbols.Table.Symbols.Get(c.Symbol)) {
					seen[c.Name]++
				}
			}
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"free_self_candidate_inventory": inventory, "seen": seen})
	for name, pin := range want {
		if seen[name] == 0 {
			t.Fatalf("PRECONDITION: no `self`-first declaration reached for %q (want unit %q, module %q): seen=%+v, every candidate of that name=%+v",
				name, pin.unit, pin.modulePath, seen, inventory)
		}
	}
}

func requireFreeSelfDeclaration(t *testing.T, name string, sym *symbols.Symbol) bool {
	t.Helper()
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
		return false
	}
	logReturnOriginCallEvidence(t, map[string]any{"free_self_symbol": name, "receiver_valid": sym.Receiver.IsValid(),
		"receiver_key": string(sym.ReceiverKey), "flags": uint32(sym.Flags), "has_self": sym.Signature.HasSelf})
	if !sym.Signature.HasSelf {
		return false
	}
	if sym.Receiver.IsValid() || sym.ReceiverKey != "" || sym.Flags&symbols.SymbolFlagMethod != 0 {
		t.Fatalf("PRECONDITION: %s is not a free `self` function: receiver=%t key=%q method=%t",
			name, sym.Receiver.IsValid(), string(sym.ReceiverKey), sym.Flags&symbols.SymbolFlagMethod != 0)
	}
	return true
}
