//nolint:errcheck // HIR nodes are checked by construction; Kind implies the Data payload type.
package hir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

func normalizeForStmt(ctx *normCtx, s *Stmt) ([]Stmt, error) {
	if ctx == nil || s == nil {
		return nil, nil
	}
	data, ok := s.Data.(ForData)
	if !ok {
		return nil, fmt.Errorf("hir: normalize for: unexpected payload %T", s.Data)
	}

	switch data.Kind {
	case ForClassic:
		return normalizeForClassic(ctx, s.Span, data)
	case ForIn:
		return normalizeForIn(ctx, s.Span, data)
	default:
		return []Stmt{*s}, nil
	}
}

func normalizeForClassic(ctx *normCtx, span source.Span, data ForData) ([]Stmt, error) {
	if ctx == nil {
		return nil, nil
	}

	var initStmts []Stmt
	if data.Init != nil {
		repl, err := normalizeStmt(ctx, data.Init)
		if err != nil {
			return nil, err
		}
		initStmts = append(initStmts, repl...)
	}
	if data.Cond != nil {
		if err := normalizeExpr(ctx, data.Cond); err != nil {
			return nil, err
		}
	}
	if data.Post != nil {
		if err := normalizeExpr(ctx, data.Post); err != nil {
			return nil, err
		}
	}
	if err := normalizeBlock(ctx, data.Body); err != nil {
		return nil, err
	}

	cond := data.Cond
	if cond == nil {
		cond = ctx.boolLit(true, span)
	}

	var postStmt *Stmt
	if data.Post != nil {
		postStmt = &Stmt{
			Kind: StmtExpr,
			Span: span,
			Data: ExprStmtData{Expr: data.Post},
		}
	}

	if postStmt != nil && data.Body != nil {
		rewriteContinues(data.Body, []Stmt{*postStmt}, 0)
		data.Body.Stmts = append(data.Body.Stmts, *postStmt)
	}

	whileStmt := Stmt{
		Kind: StmtWhile,
		Span: span,
		Data: WhileData{
			Cond: cond,
			Body: data.Body,
		},
	}

	outer := &Block{Span: span}
	outer.Stmts = append(outer.Stmts, initStmts...)
	outer.Stmts = append(outer.Stmts, whileStmt)

	return []Stmt{{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: outer}}}, nil
}

func normalizeForIn(ctx *normCtx, span source.Span, data ForData) ([]Stmt, error) {
	if ctx == nil {
		return nil, nil
	}
	if data.Iterable != nil {
		if err := normalizeExpr(ctx, data.Iterable); err != nil {
			return nil, err
		}
	}
	if err := normalizeBlock(ctx, data.Body); err != nil {
		return nil, err
	}

	if isNumericRangeFor(ctx, data.Iterable, data.VarType) {
		return normalizeNumericRangeFor(ctx, span, data)
	}
	return normalizeIterFor(ctx, span, data)
}

func isNumericRangeFor(ctx *normCtx, iterable *Expr, elemTy types.TypeID) bool {
	if ctx == nil || iterable == nil || iterable.Kind != ExprBinaryOp || elemTy == types.NoTypeID {
		return false
	}
	bin := iterable.Data.(BinaryOpData)
	if bin.Op != ast.ExprBinaryRange && bin.Op != ast.ExprBinaryRangeInclusive {
		return false
	}
	if ctx.mod == nil || ctx.mod.TypeInterner == nil {
		return false
	}
	tt, ok := ctx.mod.TypeInterner.Lookup(elemTy)
	if !ok {
		return false
	}
	return tt.Kind == types.KindInt || tt.Kind == types.KindUint
}

func normalizeNumericRangeFor(ctx *normCtx, span source.Span, data ForData) ([]Stmt, error) {
	iterable := data.Iterable
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
			Name:      loopName,
			SymbolID:  loopSym,
			Type:      loopTy,
			Value:     start,
			IsMut:     true,
			IsConst:   false,
			Ownership: ctx.inferOwnership(loopTy),
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
			Name:      endName,
			SymbolID:  endSym,
			Type:      endTy,
			Value:     end,
			IsMut:     false,
			IsConst:   false,
			Ownership: ctx.inferOwnership(endTy),
		},
	}

	condOp := ast.ExprBinaryLess
	if bin.Op == ast.ExprBinaryRangeInclusive {
		condOp = ast.ExprBinaryLessEq
	}
	cond := ctx.binary(condOp, ctx.varRef(loopName, loopSym, loopTy, span), ctx.varRef(endName, endSym, endTy, span), ctx.boolType(), span)

	incr := Stmt{
		Kind: StmtAssign,
		Span: span,
		Data: AssignData{
			Target: ctx.varRef(loopName, loopSym, loopTy, span),
			Value: ctx.binary(
				ast.ExprBinaryAdd,
				ctx.varRef(loopName, loopSym, loopTy, span),
				ctx.intLit(1, loopTy, span),
				loopTy,
				span,
			),
		},
	}

	if data.Body == nil {
		data.Body = &Block{Span: span}
	}
	rewriteContinues(data.Body, []Stmt{incr}, 0)
	data.Body.Stmts = append(data.Body.Stmts, incr)

	whileStmt := Stmt{
		Kind: StmtWhile,
		Span: span,
		Data: WhileData{
			Cond: cond,
			Body: data.Body,
		},
	}

	outer := &Block{Span: span}
	outer.Stmts = append(outer.Stmts, iLet, endLet, whileStmt)
	return []Stmt{{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: outer}}}, nil
}

func normalizeIterFor(ctx *normCtx, span source.Span, data ForData) ([]Stmt, error) {
	if ctx == nil {
		return nil, nil
	}

	iterSym, iterName := ctx.newTemp("iter")
	iterLet := Stmt{
		Kind: StmtLet,
		Span: span,
		Data: LetData{
			Name:      iterName,
			SymbolID:  iterSym,
			Type:      types.NoTypeID,
			Value:     &Expr{Kind: ExprIterInit, Type: types.NoTypeID, Span: span, Data: IterInitData{Iterable: data.Iterable}},
			IsMut:     true,
			IsConst:   false,
			Ownership: OwnershipNone,
		},
	}

	nextSym, nextName := ctx.newTemp("next")
	nextRef := ctx.varRef(nextName, nextSym, types.NoTypeID, span)

	nextLet := Stmt{
		Kind: StmtLet,
		Span: span,
		Data: LetData{
			Name:      nextName,
			SymbolID:  nextSym,
			Type:      types.NoTypeID,
			Value:     &Expr{Kind: ExprIterNext, Type: types.NoTypeID, Span: span, Data: IterNextData{Iter: ctx.varRef(iterName, iterSym, types.NoTypeID, span)}},
			IsMut:     false,
			IsConst:   false,
			Ownership: OwnershipNone,
		},
	}

	breakIfNothing := mkIf(span,
		&Expr{Kind: ExprTagTest, Type: ctx.boolType(), Span: span, Data: TagTestData{Value: nextRef, TagName: "nothing"}},
		&Block{Span: span, Stmts: []Stmt{{Kind: StmtBreak, Span: span, Data: BreakData{}}}},
	)

	var bindVarStmt *Stmt
	if data.VarName != "" && data.VarName != "_" && data.VarSym.IsValid() {
		bindTy := data.VarType
		if bindTy == types.NoTypeID {
			bindTy = ctx.bindingType(data.VarSym)
		}
		bindVarStmt = &Stmt{
			Kind: StmtLet,
			Span: span,
			Data: LetData{
				Name:      data.VarName,
				SymbolID:  data.VarSym,
				Type:      bindTy,
				Value:     &Expr{Kind: ExprTagPayload, Type: bindTy, Span: span, Data: TagPayloadData{Value: nextRef, TagName: "Some", Index: 0}},
				IsMut:     false,
				IsConst:   false,
				Ownership: ctx.inferOwnership(bindTy),
			},
		}
	}

	if data.Body == nil {
		data.Body = &Block{Span: span}
	}

	prefix := make([]Stmt, 0, 3)
	prefix = append(prefix, nextLet, breakIfNothing)
	if bindVarStmt != nil {
		prefix = append(prefix, *bindVarStmt)
	}
	data.Body.Stmts = append(prefix, data.Body.Stmts...)

	whileStmt := Stmt{
		Kind: StmtWhile,
		Span: span,
		Data: WhileData{
			Cond: ctx.boolLit(true, span),
			Body: data.Body,
		},
	}

	outer := &Block{Span: span}
	outer.Stmts = append(outer.Stmts, iterLet, whileStmt)
	return []Stmt{{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: outer}}}, nil
}

func rewriteContinues(b *Block, inject []Stmt, depth int) {
	if b == nil {
		return
	}
	out := make([]Stmt, 0, len(b.Stmts))
	for i := range b.Stmts {
		s := b.Stmts[i]
		switch s.Kind {
		case StmtContinue:
			if depth == 0 && len(inject) > 0 {
				blk := &Block{Span: s.Span}
				blk.Stmts = append(blk.Stmts, inject...)
				blk.Stmts = append(blk.Stmts, s)
				out = append(out, Stmt{Kind: StmtBlock, Span: s.Span, Data: BlockStmtData{Block: blk}})
			} else {
				out = append(out, s)
			}
		case StmtIf:
			data := s.Data.(IfStmtData)
			rewriteContinues(data.Then, inject, depth)
			rewriteContinues(data.Else, inject, depth)
			s.Data = data
			out = append(out, s)
		case StmtWhile:
			data := s.Data.(WhileData)
			rewriteContinues(data.Body, inject, depth+1)
			s.Data = data
			out = append(out, s)
		case StmtFor:
			data := s.Data.(ForData)
			rewriteContinues(data.Body, inject, depth+1)
			s.Data = data
			out = append(out, s)
		case StmtBlock:
			data := s.Data.(BlockStmtData)
			rewriteContinues(data.Block, inject, depth)
			s.Data = data
			out = append(out, s)
		default:
			out = append(out, s)
		}
	}
	b.Stmts = out
}
