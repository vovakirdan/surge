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

// unwrapOwnedTemp peels drop-emission's owned-temporary wrapper so range-shape
// checks and the range desugar see the underlying expression.
func unwrapOwnedTemp(e *Expr) *Expr {
	for e != nil && e.Kind == ExprOwnedTemp {
		od, ok := e.Data.(OwnedTempData)
		if !ok || od.Inner == nil {
			return e
		}
		e = od.Inner
	}
	return e
}

// iterableElementType resolves the element type an iterable itself
// produces, independent of the loop variable: an array (dynamic or
// fixed-size) yields its element type, and a single-type-argument
// generic struct instance (Range<T> is the only such iterable this
// protocol currently builds) yields that argument. Used as the
// element-type fallback when the loop pattern binds no symbol (`for _
// in ...`) — normalizeIterFor previously never fell back past
// VarType/VarSym, so a discarded loop variable failed MIR validation
// with an "unknown type" error on the synthesized iterator locals.
func iterableElementType(ctx *normCtx, iterable *Expr) types.TypeID {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || iterable == nil {
		return types.NoTypeID
	}
	in := ctx.mod.TypeInterner
	ty := unwrapOwnedTemp(iterable).Type
	for ty != types.NoTypeID {
		tt, ok := in.Lookup(ty)
		if !ok || (tt.Kind != types.KindReference && tt.Kind != types.KindOwn && tt.Kind != types.KindPointer) {
			break
		}
		ty = tt.Elem
	}
	if elem, ok := in.ArrayInfo(ty); ok {
		return elem
	}
	if elem, _, ok := in.ArrayFixedInfo(ty); ok {
		return elem
	}
	if args := in.StructArgs(ty); len(args) == 1 {
		return args[0]
	}
	return types.NoTypeID
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

	elemTy := data.VarType
	if elemTy == types.NoTypeID && data.VarSym.IsValid() {
		elemTy = ctx.bindingType(data.VarSym)
	}
	if elemTy == types.NoTypeID {
		elemTy = iterableElementType(ctx, data.Iterable)
	}

	iterTy := iterRangeType(ctx, elemTy)
	nextTy := iterNextType(ctx, elemTy)

	iterSym, iterName := ctx.newTemp("iter")
	iterLet := Stmt{
		Kind: StmtLet,
		Span: span,
		Data: LetData{
			Name:      iterName,
			SymbolID:  iterSym,
			Type:      iterTy,
			Value:     &Expr{Kind: ExprIterInit, Type: iterTy, Span: span, Data: IterInitData{Iterable: data.Iterable}},
			IsMut:     true,
			IsConst:   false,
			Ownership: ctx.inferOwnership(iterTy),
		},
	}

	nextSym, nextName := ctx.newTemp("next")
	nextRef := ctx.varRef(nextName, nextSym, nextTy, span)

	nextLet := Stmt{
		Kind: StmtLet,
		Span: span,
		Data: LetData{
			Name:      nextName,
			SymbolID:  nextSym,
			Type:      nextTy,
			Value:     &Expr{Kind: ExprIterNext, Type: nextTy, Span: span, Data: IterNextData{Iter: ctx.varRef(iterName, iterSym, iterTy, span)}},
			IsMut:     false,
			IsConst:   false,
			Ownership: ctx.inferOwnership(nextTy),
		},
	}

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

	// Free the per-step Option<T> box. If its payload was bound to the
	// loop variable, the box frees shallow (the payload now belongs to
	// that binding — a recursive drop here would free it a second
	// time). If the payload was discarded (`for _ in ...`), nothing else
	// owns it, so a full drop is correct and necessary. The exhausted
	// ("nothing") case carries no payload either way, so reusing the
	// same release in breakIfNothing's block below is safe regardless of
	// which form this is — a shallow release only ever skips a payload
	// that isn't there.
	var stepRelease Stmt
	if bindVarStmt != nil {
		stepRelease = Stmt{Kind: StmtEnvelopeRelease, Span: span, Data: EnvelopeReleaseData{Value: nextRef, Cursor: false}}
	} else {
		stepRelease = Stmt{Kind: StmtDrop, Span: span, Data: DropData{Value: nextRef}}
	}

	// The exhausted case never reaches the Some-path release below (the
	// break exits the iteration immediately), so its own (payload-free)
	// box needs releasing right here.
	breakIfNothing := mkIf(span,
		&Expr{Kind: ExprTagTest, Type: ctx.boolType(), Span: span, Data: TagTestData{Value: nextRef, TagName: "nothing"}},
		&Block{Span: span, Stmts: []Stmt{
			stepRelease,
			{Kind: StmtBreak, Span: span, Data: BreakData{}},
		}},
	)

	// Free the iterator cursor (array or range state). Every exit from
	// this loop must free it exactly once: natural exhaustion and any
	// `break` both fall through to right after the while statement, so
	// one release placed there covers both; a `return` from inside the
	// body escapes without passing through that point, so it needs its
	// own release injected directly before it.
	if data.Body == nil {
		data.Body = &Block{Span: span}
	}

	// An array iteration always allocates a real, fixed-shape cursor
	// (emitArrayIterInit never takes a shortcut). A Range<T> iteration
	// only does when T is pointer-shaped at the backend (the default,
	// unsized int/uint, or another bignum-style type); a FIXED-WIDTH
	// int/uint element (i32, u8, ...) makes the backend hand back the
	// original SurgeRange pointer instead of allocating a cursor (see
	// emit_iter.go's range/array iterator layout notes). That pointer is
	// owned by the range value itself, not by this loop, so freeing it
	// here would double-free it. When this can't be proven safe, skip
	// the cursor release entirely: the historical leak for
	// that narrow case is unchanged, which is the safe direction — a
	// wrong free is not.
	var cursorRelease Stmt
	hasCursorRelease := iterCursorReleaseIsSafe(ctx, data.Iterable, elemTy)
	if hasCursorRelease {
		cursorRelease = Stmt{
			Kind: StmtEnvelopeRelease,
			Span: span,
			Data: EnvelopeReleaseData{Value: ctx.varRef(iterName, iterSym, iterTy, span), Cursor: true},
		}
		injectIterCursorReleaseBeforeReturns(data.Body, cursorRelease)
	}

	prefix := make([]Stmt, 0, 4)
	prefix = append(prefix, nextLet, breakIfNothing)
	if bindVarStmt != nil {
		prefix = append(prefix, *bindVarStmt)
	}
	prefix = append(prefix, stepRelease)
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
	if hasCursorRelease {
		outer.Stmts = append(outer.Stmts, cursorRelease)
	}
	return []Stmt{{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: outer}}}, nil
}

func iterRangeType(ctx *normCtx, elem types.TypeID) types.TypeID {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || elem == types.NoTypeID {
		return types.NoTypeID
	}
	strs := ctx.mod.Symbols
	if strs == nil || strs.Table == nil || strs.Table.Strings == nil {
		return types.NoTypeID
	}
	rangeName := strs.Table.Strings.Intern("Range")

	if id, ok := ctx.mod.TypeInterner.FindStructInstance(rangeName, []types.TypeID{elem}); ok {
		return id
	}

	baseID, ok := ctx.mod.TypeInterner.FindStructInstance(rangeName, nil)
	if !ok {
		return types.NoTypeID
	}
	baseInfo, ok := ctx.mod.TypeInterner.StructInfo(baseID)
	if !ok || baseInfo == nil {
		return types.NoTypeID
	}

	instID := ctx.mod.TypeInterner.RegisterStructInstance(rangeName, baseInfo.Decl, []types.TypeID{elem})
	fields := ctx.mod.TypeInterner.StructFields(baseID)
	if len(fields) > 0 {
		for i := range fields {
			fields[i].Type = substTypeParamIndex(ctx.mod.TypeInterner, fields[i].Type, []types.TypeID{elem})
		}
		ctx.mod.TypeInterner.SetStructFields(instID, fields)
	}
	if vals := ctx.mod.TypeInterner.StructValueArgs(baseID); len(vals) > 0 {
		ctx.mod.TypeInterner.SetStructValueArgs(instID, vals)
	}
	return instID
}

func iterNextType(ctx *normCtx, elem types.TypeID) types.TypeID {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || elem == types.NoTypeID {
		return types.NoTypeID
	}
	strs := ctx.mod.Symbols
	if strs == nil || strs.Table == nil || strs.Table.Strings == nil {
		return types.NoTypeID
	}
	optionName := strs.Table.Strings.Intern("Option")

	if id, ok := ctx.mod.TypeInterner.FindUnionInstance(optionName, []types.TypeID{elem}); ok {
		return id
	}

	baseID, ok := ctx.mod.TypeInterner.FindUnionInstance(optionName, nil)
	if !ok {
		return types.NoTypeID
	}
	baseInfo, ok := ctx.mod.TypeInterner.UnionInfo(baseID)
	if !ok || baseInfo == nil {
		return types.NoTypeID
	}

	instID := ctx.mod.TypeInterner.RegisterUnionInstance(optionName, baseInfo.Decl, []types.TypeID{elem})
	if len(baseInfo.Members) > 0 {
		members := make([]types.UnionMember, len(baseInfo.Members))
		copy(members, baseInfo.Members)
		for i := range members {
			members[i].Type = substTypeParamIndex(ctx.mod.TypeInterner, members[i].Type, []types.TypeID{elem})
			if len(members[i].TagArgs) > 0 {
				tagArgs := make([]types.TypeID, len(members[i].TagArgs))
				for j := range members[i].TagArgs {
					tagArgs[j] = substTypeParamIndex(ctx.mod.TypeInterner, members[i].TagArgs[j], []types.TypeID{elem})
				}
				members[i].TagArgs = tagArgs
			}
		}
		ctx.mod.TypeInterner.SetUnionMembers(instID, members)
	}
	return instID
}

func substTypeParamIndex(typesIn *types.Interner, id types.TypeID, args []types.TypeID) types.TypeID {
	if typesIn == nil || id == types.NoTypeID || len(args) == 0 {
		return id
	}
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return id
	}

	switch tt.Kind {
	case types.KindGenericParam:
		info, ok := typesIn.TypeParamInfo(id)
		if !ok || info == nil {
			return id
		}
		idx := int(info.Index)
		if idx >= 0 && idx < len(args) && args[idx] != types.NoTypeID {
			return args[idx]
		}
		return id
	case types.KindPointer, types.KindReference, types.KindOwn:
		elem := substTypeParamIndex(typesIn, tt.Elem, args)
		if elem == tt.Elem {
			return id
		}
		clone := tt
		clone.Elem = elem
		return typesIn.Intern(clone)
	case types.KindArray:
		elem := substTypeParamIndex(typesIn, tt.Elem, args)
		if elem == tt.Elem {
			return id
		}
		clone := tt
		clone.Elem = elem
		return typesIn.Intern(clone)
	case types.KindTuple:
		info, ok := typesIn.TupleInfo(id)
		if !ok || info == nil || len(info.Elems) == 0 {
			return id
		}
		elems := make([]types.TypeID, len(info.Elems))
		changed := false
		for i := range info.Elems {
			elems[i] = substTypeParamIndex(typesIn, info.Elems[i], args)
			changed = changed || elems[i] != info.Elems[i]
		}
		if !changed {
			return id
		}
		return typesIn.RegisterTuple(elems)
	case types.KindFn:
		info, ok := typesIn.FnInfo(id)
		if !ok || info == nil {
			return id
		}
		params := make([]types.TypeID, len(info.Params))
		changed := false
		for i := range info.Params {
			params[i] = substTypeParamIndex(typesIn, info.Params[i], args)
			changed = changed || params[i] != info.Params[i]
		}
		result := substTypeParamIndex(typesIn, info.Result, args)
		changed = changed || result != info.Result
		if !changed {
			return id
		}
		return typesIn.RegisterFn(params, result)
	default:
		return id
	}
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
