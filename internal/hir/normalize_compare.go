//nolint:errcheck // HIR nodes are checked by construction; Kind implies the Data payload type.
package hir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func normalizeCompareExpr(ctx *normCtx, e *Expr) error {
	if ctx == nil || e == nil {
		return nil
	}
	data, ok := e.Data.(CompareData)
	if !ok {
		return fmt.Errorf("hir: normalize compare: unexpected payload %T", e.Data)
	}

	if data.Value != nil {
		if err := normalizeExpr(ctx, data.Value); err != nil {
			return err
		}
	}
	for i := range data.Arms {
		if data.Arms[i].Guard != nil {
			if err := normalizeExpr(ctx, data.Arms[i].Guard); err != nil {
				return err
			}
		}
		if data.Arms[i].Result != nil {
			if err := normalizeExpr(ctx, data.Arms[i].Result); err != nil {
				return err
			}
		}
	}

	valueTy := types.NoTypeID
	if data.Value != nil {
		valueTy = data.Value.Type
	}

	cmpSym, cmpName := ctx.newTemp("cmp")
	cmpRef := ctx.varRef(cmpName, cmpSym, valueTy, e.Span)

	stmts := make([]Stmt, 0, 2+len(data.Arms)*2)
	stmts = append(stmts, Stmt{
		Kind: StmtLet,
		Span: e.Span,
		Data: LetData{
			Name:      cmpName,
			SymbolID:  cmpSym,
			Type:      valueTy,
			Value:     data.Value,
			IsMut:     false,
			IsConst:   false,
			Ownership: ctx.inferOwnership(valueTy),
		},
	})

	for _, arm := range data.Arms {
		armStmts := lowerCompareArm(ctx, cmpRef, valueTy, arm)
		stmts = append(stmts, armStmts...)
	}

	if !compareExhaustive(ctx, valueTy, data.Arms) {
		// When no arm matches, fall back to a default value of the compare expression type.
		// This keeps the normalized HIR well-typed without introducing new semantic checks.
		stmts = append(stmts, Stmt{
			Kind: StmtReturn,
			Span: e.Span,
			Data: ReturnData{
				Value: &Expr{
					Kind: ExprCall,
					Type: e.Type,
					Span: e.Span,
					Data: CallData{
						Callee: &Expr{
							Kind: ExprVarRef,
							Type: types.NoTypeID,
							Span: e.Span,
							Data: VarRefData{Name: "default", SymbolID: symbols.NoSymbolID},
						},
						Args:     nil,
						SymbolID: symbols.NoSymbolID,
					},
				},
				IsTail:     false,
				IsImplicit: true,
			},
		})
	}

	e.Kind = ExprBlock
	e.Data = BlockExprData{Block: &Block{Stmts: stmts, Span: e.Span}}
	return nil
}

func lowerCompareArm(ctx *normCtx, subject *Expr, subjectTy types.TypeID, arm CompareArm) []Stmt {
	if ctx == nil {
		return nil
	}
	span := arm.Span
	if span == (source.Span{}) && subject != nil {
		span = subject.Span
	}
	if span == (source.Span{}) && ctx.fn != nil {
		span = ctx.fn.Span
	}

	// `finally` is a wildcard default arm.
	if arm.IsFinally || isWildcardPattern(arm.Pattern) {
		if arm.Guard == nil {
			return []Stmt{mkReturn(span, arm.Result)}
		}
		return []Stmt{mkIf(span, arm.Guard, &Block{Stmts: []Stmt{mkReturn(span, arm.Result)}, Span: span})}
	}

	if isNothingPattern(arm.Pattern) {
		return []Stmt{mkMatchIf(span, &Expr{Kind: ExprTagTest, Type: ctx.boolType(), Span: span, Data: TagTestData{Value: subject, TagName: "nothing"}}, nil, arm.Guard, arm.Result)}
	}

	if tagName, payloadPats, ok := tagPattern(ctx, arm.Pattern); ok {
		return []Stmt{lowerTagArm(ctx, span, subject, tagName, payloadPats, arm.Guard, arm.Result)}
	}

	if tupleElems, ok := tuplePattern(arm.Pattern); ok {
		return []Stmt{lowerTupleArm(ctx, span, subject, subjectTy, tupleElems, arm.Guard, arm.Result)}
	}

	if name, sym, ok := bindingPattern(ctx, arm.Pattern); ok {
		ty := ctx.bindingType(sym)
		if ty == types.NoTypeID {
			ty = subjectTy
		}
		body := &Block{Span: span}
		body.Stmts = append(body.Stmts, Stmt{
			Kind: StmtLet,
			Span: span,
			Data: LetData{
				Name:      name,
				SymbolID:  sym,
				Type:      ty,
				Value:     subject,
				IsMut:     false,
				IsConst:   false,
				Ownership: ctx.inferOwnership(ty),
			},
		})
		if arm.Guard == nil {
			body.Stmts = append(body.Stmts, mkReturn(span, arm.Result))
		} else {
			body.Stmts = append(body.Stmts, mkIf(span, arm.Guard, &Block{Stmts: []Stmt{mkReturn(span, arm.Result)}, Span: span}))
		}
		return []Stmt{{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: body}}}
	}

	// Fallback: treat the pattern as a literal equality check.
	cond := ctx.binary(ast.ExprBinaryEq, subject, arm.Pattern, ctx.boolType(), span)
	return []Stmt{mkMatchIf(span, cond, nil, arm.Guard, arm.Result)}
}

func isWildcardPattern(p *Expr) bool {
	if p == nil || p.Kind != ExprVarRef {
		return false
	}
	data := p.Data.(VarRefData)
	return data.Name == "_"
}

func isNothingPattern(p *Expr) bool {
	if p == nil {
		return false
	}
	switch p.Kind {
	case ExprLiteral:
		data := p.Data.(LiteralData)
		return data.Kind == LiteralNothing
	case ExprVarRef:
		data := p.Data.(VarRefData)
		return data.Name == "nothing"
	default:
		return false
	}
}

func bindingPattern(ctx *normCtx, p *Expr) (string, symbols.SymbolID, bool) {
	if ctx == nil || p == nil || p.Kind != ExprVarRef {
		if p == nil || p.Kind != ExprUnaryOp {
			return "", symbols.NoSymbolID, false
		}
		data := p.Data.(UnaryOpData)
		switch data.Op {
		case ast.ExprUnaryRef, ast.ExprUnaryRefMut, ast.ExprUnaryDeref:
			if data.Operand == nil || data.Operand.Kind != ExprVarRef {
				return "", symbols.NoSymbolID, false
			}
			p = data.Operand
		default:
			return "", symbols.NoSymbolID, false
		}
	}
	data := p.Data.(VarRefData)
	if data.Name == "" || data.Name == "_" || data.Name == "nothing" {
		return "", symbols.NoSymbolID, false
	}
	if data.SymbolID.IsValid() && ctx.isTagSymbol(data.SymbolID) {
		return "", symbols.NoSymbolID, false
	}
	return data.Name, data.SymbolID, data.SymbolID.IsValid()
}

func tagPattern(ctx *normCtx, p *Expr) (string, []*Expr, bool) {
	if ctx == nil || p == nil {
		return "", nil, false
	}
	switch p.Kind {
	case ExprCall:
		call := p.Data.(CallData)
		if call.Callee == nil || call.Callee.Kind != ExprVarRef {
			return "", nil, false
		}
		name := call.Callee.Data.(VarRefData).Name
		if name == "" {
			return "", nil, false
		}
		return name, call.Args, true
	case ExprVarRef:
		data := p.Data.(VarRefData)
		if data.SymbolID.IsValid() && ctx.isTagSymbol(data.SymbolID) {
			return data.Name, nil, true
		}
		return "", nil, false
	default:
		return "", nil, false
	}
}

func tuplePattern(p *Expr) ([]*Expr, bool) {
	if p == nil || p.Kind != ExprTupleLit {
		return nil, false
	}
	data := p.Data.(TupleLitData)
	return data.Elements, true
}

func compareExhaustive(ctx *normCtx, subjectTy types.TypeID, arms []CompareArm) bool {
	if ctx == nil {
		return false
	}
	for _, arm := range arms {
		if arm.Guard != nil {
			continue
		}
		if arm.IsFinally || isWildcardPattern(arm.Pattern) {
			return true
		}
		if _, _, ok := bindingPattern(ctx, arm.Pattern); ok {
			return true
		}
	}

	if ctx.mod == nil || ctx.mod.TypeInterner == nil || ctx.mod.Symbols == nil || ctx.mod.Symbols.Table == nil || ctx.mod.Symbols.Table.Strings == nil {
		return false
	}
	subjectTy = compareUnionSubjectType(ctx.mod.TypeInterner, subjectTy)
	info, ok := ctx.mod.TypeInterner.UnionInfo(subjectTy)
	if !ok || info == nil || len(info.Members) == 0 {
		return false
	}

	needTags := make(map[source.StringID]struct{})
	needNothing := false
	for _, m := range info.Members {
		switch m.Kind {
		case types.UnionMemberTag:
			if m.TagName != source.NoStringID {
				needTags[m.TagName] = struct{}{}
			}
		case types.UnionMemberNothing:
			needNothing = true
		default:
			return false
		}
	}

	coveredTags := make(map[source.StringID]struct{})
	coveredNothing := false
	for _, arm := range arms {
		if arm.Guard != nil {
			continue
		}
		if isNothingPattern(arm.Pattern) {
			coveredNothing = true
			continue
		}
		tagName, payloadPats, ok := tagPattern(ctx, arm.Pattern)
		if !ok {
			continue
		}
		if !payloadPatternsCoverAll(ctx, payloadPats) {
			continue
		}
		coveredTags[ctx.mod.Symbols.Table.Strings.Intern(tagName)] = struct{}{}
	}

	if needNothing && !coveredNothing {
		return false
	}
	for tag := range needTags {
		if _, ok := coveredTags[tag]; !ok {
			return false
		}
	}
	return true
}

func payloadPatternsCoverAll(ctx *normCtx, payload []*Expr) bool {
	if ctx == nil || len(payload) == 0 {
		return true
	}
	for _, pat := range payload {
		if pat == nil {
			continue
		}
		if isWildcardPattern(pat) {
			continue
		}
		if _, _, ok := bindingPattern(ctx, pat); ok {
			continue
		}
		return false
	}
	return true
}

func resolveAlias(typesIn *types.Interner, id types.TypeID, depth int) types.TypeID {
	if typesIn == nil || id == types.NoTypeID {
		return id
	}
	if depth > 64 {
		return id
	}
	tt, ok := typesIn.Lookup(id)
	if !ok || tt.Kind != types.KindAlias {
		return id
	}
	target, ok := typesIn.AliasTarget(id)
	if !ok || target == types.NoTypeID {
		return id
	}
	return resolveAlias(typesIn, target, depth+1)
}

func compareUnionSubjectType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil || id == types.NoTypeID {
		return id
	}
	normalized := stripOwnType(typesIn, resolveAlias(typesIn, id, 0))
	if tt, ok := typesIn.Lookup(normalized); ok && tt.Kind == types.KindReference {
		normalized = stripOwnType(typesIn, resolveAlias(typesIn, tt.Elem, 0))
	}
	return normalized
}

func tagPayloadType(ctx *normCtx, subject types.TypeID, tag string, index int) types.TypeID {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || ctx.mod.Symbols == nil || ctx.mod.Symbols.Table == nil || ctx.mod.Symbols.Table.Strings == nil {
		return types.NoTypeID
	}
	typesIn := ctx.mod.TypeInterner
	normalized := stripOwnType(typesIn, resolveAlias(typesIn, subject, 0))
	isRef := false
	refMut := false
	if tt, ok := typesIn.Lookup(normalized); ok && tt.Kind == types.KindReference {
		isRef = true
		refMut = tt.Mutable
		normalized = stripOwnType(typesIn, resolveAlias(typesIn, tt.Elem, 0))
	}
	info, ok := typesIn.UnionInfo(normalized)
	if !ok || info == nil {
		return types.NoTypeID
	}
	tagID := ctx.mod.Symbols.Table.Strings.Intern(tag)
	if tagID == source.NoStringID {
		return types.NoTypeID
	}
	for _, member := range info.Members {
		if member.Kind != types.UnionMemberTag || member.TagName != tagID {
			continue
		}
		if index < 0 || index >= len(member.TagArgs) {
			return types.NoTypeID
		}
		payload := member.TagArgs[index]
		if !isRef || payload == types.NoTypeID {
			return payload
		}
		resolved := resolveAlias(typesIn, payload, 0)
		if tt, ok := typesIn.Lookup(resolved); ok && tt.Kind == types.KindReference {
			return payload
		}
		return typesIn.Intern(types.MakeReference(payload, refMut))
	}
	return types.NoTypeID
}

func derefReferenceType(ctx *normCtx, id types.TypeID) types.TypeID {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || id == types.NoTypeID {
		return types.NoTypeID
	}
	typesIn := ctx.mod.TypeInterner
	resolved := resolveAlias(typesIn, id, 0)
	tt, ok := typesIn.Lookup(resolved)
	if !ok || tt.Kind != types.KindReference {
		return types.NoTypeID
	}
	return tt.Elem
}

func stripOwnType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil || id == types.NoTypeID {
		return id
	}
	for {
		tt, ok := typesIn.Lookup(id)
		if !ok || tt.Kind != types.KindOwn {
			return id
		}
		if tt.Elem == types.NoTypeID {
			return id
		}
		id = resolveAlias(typesIn, tt.Elem, 0)
	}
}

func mkReturn(span source.Span, value *Expr) Stmt {
	return Stmt{Kind: StmtReturn, Span: span, Data: ReturnData{Value: value, IsTail: false, IsImplicit: true}}
}

func mkIf(span source.Span, cond *Expr, thenB *Block) Stmt {
	return Stmt{Kind: StmtIf, Span: span, Data: IfStmtData{Cond: cond, Then: thenB}}
}

// mkMatchIf builds:
//
//	if cond { <bindings>; if guard { return result } }.
func mkMatchIf(span source.Span, cond *Expr, bindings []Stmt, guard, result *Expr) Stmt {
	thenB := &Block{Span: span}
	thenB.Stmts = append(thenB.Stmts, bindings...)
	if guard == nil {
		thenB.Stmts = append(thenB.Stmts, mkReturn(span, result))
	} else {
		thenB.Stmts = append(thenB.Stmts, mkIf(span, guard, &Block{Span: span, Stmts: []Stmt{mkReturn(span, result)}}))
	}
	return mkIf(span, cond, thenB)
}

func lowerTagArm(ctx *normCtx, span source.Span, subject *Expr, tag string, payload []*Expr, guard, result *Expr) Stmt {
	cond := &Expr{
		Kind: ExprTagTest,
		Type: ctx.boolType(),
		Span: span,
		Data: TagTestData{
			Value:   subject,
			TagName: tag,
		},
	}

	thenB := &Block{Span: span}

	var payloadConds []*Expr
	for i, pat := range payload {
		if pat == nil {
			continue
		}
		if isWildcardPattern(pat) {
			continue
		}
		payloadType := tagPayloadType(ctx, subject.Type, tag, i)
		payloadExpr := &Expr{
			Kind: ExprTagPayload,
			Type: payloadType,
			Span: span,
			Data: TagPayloadData{Value: subject, TagName: tag, Index: i},
		}
		payloadValue := payloadExpr
		if derefType := derefReferenceType(ctx, payloadType); derefType != types.NoTypeID {
			payloadValue = &Expr{
				Kind: ExprUnaryOp,
				Type: derefType,
				Span: span,
				Data: UnaryOpData{Op: ast.ExprUnaryDeref, Operand: payloadExpr},
			}
		}
		if isNothingPattern(pat) {
			// Payload cannot be `nothing` in v1 lowering; treat as a best-effort literal check.
			payloadConds = append(payloadConds, ctx.binary(ast.ExprBinaryEq, payloadValue, pat, ctx.boolType(), span))
			continue
		}

		if name, sym, ok := bindingPattern(ctx, pat); ok {
			ty := ctx.bindingType(sym)
			if ty == types.NoTypeID {
				ty = payloadType
			}
			thenB.Stmts = append(thenB.Stmts, Stmt{
				Kind: StmtLet,
				Span: span,
				Data: LetData{
					Name:      name,
					SymbolID:  sym,
					Type:      ty,
					Value:     payloadExpr,
					IsMut:     false,
					IsConst:   false,
					Ownership: ctx.inferOwnership(ty),
				},
			})
			continue
		}

		// Literal pattern: compare payload slot to the literal.
		if pat.Kind == ExprLiteral {
			payloadConds = append(payloadConds, ctx.binary(ast.ExprBinaryEq,
				payloadValue,
				pat,
				ctx.boolType(),
				span,
			))
			continue
		}
	}

	var innerCond *Expr
	if len(payloadConds) > 0 {
		innerCond = payloadConds[0]
		for i := 1; i < len(payloadConds); i++ {
			innerCond = ctx.binary(ast.ExprBinaryLogicalAnd, innerCond, payloadConds[i], ctx.boolType(), span)
		}
	}
	if guard != nil {
		if innerCond == nil {
			innerCond = guard
		} else {
			innerCond = ctx.binary(ast.ExprBinaryLogicalAnd, innerCond, guard, ctx.boolType(), span)
		}
	}

	if innerCond == nil {
		thenB.Stmts = append(thenB.Stmts, mkReturn(span, result))
	} else {
		thenB.Stmts = append(thenB.Stmts, mkIf(span, innerCond, &Block{Span: span, Stmts: []Stmt{mkReturn(span, result)}}))
	}

	return mkIf(span, cond, thenB)
}

func lowerTupleArm(ctx *normCtx, span source.Span, subject *Expr, subjectTy types.TypeID, elems []*Expr, guard, result *Expr) Stmt {
	body := &Block{Span: span}
	var conds []*Expr
	var elemTypes []types.TypeID
	if ctx != nil && ctx.mod != nil && ctx.mod.TypeInterner != nil && subjectTy != types.NoTypeID {
		base := resolveAlias(ctx.mod.TypeInterner, subjectTy, 0)
		if info, ok := ctx.mod.TypeInterner.TupleInfo(base); ok && info != nil {
			elemTypes = info.Elems
		}
	}

	for i, pat := range elems {
		if pat == nil || isWildcardPattern(pat) {
			continue
		}

		fieldType := types.NoTypeID
		if i < len(elemTypes) {
			fieldType = elemTypes[i]
		}

		field := &Expr{
			Kind: ExprFieldAccess,
			Type: fieldType,
			Span: span,
			Data: FieldAccessData{
				Object:   subject,
				FieldIdx: i,
			},
		}

		if name, sym, ok := bindingPattern(ctx, pat); ok {
			ty := ctx.bindingType(sym)
			if ty == types.NoTypeID {
				if fieldType != types.NoTypeID {
					ty = fieldType
				} else {
					ty = subjectTy
				}
			}
			field.Type = ty
			body.Stmts = append(body.Stmts, Stmt{
				Kind: StmtLet,
				Span: span,
				Data: LetData{
					Name:      name,
					SymbolID:  sym,
					Type:      ty,
					Value:     field,
					Ownership: ctx.inferOwnership(ty),
				},
			})
			continue
		}

		if pat.Kind == ExprLiteral {
			conds = append(conds, ctx.binary(ast.ExprBinaryEq, field, pat, ctx.boolType(), span))
			continue
		}
	}

	var armCond *Expr
	if len(conds) > 0 {
		armCond = conds[0]
		for i := 1; i < len(conds); i++ {
			armCond = ctx.binary(ast.ExprBinaryLogicalAnd, armCond, conds[i], ctx.boolType(), span)
		}
	}
	if guard != nil {
		if armCond == nil {
			armCond = guard
		} else {
			armCond = ctx.binary(ast.ExprBinaryLogicalAnd, armCond, guard, ctx.boolType(), span)
		}
	}

	if armCond == nil {
		body.Stmts = append(body.Stmts, mkReturn(span, result))
	} else {
		body.Stmts = append(body.Stmts, mkIf(span, armCond, &Block{Span: span, Stmts: []Stmt{mkReturn(span, result)}}))
	}

	return Stmt{Kind: StmtBlock, Span: span, Data: BlockStmtData{Block: body}}
}
