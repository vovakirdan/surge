//nolint:errcheck // HIR nodes are checked by construction; Kind implies the Data payload type.
package hir

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

func isNumericRangeFor(ctx *normCtx, iterable *Expr, elemTy types.TypeID) bool {
	if ctx == nil || iterable == nil {
		return false
	}
	// Drop-emission wraps an owned range value in ExprOwnedTemp before the loop
	// is normalized; peel it so the range binary-op shape is visible. Without
	// this the fast path never matched and every integer `for i in a..=b` fell
	// into the generic iterator protocol.
	iterable = unwrapOwnedTemp(iterable)
	if iterable.Kind != ExprBinaryOp {
		return false
	}
	bin := iterable.Data.(BinaryOpData)
	if bin.Op != ast.ExprBinaryRange && bin.Op != ast.ExprBinaryRangeInclusive {
		return false
	}
	// A finite numeric range desugars to a `while` loop, so both bounds must be
	// present; open-bounded ranges keep the iterator protocol.
	if bin.Left == nil || bin.Right == nil {
		return false
	}
	// Prefer the loop variable's type; fall back to the range bound types.
	return isIntOrUintKind(ctx, elemTy) ||
		isIntOrUintKind(ctx, bin.Left.Type) ||
		isIntOrUintKind(ctx, bin.Right.Type)
}

func isIntOrUintKind(ctx *normCtx, ty types.TypeID) bool {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || ty == types.NoTypeID {
		return false
	}
	tt, ok := ctx.mod.TypeInterner.Lookup(ty)
	if !ok {
		return false
	}
	return tt.Kind == types.KindInt || tt.Kind == types.KindUint
}

func normalizeNumericRangeFor(ctx *normCtx, span source.Span, data ForData) ([]Stmt, error) {
	iterable := unwrapOwnedTemp(data.Iterable)
	if iterable == nil || iterable.Kind != ExprBinaryOp {
		return normalizeIterFor(ctx, span, data)
	}
	bin := iterable.Data.(BinaryOpData)

	start := bin.Left
	end := bin.Right
	if start != nil {
		if err := normalizeExpr(ctx, start); err != nil {
			return nil, err
		}
	}
	if end != nil {
		if err := normalizeExpr(ctx, end); err != nil {
			return nil, err
		}
	}

	loopName := data.VarName
	loopSym := data.VarSym
	loopTy := data.VarType

	if loopName == "" || loopName == "_" || !loopSym.IsValid() {
		loopSym, loopName = ctx.newTemp("i")
	}
	if loopTy == types.NoTypeID {
		if start != nil && start.Type != types.NoTypeID {
			loopTy = start.Type
		} else if end != nil {
			loopTy = end.Type
		}
	}

	iLet := Stmt{
		Kind: StmtLet,
		Span: span,
		Data: LetData{
			Name:          loopName,
			SymbolID:      loopSym,
			Type:          loopTy,
			Value:         start,
			IsMut:         true,
			IsConst:       false,
			Ownership:     ctx.inferOwnership(loopTy),
			GeneratedDrop: GeneratedDropCountedScalar,
		},
	}

	endSym, endName := ctx.newTemp("end")
	endTy := loopTy
	if end != nil && end.Type != types.NoTypeID {
		endTy = end.Type
	}
	endLet := Stmt{
		Kind: StmtLet,
		Span: span,
		Data: LetData{
			Name:          endName,
			SymbolID:      endSym,
			Type:          endTy,
			Value:         end,
			IsMut:         false,
			IsConst:       false,
			Ownership:     ctx.inferOwnership(endTy),
			GeneratedDrop: GeneratedDropCountedScalar,
		},
	}

	condOp := ast.ExprBinaryLess
	if bin.Op == ast.ExprBinaryRangeInclusive {
		condOp = ast.ExprBinaryLessEq
	}
	cond := ctx.binary(condOp, ctx.varRef(loopName, loopSym, loopTy, span), ctx.varRef(endName, endSym, endTy, span), ctx.boolType(), span)

	post := &Expr{
		Kind: ExprBinaryOp,
		Type: loopTy,
		Span: span,
		Data: BinaryOpData{
			Op:   ast.ExprBinaryAssign,
			Left: ctx.varRef(loopName, loopSym, loopTy, span),
			Right: ctx.binary(
				ast.ExprBinaryAdd,
				ctx.varRef(loopName, loopSym, loopTy, span),
				ctx.intLit(1, loopTy, span),
				loopTy,
				span,
			),
			DropOverwritten: true,
		},
	}

	if data.Body == nil {
		data.Body = &Block{Span: span}
	}
	whileStmt := Stmt{
		Kind: StmtWhile,
		Span: span,
		Data: WhileData{
			Cond: cond,
			Body: data.Body,
			Post: post,
		},
	}

	outer := &Block{Span: span}
	outer.Stmts = append(outer.Stmts, iLet, endLet, whileStmt)
	return []Stmt{{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: outer}}}, nil
}
