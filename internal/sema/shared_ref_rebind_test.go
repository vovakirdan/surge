package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

// These source checks distinguish replacing a reference slot from writing its
// referent. The copied holder also keeps the old loan alive after replacement.
func TestSharedReferenceRebind(t *testing.T) {
	const prefix = "@copy type RebindLeaf = { value: int64 }\n"
	const locals = `fn check() -> int64 {
	let mut a = RebindLeaf { value = 1 };
	let b = RebindLeaf { value = 2 };
	let mut alias: &RebindLeaf = &a;
`
	cases := []struct {
		name    string
		src     string
		code    diag.Code
		refusal string
		check   func(*sharedRebindAnalysis)
	}{
		{
			name: "rebind_updates_alias_slot",
			src: locals + `
	alias = &b;
	let probe = &*alias;
	@drop probe;
	@drop alias;
	return a.value;
}`,
			check: func(a *sharedRebindAnalysis) {
				a.write("alias = &b", "alias", BorrowIssueNone)
				a.borrow("&*alias", "b")
				a.drop("alias", a.borrow("&b", "b").Borrow)
			},
		},
		{
			name: "self_assignment_preserves_loan",
			src: locals + `
	alias = alias;
	@drop alias;
	return a.value;
}`,
			check: func(a *sharedRebindAnalysis) {
				a.write("alias = alias", "alias", BorrowIssueNone)
				a.drop("alias", a.borrow("&a", "a").Borrow)
			},
		},
		{
			name: "copied_alias_preserves_old_loan",
			src: locals + `
	let keep = alias;
	alias = &b;
	@drop alias;
	let probe = &*keep;
	@drop probe;
	return keep.value;
}`,
			check: func(a *sharedRebindAnalysis) {
				a.reference("keep", false)
				a.write("alias = &b", "alias", BorrowIssueNone)
				a.drop("alias", a.borrow("&b", "b").Borrow)
				a.borrow("&*keep", "a")
			},
		},
		{
			name: "old_owner_write_is_refused",
			src: locals + `
	let keep = alias;
	alias = &b;
	@drop alias;
	a.value = 3;
	return keep.value;
}`,
			code:    diag.SemaBorrowMutation,
			refusal: "a.value = 3",
			check: func(a *sharedRebindAnalysis) {
				a.reference("keep", false)
				a.write("alias = &b", "alias", BorrowIssueNone)
				a.drop("alias", a.borrow("&b", "b").Borrow)
				write := a.write("a.value = 3", "a", BorrowIssueFrozen)
				a.sameLoan(write.IssueBorrow, a.borrow("&a", "a").Borrow)
			},
		},
		{
			name: "borrowed_alias_slot_is_refused",
			// A reference parameter has no local referent loan to expand.
			// Consequently &alias records a real loan on the alias slot.
			src: `fn check(seed: &RebindLeaf, replacement: &RebindLeaf) -> int64 {
	let mut alias = seed;
	let holder = &alias;
	alias = replacement;
	return (**holder).value;
}`,
			code:    diag.SemaBorrowMutation,
			refusal: "alias = replacement",
			check: func(a *sharedRebindAnalysis) {
				write := a.write("alias = replacement", "alias", BorrowIssueFrozen)
				a.sameLoan(write.IssueBorrow, a.borrow("&alias", "alias").Borrow)
			},
		},
		{
			name: "projected_shared_store_is_refused",
			src: locals + `
	alias.value = 3;
	return alias.value;
}`,
			code:    diag.SemaStoreThroughSharedRef,
			refusal: "alias.value = 3",
			check: func(a *sharedRebindAnalysis) {
				a.borrow("&a", "a")
				for _, event := range a.result.BorrowEvents {
					if event.Kind == BorrowEvWrite {
						a.t.Fatalf("refused projection reached write bookkeeping: %+v", event)
					}
				}
			},
		},
		{
			name: "mutable_deref_write_through",
			src: `fn check() -> int64 {
	let mut value: int64 = 1;
	let alias: &mut int64 = &mut value;
	*alias = 2;
	@drop alias;
	return value;
}`,
			check: func(a *sharedRebindAnalysis) {
				a.exclusiveWrite("*alias = 2")
			},
		},
		{
			name: "mutable_field_write_through",
			src: `fn check() -> int64 {
	let mut value = RebindLeaf { value = 1 };
	let alias: &mut RebindLeaf = &mut value;
	alias.value = 2;
	@drop alias;
	return value.value;
}`,
			check: func(a *sharedRebindAnalysis) {
				a.exclusiveWrite("alias.value = 2")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := analyzeSharedRebind(t, prefix+tc.src, tc.code, tc.refusal)
			a.reference("alias", strings.HasPrefix(tc.name, "mutable_"))
			tc.check(a)
		})
	}
}

type sharedRebindAnalysis struct {
	t        *testing.T
	src      string
	result   Result
	bindings map[string]symbols.SymbolID
}

func analyzeSharedRebind(t *testing.T, src string, code diag.Code, refusal string) *sharedRebindAnalysis {
	t.Helper()
	builder, fileID, parseBag := parseSnippet(t, src)
	bag := diag.NewBag(32)
	requireNoSemaErrors(t, parseBag, bag)
	syms := symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
	})
	requireNoSemaErrors(t, parseBag, bag)
	result := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: bag}, Symbols: &syms,
	})
	if code == 0 {
		requireNoSemaErrors(t, parseBag, bag)
	} else {
		requireSemaCodeCount(t, bag, code, 1)
		span := bag.Items()[0].Primary
		start := strings.Index(src, refusal)
		if refusal == "" || start < 0 || strings.Count(src, refusal) != 1 || int(span.Start) != start || int(span.End) != start+len(refusal) {
			t.Fatalf("diagnostic must identify %q, got %+v", refusal, span)
		}
	}
	a := &sharedRebindAnalysis{t: t, src: src, result: result, bindings: make(map[string]symbols.SymbolID)}
	for id, ty := range result.BindingTypes {
		sym := syms.Table.Symbols.Get(id)
		if sym == nil || (sym.Kind != symbols.SymbolLet && sym.Kind != symbols.SymbolParam) {
			continue
		}
		name := builder.StringsInterner.MustLookup(sym.Name)
		if ty == types.NoTypeID || a.bindings[name].IsValid() {
			t.Fatalf("missing type or duplicate source binding %q", name)
		}
		a.bindings[name] = id
	}
	if result.TypeInterner == nil || len(result.BorrowEvents) == 0 {
		t.Fatal("source did not reach typed borrow analysis")
	}
	return a
}

func (a *sharedRebindAnalysis) binding(name string) symbols.SymbolID {
	a.t.Helper()
	id := a.bindings[name]
	if !id.IsValid() {
		a.t.Fatalf("missing typed binding %q", name)
	}
	return id
}

func (a *sharedRebindAnalysis) reference(name string, mutable bool) {
	a.t.Helper()
	ty, ok := a.result.TypeInterner.Lookup(a.result.BindingTypes[a.binding(name)])
	if !ok || ty.Kind != types.KindReference || ty.Mutable != mutable {
		a.t.Fatalf("%s must be a resolved reference (mutable=%v), got %+v", name, mutable, ty)
	}
}

func (a *sharedRebindAnalysis) event(kind BorrowEventKind, text string) BorrowEvent {
	a.t.Helper()
	start := strings.Index(a.src, text)
	if start < 0 || strings.Count(a.src, text) != 1 {
		a.t.Fatalf("event source must occur exactly once: %q", text)
	}
	var found []BorrowEvent
	for _, event := range a.result.BorrowEvents {
		if event.Kind == kind && int(event.Span.Start) == start && int(event.Span.End) == start+len(text) {
			found = append(found, event)
		}
	}
	if len(found) != 1 {
		a.t.Fatalf("want one %s at %q, got %d; events=%+v", kind, text, len(found), a.result.BorrowEvents)
	}
	return found[0]
}

func (a *sharedRebindAnalysis) write(text, base string, issue BorrowIssueKind) BorrowEvent {
	a.t.Helper()
	event := a.event(BorrowEvWrite, text)
	if event.Place.Base != a.binding(base) || event.Issue != issue {
		a.t.Fatalf("write %q must target %s with issue %v, got %+v", text, base, issue, event)
	}
	return event
}

func (a *sharedRebindAnalysis) borrow(text, base string) BorrowEvent {
	a.t.Helper()
	event := a.event(BorrowEvBorrowStart, text)
	if event.Place.Base != a.binding(base) || event.Borrow == NoBorrowID || event.Issue != BorrowIssueNone {
		a.t.Fatalf("borrow %q must originate at %s, got %+v", text, base, event)
	}
	for _, info := range a.result.Borrows {
		if info.ID == event.Borrow && info.Place == event.Place {
			// Explicit @drop removes ExprBorrows; the typed source and retained
			// BorrowInfo/event still identify the loan that was created.
			ty, ok := a.result.TypeInterner.Lookup(a.result.ExprTypes[info.Life.FromExpr])
			if !info.Life.FromExpr.IsValid() || info.Span != event.Span || !ok || ty.Kind != types.KindReference {
				a.t.Fatalf("borrow lacks typed expression authority: %+v", info)
			}
			return event
		}
	}
	a.t.Fatalf("missing borrow record: %+v", event)
	return BorrowEvent{}
}

func (a *sharedRebindAnalysis) drop(name string, loan BorrowID) {
	a.t.Helper()
	count := 0
	for _, event := range a.result.BorrowEvents {
		if event.Kind == BorrowEvDrop && event.Binding == a.binding(name) {
			count++
			a.sameLoan(event.Borrow, loan)
		}
	}
	if count != 1 {
		a.t.Fatalf("want one explicit drop of %s, got %d", name, count)
	}
}

func (a *sharedRebindAnalysis) sameLoan(got, want BorrowID) {
	a.t.Helper()
	if got == NoBorrowID || got != want {
		a.t.Fatalf("loan = %v, want live source loan %v", got, want)
	}
}

func (a *sharedRebindAnalysis) exclusiveWrite(text string) {
	a.t.Helper()
	event := a.write(text, "alias", BorrowIssueNone)
	if event.Note != "write_through_mut_ref" || event.Place.Path == "" {
		a.t.Fatalf("exclusive write lost its projection/authority: %+v", event)
	}
}
