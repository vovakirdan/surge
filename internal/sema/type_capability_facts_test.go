package sema

import (
	"context"
	"maps"
	"reflect"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

// factsFromSnippet checks one snippet and returns the detached attribute facts
// it flushed, refusing anything that also produced a diagnostic — a test that
// asserts a fact travelled has no business reading a broken program's table.
func factsFromSnippet(t *testing.T, src string) map[types.TypeID]TypeAttrFacts {
	t.Helper()
	parseBag, semaBag, res := runSemaOnSnippetResult(t, src)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatalf("expected a semantic result")
	}
	return res.TypeAttrFacts
}

// onlyFacts returns the single row of a one-attributed-type snippet, so a case
// can assert both what was recorded and that nothing else was.
func onlyFacts(t *testing.T, facts map[types.TypeID]TypeAttrFacts) TypeAttrFacts {
	t.Helper()
	if len(facts) != 1 {
		t.Fatalf("expected exactly one attributed type, got %d: %v", len(facts), facts)
	}
	for _, row := range facts {
		return row
	}
	return TypeAttrFacts{}
}

func TestEachCapabilityAttributeReachesTheResult(t *testing.T) {
	cases := []struct {
		attr string
		want TypeAttrFacts
	}{
		{"shard_movable", TypeAttrFacts{ShardMovable: true}},
		{"shard_pinned", TypeAttrFacts{ShardPinned: true}},
		{"nosend", TypeAttrFacts{NoSend: true}},
		{"send", TypeAttrFacts{Send: true}},
	}
	for _, tc := range cases {
		t.Run(tc.attr, func(t *testing.T) {
			src := "@" + tc.attr + "\ntype Marked = { id: int };\n"
			if got := onlyFacts(t, factsFromSnippet(t, src)); got != tc.want {
				t.Fatalf("@%s: got %+v, want %+v", tc.attr, got, tc.want)
			}
		})
	}
}

func TestAnUnrelatedAttributeRecordsNoCapabilityFact(t *testing.T) {
	facts := factsFromSnippet(t, "@copy\ntype Plain = { id: int };\n")
	if len(facts) != 0 {
		t.Fatalf("expected no capability facts for @copy alone, got %v", facts)
	}
}

// TestImportedCapabilityAttributesReachTheResult drives the name-only import
// transport: the provider's attributes leave through Export.TypeAttrNames and
// have to arrive as the same facts a local declaration would have flushed.
func TestImportedCapabilityAttributesReachTheResult(t *testing.T) {
	interner := types.NewInterner()

	providerSrc := `pragma module::provider
@shard_movable
pub type Widget = { id: int };
`
	providerBuilder, providerFile, parseBag := parseSource(t, providerSrc)
	if parseBag.HasErrors() {
		t.Fatalf("provider parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	providerSyms := resolveSymbols(t, providerBuilder, providerFile)
	providerBag := diag.NewBag(32)
	providerRes := Check(context.Background(), providerBuilder, providerFile, Options{
		Reporter:   &diag.BagReporter{Bag: providerBag},
		Symbols:    providerSyms,
		Types:      interner,
		ModulePath: providerBuilder.StringsInterner.Intern("provider"),
	})
	if providerBag.HasErrors() {
		t.Fatalf("provider sema diagnostics: %s", diagnosticsSummary(providerBag))
	}
	widget := onlyFactsKey(t, providerRes.TypeAttrFacts)

	exports := symbols.CollectExports(providerBuilder, *providerSyms, "provider")
	if exports == nil {
		t.Fatalf("expected provider exports")
	}
	assertExportsCarryAttr(t, exports, "Widget", "shard_movable")

	consumerBuilder, consumerFile, parseBag := parseSource(t, "fn use_it() -> int { return 1; }\n")
	if parseBag.HasErrors() {
		t.Fatalf("consumer parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	consumerSyms := resolveSymbols(t, consumerBuilder, consumerFile)
	consumerBag := diag.NewBag(32)
	consumerRes := Check(context.Background(), consumerBuilder, consumerFile, Options{
		Reporter: &diag.BagReporter{Bag: consumerBag},
		Symbols:  consumerSyms,
		Types:    interner,
		Exports:  map[string]*symbols.ModuleExports{"provider": exports},
	})
	if consumerBag.HasErrors() {
		t.Fatalf("consumer sema diagnostics: %s", diagnosticsSummary(consumerBag))
	}

	got, ok := consumerRes.TypeAttrFacts[widget]
	if !ok {
		t.Fatalf("imported type %d carries no facts; table is %v", uint32(widget), consumerRes.TypeAttrFacts)
	}
	if want := (TypeAttrFacts{ShardMovable: true}); got != want {
		t.Fatalf("imported facts: got %+v, want %+v", got, want)
	}
}

func onlyFactsKey(t *testing.T, facts map[types.TypeID]TypeAttrFacts) types.TypeID {
	t.Helper()
	if len(facts) != 1 {
		t.Fatalf("expected exactly one attributed type, got %d: %v", len(facts), facts)
	}
	for id := range facts {
		return id
	}
	return types.NoTypeID
}

func assertExportsCarryAttr(t *testing.T, exports *symbols.ModuleExports, name, attr string) {
	t.Helper()
	for _, exp := range exports.Lookup(name) {
		for _, got := range exp.TypeAttrNames {
			if got == attr {
				return
			}
		}
	}
	t.Fatalf("export %q does not carry @%s; the import transport is not being exercised", name, attr)
}

// TestTypeAttrFactMergeIsOrderIndependent pins the property the whole pre-pass
// rests on: records arrive in whatever order the driver walks them, and the
// merged table may not depend on that.
func TestTypeAttrFactMergeIsOrderIndependent(t *testing.T) {
	records := []*Result{
		{TypeAttrFacts: map[types.TypeID]TypeAttrFacts{1: {ShardMovable: true}, 2: {Send: true}}},
		{TypeAttrFacts: map[types.TypeID]TypeAttrFacts{1: {NoSend: true}, 3: {ShardPinned: true}}},
		{TypeAttrFacts: map[types.TypeID]TypeAttrFacts{2: {ShardMovable: true}, 3: {ShardPinned: true}}},
	}
	want := mergeInOrder(records, []int{0, 1, 2})
	for _, order := range [][]int{
		{0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0},
	} {
		if got := mergeInOrder(records, order); !maps.Equal(got, want) {
			t.Fatalf("merge order %v produced %v, want %v", order, got, want)
		}
	}
}

func mergeInOrder(records []*Result, order []int) map[types.TypeID]TypeAttrFacts {
	dst := &Result{}
	for _, i := range order {
		MergeTypeAttrFacts(dst, records[i])
	}
	return dst.TypeAttrFacts
}

// TestMergedContradictionNamesEveryContributingModule covers the fail-closed
// half: two modules that each hold a defensible fact describe, together, a type
// with no capability answer.
func TestMergedContradictionNamesEveryContributingModule(t *testing.T) {
	refuse := func() string {
		t.Helper()
		merge := NewTypeAttrFactMerge()
		dst := &Result{}
		merge.Fold(dst, &Result{TypeAttrFacts: map[types.TypeID]TypeAttrFacts{7: {ShardMovable: true}}}, "zeta/mover")
		merge.Fold(dst, &Result{TypeAttrFacts: map[types.TypeID]TypeAttrFacts{7: {ShardPinned: true}}}, "alpha/pinner")
		merge.Fold(dst, &Result{TypeAttrFacts: map[types.TypeID]TypeAttrFacts{7: {ShardMovable: true}}}, "beta/mover")
		err := merge.Validate(dst)
		if err == nil {
			t.Fatalf("expected a refusal for a type that is both movable and pinned")
		}
		return err.Error()
	}

	msg := refuse()
	want := "type 7 is @shard_movable in beta/mover, zeta/mover and @shard_pinned in alpha/pinner"
	if msg != "merged type attribute facts contradict: "+want {
		t.Fatalf("refusal reads %q, want it to be %q", msg, want)
	}
	if again := refuse(); again != msg {
		t.Fatalf("refusal is not deterministic:\nfirst  %q\nsecond %q", msg, again)
	}
}

func TestMergedContradictionReportsEachPairOnce(t *testing.T) {
	merge := NewTypeAttrFactMerge()
	dst := &Result{}
	for _, path := range []string{"a", "b", "c"} {
		merge.Fold(dst, &Result{TypeAttrFacts: map[types.TypeID]TypeAttrFacts{
			4: {Send: true, NoSend: true},
		}}, path)
	}
	err := merge.Validate(dst)
	if err == nil {
		t.Fatalf("expected a refusal")
	}
	if got := countSubstring(err.Error(), "type 4 is @send"); got != 1 {
		t.Fatalf("expected the pair reported once, got %d times in %q", got, err.Error())
	}
}

func countSubstring(haystack, needle string) int {
	count := 0
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			count++
		}
	}
	return count
}

// TestADiagnosedContradictionDoesNotTravel is the other side of the refusal: a
// file that already told its author about the conflict must not make the merge
// say it again, in the wrong voice.
func TestADiagnosedContradictionDoesNotTravel(t *testing.T) {
	cases := []struct {
		name string
		src  string
		code diag.Code
	}{
		{
			name: "shard",
			src:  "@shard_movable\n@shard_pinned\ntype Conflicted = { id: int };\n",
			code: diag.SemaShardAttrConflict,
		},
		{
			name: "send",
			src:  "@send\n@nosend\ntype Conflicted = { id: int };\n",
			code: diag.SemaAttrSendNosend,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag, res := runSemaOnSnippetResult(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if !bagContainsCode(semaBag, tc.code) {
				t.Fatalf("expected %v; got %s", tc.code, diagnosticsSummary(semaBag))
			}
			assertContradictionStaysHome(t, res)
		})
	}
}

// TestAnUndiagnosedContradictionDoesNotTravel covers the declaration form the
// conflict check does not reach: a type alias may carry both shard attributes
// and be accepted in silence. Letting that pair into the merge would fail a
// build that succeeds today, and would report a one-file mistake as a
// disagreement between modules.
func TestAnUndiagnosedContradictionDoesNotTravel(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, "@shard_movable\n@shard_pinned\ntype Aliased = int;\n")
	requireNoSemaErrors(t, parseBag, semaBag)
	assertContradictionStaysHome(t, res)
}

func assertContradictionStaysHome(t *testing.T, res *Result) {
	t.Helper()
	if len(res.TypeAttrFacts) != 0 {
		t.Fatalf("a file-local contradiction was flushed anyway: %v", res.TypeAttrFacts)
	}
	merge := NewTypeAttrFactMerge()
	dst := &Result{}
	merge.Fold(dst, res, "demo")
	if err := merge.Validate(dst); err != nil {
		t.Fatalf("the merge refused a contradiction that belongs to one file: %v", err)
	}
}

// TestTypeAttrFactsOwnNothing keeps the record detached. The checker's own
// attribute store holds AST nodes that die with the file; a fact that reached
// for one would read freed structure the first time a later pass asked.
func TestTypeAttrFactsOwnNothing(t *testing.T) {
	assertNoReferenceFields(t, reflect.TypeOf(TypeAttrFacts{}), "TypeAttrFacts")
}

func assertNoReferenceFields(t *testing.T, ty reflect.Type, path string) {
	t.Helper()
	for i := range ty.NumField() {
		field := ty.Field(i)
		fieldPath := path + "." + field.Name
		switch field.Type.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice,
			reflect.Func, reflect.Chan, reflect.UnsafePointer:
			t.Fatalf("%s is a %s; detached facts may hold no references", fieldPath, field.Type.Kind())
		case reflect.Struct:
			assertNoReferenceFields(t, field.Type, fieldPath)
		case reflect.Array:
			if elem := field.Type.Elem(); elem.Kind() == reflect.Struct {
				assertNoReferenceFields(t, elem, fieldPath+"[]")
			}
		}
	}
}
