package driver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"regexp"
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

const (
	backingLoanDiscard  = "storage loan would be discarded by a payload-free value"
	backingInvalidStore = "selected index store lacks a valid original symbol"
	backingInvalidIndex = "selected index lacks a valid original symbol"
)

// backingPending is one required obligation; start < 0 means anywhere in the body.
type backingPending struct {
	start, end int
	reason     string
}

type backingEscape struct {
	start, end         int
	owner              string
	noteStart, noteEnd int
}

// backingCheck is one body's AFTER expectation (R4 §7.2–§7.3); its diagnostic multiset is exact unless anyDiagnostics.
type backingCheck struct {
	function, unit, decl string
	slots                []uint32
	summary, unknown     bool
	clean, only          bool
	anyDiagnostics       bool
	pending              []backingPending
	escapes              []backingEscape
}

// A leaf may name a scalar read whose recorded conversion makes it a gap, and may mutate its own detached fixture.
type backingLeaf struct {
	name       string
	checks     []backingCheck
	conversion [2]int
	mutate     func(t *testing.T, f backingFixture)
}

type backingFixture struct {
	res      *DiagnoseResult
	units    []sema.ReturnOriginUnit
	root     sema.ReturnOriginUnit
	text     string
	analysis *sema.ReturnOriginAnalysis
}

// 1 parent + 20 source leaves + 38 function leaves = 59 RUN.
func backingLeaves() map[string][]backingLeaf {
	one := func(name string, checks ...backingCheck) backingLeaf { return backingLeaf{name: name, checks: checks} }
	clean := func(function string, slots ...uint32) backingCheck {
		return backingCheck{function: function, slots: slots, summary: true, clean: true}
	}
	discard := func(function string, start, end int) backingCheck {
		return backingCheck{function: function, pending: []backingPending{{start, end, backingLoanDiscard}}}
	}
	escapes := func(function string, list ...backingEscape) backingCheck {
		return backingCheck{function: function, escapes: list}
	}
	fixed := func(start, end, noteStart, noteEnd int) backingEscape {
		return backingEscape{start, end, "fixed", noteStart, noteEnd}
	}
	core := func(decl, name string, slots ...uint32) backingCheck {
		return backingCheck{function: name, unit: "core/array.sg", decl: decl, slots: slots, summary: true, clean: true}
	}
	invalidStore := backingLeaf{name: "invalid_set_selection", mutate: backingInvalidate(278, 284, true),
		checks: []backingCheck{{function: "partial", pending: []backingPending{{-1, -1, backingInvalidStore}}}}}
	invalidRange := backingLeaf{name: "invalid_range_selection", mutate: backingInvalidate(52, 62, false),
		checks: []backingCheck{{function: "through_ref", unknown: true, pending: []backingPending{{52, 62, backingInvalidIndex}}}}}
	callLoad := one("call_load", clean("call_load"), clean("id"))
	callLoad.conversion = [2]int{100, 105}
	storeLoad := one("store_load", clean("store_load"))
	storeLoad.conversion = [2]int{97, 102}
	return map[string][]backingLeaf{
		"p05_default_writes": {one("empty", clean("empty")), one("zero", clean("zero")),
			one("partial", clean("partial", 0)), one("full", clean("full", 0)), invalidStore},
		"p06_core_array_paths": {one("core_bodies",
			core("pub fn extend(self: &mut Array<T>, other: &Array<T>) -> nothing", "extend"),
			core("pub fn reverse_in_place(self: &mut Array<T>) -> nothing", "reverse_in_place"),
			core("pub fn to_array(self: &ArrayFixed<T, N>) -> Array<T>", "to_array", 0),
			core("pub fn with_len_value(length: uint, value: T) -> ArrayFixed<T, N>", "with_len_value", 1),
			core("pub fn push(self: &mut Array<T>, value: T) -> nothing", "push"),
			core("pub fn reserve(self: &mut Array<T>, new_cap: uint) -> nothing", "reserve"),
			core("pub fn array_push<T>(a: &mut Array<T>, value: T) -> nothing", "array_push"),
			core("pub fn array_reserve<T>(a: &mut Array<T>, new_cap: uint) -> nothing", "array_reserve")),
			one("probe", clean("probe"))},
		"p07_repeated_site": {one("probe", clean("probe"))},
		"p10_view_cursor_lifetime": {one("through_ref", clean("through_ref", 0)), one("dynamic", clean("dynamic")),
			one("cursor_ref", clean("cursor_ref", 0)), one("probe", clean("probe")), invalidRange},
		"w1_weak_sites":        {one("both", clean("both", 0, 1)), one("looped", clean("looped", 0, 1))},
		"w2_to_array_contents": {one("copied_fixed", clean("copied_fixed"))},
		"w4_view_binding":      {one("via_binding", clean("via_binding", 0))},
		"w5_loan_discard":      {one("laundered", discard("laundered", 157, 173), clean("pass"))},
		"w6_view_of_view": {one("sub", clean("sub", 0)), one("keep_param", clean("keep_param", 0)),
			// The owner note spans the owner's whole let statement (its symbol Span).
			one("leak_param", escapes("leak_param", fixed(266, 281, 160, 224))),
			one("leak_local", escapes("leak_local", fixed(424, 441, 318, 382))),
			one("inner_rebind", escapes("inner_rebind", fixed(540, 684, 550, 614), fixed(689, 698, 550, 614), fixed(696, 697, 550, 614)))},
		"w6f_inner_local":   {one("inner_local", escapes("inner_local", fixed(125, 267, 135, 199)), clean("sub", 0))},
		"w7_cursor_binding": {one("cursor_binding", clean("cursor_binding", 0))},
		// Recorded gap (amendment A1.2): an uninstantiated template's selected index has no finalized use.
		"w8_alias_formals": {one("alias_formals", backingCheck{function: "alias_formals",
			pending: []backingPending{{103, 109, "generic index lacks its finalized concrete use"}}})},
		"w9_constructor_discard": {one("boxed", discard("boxed", 110, 138))},
		"w10_push_discard":       {one("stash", discard("stash", 132, 151))},
		"w11_callable_launder":   {one("launder", discard("launder", 173, 186))},
		"w12_extend_union": {one("wrap", clean("wrap", 0)),
			one("joined_escape", escapes("joined", backingEscape{181, 280, "s", 191, 215})),
			one("joined_clean", backingCheck{function: "joined", clean: true, anyDiagnostics: true})},
		"w13_phantom_floor":  {one("keep", clean("keep", 0)), one("through", clean("through", 0))},
		"w14_scalar_load":    {callLoad, one("cast_load", clean("cast_load"))},
		"w15_opaque_generic": {one("hide", backingCheck{function: "hide", only: true, pending: []backingPending{{134, 163, backingLoanDiscard}}})},
		"w16s_scalar_store":  {storeLoad},
	}
}

// The backing transfer over P0 fixtures and admitted witnesses; every assertion is local to one body.
func TestAnalyzeTypedReturnOriginBacking(t *testing.T) {
	logBackingLandingCounts(t)
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(backingW6[:444]))); got != backingW6RDigest {
		t.Fatalf("PRECONDITION: the W6R replacement prefix changed: %s", got)
	}
	sources, leaves, total := backingSources(), backingLeaves(), 0
	for _, list := range leaves {
		total += len(list)
	}
	if len(sources) != 20 || len(leaves) != 20 || total != 38 {
		t.Fatalf("PRECONDITION: frozen roster changed: sources=%d leaf sources=%d leaves=%d", len(sources), len(leaves), total)
	}
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			text := backingSourceText(t, src)
			f := analyzeBackingSource(t, src, text, nil)
			logBackingSource(t, src.name, f)
			for _, leaf := range leaves[src.name] {
				t.Run(leaf.name, func(t *testing.T) {
					g := f
					if leaf.mutate != nil {
						g = analyzeBackingSource(t, src, text, leaf.mutate)
					}
					if leaf.conversion != [2]int{} {
						backingConversionGap(t, g, leaf.conversion[0], leaf.conversion[1])
					}
					for _, check := range leaf.checks {
						checkBackingFunction(t, g, check)
					}
				})
			}
		})
	}
}

func backingSourceText(t *testing.T, src backingSource) string {
	t.Helper()
	text, found := src.text, 0
	for _, tc := range storageP0Cases {
		if src.fixture != "" && tc.name == src.fixture {
			text, found = tc.source, found+1
		}
	}
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	logReturnOriginCallEvidence(t, map[string]any{"backing_source": src.name, "source": text, "source_sha256": got})
	if (src.fixture != "" && found != 1) || got != src.digest {
		t.Fatalf("PRECONDITION: frozen source %s changed or is missing: %s", src.name, got)
	}
	return text
}

func analyzeBackingSource(t *testing.T, src backingSource, text string, mutate func(*testing.T, backingFixture)) backingFixture {
	t.Helper()
	res := returnOriginStdlibFixture(t, text, src.escape)
	closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
	logReturnOriginCallEvidence(t, map[string]any{"backing_source": src.name, "stage": "closure", "closure_error": errorReturnOriginCallText(closureErr), "closure": res.Sema.InstantiationClosure})
	checkReturnOriginStdlibBags(t, res, src.escape)
	inputs, err := collectReturnOriginUnits(res)
	if closureErr != nil || res.Sema.InstantiationClosure == nil || err != nil {
		t.Fatalf("PRECONDITION: generic closure or owning-unit collection did not finish: %v, %v", closureErr, err)
	}
	rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
	f := backingFixture{res: res, units: inputs.units, text: text}
	for _, unit := range inputs.units {
		if unit.SourceKey == rootKey {
			f.root = unit
		}
	}
	if mutate != nil {
		mutate(t, f)
	}
	analysis, analysisErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	logReturnOriginCallEvidence(t, map[string]any{"backing_source": src.name, "stage": "analysis", "mutated": mutate != nil, "analysis": analysis, "analysis_error": errorReturnOriginCallText(analysisErr), "analysis_complete": analysis.Complete()})
	if analysisErr != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", analysisErr)
	}
	f.analysis = analysis
	return f
}

// logBackingSource records every range-index and store selection, the p05/w1/w9 tag owner search, and W13/W15 descriptors.
func logBackingSource(t *testing.T, name string, f backingFixture) {
	t.Helper()
	u, in := f.root, f.root.Sema.TypeInterner
	file := u.Builder.Files.Get(u.FileID).Span.File
	for raw := uint32(1); raw <= u.Builder.Exprs.Arena.Len(); raw++ {
		id := ast.ExprID(raw)
		node := u.Builder.Exprs.Get(id)
		if node == nil || node.Span.File != file {
			continue
		}
		if node.Kind == ast.ExprIndex {
			selected, present := u.Sema.IndexSymbols[id]
			logReturnOriginCallEvidence(t, map[string]any{"backing_source": name, "stage": "index_selection", "span": node.Span, "present": present, "selected": selected})
		}
		if data, ok := u.Builder.Exprs.Binary(id); node.Kind == ast.ExprBinary && ok && data != nil && data.Op == ast.ExprBinaryAssign {
			selected, present := u.Sema.IndexSetSymbols[data.Left]
			logReturnOriginCallEvidence(t, map[string]any{"backing_source": name, "stage": "store_selection", "span": node.Span, "present": present, "selected": selected})
		}
		sym := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[id])
		if node.Kind == ast.ExprCall && sym != nil && sym.Kind == symbols.SymbolTag &&
			(name == "p05_default_writes" || name == "w1_weak_sites" || name == "w9_constructor_discard") {
			logStorageTagOwnerSearch(t, f.res, f.units, u, id)
		}
	}
	if name == "w13_phantom_floor" {
		view := backingExprAt(t, f, ast.ExprIndex, 190, 200)
		data, ok := u.Builder.Exprs.Index(view)
		if !ok || data == nil {
			t.Fatal("PRECONDITION: the phantom view lost its operands")
		}
		element := types.NoTypeID
		if info, known := in.StructInfo(u.Sema.ExprTypes[view]); known && info != nil && len(info.TypeArgs) != 0 {
			element = info.TypeArgs[0]
		}
		logReturnOriginCallEvidence(t, map[string]any{"backing_source": name, "stage": "phantom_descriptors",
			"formal": storageP0TypeFacts(in, u.Sema.ExprTypes[data.Target]), "view": storageP0TypeFacts(in, u.Sema.ExprTypes[view]),
			"element": storageP0TypeFacts(in, element)})
	}
	for _, c := range f.res.Sema.CallableCandidates {
		if name != "w15_opaque_generic" || c.Name != "stash" || c.Source.File != file {
			continue
		}
		var instances []sema.InstantiationInstance
		for _, instance := range f.res.Sema.InstantiationClosure.Instances {
			if instance.Template == c.Symbol {
				instances = append(instances, instance)
			}
		}
		logReturnOriginCallEvidence(t, map[string]any{"backing_source": name, "stage": "opaque_generic_identity",
			"candidate": c, "builtin": c.Builtin, "source_key": c.SourceKey, "body_key": c.BodyKey, "instances": instances})
	}
}

func backingConversionGap(t *testing.T, f backingFixture, start, end int) {
	t.Helper()
	id := backingExprAt(t, f, ast.ExprIndex, start, end)
	conversion, present := f.root.Sema.ImplicitConversions[id]
	logReturnOriginCallEvidence(t, map[string]any{"stage": "scalar_load_conversion", "text": f.text[start:end], "expr_id": id,
		"present": present, "conversion": conversion, "type": storageP0TypeFacts(f.root.Sema.TypeInterner, f.root.Sema.ExprTypes[id])})
	if present {
		t.Fatalf("PRECONDITION: explicit gap: %q carries a recorded conversion, so this row makes no loan-guard claim", f.text[start:end])
	}
}

// The P1g landing counts are evidence for the joint caps, never an assertion.
func logBackingLandingCounts(t *testing.T) {
	t.Helper()
	counts := make(map[string]any)
	for _, path := range []string{"../sema/return_origin_expr.go", "../sema/return_origin_operators.go",
		"../sema/return_origin_operators_test.go", "../sema/return_origin_conditions_test.go", "return_origin_operator_test.go"} {
		data, err := os.ReadFile(path)
		counts[path] = strings.Count(string(data), "\n")
		if err != nil {
			counts[path] = err.Error()
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "p1g_landing_counts", "counts": counts})
}

func backingInvalidate(start, end int, store bool) func(*testing.T, backingFixture) {
	return func(t *testing.T, f backingFixture) {
		t.Helper()
		id := backingExprAt(t, f, ast.ExprIndex, start, end)
		selections := f.root.Sema.IndexSymbols
		if store {
			selections = f.root.Sema.IndexSetSymbols
		}
		previous, present := selections[id]
		logReturnOriginCallEvidence(t, map[string]any{"stage": "detached_selection_mutation", "text": f.text[start:end],
			"expr_id": id, "store": store, "present": present, "previous": previous})
		if !present || !previous.IsValid() {
			t.Fatalf("PRECONDITION: %q has no valid selection to invalidate", f.text[start:end])
		}
		selections[id] = symbols.NoSymbolID
	}
}

func backingExprAt(t *testing.T, f backingFixture, kind ast.ExprKind, start, end int) ast.ExprID {
	t.Helper()
	u := f.root
	file := u.Builder.Files.Get(u.FileID).Span.File
	var found ast.ExprID
	for raw := uint32(1); raw <= u.Builder.Exprs.Arena.Len(); raw++ {
		node := u.Builder.Exprs.Get(ast.ExprID(raw))
		if node != nil && node.Kind == kind && node.Span.File == file && int(node.Span.Start) == start && int(node.Span.End) == end {
			if found.IsValid() {
				t.Fatalf("PRECONDITION: two expressions of kind %d at %d:%d", kind, start, end)
			}
			found = ast.ExprID(raw)
		}
	}
	if !found.IsValid() {
		t.Fatalf("PRECONDITION: no expression of kind %d at %d:%d (%q)", kind, start, end, f.text[start:end])
	}
	return found
}

func checkBackingFunction(t *testing.T, f backingFixture, check backingCheck) {
	t.Helper()
	unit, units := f.root, 0
	for _, candidate := range f.units {
		if check.unit != "" && candidate.SourceKey == check.unit {
			unit = candidate
			units++
		}
	}
	if check.unit != "" && units != 1 {
		t.Fatalf("PRECONDITION: owning unit %s is missing or repeated", check.unit)
	}
	file := f.res.FileSet.Get(unit.Builder.Files.Get(unit.FileID).Span.File)
	item := backingFnItem(t, unit, string(file.Content), file.ID, check)
	var summary *sema.ReturnOriginSummary
	for i := range f.analysis.Summaries {
		if s := &f.analysis.Summaries[i]; s.Name == check.function && s.Source == item.NameSpan {
			if summary != nil {
				t.Fatalf("PRECONDITION: %s has two body summaries", check.function)
			}
			summary = s
		}
	}
	var pending []sema.ReturnOriginPending
	for _, p := range f.analysis.Pending {
		if p.SourceKey == unit.SourceKey && p.Span.File == file.ID && p.Span.Start >= item.Span.Start && p.Span.End <= item.Span.End {
			pending = append(pending, p)
		}
	}
	var got, want []string
	for _, d := range f.analysis.Diagnostics {
		if d.Primary.File == file.ID && d.Primary.Start >= item.Span.Start && d.Primary.End <= item.Span.End {
			note := "without exactly one note"
			if len(d.Notes) == 1 {
				note = fmt.Sprintf("%d:%d", d.Notes[0].Span.Start, d.Notes[0].Span.End)
			}
			got = append(got, fmt.Sprintf("%v %d:%d %q note %s", d.Code, d.Primary.Start, d.Primary.End, d.Message, note))
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "backing_function", "function": check.function, "source_key": unit.SourceKey,
		"span": item.Span, "summary": summary, "pending": pending, "diagnostics": got})
	if summary == nil {
		t.Fatalf("PRECONDITION: %s has no body summary", check.function)
	}
	if check.summary && (summary.NoNormalReturn || summary.Unknown || !slices.Equal(summary.ParamSlots, check.slots)) {
		t.Errorf("%s summary = %+v, want a normal result with slots %v", check.function, *summary, check.slots)
	}
	if check.unknown && !summary.Unknown {
		t.Errorf("%s summary = %+v, want an Unknown result", check.function, *summary)
	}
	for _, need := range check.pending {
		if !slices.ContainsFunc(pending, func(p sema.ReturnOriginPending) bool { return backingPendingMatches(p, need) }) {
			t.Errorf("%s lacks Pending %q at %d:%d; got %+v", check.function, need.reason, need.start, need.end, pending)
		}
	}
	for _, p := range pending {
		listed := slices.ContainsFunc(check.pending, func(need backingPending) bool { return backingPendingMatches(p, need) })
		if check.clean || (check.only && !listed) {
			t.Errorf("%s left Pending %q at %d:%d %q", check.function, p.Reason, p.Span.Start, p.Span.End, file.Content[p.Span.Start:p.Span.End])
		}
	}
	for _, e := range check.escapes {
		message := fmt.Sprintf("borrow of '%s' outlives its owner when this scope exits", e.owner)
		want = append(want, fmt.Sprintf("%v %d:%d %q note %d:%d", diag.SemaBorrowEscapesReturn, e.start, e.end, message, e.noteStart, e.noteEnd))
	}
	slices.Sort(got)
	slices.Sort(want)
	if !check.anyDiagnostics && !slices.Equal(got, want) {
		t.Errorf("%s diagnostic multiset = %q, want %q", check.function, got, want)
	}
}

func backingPendingMatches(p sema.ReturnOriginPending, want backingPending) bool {
	return p.Reason == want.reason && (want.start < 0 || (int(p.Span.Start) == want.start && int(p.Span.End) == want.end))
}

// backingFnItem finds the FnItem named inside the exact declaration text (top-level or extern member).
func backingFnItem(t *testing.T, unit sema.ReturnOriginUnit, text string, file source.FileID, check backingCheck) *ast.FnItem {
	t.Helper()
	pattern := regexp.QuoteMeta(check.decl)
	if check.decl == "" {
		pattern = `fn ` + regexp.QuoteMeta(check.function) + `[(<]`
	}
	found := regexp.MustCompile(pattern).FindAllStringIndex(text, -1)
	if len(found) != 1 {
		t.Fatalf("PRECONDITION: %s has %d declaration texts in %s, want 1", check.function, len(found), unit.SourceKey)
	}
	var item *ast.FnItem
	consider := func(fn *ast.FnItem) {
		if fn == nil || fn.NameSpan.File != file || int(fn.NameSpan.Start) < found[0][0] || int(fn.NameSpan.End) > found[0][1] {
			return
		}
		if item != nil && item.NameSpan != fn.NameSpan {
			t.Fatalf("PRECONDITION: %s names two declarations", check.function)
		}
		item = fn
	}
	for _, id := range unit.Builder.Files.Get(unit.FileID).Items {
		if fn, ok := unit.Builder.Items.Fn(id); ok {
			consider(fn)
		}
	}
	for memberID := range unit.Symbols.ExternSyms {
		if member := unit.Builder.Items.ExternMember(memberID); member != nil && member.Kind == ast.ExternMemberFn {
			consider(unit.Builder.Items.FnByPayload(member.Fn))
		}
	}
	if item == nil {
		t.Fatalf("PRECONDITION: %s has no original FnItem in %s", check.function, unit.SourceKey)
	}
	return item
}
