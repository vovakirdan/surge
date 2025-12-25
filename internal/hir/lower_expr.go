package hir

import (
	"strconv"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// lowerExpr lowers an AST expression to HIR.
func (l *lowerer) lowerExpr(exprID ast.ExprID) *Expr {
	result := l.lowerExprCore(exprID)

	// Check for implicit tag injection (Some/Success wrapping)
	if result != nil && l.semaRes != nil && l.semaRes.ImplicitConversions != nil {
		if conv, ok := l.semaRes.ImplicitConversions[exprID]; ok {
			switch conv.Kind {
			case sema.ImplicitConversionSome:
				result = l.wrapInSome(result, conv.Target)
			case sema.ImplicitConversionSuccess:
				result = l.wrapInSuccess(result, conv.Target)
			case sema.ImplicitConversionTo:
				result = &Expr{
					Kind: ExprCast,
					Type: conv.Target,
					Span: result.Span,
					Data: CastData{
						Value:    result,
						TargetTy: conv.Target,
					},
				}
			}
		}
	}

	return result
}

// lowerExprCore does the actual lowering without implicit conversion handling.
func (l *lowerer) lowerExprCore(exprID ast.ExprID) *Expr {
	if !exprID.IsValid() {
		return nil
	}

	expr := l.builder.Exprs.Arena.Get(uint32(exprID))
	if expr == nil {
		return nil
	}

	// Get type from sema
	ty := l.semaRes.ExprTypes[exprID]

	switch expr.Kind {
	case ast.ExprGroup:
		// Unwrap grouping - minimal desugaring
		groupData := l.builder.Exprs.Groups.Get(uint32(expr.Payload))
		if groupData != nil {
			return l.lowerExpr(groupData.Inner)
		}
		return nil

	case ast.ExprIdent:
		return l.lowerIdentExpr(exprID, expr, ty)

	case ast.ExprLit:
		return l.lowerLiteralExpr(expr, ty)

	case ast.ExprBinary:
		return l.lowerBinaryExpr(expr, ty)

	case ast.ExprUnary:
		return l.lowerUnaryExpr(expr, ty)

	case ast.ExprCall:
		return l.lowerCallExpr(exprID, expr, ty)

	case ast.ExprMember:
		return l.lowerMemberExpr(expr, ty)

	case ast.ExprIndex:
		return l.lowerIndexExpr(expr, ty)

	case ast.ExprTuple:
		return l.lowerTupleExpr(expr, ty)

	case ast.ExprArray:
		return l.lowerArrayExpr(expr, ty)

	case ast.ExprRangeLit:
		return l.lowerRangeLitExpr(expr, ty)

	case ast.ExprStruct:
		return l.lowerStructExpr(expr, ty)

	case ast.ExprTernary:
		return l.lowerTernaryExpr(expr, ty)

	case ast.ExprCompare:
		return l.lowerCompareExpr(expr, ty)

	case ast.ExprAwait:
		return l.lowerAwaitExpr(expr, ty)

	case ast.ExprSpawn:
		return l.lowerSpawnExpr(expr, ty)

	case ast.ExprAsync:
		return l.lowerAsyncExpr(expr, ty)

	case ast.ExprCast:
		return l.lowerCastExpr(expr, ty)

	case ast.ExprBlock:
		return l.lowerBlockExpr(expr, ty)

	case ast.ExprTupleIndex:
		return l.lowerTupleIndexExpr(expr, ty)

	case ast.ExprSpread:
		// Spread is typically inlined at the call/array site
		spreadData := l.builder.Exprs.Spreads.Get(uint32(expr.Payload))
		if spreadData != nil {
			return l.lowerExpr(spreadData.Value)
		}
		return nil

	case ast.ExprParallel:
		// Parallel is reserved for v2+
		return nil

	default:
		return nil
	}
}

// lowerRangeLitExpr lowers a range literal expression.
func (l *lowerer) lowerRangeLitExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	rangeData := l.builder.Exprs.RangeLits.Get(uint32(expr.Payload))
	if rangeData == nil {
		return nil
	}

	var (
		name string
		args []*Expr
	)

	switch {
	case rangeData.Start.IsValid() && rangeData.End.IsValid():
		name = "rt_range_int_new"
		args = append(args, l.lowerExpr(rangeData.Start), l.lowerExpr(rangeData.End), l.boolLiteralExpr(expr.Span, rangeData.Inclusive))
	case rangeData.Start.IsValid():
		name = "rt_range_int_from_start"
		args = append(args, l.lowerExpr(rangeData.Start), l.boolLiteralExpr(expr.Span, rangeData.Inclusive))
	case rangeData.End.IsValid():
		name = "rt_range_int_to_end"
		args = append(args, l.lowerExpr(rangeData.End), l.boolLiteralExpr(expr.Span, rangeData.Inclusive))
	default:
		name = "rt_range_int_full"
		args = append(args, l.boolLiteralExpr(expr.Span, rangeData.Inclusive))
	}

	callee, symID := l.intrinsicCallee(name, expr.Span)
	return &Expr{
		Kind: ExprCall,
		Type: ty,
		Span: expr.Span,
		Data: CallData{
			Callee:   callee,
			Args:     args,
			SymbolID: symID,
		},
	}
}

// lowerIdentExpr lowers an identifier expression.
func (l *lowerer) lowerIdentExpr(exprID ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
	identData := l.builder.Exprs.Idents.Get(uint32(expr.Payload))
	if identData == nil {
		return nil
	}

	name := l.lookupString(identData.Name)
	var symID symbols.SymbolID
	if l.symRes != nil {
		symID = l.symRes.ExprSymbols[exprID]
	}

	return &Expr{
		Kind: ExprVarRef,
		Type: ty,
		Span: expr.Span,
		Data: VarRefData{
			Name:     name,
			SymbolID: symID,
		},
	}
}

// lowerLiteralExpr lowers a literal expression.
func (l *lowerer) lowerLiteralExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	litData := l.builder.Exprs.Literals.Get(uint32(expr.Payload))
	if litData == nil {
		return nil
	}

	data := LiteralData{}
	rawValue := l.lookupString(litData.Value)

	switch litData.Kind {
	case ast.ExprLitInt, ast.ExprLitUint:
		data.Kind = LiteralInt
		data.Text = rawValue
		data.IntValue = parseIntLiteral(rawValue)
	case ast.ExprLitFloat:
		data.Kind = LiteralFloat
		data.Text = rawValue
		data.FloatValue = parseFloatLiteral(rawValue)
	case ast.ExprLitString:
		data.Kind = LiteralString
		data.StringValue = rawValue
	case ast.ExprLitTrue:
		data.Kind = LiteralBool
		data.BoolValue = true
	case ast.ExprLitFalse:
		data.Kind = LiteralBool
		data.BoolValue = false
	case ast.ExprLitNothing:
		data.Kind = LiteralNothing
	}

	return &Expr{
		Kind: ExprLiteral,
		Type: ty,
		Span: expr.Span,
		Data: data,
	}
}

// lowerBinaryExpr lowers a binary expression.
func (l *lowerer) lowerBinaryExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	binData := l.builder.Exprs.Binaries.Get(uint32(expr.Payload))
	if binData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprBinaryOp,
		Type: ty,
		Span: expr.Span,
		Data: BinaryOpData{
			Op:    binData.Op,
			Left:  l.lowerExpr(binData.Left),
			Right: l.lowerExpr(binData.Right),
		},
	}
}

// lowerUnaryExpr lowers a unary expression.
func (l *lowerer) lowerUnaryExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	unaryData := l.builder.Exprs.Unaries.Get(uint32(expr.Payload))
	if unaryData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprUnaryOp,
		Type: ty,
		Span: expr.Span,
		Data: UnaryOpData{
			Op:      unaryData.Op,
			Operand: l.lowerExpr(unaryData.Operand),
		},
	}
}

// lowerCallExpr lowers a function call expression.
func (l *lowerer) lowerCallExpr(exprID ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
	callData := l.builder.Exprs.Calls.Get(uint32(expr.Payload))
	if callData == nil {
		return nil
	}

	args := make([]*Expr, len(callData.Args))
	for i, arg := range callData.Args {
		args[i] = l.lowerExpr(arg.Value)
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		symID = l.symRes.ExprSymbols[exprID]
	}

	if member, ok := l.builder.Exprs.Member(callData.Target); ok && member != nil {
		if symID.IsValid() {
			recv := l.lowerExpr(member.Target)
			if recv != nil && recv.Type != types.NoTypeID {
				recv = l.applySelfBorrow(symID, recv)
				args = append([]*Expr{recv}, args...)
			}
			callee := l.varRefForSymbol(symID, expr.Span)
			return &Expr{
				Kind: ExprCall,
				Type: ty,
				Span: expr.Span,
				Data: CallData{
					Callee:   callee,
					Args:     args,
					SymbolID: symID,
				},
			}
		}
		if l.symRes != nil {
			if memberSym := l.symRes.ExprSymbols[callData.Target]; memberSym.IsValid() {
				callee := l.varRefForSymbol(memberSym, expr.Span)
				return &Expr{
					Kind: ExprCall,
					Type: ty,
					Span: expr.Span,
					Data: CallData{
						Callee:   callee,
						Args:     args,
						SymbolID: memberSym,
					},
				}
			}
		}
	}

	callee := l.lowerExpr(callData.Target)
	return &Expr{
		Kind: ExprCall,
		Type: ty,
		Span: expr.Span,
		Data: CallData{
			Callee:   callee,
			Args:     args,
			SymbolID: symID,
		},
	}
}

// lowerMemberExpr lowers a member access expression.
func (l *lowerer) lowerMemberExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	memberData := l.builder.Exprs.Members.Get(uint32(expr.Payload))
	if memberData == nil {
		return nil
	}

	if lit := l.enumVariantLiteral(memberData, ty, expr.Span); lit != nil {
		return lit
	}

	return &Expr{
		Kind: ExprFieldAccess,
		Type: ty,
		Span: expr.Span,
		Data: FieldAccessData{
			Object:    l.lowerExpr(memberData.Target),
			FieldName: l.lookupString(memberData.Field),
			FieldIdx:  -1,
		},
	}
}

func (l *lowerer) enumVariantLiteral(member *ast.ExprMemberData, ty types.TypeID, span source.Span) *Expr {
	if l == nil || member == nil || l.symRes == nil || l.symRes.Table == nil || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return nil
	}
	symID, ok := l.symRes.ExprSymbols[member.Target]
	if !ok || !symID.IsValid() {
		return nil
	}
	sym := l.symRes.Table.Symbols.Get(symID)
	if sym == nil || sym.Kind != symbols.SymbolType || sym.Type == types.NoTypeID {
		return nil
	}
	enumInfo, ok := l.semaRes.TypeInterner.EnumInfo(sym.Type)
	if !ok || enumInfo == nil {
		return nil
	}
	for _, variant := range enumInfo.Variants {
		if variant.Name != member.Field {
			continue
		}
		if variant.IsString {
			return &Expr{
				Kind: ExprLiteral,
				Type: ty,
				Span: span,
				Data: LiteralData{
					Kind:        LiteralString,
					StringValue: l.lookupString(variant.StringValue),
				},
			}
		}
		text := strconv.FormatInt(variant.IntValue, 10)
		return &Expr{
			Kind: ExprLiteral,
			Type: ty,
			Span: span,
			Data: LiteralData{
				Kind:     LiteralInt,
				Text:     text,
				IntValue: variant.IntValue,
			},
		}
	}
	return nil
}

// lowerIndexExpr lowers an index expression.
func (l *lowerer) lowerIndexExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	indexData := l.builder.Exprs.Indices.Get(uint32(expr.Payload))
	if indexData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprIndex,
		Type: ty,
		Span: expr.Span,
		Data: IndexData{
			Object: l.lowerExpr(indexData.Target),
			Index:  l.lowerExpr(indexData.Index),
		},
	}
}

// lowerTupleExpr lowers a tuple expression.
func (l *lowerer) lowerTupleExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	tupleData := l.builder.Exprs.Tuples.Get(uint32(expr.Payload))
	if tupleData == nil {
		return nil
	}

	elements := make([]*Expr, len(tupleData.Elements))
	for i, elem := range tupleData.Elements {
		elements[i] = l.lowerExpr(elem)
	}

	return &Expr{
		Kind: ExprTupleLit,
		Type: ty,
		Span: expr.Span,
		Data: TupleLitData{Elements: elements},
	}
}

// lowerTupleIndexExpr lowers a tuple index expression.
func (l *lowerer) lowerTupleIndexExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	tupleIdxData := l.builder.Exprs.TupleIndices.Get(uint32(expr.Payload))
	if tupleIdxData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprFieldAccess,
		Type: ty,
		Span: expr.Span,
		Data: FieldAccessData{
			Object:   l.lowerExpr(tupleIdxData.Target),
			FieldIdx: int(tupleIdxData.Index),
		},
	}
}

// lowerArrayExpr lowers an array expression.
func (l *lowerer) lowerArrayExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	arrayData := l.builder.Exprs.Arrays.Get(uint32(expr.Payload))
	if arrayData == nil {
		return nil
	}

	elements := make([]*Expr, len(arrayData.Elements))
	for i, elem := range arrayData.Elements {
		elements[i] = l.lowerExpr(elem)
	}

	return &Expr{
		Kind: ExprArrayLit,
		Type: ty,
		Span: expr.Span,
		Data: ArrayLitData{Elements: elements},
	}
}

// lowerStructExpr lowers a struct literal expression.
func (l *lowerer) lowerStructExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	structData := l.builder.Exprs.Structs.Get(uint32(expr.Payload))
	if structData == nil {
		return nil
	}

	fields := make([]StructFieldInit, len(structData.Fields))
	for i, f := range structData.Fields {
		fields[i] = StructFieldInit{
			Name:  l.lookupString(f.Name),
			Value: l.lowerExpr(f.Value),
		}
	}

	return &Expr{
		Kind: ExprStructLit,
		Type: ty,
		Span: expr.Span,
		Data: StructLitData{
			TypeID: ty,
			Fields: fields,
		},
	}
}

// lowerTernaryExpr lowers a ternary expression to ExprIf.
func (l *lowerer) lowerTernaryExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	ternData := l.builder.Exprs.Ternaries.Get(uint32(expr.Payload))
	if ternData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprIf,
		Type: ty,
		Span: expr.Span,
		Data: IfData{
			Cond: l.lowerExpr(ternData.Cond),
			Then: l.lowerExpr(ternData.TrueExpr),
			Else: l.lowerExpr(ternData.FalseExpr),
		},
	}
}

// lowerCompareExpr lowers a compare expression (pattern matching).
func (l *lowerer) lowerCompareExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	cmpData := l.builder.Exprs.Compares.Get(uint32(expr.Payload))
	if cmpData == nil {
		return nil
	}

	arms := make([]CompareArm, len(cmpData.Arms))
	for i, arm := range cmpData.Arms {
		arms[i] = CompareArm{
			Pattern:   l.lowerExpr(arm.Pattern),
			Guard:     l.lowerExpr(arm.Guard),
			Result:    l.lowerExpr(arm.Result),
			IsFinally: arm.IsFinally,
			Span:      arm.PatternSpan,
		}
	}

	return &Expr{
		Kind: ExprCompare,
		Type: ty,
		Span: expr.Span,
		Data: CompareData{
			Value: l.lowerExpr(cmpData.Value),
			Arms:  arms,
		},
	}
}

// lowerAwaitExpr lowers an await expression.
func (l *lowerer) lowerAwaitExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	awaitData := l.builder.Exprs.Awaits.Get(uint32(expr.Payload))
	if awaitData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprAwait,
		Type: ty,
		Span: expr.Span,
		Data: AwaitData{Value: l.lowerExpr(awaitData.Value)},
	}
}

// lowerSpawnExpr lowers a spawn expression.
func (l *lowerer) lowerSpawnExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	spawnData := l.builder.Exprs.Spawns.Get(uint32(expr.Payload))
	if spawnData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprSpawn,
		Type: ty,
		Span: expr.Span,
		Data: SpawnData{Value: l.lowerExpr(spawnData.Value)},
	}
}

// lowerAsyncExpr lowers an async block expression.
func (l *lowerer) lowerAsyncExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	asyncData := l.builder.Exprs.Asyncs.Get(uint32(expr.Payload))
	if asyncData == nil {
		return nil
	}

	var body *Block
	if asyncData.Body.IsValid() {
		body = l.lowerBlockOrWrap(asyncData.Body)
	}

	return &Expr{
		Kind: ExprAsync,
		Type: ty,
		Span: expr.Span,
		Data: AsyncData{Body: body},
	}
}

// lowerCastExpr lowers a cast expression.
func (l *lowerer) lowerCastExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	castData := l.builder.Exprs.Casts.Get(uint32(expr.Payload))
	if castData == nil {
		return nil
	}

	targetTy := l.lookupTypeFromAST(castData.Type)

	return &Expr{
		Kind: ExprCast,
		Type: ty,
		Span: expr.Span,
		Data: CastData{
			Value:    l.lowerExpr(castData.Value),
			TargetTy: targetTy,
		},
	}
}

// lowerBlockExpr lowers a block expression.
func (l *lowerer) lowerBlockExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	blockData := l.builder.Exprs.Blocks.Get(uint32(expr.Payload))
	if blockData == nil {
		return nil
	}

	block := &Block{Span: expr.Span}
	for _, stmtID := range blockData.Stmts {
		if s := l.lowerStmt(stmtID); s != nil {
			block.Stmts = append(block.Stmts, *s)
		}
	}

	return &Expr{
		Kind: ExprBlock,
		Type: ty,
		Span: expr.Span,
		Data: BlockExprData{Block: block},
	}
}
