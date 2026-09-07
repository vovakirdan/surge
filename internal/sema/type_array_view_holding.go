package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
)

// A view reaches a crossing capture without ever being the value a `let` was
// bound to, and two such routes were measured on this tree. Both compiled, both
// crossed into an `on` body, and in both the destination shard wrote through
// into the origin shard's own buffer at 2 and at 8 shards:
//
//	let v: int[] = base[[1..3]];
//	let xs: int[][] = [v];             // the ARRAY that crosses HOLDS the view
//
//	let pair: (int[], int) = (base[[1..3]], 1);
//	let v: int[] = own pair.0;         // the view came back out of a TUPLE
//
// So the checker keeps a second fact beside "this binding is a view": this
// binding HOLDS one, and where. Taking the holder apart -- indexing the array,
// reading the tuple position -- produces a view again, which is what makes the
// second route answer the ordinary view rule.
//
// Like the gate's own marker this is a MAY fact, and like it, it is read by the
// crossing gate and by nothing else. An index read out of an array holder is a
// view whatever the subscript says, because which slot a subscript names is not
// a question this checker answers; a tuple position is exact, because a tuple
// literal writes every position down. That guess is affordable only where the
// runtime's view registry stands behind it: handed to the resize rule instead,
// it refused `xs[1].push(9)` on an `int[][]` whose position 0 held a view, where
// element 1 owned its own buffer and growing it could not touch anything else.
// So `isArrayViewExpr` does not consult it; `mayBeArrayView` does.
type arrayViewHolding struct {
	// elem names the view being held, when the view had a name of its own. A
	// slice written straight into the literal has none, and the diagnostic says
	// "one of its elements" instead.
	elem symbols.SymbolID
	// positions are the tuple positions that hold a view. A nil map means the
	// holder is an ARRAY, where every element read has to be treated as one.
	positions map[uint32]struct{}
}

func (h arrayViewHolding) holdsPosition(index uint32) bool {
	if h.positions == nil {
		return true
	}
	_, ok := h.positions[index]
	return ok
}

// noteArrayViewHolding carries the holding fact to a binding. Like the view
// marker it only ever adds, and for the same reason: a rebind on one branch
// does not empty the value the other branch put there.
func (tc *typeChecker) noteArrayViewHolding(symID symbols.SymbolID, valueExpr ast.ExprID) {
	if !symID.IsValid() || tc.arrayViewHolders == nil {
		return
	}
	if holding, ok := tc.arrayViewHoldingOf(valueExpr); ok {
		tc.arrayViewHolders[symID] = holding
	}
}

// arrayViewHolderOf is the capture gate's question: does this binding hold a
// view, and which one is it?
func (tc *typeChecker) arrayViewHolderOf(symID symbols.SymbolID) (arrayViewHolding, bool) {
	if !symID.IsValid() || tc.arrayViewHolders == nil {
		return arrayViewHolding{}, false
	}
	holding, ok := tc.arrayViewHolders[symID]
	return holding, ok
}

// arrayViewHoldingOf walks ONE expression for a view sitting inside the value it
// produces. It descends only into children -- literal elements and the arms of a
// choice -- so it walks a finite tree and stops.
func (tc *typeChecker) arrayViewHoldingOf(expr ast.ExprID) (arrayViewHolding, bool) {
	if tc.builder == nil || tc.builder.Exprs == nil {
		return arrayViewHolding{}, false
	}
	expr = tc.unwrapArrayViewExpr(expr)
	if !expr.IsValid() {
		return arrayViewHolding{}, false
	}
	if symID := tc.symbolForExpr(expr); symID.IsValid() {
		if holding, ok := tc.arrayViewHolderOf(symID); ok {
			return holding, true
		}
	}
	if array, ok := tc.builder.Exprs.Array(expr); ok && array != nil {
		for _, element := range array.Elements {
			if named, held := tc.arrayViewInsideElement(element); held {
				return arrayViewHolding{elem: named}, true
			}
		}
		return arrayViewHolding{}, false
	}
	if tuple, ok := tc.builder.Exprs.Tuple(expr); ok && tuple != nil {
		return tc.tupleArrayViewHolding(tuple)
	}
	for _, arm := range tc.arrayViewChoiceArms(expr) {
		if holding, ok := tc.arrayViewHoldingOf(arm); ok {
			return holding, true
		}
	}
	return arrayViewHolding{}, false
}

func (tc *typeChecker) tupleArrayViewHolding(tuple *ast.ExprTupleData) (arrayViewHolding, bool) {
	holding := arrayViewHolding{positions: make(map[uint32]struct{}, len(tuple.Elements))}
	for i, element := range tuple.Elements {
		named, held := tc.arrayViewInsideElement(element)
		if !held {
			continue
		}
		holding.positions[uint32(i)] = struct{}{}
		if !holding.elem.IsValid() {
			holding.elem = named
		}
	}
	if len(holding.positions) == 0 {
		return arrayViewHolding{}, false
	}
	return holding, true
}

// arrayViewInsideElement answers about ONE element of a literal: is it a view,
// or does it hold one? The symbol it returns is the view's own name, which is
// what the refusal quotes back to the reader.
func (tc *typeChecker) arrayViewInsideElement(element ast.ExprID) (symbols.SymbolID, bool) {
	if tc.mayBeArrayView(element) {
		return tc.symbolForExpr(tc.unwrapArrayViewExpr(element)), true
	}
	if holding, ok := tc.arrayViewHoldingOf(element); ok {
		return holding.elem, true
	}
	return symbols.NoSymbolID, false
}

// arrayViewReadOutOfAHolder answers whether an expression reads a view back OUT
// of a value that holds one.
func (tc *typeChecker) arrayViewReadOutOfAHolder(expr ast.ExprID) bool {
	if tc.builder == nil || tc.builder.Exprs == nil || !expr.IsValid() {
		return false
	}
	if tupleIndex, ok := tc.builder.Exprs.TupleIndex(expr); ok && tupleIndex != nil {
		holding, held := tc.arrayViewHoldingOf(tupleIndex.Target)
		return held && holding.holdsPosition(tupleIndex.Index)
	}
	if index, ok := tc.builder.Exprs.Index(expr); ok && index != nil {
		holding, held := tc.arrayViewHoldingOf(index.Target)
		return held && holding.positions == nil
	}
	return false
}

// arrayViewChoiceArms returns the values an expression may evaluate to when it
// is a CHOICE -- a `compare`, a ternary, or a block whose tail is an expression.
// Which arm runs is not a question this checker answers, so every arm is a value
// the name may end up holding.
func (tc *typeChecker) arrayViewChoiceArms(expr ast.ExprID) []ast.ExprID {
	if tc.builder == nil || tc.builder.Exprs == nil || !expr.IsValid() {
		return nil
	}
	if compare, ok := tc.builder.Exprs.Compare(expr); ok && compare != nil {
		arms := make([]ast.ExprID, 0, len(compare.Arms))
		for _, arm := range compare.Arms {
			arms = append(arms, arm.Result)
		}
		return arms
	}
	if ternary, ok := tc.builder.Exprs.Ternary(expr); ok && ternary != nil {
		return []ast.ExprID{ternary.TrueExpr, ternary.FalseExpr}
	}
	if block, ok := tc.builder.Exprs.Block(expr); ok && block != nil {
		return tc.arrayViewBlockTail(block)
	}
	return nil
}

func (tc *typeChecker) arrayViewBlockTail(block *ast.ExprBlockData) []ast.ExprID {
	if len(block.Stmts) == 0 || tc.builder.Stmts == nil {
		return nil
	}
	tail := tc.builder.Stmts.Expr(block.Stmts[len(block.Stmts)-1])
	if tail == nil || !tail.Expr.IsValid() {
		return nil
	}
	return []ast.ExprID{tail.Expr}
}
