package sema

import (
	"fmt"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Stage B of the backing transfer (packet R4 §7.6): private laws on core-free
// snippets. Each snippet's ordinary admission is unmeasured and fails visibly.
const returnOriginBackingRosterSource = `fn roster(dst: &mut uint64[], src: &uint64[], cell: &mut &string, owned: uint64[], value: &string) -> nothing {
    let local: string = "local";
    return nothing;
}
`

// merge keeps R4's descriptors; its two locals are the real Local targets B5/B6 need.
const returnOriginBackingMergeSource = `pragma no_std;
tag Has<T>(T);
type Opt<T> = Has(T) | nothing;
fn merge(a: &mut Opt<&string>[], b: &Opt<&string>[], v: &string) -> nothing {
    let mut first: Opt<&string>[] = [];
    let second: Opt<&string>[] = [];
    return nothing;
}
`

// root is the ordinary root body the condition fixture needs to seed its closure.
const returnOriginBackingShapeSource = `type Tagged<T> = { n: uint64 };
fn probe<T>(a: T[], b: Tagged<T>[], c: uint64[], d: &string) -> nothing {
    return nothing;
}
fn root() -> nothing {
    return nothing;
}
`

func backingFactRoot(slot uint32, selector returnOriginInputSelector) returnOrigin {
	return returnOrigin{kind: returnOriginParam, param: slot, selector: selector}
}

func backingFactLocalRoot(id symbols.SymbolID, sym *symbols.Symbol) returnOrigin {
	return returnOrigin{kind: returnOriginLocal, binding: id, scope: sym.Scope}
}

func backingFactFunction(t *testing.T, text, name string) (*returnOriginAnalyzer, *returnOriginFunction) {
	t.Helper()
	a := returnOriginConditionAnalyzer(t, text)
	for _, fn := range a.functions {
		if fn.name == name {
			t.Logf("BACKING_FACT body=%s backings=%v mutable=%v cells=%v pending=%+v diagnostics=%+v",
				fn.key, fn.backingSlots, fn.mutableBackingSlots, fn.cellSlots, a.report.Pending, a.report.Diagnostics)
			return a, fn
		}
	}
	t.Fatalf("PRECONDITION: body %s is missing", name)
	return nil, nil
}

// backingFactContainer reads a descriptor through the canonical-container identity G1 uses.
func backingFactContainer(t *testing.T, fn *returnOriginFunction, id types.TypeID) returnOriginIndexType {
	t.Helper()
	in := fn.unit.Sema.TypeInterner
	container, ok := returnOriginIndexContainer(in, id)
	if !ok || container.family == in.Builtins().String {
		t.Fatalf("PRECONDITION: type %d is not a canonical container: %+v", id, container)
	}
	return container
}

// backingFactEnv binds each formal to its incoming value and seeds its cells and
// backings, as analyze does before the body's first statement.
func backingFactEnv(fn *returnOriginFunction) returnOriginEnv {
	env := newReturnOriginEnv()
	slot := uint32(0)
	for _, param := range fn.params {
		env = env.assign(param, fn.scope, returnOriginValueOf(backingFactRoot(slot, returnOriginInputValue)))
		slot++
	}
	return fn.initBackings(fn.initExternalCells(env))
}

func backingFactLocal(t *testing.T, fn *returnOriginFunction, name string) (symbols.SymbolID, *symbols.Symbol) {
	t.Helper()
	u := fn.unit
	for _, id := range u.stmtSymbols {
		sym := u.Symbols.Table.Symbols.Get(id)
		if text, _ := u.Symbols.Table.Strings.Lookup(sym.Name); sym.Kind == symbols.SymbolLet && text == name {
			return id, sym
		}
	}
	t.Fatalf("PRECONDITION: %s has no local %s", fn.name, name)
	return symbols.NoSymbolID, nil
}

func TestReturnOriginBackingFacts(t *testing.T) {
	v := func(slot uint32) returnOrigin { return backingFactRoot(slot, returnOriginInputValue) }
	e := func(slot uint32) returnOrigin { return backingFactRoot(slot, returnOriginInputElements) }
	l := func(slot uint32) returnOrigin { return backingFactRoot(slot, returnOriginInputLoans) }
	unknown := returnOrigin{kind: returnOriginUnknown}
	const discard = "storage loan would be discarded by a payload-free value"
	t.Run("roster_descriptors", func(t *testing.T) {
		_, fn := backingFactFunction(t, returnOriginBackingRosterSource, "roster")
		if !slices.Equal(fn.backingSlots, []uint32{0, 1}) || !slices.Equal(fn.mutableBackingSlots, []uint32{0}) ||
			!slices.Equal(fn.cellSlots, []uint32{2}) || slices.Contains(fn.backingSlots, 3) {
			t.Errorf("roster backings=%v mutable=%v cells=%v, want [0 1] [0] [2] with owned absent",
				fn.backingSlots, fn.mutableBackingSlots, fn.cellSlots)
		}
	})
	t.Run("env_lattice", func(t *testing.T) {
		_, fn := backingFactFunction(t, returnOriginBackingRosterSource, "roster")
		seeded := backingFactEnv(fn)
		checkCell(t, "seeded B0", seeded.backing(0), e(0))
		checkCell(t, "missing B5", seeded.backing(5), unknown)
		if bottom := (returnOriginEnv{}).backing(0); bottom.normal || len(bottom.roots) != 0 {
			t.Errorf("an unreachable environment answered a backing: %+v", bottom)
		}
		written := seeded.withBacking(0, returnOriginValueOf(v(4)))
		checkCell(t, "written|unreachable B0", written.join(returnOriginEnv{}).backing(0), v(4))
		checkCell(t, "written|unseeded B0", written.join(newReturnOriginEnv()).backing(0), unknown, v(4))
		copied := written.clone()
		copied.backings[0] = returnOriginValueOf(v(1))
		checkCell(t, "clone detaches B0", written.backing(0), v(4))
		if written.equal(seeded) || !written.equal(written.clone()) {
			t.Error("environment equality ignores backings")
		}
	})
	t.Run("scope_exit", func(t *testing.T) {
		a, fn := backingFactFunction(t, returnOriginBackingRosterSource, "roster")
		localID, local := backingFactLocal(t, fn, "local")
		b := &returnOriginBody{analyzer: a, function: fn}
		env := backingFactEnv(fn).withBacking(0, returnOriginValueOf(backingFactLocalRoot(localID, local)))
		a.report.Diagnostics = nil
		closed := b.closeOutcome(returnOriginOutcome{env: env, value: returnOriginValueOf()}, local.Scope, fn.item.Span)
		if backing, kept := closed.env.backings[0]; !kept || len(backing.roots) != 1 || backing.roots[0].kind != returnOriginLocal || !backing.roots[0].expired {
			t.Errorf("scope exit lost B0 or left its local loan live: %+v", closed.env.backings)
		}
		if len(a.report.Diagnostics) != 1 || a.report.Diagnostics[0].Code != diag.SemaBorrowEscapesReturn ||
			len(a.report.Diagnostics[0].Notes) != 1 || a.report.Diagnostics[0].Notes[0].Span != local.Span {
			t.Errorf("a local loan in B0 escaped without its owner diagnostic: %+v", a.report.Diagnostics)
		}
		whole := b.closeOutcome(returnOriginOutcome{env: env, value: returnOriginValueOf()}, fn.scope, fn.item.Span)
		if _, kept := whole.env.backings[0]; !kept {
			t.Errorf("closing the function dropped B0: %+v", whole.env.backings)
		}
	})
	t.Run("posts_bottom_vs_normal", func(t *testing.T) {
		a, fn := backingFactFunction(t, returnOriginBackingRosterSource, "roster")
		fact := a.summaries[fn.key]
		digest := fmt.Sprintf("%+v", fact)
		if post, ok := fact.postBackings[0]; !fact.value.normal || len(fact.postBackings) != 1 || !ok || !post.normal {
			t.Fatalf("PRECONDITION: roster lacks its complete normal backing post: %s", digest)
		}
		checkCell(t, "untouched post B0", fact.postBackings[0], e(0))
		bottom := returnOriginSummaryFact{}
		for i, joined := range []returnOriginSummaryFact{fact.join(bottom), bottom.join(fact), fact.join(bottom).join(fact).join(bottom)} {
			if !joined.equal(fact) {
				t.Errorf("join %d with bottom changed the normal fact: %+v", i, joined)
			}
		}
		if twice := bottom.join(bottom); twice.value.normal || len(twice.postBackings) != 0 {
			t.Errorf("bottom gained a normal continuation or backing posts: %+v", twice)
		}
		if fmt.Sprintf("%+v", fact) != digest {
			t.Error("joining a fact aliased its input")
		}
	})
	t.Run("weak_store_never_replaces", func(t *testing.T) {
		a, fn := backingFactFunction(t, returnOriginBackingMergeSource, "merge")
		if !slices.Equal(fn.backingSlots, []uint32{0, 1}) || !slices.Equal(fn.mutableBackingSlots, []uint32{0}) {
			t.Fatalf("PRECONDITION: merge lacks its two container formals: %v %v", fn.backingSlots, fn.mutableBackingSlots)
		}
		b := &returnOriginBody{analyzer: a, function: fn}
		firstID, first := backingFactLocal(t, fn, "first")
		env := backingFactEnv(fn).assign(firstID, first.Scope, returnOriginValueOf(v(2)))
		formalContainer := backingFactContainer(t, fn, fn.info.Params[0])
		formal, proven := b.backingTargets(formalContainer, returnOriginValueOf(v(0)), env, true)
		if !proven {
			t.Fatal("PRECONDITION: formal 0 is not a proven mutable backing target")
		}
		next := b.storeBackingContents(env, formalContainer, formal, returnOriginValueOf(v(2)), nil, fn.item.Span)
		checkCell(t, "formal store B0", next.backing(0), e(0), v(2))
		checkCell(t, "formal store reaches B1", next.backing(1), e(1), v(2))
		container := backingFactContainer(t, fn, fn.unit.Sema.BindingTypes[firstID])
		local, proven := b.backingTargets(container, returnOriginValueOf(backingFactLocalRoot(firstID, first)), env, true)
		if !proven {
			t.Fatal("PRECONDITION: first is not a proven local backing target")
		}
		again := b.storeBackingContents(next, container, local, returnOriginValueOf(e(0)), nil, fn.item.Span)
		checkCell(t, "local store keeps first", again.value(firstID), v(2), e(0))
		checkCell(t, "local store leaves B0", again.backing(0), e(0), v(2))
		checkCell(t, "frozen env B0", env.backing(0), e(0))
	})
	t.Run("instantiate_weak_merge", func(t *testing.T) {
		a, fn := backingFactFunction(t, returnOriginBackingMergeSource, "merge")
		b := &returnOriginBody{analyzer: a, function: fn}
		if b.elementsFree(backingFactContainer(t, fn, fn.info.Params[0])) {
			t.Fatal("PRECONDITION: Opt<&string> elements were read as payload-free")
		}
		firstID, first := backingFactLocal(t, fn, "first")
		secondID, second := backingFactLocal(t, fn, "second")
		pre := backingFactEnv(fn).assign(firstID, first.Scope, returnOriginValueOf(v(1))).assign(secondID, second.Scope, returnOriginValueOf(v(2)))
		firstRoot := returnOriginValueOf(backingFactLocalRoot(firstID, first))
		secondRoot := returnOriginValueOf(backingFactLocalRoot(secondID, second))
		firstContainer := backingFactContainer(t, fn, fn.unit.Sema.BindingTypes[firstID])
		secondContainer := backingFactContainer(t, fn, fn.unit.Sema.BindingTypes[secondID])
		toFirst, firstProven := b.backingTargets(firstContainer, firstRoot, pre, true)
		toSecond, secondProven := b.backingTargets(secondContainer, secondRoot, pre, false)
		if !firstProven || !secondProven {
			t.Fatal("PRECONDITION: the two locals are not proven backing targets")
		}
		call := returnOriginBackingCall{callee: fn,
			containers: map[int]returnOriginIndexType{0: firstContainer, 1: secondContainer},
			targets:    map[int]returnOriginBackingTargets{0: toFirst, 1: toSecond}}
		posts := map[uint32]returnOriginValue{0: returnOriginValueOf(e(0), e(1))}
		actuals := []returnOriginValue{firstRoot, secondRoot, returnOriginValueOf()}
		span := fn.item.Span
		value, after := b.instantiateBackingCall(call, returnOriginValueOf(e(0)), posts, actuals, pre, span)
		checkCell(t, "E0 result read from PRE", value, v(1))
		checkCell(t, "first after the post", after.value(firstID), v(1), v(2))
		checkCell(t, "second after the post", after.value(secondID), v(2))
		checkCell(t, "frozen PRE first", pre.value(firstID), v(1))
		loans, _ := b.instantiateBackingCall(call, returnOriginValueOf(l(1)), posts, actuals, pre, span)
		checkCell(t, "L1 result read from PRE", loans, v(2))
		for _, root := range []returnOrigin{e(0), l(1)} {
			refused, _, ok := b.refuseLegacyBackingSummary(nil, returnOriginValueOf(root), nil, nil, pre, types.NoTypeID, span)
			if !ok || !cellPendingAt(a, span, "container-content result lacks its checked backing call transfer") {
				t.Errorf("an unconsumed %+v reached the legacy V substitution", root)
			}
			checkCell(t, "refused legacy result", refused, unknown)
		}
		if _, _, ok := b.refuseLegacyBackingSummary(nil, returnOriginValueOf(v(0)), nil, nil, pre, types.NoTypeID, span); ok {
			t.Error("a callee V0 was refused as container contents")
		}
	})
	t.Run("elements_free_and_loans", func(t *testing.T) {
		a, fn := backingFactFunction(t, returnOriginBackingRosterSource, "roster")
		b := &returnOriginBody{analyzer: a, function: fn}
		if !b.elementsFree(backingFactContainer(t, fn, fn.info.Params[1])) {
			t.Error("&uint64[] elements were not read as payload-free")
		}
		checkCell(t, "loans of the payload-free formal", b.containerLoans(returnOriginValueOf(v(1)), backingFactEnv(fn), fn.item.Span), l(1))
		localID, local := backingFactLocal(t, fn, "local")
		checkCell(t, "discarded loan", b.discardLoans(returnOriginValueOf(backingFactLocalRoot(localID, local)), fn.item.Span))
		if !cellPendingAt(a, fn.item.Span, discard) {
			t.Errorf("a discarded local loan left no Pending: %+v", a.report.Pending)
		}
	})
	t.Run("generic_container_shape", func(t *testing.T) {
		_, fn := backingFactFunction(t, returnOriginBackingShapeSource, "probe")
		in := fn.unit.Sema.TypeInterner
		if len(fn.info.Params) != 4 || fn.candidate == nil || len(fn.candidate.TemplateParams) != 1 {
			t.Fatal("PRECONDITION: probe lost its four formals or its one template parameter")
		}
		for i := range 3 {
			if c := backingFactContainer(t, fn, fn.info.Params[i]); c.reference || c.family != in.ArrayNominalType() {
				t.Fatalf("PRECONDITION: probe formal %d is not a canonical Array: %+v", i, c)
			}
		}
		template := returnOriginView(fn)
		for i, want := range []returnOriginShape{returnOriginCarriesRef, returnOriginCarriesRef, returnOriginRefFree} {
			if got := template.shape(fn.info.Params[i]); got != want {
				t.Errorf("template shape of formal %d = %d, want %d", i, got, want)
			}
		}
		if got := returnOriginBoundView(fn, nil, []types.TypeID{in.Builtins().Int}).shape(fn.info.Params[0]); got != returnOriginRefFree {
			t.Errorf("T[] bound to int has shape %d, want RefFree", got)
		}
		if got := returnOriginBoundView(fn, nil, []types.TypeID{fn.info.Params[3]}).shape(fn.info.Params[0]); got != returnOriginCarriesRef {
			t.Errorf("T[] bound to &string has shape %d, want CarriesRef", got)
		}
		if children, valid := returnOriginTypeChildren(fn, fn.info.Params[0]); valid || len(children) != 0 {
			t.Errorf("canonical Array gained children %v", children)
		}
		if !template.requirement(returnOriginNoBorrowedState, fn.info.Params[0]).unsupported {
			t.Error("canonical Array became NoBorrowedState-classifiable")
		}
	})
}
