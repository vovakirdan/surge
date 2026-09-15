package sema

import (
	"fmt"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// Two mutable cells and a local owner, for the transfer laws P08 cannot show.
const returnOriginTwoCellSource = `fn two(dst: &mut &string, other: &mut &string, replacement: &string) -> &string {
    let local: string = "local";
    let old: &string = *other;
    *dst = replacement;
    return old;
}
`

func cellV(slot uint32) returnOrigin {
	return returnOrigin{kind: returnOriginParam, param: slot}
}

func cellR(slot uint32) returnOrigin {
	return returnOrigin{kind: returnOriginParam, param: slot, selector: returnOriginInputContents}
}

var cellUnknown = returnOrigin{kind: returnOriginUnknown}

func checkCell(t *testing.T, label string, got returnOriginValue, roots ...returnOrigin) {
	t.Helper()
	if want := returnOriginValueOf(roots...); !got.equal(want) {
		t.Errorf("%s=%+v, want %+v", label, got.roots, want.roots)
	}
}

// cellBodyEnv binds each formal to its incoming value and seeds its cells, as
// analyze does before the body's first statement.
func cellBodyEnv(fn *returnOriginFunction) returnOriginEnv {
	env := newReturnOriginEnv()
	slot := uint32(0)
	for _, param := range fn.params {
		env = env.assign(param, fn.scope, returnOriginValueOf(cellV(slot)))
		slot++
	}
	return fn.initExternalCells(env)
}

// cellExprsAt returns fn's typed P08 expressions of one kind spelled exactly as
// want, in source order.
func cellExprsAt(t *testing.T, fn *returnOriginFunction, kind ast.ExprKind, want string) []ast.ExprID {
	t.Helper()
	u, text := fn.unit, returnOriginExternalCellSource
	var found []ast.ExprID
	for id := range u.Sema.ExprTypes {
		node := u.Builder.Exprs.Get(id)
		if node != nil && node.Kind == kind && node.Span.Start >= fn.item.Span.Start && node.Span.End <= fn.item.Span.End &&
			int(node.Span.End) <= len(text) && text[node.Span.Start:node.Span.End] == want {
			found = append(found, id)
		}
	}
	slices.SortFunc(found, func(a, b ast.ExprID) int {
		return compareReturnOriginSpans(u.Builder.Exprs.Get(a).Span, u.Builder.Exprs.Get(b).Span)
	})
	if len(found) == 0 {
		t.Fatalf("PRECONDITION: %s has no typed %q", fn.name, want)
	}
	return found
}

func cellPendingAt(a *returnOriginAnalyzer, span source.Span, reason string) bool {
	return slices.Contains(a.report.Pending, ReturnOriginPending{SourceKey: a.units[0].SourceKey, Span: span, Reason: reason})
}

func returnOriginTwoCell(t *testing.T) (*returnOriginAnalyzer, *returnOriginFunction) {
	t.Helper()
	a := returnOriginConditionAnalyzer(t, returnOriginTwoCellSource)
	if len(a.functions) != 1 || len(a.report.Diagnostics) != 0 {
		t.Fatalf("PRECONDITION: the two-cell fixture was not ordinarily admitted: functions=%d diagnostics=%+v", len(a.functions), a.report.Diagnostics)
	}
	two := a.functions[0]
	fact := a.summaries[two.key]
	t.Logf("CELL_TWO body=%s cells=%v mutable=%v result=%+v posts=%+v pending=%+v", two.key, two.cellSlots, two.mutableCellSlots, fact.value.roots, fact.postCells, a.report.Pending)
	if two.name != "two" || !slices.Equal(two.cellSlots, []uint32{0, 1}) || !slices.Equal(two.mutableCellSlots, []uint32{0, 1}) {
		t.Fatal("PRECONDITION: two lacks its two mutable cell formals")
	}
	return a, two
}

func TestReturnOriginExternalCellFacts(t *testing.T) {
	t.Run("untouched_normal_join", func(t *testing.T) {
		a := returnOriginConditionAnalyzer(t, returnOriginExternalCellSource)
		fn := checkReturnOriginCellAuthority(t, a)["read_new"]
		identity := cellBodyEnv(fn)
		written := identity.withCell(0, returnOriginValueOf(cellV(1)))
		// One arm wrote C0 and the other left it as it came in: both survive.
		checkCell(t, "written|identity C0", written.join(identity).cell(0), cellR(0), cellV(1))
		checkCell(t, "identity|written C0", identity.join(written).cell(0), cellR(0), cellV(1))
		// A reachable arm that never held C0 knows nothing about it.
		checkCell(t, "written|unseeded C0", written.join(newReturnOriginEnv()).cell(0), cellUnknown, cellV(1))
		checkCell(t, "written|unreachable C0", written.join(returnOriginEnv{}).cell(0), cellV(1))
	})
	t.Run("bottom_vs_normal", func(t *testing.T) {
		a := returnOriginConditionAnalyzer(t, returnOriginExternalCellSource)
		fn := checkReturnOriginCellAuthority(t, a)["read_old"]
		fact := a.summaries[fn.key]
		digest := fmt.Sprintf("%+v", fact)
		if post, ok := fact.postCells[0]; !fact.value.normal || len(fact.postCells) != 1 || !ok || !post.normal {
			t.Fatalf("PRECONDITION: read_old lacks its complete normal post vector: %s", digest)
		}
		bottom := returnOriginSummaryFact{}
		for i, joined := range []returnOriginSummaryFact{fact.join(bottom), bottom.join(fact), fact.join(bottom).join(fact).join(bottom)} {
			if !joined.equal(fact) {
				t.Errorf("join %d with bottom changed the normal fact: %+v", i, joined)
			}
		}
		if twice := bottom.join(bottom); twice.value.normal || len(twice.postCells) != 0 {
			t.Errorf("bottom gained a normal continuation or post entries: %+v", twice)
		}
		if fmt.Sprintf("%+v", fact) != digest {
			t.Error("joining a fact aliased its input")
		}
	})
	t.Run("precall_alias_merge", func(t *testing.T) {
		a, two := returnOriginTwoCell(t)
		fact := a.summaries[two.key]
		checkCell(t, "two result", fact.value, cellR(1))
		checkCell(t, "two post C0", fact.postCells[0], cellV(2))
		checkCell(t, "two post C1", fact.postCells[1], cellR(1), cellV(2))
		b := &returnOriginBody{analyzer: a, function: two}
		pre := cellBodyEnv(two)
		// Both formals name caller cell 0: the result is C0 before the call, and
		// both posts union into C0 before its one replacement.
		same := returnOriginCellCall{callee: two, targets: map[int][]uint32{0: {0}, 1: {0}}}
		actuals := []returnOriginValue{returnOriginValueOf(cellV(0)), returnOriginValueOf(cellV(0)), returnOriginValueOf(cellV(2))}
		value, after := b.instantiateCellCall(same, two.info.Result, fact.value, fact.postCells, actuals, pre, two.item.Span)
		checkCell(t, "same-target result", value, cellR(0))
		checkCell(t, "same-target C0", after.cell(0), cellR(0), cellV(2))
		checkCell(t, "same-target unnamed C1", after.cell(1), cellR(0), cellR(1), cellV(2))
		// One formal may name either cell, so nothing it reaches is replaced.
		multi := returnOriginCellCall{callee: two, targets: map[int][]uint32{0: {0, 1}, 1: {1}}}
		actuals[0], actuals[1] = returnOriginValueOf(cellV(0), cellV(1)), returnOriginValueOf(cellV(1))
		value, after = b.instantiateCellCall(multi, two.info.Result, fact.value, fact.postCells, actuals, pre, two.item.Span)
		checkCell(t, "multi-target result", value, cellR(1))
		checkCell(t, "multi-target C0", after.cell(0), cellR(0), cellV(2))
		checkCell(t, "multi-target C1", after.cell(1), cellR(1), cellV(2))
		checkCell(t, "frozen pre C0", pre.cell(0), cellR(0))
		checkCell(t, "frozen pre C1", pre.cell(1), cellR(1))
	})
	t.Run("external_cell_scope_exit", func(t *testing.T) {
		a, two := returnOriginTwoCell(t)
		u := two.unit
		var localID symbols.SymbolID
		for _, id := range u.stmtSymbols {
			sym := u.Symbols.Table.Symbols.Get(id)
			if name, _ := u.Symbols.Table.Strings.Lookup(sym.Name); sym.Kind == symbols.SymbolLet && name == "local" {
				localID = id
			}
		}
		local := u.Symbols.Table.Symbols.Get(localID)
		if local == nil {
			t.Fatal("PRECONDITION: two has no local string owner")
		}
		b := &returnOriginBody{analyzer: a, function: two}
		// Detached abstract input: a loan of `local` stored in the caller's C0.
		env := cellBodyEnv(two).withCell(0, returnOriginValueOf(returnOrigin{kind: returnOriginLocal, binding: localID, scope: local.Scope}))
		a.report.Diagnostics = nil
		closed := b.closeOutcome(returnOriginOutcome{env: env, value: returnOriginValueOf()}, local.Scope, two.item.Span)
		if cell, kept := closed.env.cells[0]; !kept || len(cell.roots) != 1 || cell.roots[0].kind != returnOriginLocal || !cell.roots[0].expired {
			t.Errorf("scope exit lost C0 or left its local loan live: %+v", closed.env.cells)
		}
		if len(a.report.Diagnostics) != 1 || a.report.Diagnostics[0].Code != diag.SemaBorrowEscapesReturn ||
			len(a.report.Diagnostics[0].Notes) != 1 || a.report.Diagnostics[0].Notes[0].Span != local.Span {
			t.Errorf("a local loan in C0 escaped without its owner diagnostic: %+v", a.report.Diagnostics)
		}
		whole := b.closeOutcome(returnOriginOutcome{env: env, value: returnOriginValueOf()}, two.scope, two.item.Span)
		if _, kept := whole.env.cells[0]; !kept || len(whole.env.bindings) != 0 {
			t.Errorf("closing the function dropped C0 or kept a formal binding: cells=%+v bindings=%d", whole.env.cells, len(whole.env.bindings))
		}
	})
	t.Run("missing_normal_poststate", func(t *testing.T) {
		a := returnOriginConditionAnalyzer(t, returnOriginExternalCellSource)
		bodies := checkReturnOriginCellAuthority(t, a)
		probe, callee := bodies["probe"], bodies["returned_alias"]
		original := a.summaries[callee.key]
		digest := fmt.Sprintf("%+v", original)
		detached := original.join(returnOriginSummaryFact{})
		delete(detached.postCells, 0)
		callID := cellExprsAt(t, probe, ast.ExprCall, "returned_alias(dst, replacement)")[0]
		span := probe.unit.Builder.Exprs.Get(callID).Span
		b := &returnOriginBody{analyzer: a, function: probe}
		a.summaries[callee.key] = detached
		out, err := b.call(callID, cellBodyEnv(probe), returnOriginTargets{scope: probe.scope})
		a.summaries[callee.key] = original
		if err != nil {
			t.Fatal(err)
		}
		if !cellPendingAt(a, span, "external cell call lacks its callee's complete post-state") {
			t.Errorf("a normal callee without its C0 post entry left no Pending: %+v", a.report.Pending)
		}
		checkCell(t, "C0 after a missing post entry", out.flow.normal.cell(0), cellUnknown)
		if fmt.Sprintf("%+v", a.summaries[callee.key]) != digest {
			t.Error("detaching the callee summary changed its original fact")
		}
	})
	t.Run("unknown_mutable_taint", func(t *testing.T) {
		a := returnOriginConditionAnalyzer(t, returnOriginExternalCellSource)
		bodies := checkReturnOriginCellAuthority(t, a)
		probe, callee, readNew := bodies["probe"], bodies["returned_alias"], bodies["read_new"]
		b := &returnOriginBody{analyzer: a, function: probe}
		env := cellBodyEnv(probe)
		callID := cellExprsAt(t, probe, ast.ExprCall, "returned_alias(dst, replacement)")[0]
		span := probe.unit.Builder.Exprs.Get(callID).Span
		tainted := b.taintExternalCellEffects(env, span, "mutable argument may replace reference-bearing contents")
		if loaded, handled := b.loadExternalCells(probe.info.Params[0], returnOriginValueOf(cellV(0)), tainted, span); !handled ||
			!slices.Contains(loaded.roots, cellUnknown) || len(a.report.Pending) == 0 {
			t.Errorf("an unproved effect left a precise later load or a complete report: %+v pending=%+v", loaded.roots, a.report.Pending)
		}
		// A known target joined with Unknown proves no singleton to replace.
		store := cellExprsAt(t, readNew, ast.ExprBinary, "*dst = replacement")[0]
		data, _ := readNew.unit.Builder.Exprs.Binary(store)
		rb := &returnOriginBody{analyzer: a, function: readNew}
		mixed := returnOriginValueOf(cellV(0), cellUnknown)
		if next, stored := rb.storeExternalCells(data.Left, mixed, returnOriginValueOf(cellV(1)), cellBodyEnv(readNew)); stored {
			t.Errorf("a target set with Unknown replaced C0: %+v", next.cell(0).roots)
		}
		// An unconsumed callee R(i) is refused rather than read as V(i).
		derefs := cellExprsAt(t, probe, ast.ExprUnary, "*alias")
		loadID := derefs[len(derefs)-1]
		required := b.required
		value, next, refused := b.refuseLegacyCellSummary(loadID, returnOriginValueOf(cellR(0)), env, span)
		if !refused || !cellPendingAt(a, span, "referent-content result lacks its checked cell call transfer") {
			t.Error("a normal callee R(0) reached the legacy V substitution")
		}
		checkCell(t, "refused callee R result", value, cellUnknown)
		checkCell(t, "C0 after a refused callee R", next.cell(0), cellR(0), cellUnknown)
		// A callee V0 copies what the caller passed, even the caller's own R0.
		if _, _, refused := b.refuseLegacyCellSummary(loadID, returnOriginValueOf(cellV(0)), env, span); refused {
			t.Error("a callee V0 was mistaken for referent contents")
		}
		copied := b.substituteCellCall(returnOriginCellCall{callee: callee}, returnOriginValueOf(cellV(0)),
			[]returnOriginValue{returnOriginValueOf(cellR(0)), returnOriginValueOf()}, env, span)
		checkCell(t, "callee V0 over caller R0", copied, cellR(0))
		// Bottom has no normal continuation: neither post-state nor a Cell refusal.
		pending := len(a.report.Pending)
		original := a.summaries[callee.key]
		a.summaries[callee.key] = returnOriginSummaryFact{}
		out, err := b.call(callID, env, returnOriginTargets{scope: probe.scope})
		a.summaries[callee.key] = original
		if err != nil || out.flow.normal.reachable || len(a.report.Pending) != pending {
			t.Errorf("a bottom callee continued or was refused: err=%v reachable=%v pending=%+v", err, out.flow.normal.reachable, a.report.Pending[pending:])
		}
		if !b.required.equal(required) {
			t.Errorf("cell boundaries changed inherited requirements: %+v, want %+v", b.required, required)
		}
	})
}
