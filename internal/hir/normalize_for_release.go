package hir

import (
	"surge/internal/source"
	"surge/internal/types"
)

// The for-loop cursor-release half of normalizeIterFor: deciding WHETHER
// the loop may free its iterator cursor (the backend's fast-path ranges
// hand back the range value itself), and injecting the release before
// every genuine return that escapes the loop.

// iterCursorReleaseIsSafe reports whether normalizeIterFor may emit a
// fixed-size release of this loop's iterator cursor (see the call site
// for the exact reasoning). Errs toward false: a caller that can't
// prove the release is safe keeps the pre-existing (leaky) behavior
// rather than risk a wrong free.
func iterCursorReleaseIsSafe(ctx *normCtx, iterable *Expr, elemTy types.TypeID) bool {
	if ctx == nil {
		return false
	}
	if !iterableIsRangeType(ctx, iterable) {
		return true // array iteration always allocates a real cursor
	}
	return rangeElementIsPointerShaped(ctx, elemTy)
}

// rangeElementIsPointerShaped reports whether a Range<T>'s bound values
// are pointer-shaped at the LLVM level — exactly the condition
// emitRangeIterInit uses to decide between allocating a real cursor and
// handing back the original SurgeRange pointer (its rangeTy/elemLLVM
// check). Only a FIXED-WIDTH int/uint/float (i8/16/32/64, f16/32/64 —
// never the default, unsized int/uint/float) is inline rather than
// pointer-shaped; every other element representation this backend
// produces (string, struct, union, array, tuple, or the default numeric
// types) is a heap handle. Errs toward false (unresolvable type) — the
// safe direction, matching the caller.
func rangeElementIsPointerShaped(ctx *normCtx, elemTy types.TypeID) bool {
	if ctx == nil || elemTy == types.NoTypeID || ctx.mod == nil || ctx.mod.TypeInterner == nil {
		return false
	}
	tt, ok := ctx.mod.TypeInterner.Lookup(elemTy)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindInt, types.KindUint, types.KindFloat:
		return tt.Width == types.WidthAny
	case types.KindBool:
		// Bool is always an inline scalar, never pointer-tagged.
		return false
	default:
		return true
	}
}

// iterableIsRangeType reports whether the for-in loop's iterable is
// itself a Range<T> value, as opposed to an array.
func iterableIsRangeType(ctx *normCtx, iterable *Expr) bool {
	if ctx == nil || iterable == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil {
		return false
	}
	strs := ctx.mod.Symbols
	if strs == nil || strs.Table == nil || strs.Table.Strings == nil {
		return false
	}
	iterable = unwrapOwnedTemp(iterable)
	ty := stripOwnType(ctx.mod.TypeInterner, resolveAlias(ctx.mod.TypeInterner, iterable.Type, 0))
	if tt, ok := ctx.mod.TypeInterner.Lookup(ty); ok && tt.Kind == types.KindReference {
		ty = stripOwnType(ctx.mod.TypeInterner, resolveAlias(ctx.mod.TypeInterner, tt.Elem, 0))
	}
	if ty == types.NoTypeID {
		return false
	}
	info, ok := ctx.mod.TypeInterner.StructInfo(ty)
	if !ok || info == nil {
		return false
	}
	return info.Name == strs.Table.Strings.Intern("Range")
}

// injectIterCursorReleaseBeforeReturns prefixes every genuine `return`
// reachable from a for-loop body with a release of THIS loop's iterator
// cursor. It descends through everything that can carry one: nested
// statement blocks (blocks/ifs/nested loops — no depth tracking needed,
// unlike continue/break rewriting, since a real `return` escapes every
// enclosing scope regardless of nesting) AND expression-embedded blocks
// (an `if`/`match`/`{ ... }` used as a value can still contain an
// explicit `return` that exits the function outright — MIR's block-expr
// lowering only intercepts IMPLICIT returns, see below). The natural-exit
// and `break` cases are covered separately, once, right after the loop
// (see normalizeIterFor).
//
// A StmtReturn only qualifies when NOT IsImplicit. An implicit return is
// synthesized block-VALUE production (an arm's result, a compare's
// fallback default) that MIR routes to the innermost enclosing block
// expression's exit — not a real function exit — whenever one is active,
// exactly like StmtRet (a block-local value-yield, excluded for the same
// reason): releasing the cursor there would free it while iteration is
// still expected to continue.
func injectIterCursorReleaseBeforeReturns(b *Block, release Stmt) {
	if b == nil {
		return
	}
	out := make([]Stmt, 0, len(b.Stmts))
	for i := range b.Stmts {
		s := b.Stmts[i]
		switch s.Kind {
		case StmtReturn:
			data, ok := s.Data.(ReturnData)
			if ok && !data.IsImplicit {
				injectIterCursorReleaseInExpr(data.Value, release)
				wrap := &Block{Span: s.Span, Stmts: []Stmt{release, s}}
				out = append(out, Stmt{Kind: StmtBlock, Span: s.Span, Data: BlockStmtData{Block: wrap}})
				continue
			}
			if ok {
				injectIterCursorReleaseInExpr(data.Value, release)
			}
			out = append(out, s)
		case StmtLet:
			data, ok := s.Data.(LetData)
			if !ok {
				out = append(out, s)
				continue
			}
			injectIterCursorReleaseInExpr(data.Value, release)
			out = append(out, s)
		case StmtExpr:
			data, ok := s.Data.(ExprStmtData)
			if !ok {
				out = append(out, s)
				continue
			}
			injectIterCursorReleaseInExpr(data.Expr, release)
			out = append(out, s)
		case StmtAssign:
			data, ok := s.Data.(AssignData)
			if !ok {
				out = append(out, s)
				continue
			}
			injectIterCursorReleaseInExpr(data.Target, release)
			injectIterCursorReleaseInExpr(data.Value, release)
			out = append(out, s)
		case StmtRet:
			data, ok := s.Data.(RetData)
			if !ok {
				out = append(out, s)
				continue
			}
			injectIterCursorReleaseInExpr(data.Value, release)
			out = append(out, s)
		case StmtIf:
			data, ok := s.Data.(IfStmtData)
			if !ok {
				out = append(out, s)
				continue
			}
			injectIterCursorReleaseInExpr(data.Cond, release)
			injectIterCursorReleaseBeforeReturns(data.Then, release)
			injectIterCursorReleaseBeforeReturns(data.Else, release)
			s.Data = data
			out = append(out, s)
		case StmtWhile:
			data, ok := s.Data.(WhileData)
			if !ok {
				out = append(out, s)
				continue
			}
			injectIterCursorReleaseInExpr(data.Cond, release)
			injectIterCursorReleaseBeforeReturns(data.Body, release)
			s.Data = data
			out = append(out, s)
		case StmtFor:
			data, ok := s.Data.(ForData)
			if !ok {
				out = append(out, s)
				continue
			}
			injectIterCursorReleaseBeforeReturns(data.Body, release)
			s.Data = data
			out = append(out, s)
		case StmtBlock:
			data, ok := s.Data.(BlockStmtData)
			if !ok {
				out = append(out, s)
				continue
			}
			injectIterCursorReleaseBeforeReturns(data.Block, release)
			s.Data = data
			out = append(out, s)
		default:
			out = append(out, s)
		}
	}
	b.Stmts = out
}

// injectIterCursorReleaseInExpr descends into every sub-expression
// looking for embedded statement blocks (mirroring normalizeExpr's
// recursion shape) so injectIterCursorReleaseBeforeReturns can reach a
// `return` sitting inside an `if`/`match`/`{ ... }` used as a value, or
// buried inside a call argument, operand, or literal field.
//
// Deliberately does NOT descend into ExprAsync/ExprBlocking/ExprCrossing
// bodies: those lower to a SEPARATE frame (a spawned/crossing body, or a
// blocking task), so a `return` textually inside one exits THAT frame,
// never this function — this loop's cursor is not in its scope at all.
func injectIterCursorReleaseInExpr(e *Expr, release Stmt) {
	if e == nil {
		return
	}
	switch e.Kind {
	case ExprOwnedTemp:
		data, ok := e.Data.(OwnedTempData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Inner, release)
	case ExprRaiseReleaseGuard:
		data, ok := e.Data.(RaiseReleaseGuardData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Inner, release)
	case ExprUnaryOp:
		data, ok := e.Data.(UnaryOpData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Operand, release)
	case ExprBinaryOp:
		data, ok := e.Data.(BinaryOpData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Left, release)
		injectIterCursorReleaseInExpr(data.Right, release)
	case ExprCall:
		data, ok := e.Data.(CallData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Callee, release)
		for _, arg := range data.Args {
			injectIterCursorReleaseInExpr(arg, release)
		}
	case ExprFieldAccess:
		data, ok := e.Data.(FieldAccessData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Object, release)
	case ExprIndex:
		data, ok := e.Data.(IndexData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Object, release)
		injectIterCursorReleaseInExpr(data.Index, release)
	case ExprStructLit:
		data, ok := e.Data.(StructLitData)
		if !ok {
			return
		}
		for i := range data.Fields {
			injectIterCursorReleaseInExpr(data.Fields[i].Value, release)
		}
	case ExprArrayLit:
		data, ok := e.Data.(ArrayLitData)
		if !ok {
			return
		}
		for _, elem := range data.Elements {
			injectIterCursorReleaseInExpr(elem, release)
		}
	case ExprMapLit:
		data, ok := e.Data.(MapLitData)
		if !ok {
			return
		}
		for i := range data.Entries {
			injectIterCursorReleaseInExpr(data.Entries[i].Key, release)
			injectIterCursorReleaseInExpr(data.Entries[i].Value, release)
		}
	case ExprTupleLit:
		data, ok := e.Data.(TupleLitData)
		if !ok {
			return
		}
		for _, elem := range data.Elements {
			injectIterCursorReleaseInExpr(elem, release)
		}
	case ExprSelect, ExprRace:
		data, ok := e.Data.(SelectData)
		if !ok {
			return
		}
		for i := range data.Arms {
			injectIterCursorReleaseInExpr(data.Arms[i].Await, release)
			injectIterCursorReleaseInExpr(data.Arms[i].Result, release)
		}
	case ExprTagTest:
		data, ok := e.Data.(TagTestData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Value, release)
	case ExprTagPayload:
		data, ok := e.Data.(TagPayloadData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Value, release)
	case ExprIterInit:
		data, ok := e.Data.(IterInitData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Iterable, release)
	case ExprIterNext:
		data, ok := e.Data.(IterNextData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Iter, release)
	case ExprIf:
		data, ok := e.Data.(IfData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Cond, release)
		injectIterCursorReleaseInExpr(data.Then, release)
		injectIterCursorReleaseInExpr(data.Else, release)
	case ExprAwait:
		data, ok := e.Data.(AwaitData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Value, release)
	case ExprTask:
		data, ok := e.Data.(TaskData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Value, release)
	case ExprSpawn:
		data, ok := e.Data.(SpawnData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Value, release)
	case ExprCast:
		data, ok := e.Data.(CastData)
		if !ok {
			return
		}
		injectIterCursorReleaseInExpr(data.Value, release)
	case ExprBlock:
		data, ok := e.Data.(BlockExprData)
		if !ok {
			return
		}
		injectIterCursorReleaseBeforeReturns(data.Block, release)
	}
}

// iterableHoist carries the decision to give the loop's ITERABLE a binding of
// its own, plus the release that binding then needs.
type iterableHoist struct {
	let  *Stmt
	drop Stmt
	live bool
}

// hoistIterableTemporary decides whether `for v in <expr>` needs its iterable
// bound to a name for the duration of the loop, and builds that binding. It
// returns the expression `iter_init` should read.
//
// WHY IT IS NEEDED AT ALL. `for v in build()` evaluates something nobody names,
// and the statement-end machinery frees it at the end of the
// `let __iter = iter_init(...)` statement this normalization builds — which is
// BEFORE the loop that reads through the cursor it just made. MIR showed it
// plainly: `drop L3` sat in the entry block, two instructions after
// `iter_init`. The loop then read freed storage, and with five elements the
// first two values came back as garbage while the count and the tail were right
// (RV2-DEBT-221). No slice or view is involved — an ordinary array returned by
// an ordinary function does it.
//
// The fix is the transformation an author writes by hand, and which measures
// clean: bind the value first, iterate the binding. That moves the release to
// where the CURSOR's release already is — after the loop, and before every
// return that escapes it — which is why this lives beside that plumbing and
// reuses it rather than inventing a second placement.
//
// ONLY AN UNCONDITIONAL, WHOLE-VALUE TEMPORARY IS HOISTED. A residual plan
// (fields already taken out of it) or a guarded one (a choice expression that
// owns on some paths and forwards a place on others) describes a release this
// plain drop would get wrong, and a wrong free is worse than the leak that not
// hoisting leaves behind. That is the direction iterCursorReleaseIsSafe errs
// in, for the same reason.
func hoistIterableTemporary(ctx *normCtx, iterable *Expr, span source.Span) (iterableHoist, *Expr) {
	if ctx == nil || iterable == nil || iterable.Kind != ExprOwnedTemp {
		return iterableHoist{}, iterable
	}
	data, ok := iterable.Data.(OwnedTempData)
	if !ok || data.Inner == nil || len(data.Steps) > 0 || data.Guarded {
		return iterableHoist{}, iterable
	}
	ty := data.Inner.Type
	if ty == types.NoTypeID {
		return iterableHoist{}, iterable
	}
	sym, name := ctx.newTemp("src")
	hoist := iterableHoist{
		let: &Stmt{Kind: StmtLet, Span: span, Data: LetData{
			Name:      name,
			SymbolID:  sym,
			Type:      ty,
			Value:     data.Inner,
			Ownership: ctx.inferOwnership(ty),
		}},
		drop: Stmt{Kind: StmtDrop, Span: span, Data: DropData{Value: ctx.varRef(name, sym, ty, span)}},
		live: true,
	}
	return hoist, ctx.varRef(name, sym, ty, span)
}

// injectBeforeReturns gives the hoisted binding the treatment the cursor
// already gets: a release before every return that escapes the loop.
//
// Call it AFTER the cursor's own injection. The cursor points INTO this value,
// so it has to be released first, and freeing storage before the thing pointing
// at it is the very ordering error this change exists to fix.
func (h iterableHoist) injectBeforeReturns(body *Block) {
	if !h.live {
		return
	}
	injectIterCursorReleaseBeforeReturns(body, h.drop)
}

// iterForBlock assembles the normalized loop: the hoisted iterable when there is
// one, the cursor, the loop itself, and the releases in reverse order of
// acquisition.
func iterForBlock(
	span source.Span,
	h iterableHoist,
	iterLet, whileStmt, cursorRelease Stmt,
	hasCursorRelease bool,
) []Stmt {
	outer := &Block{Span: span}
	if h.live {
		outer.Stmts = append(outer.Stmts, *h.let)
	}
	outer.Stmts = append(outer.Stmts, iterLet, whileStmt)
	if hasCursorRelease {
		outer.Stmts = append(outer.Stmts, cursorRelease)
	}
	if h.live {
		outer.Stmts = append(outer.Stmts, h.drop)
	}
	return []Stmt{{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: outer}}}
}
