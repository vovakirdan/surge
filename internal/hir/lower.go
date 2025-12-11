package hir

import (
	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Lower transforms an AST module with semantic analysis results into HIR.
// It performs minimal desugaring:
// - Inserts explicit return for last expression in non-void functions
// - Removes ExprGroup (parentheses) by unwrapping
//
// The lowering preserves high-level constructs like compare, for, async/spawn.
func Lower(
	builder *ast.Builder,
	fileID ast.FileID,
	semaRes *sema.Result,
	symRes *symbols.Result,
) (*Module, error) {
	if builder == nil || fileID == ast.NoFileID || semaRes == nil {
		return nil, nil
	}

	l := &lowerer{
		builder:  builder,
		semaRes:  semaRes,
		symRes:   symRes,
		strings:  builder.StringsInterner,
		nextFnID: 1,
		module:   &Module{SourceAST: fileID},
	}

	l.lowerFile(fileID)

	return l.module, nil
}

// lowerer holds context for the lowering pass.
type lowerer struct {
	builder  *ast.Builder
	semaRes  *sema.Result
	symRes   *symbols.Result
	strings  *source.Interner
	module   *Module
	nextFnID FuncID
}

// lowerFile processes all items in an AST file.
func (l *lowerer) lowerFile(fileID ast.FileID) {
	file := l.builder.Files.Get(fileID)
	if file == nil {
		return
	}

	for _, itemID := range file.Items {
		item := l.builder.Items.Arena.Get(uint32(itemID))
		if item == nil {
			continue
		}

		switch item.Kind {
		case ast.ItemFn:
			if fn := l.lowerFnItem(itemID); fn != nil {
				l.module.Funcs = append(l.module.Funcs, fn)
			}
		case ast.ItemLet:
			if v := l.lowerLetItem(itemID); v != nil {
				l.module.Globals = append(l.module.Globals, *v)
			}
		case ast.ItemConst:
			if c := l.lowerConstItem(itemID); c != nil {
				l.module.Consts = append(l.module.Consts, *c)
			}
		case ast.ItemType:
			if t := l.lowerTypeItem(itemID); t != nil {
				l.module.Types = append(l.module.Types, *t)
			}
		case ast.ItemTag:
			if t := l.lowerTagItem(itemID); t != nil {
				l.module.Types = append(l.module.Types, *t)
			}
		case ast.ItemContract:
			if c := l.lowerContractItem(itemID); c != nil {
				l.module.Types = append(l.module.Types, *c)
			}
			// ItemImport, ItemPragma, ItemExtern, ItemMacro are not lowered to HIR
		}
	}
}

// lowerFnItem lowers a function declaration to HIR.
func (l *lowerer) lowerFnItem(itemID ast.ItemID) *Func {
	fnItem, ok := l.builder.Items.Fn(itemID)
	if !ok || fnItem == nil {
		return nil
	}

	name := l.lookupString(fnItem.Name)
	fnID := l.nextFnID
	l.nextFnID++

	// Get symbol ID for this function
	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	fn := &Func{
		ID:       fnID,
		Name:     name,
		SymbolID: symID,
		Span:     fnItem.Span,
		Result:   types.NoTypeID,
	}

	// Process modifiers/flags
	if fnItem.Flags&ast.FnModifierAsync != 0 {
		fn.Flags |= FuncAsync
	}
	if fnItem.Flags&ast.FnModifierPublic != 0 {
		fn.Flags |= FuncPublic
	}

	// Check for attributes (@intrinsic, @entrypoint, @overload, @override)
	fn.Flags |= l.extractFnFlags(fnItem)

	// Process generic parameters
	fn.GenericParams = l.lowerGenericParams(fnItem)

	// Process function parameters
	fn.Params = l.lowerFnParams(fnItem)

	// Process return type
	if fnItem.ReturnType.IsValid() {
		// Get the actual resolved type from sema (if we can find it)
		// For now, we just note that there's a return type
		fn.Result = l.lookupTypeFromAST(fnItem.ReturnType)
	}

	// Process body
	if fnItem.Body.IsValid() {
		fn.Body = l.lowerBlockStmt(fnItem.Body)

		// Insert explicit return for last expression if needed
		l.ensureExplicitReturn(fn)
	}

	return fn
}

// extractFnFlags extracts function flags from attributes.
func (l *lowerer) extractFnFlags(fnItem *ast.FnItem) FuncFlags {
	var flags FuncFlags
	if fnItem.AttrCount == 0 || !fnItem.AttrStart.IsValid() {
		return flags
	}

	for i := range fnItem.AttrCount {
		attrID := ast.AttrID(uint32(fnItem.AttrStart) + i)
		attr := l.builder.Items.Attrs.Get(uint32(attrID))
		if attr == nil {
			continue
		}
		name := l.lookupString(attr.Name)
		switch name {
		case "intrinsic":
			flags |= FuncIntrinsic
		case "entrypoint":
			flags |= FuncEntrypoint
		case "overload":
			flags |= FuncOverload
		case "override":
			flags |= FuncOverride
		}
	}
	return flags
}

// lowerGenericParams lowers generic type parameters.
func (l *lowerer) lowerGenericParams(fnItem *ast.FnItem) []GenericParam {
	typeParamIDs := l.builder.Items.GetFnTypeParamIDs(fnItem)
	if len(typeParamIDs) == 0 {
		return nil
	}

	params := make([]GenericParam, 0, len(typeParamIDs))
	for _, tpID := range typeParamIDs {
		tp := l.builder.Items.TypeParams.Get(uint32(tpID))
		if tp == nil {
			continue
		}
		params = append(params, GenericParam{
			Name: l.lookupString(tp.Name),
			Span: tp.Span,
			// Bounds would be processed here if needed
		})
	}
	return params
}

// lowerFnParams lowers function parameters.
func (l *lowerer) lowerFnParams(fnItem *ast.FnItem) []Param {
	paramIDs := l.builder.Items.GetFnParamIDs(fnItem)
	if len(paramIDs) == 0 {
		return nil
	}

	params := make([]Param, 0, len(paramIDs))
	for _, paramID := range paramIDs {
		param := l.builder.Items.FnParam(paramID)
		if param == nil {
			continue
		}

		p := Param{
			Name:       l.lookupString(param.Name),
			Type:       l.lookupTypeFromAST(param.Type),
			Ownership:  l.inferOwnership(l.lookupTypeFromAST(param.Type)),
			Span:       param.Span,
			HasDefault: param.Default.IsValid(),
		}

		if param.Default.IsValid() {
			p.Default = l.lowerExpr(param.Default)
		}

		params = append(params, p)
	}
	return params
}

// lowerBlockStmt lowers a block statement to an HIR Block.
func (l *lowerer) lowerBlockStmt(stmtID ast.StmtID) *Block {
	blockData := l.builder.Stmts.Block(stmtID)
	if blockData == nil {
		return &Block{}
	}

	stmt := l.builder.Stmts.Get(stmtID)
	block := &Block{
		Span: stmt.Span,
	}

	for _, childID := range blockData.Stmts {
		if s := l.lowerStmt(childID); s != nil {
			block.Stmts = append(block.Stmts, *s)
		}
	}

	return block
}

// lowerStmt lowers an AST statement to HIR.
func (l *lowerer) lowerStmt(stmtID ast.StmtID) *Stmt {
	stmt := l.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return nil
	}

	switch stmt.Kind {
	case ast.StmtBlock:
		block := l.lowerBlockStmt(stmtID)
		return &Stmt{
			Kind: StmtBlock,
			Span: stmt.Span,
			Data: BlockStmtData{Block: block},
		}

	case ast.StmtLet:
		return l.lowerLetStmt(stmtID, stmt)

	case ast.StmtConst:
		return l.lowerConstStmt(stmtID, stmt)

	case ast.StmtExpr:
		exprStmt := l.builder.Stmts.Expr(stmtID)
		if exprStmt == nil {
			return nil
		}
		return &Stmt{
			Kind: StmtExpr,
			Span: stmt.Span,
			Data: ExprStmtData{Expr: l.lowerExpr(exprStmt.Expr)},
		}

	case ast.StmtReturn:
		retStmt := l.builder.Stmts.Return(stmtID)
		var value *Expr
		if retStmt != nil && retStmt.Expr.IsValid() {
			value = l.lowerExpr(retStmt.Expr)
		}
		return &Stmt{
			Kind: StmtReturn,
			Span: stmt.Span,
			Data: ReturnData{Value: value},
		}

	case ast.StmtBreak:
		return &Stmt{
			Kind: StmtBreak,
			Span: stmt.Span,
			Data: BreakData{},
		}

	case ast.StmtContinue:
		return &Stmt{
			Kind: StmtContinue,
			Span: stmt.Span,
			Data: ContinueData{},
		}

	case ast.StmtIf:
		return l.lowerIfStmt(stmtID, stmt)

	case ast.StmtWhile:
		return l.lowerWhileStmt(stmtID, stmt)

	case ast.StmtForClassic:
		return l.lowerForClassicStmt(stmtID, stmt)

	case ast.StmtForIn:
		return l.lowerForInStmt(stmtID, stmt)

	case ast.StmtDrop:
		dropStmt := l.builder.Stmts.Drop(stmtID)
		if dropStmt == nil {
			return nil
		}
		return &Stmt{
			Kind: StmtDrop,
			Span: stmt.Span,
			Data: DropData{Value: l.lowerExpr(dropStmt.Expr)},
		}

	case ast.StmtSignal:
		// Signal is reserved for v2+, skip
		return nil

	default:
		return nil
	}
}

// lowerLetStmt lowers a let statement.
func (l *lowerer) lowerLetStmt(stmtID ast.StmtID, stmt *ast.Stmt) *Stmt {
	letStmt := l.builder.Stmts.Let(stmtID)
	if letStmt == nil {
		return nil
	}

	data := LetData{
		Name:    l.lookupString(letStmt.Name),
		IsMut:   letStmt.IsMut,
		IsConst: false,
	}

	if letStmt.Type.IsValid() {
		data.Type = l.lookupTypeFromAST(letStmt.Type)
	}

	if letStmt.Value.IsValid() {
		data.Value = l.lowerExpr(letStmt.Value)
		// If no explicit type, try to get from sema
		if data.Type == types.NoTypeID && data.Value != nil {
			data.Type = data.Value.Type
		}
	}

	if letStmt.Pattern.IsValid() {
		data.Pattern = l.lowerExpr(letStmt.Pattern)
	}

	data.Ownership = l.inferOwnership(data.Type)

	return &Stmt{
		Kind: StmtLet,
		Span: stmt.Span,
		Data: data,
	}
}

// lowerConstStmt lowers a const statement.
func (l *lowerer) lowerConstStmt(stmtID ast.StmtID, stmt *ast.Stmt) *Stmt {
	constStmt := l.builder.Stmts.Const(stmtID)
	if constStmt == nil {
		return nil
	}

	data := LetData{
		Name:    l.lookupString(constStmt.Name),
		IsMut:   false,
		IsConst: true,
	}

	if constStmt.Type.IsValid() {
		data.Type = l.lookupTypeFromAST(constStmt.Type)
	}

	if constStmt.Value.IsValid() {
		data.Value = l.lowerExpr(constStmt.Value)
		if data.Type == types.NoTypeID && data.Value != nil {
			data.Type = data.Value.Type
		}
	}

	data.Ownership = l.inferOwnership(data.Type)

	return &Stmt{
		Kind: StmtLet, // Const is lowered to immutable let
		Span: stmt.Span,
		Data: data,
	}
}

// lowerIfStmt lowers an if statement.
func (l *lowerer) lowerIfStmt(stmtID ast.StmtID, stmt *ast.Stmt) *Stmt {
	ifStmt := l.builder.Stmts.If(stmtID)
	if ifStmt == nil {
		return nil
	}

	data := IfStmtData{
		Cond: l.lowerExpr(ifStmt.Cond),
	}

	if ifStmt.Then.IsValid() {
		data.Then = l.lowerBlockOrWrap(ifStmt.Then)
	}

	if ifStmt.Else.IsValid() {
		data.Else = l.lowerBlockOrWrap(ifStmt.Else)
	}

	return &Stmt{
		Kind: StmtIf,
		Span: stmt.Span,
		Data: data,
	}
}

// lowerWhileStmt lowers a while statement.
func (l *lowerer) lowerWhileStmt(stmtID ast.StmtID, stmt *ast.Stmt) *Stmt {
	whileStmt := l.builder.Stmts.While(stmtID)
	if whileStmt == nil {
		return nil
	}

	data := WhileData{
		Cond: l.lowerExpr(whileStmt.Cond),
	}

	if whileStmt.Body.IsValid() {
		data.Body = l.lowerBlockOrWrap(whileStmt.Body)
	}

	return &Stmt{
		Kind: StmtWhile,
		Span: stmt.Span,
		Data: data,
	}
}

// lowerForClassicStmt lowers a classic for statement.
func (l *lowerer) lowerForClassicStmt(stmtID ast.StmtID, stmt *ast.Stmt) *Stmt {
	forStmt := l.builder.Stmts.ForClassic(stmtID)
	if forStmt == nil {
		return nil
	}

	data := ForData{
		Kind: ForClassic,
	}

	if forStmt.Init.IsValid() {
		data.Init = l.lowerStmt(forStmt.Init)
	}
	if forStmt.Cond.IsValid() {
		data.Cond = l.lowerExpr(forStmt.Cond)
	}
	if forStmt.Post.IsValid() {
		data.Post = l.lowerExpr(forStmt.Post)
	}
	if forStmt.Body.IsValid() {
		data.Body = l.lowerBlockOrWrap(forStmt.Body)
	}

	return &Stmt{
		Kind: StmtFor,
		Span: stmt.Span,
		Data: data,
	}
}

// lowerForInStmt lowers a for-in statement.
func (l *lowerer) lowerForInStmt(stmtID ast.StmtID, stmt *ast.Stmt) *Stmt {
	forStmt := l.builder.Stmts.ForIn(stmtID)
	if forStmt == nil {
		return nil
	}

	data := ForData{
		Kind:    ForIn,
		VarName: l.lookupString(forStmt.Pattern),
	}

	if forStmt.Type.IsValid() {
		data.VarType = l.lookupTypeFromAST(forStmt.Type)
	}
	if forStmt.Iterable.IsValid() {
		data.Iterable = l.lowerExpr(forStmt.Iterable)
	}
	if forStmt.Body.IsValid() {
		data.Body = l.lowerBlockOrWrap(forStmt.Body)
	}

	return &Stmt{
		Kind: StmtFor,
		Span: stmt.Span,
		Data: data,
	}
}

// lowerBlockOrWrap ensures a statement is wrapped in a block if needed.
func (l *lowerer) lowerBlockOrWrap(stmtID ast.StmtID) *Block {
	stmt := l.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return &Block{}
	}

	if stmt.Kind == ast.StmtBlock {
		return l.lowerBlockStmt(stmtID)
	}

	// Wrap single statement in a block
	s := l.lowerStmt(stmtID)
	if s == nil {
		return &Block{}
	}
	return &Block{
		Stmts: []Stmt{*s},
		Span:  stmt.Span,
	}
}

// lowerExpr lowers an AST expression to HIR.
func (l *lowerer) lowerExpr(exprID ast.ExprID) *Expr {
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
		return l.lowerLiteralExpr(exprID, expr, ty)

	case ast.ExprBinary:
		return l.lowerBinaryExpr(exprID, expr, ty)

	case ast.ExprUnary:
		return l.lowerUnaryExpr(exprID, expr, ty)

	case ast.ExprCall:
		return l.lowerCallExpr(exprID, expr, ty)

	case ast.ExprMember:
		return l.lowerMemberExpr(exprID, expr, ty)

	case ast.ExprIndex:
		return l.lowerIndexExpr(exprID, expr, ty)

	case ast.ExprTuple:
		return l.lowerTupleExpr(exprID, expr, ty)

	case ast.ExprArray:
		return l.lowerArrayExpr(exprID, expr, ty)

	case ast.ExprStruct:
		return l.lowerStructExpr(exprID, expr, ty)

	case ast.ExprTernary:
		return l.lowerTernaryExpr(exprID, expr, ty)

	case ast.ExprCompare:
		return l.lowerCompareExpr(exprID, expr, ty)

	case ast.ExprAwait:
		return l.lowerAwaitExpr(exprID, expr, ty)

	case ast.ExprSpawn:
		return l.lowerSpawnExpr(exprID, expr, ty)

	case ast.ExprAsync:
		return l.lowerAsyncExpr(exprID, expr, ty)

	case ast.ExprCast:
		return l.lowerCastExpr(exprID, expr, ty)

	case ast.ExprBlock:
		return l.lowerBlockExpr(exprID, expr, ty)

	case ast.ExprTupleIndex:
		return l.lowerTupleIndexExpr(exprID, expr, ty)

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
func (l *lowerer) lowerLiteralExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
	litData := l.builder.Exprs.Literals.Get(uint32(expr.Payload))
	if litData == nil {
		return nil
	}

	data := LiteralData{}
	rawValue := l.lookupString(litData.Value)

	switch litData.Kind {
	case ast.ExprLitInt, ast.ExprLitUint:
		data.Kind = LiteralInt
		// Parse the value - simplified, would need proper parsing
		data.IntValue = parseIntLiteral(rawValue)
	case ast.ExprLitFloat:
		data.Kind = LiteralFloat
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
func (l *lowerer) lowerBinaryExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
	binData := l.builder.Exprs.Binaries.Get(uint32(expr.Payload))
	if binData == nil {
		return nil
	}

	// Check if this is an assignment - special handling
	if isAssignOp(binData.Op) {
		// This should probably be a statement, but we'll handle it as expression
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
func (l *lowerer) lowerUnaryExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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

	callee := l.lowerExpr(callData.Target)
	args := make([]*Expr, len(callData.Args))
	for i, arg := range callData.Args {
		args[i] = l.lowerExpr(arg.Value)
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		symID = l.symRes.ExprSymbols[exprID]
	}

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
func (l *lowerer) lowerMemberExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
	memberData := l.builder.Exprs.Members.Get(uint32(expr.Payload))
	if memberData == nil {
		return nil
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

// lowerIndexExpr lowers an index expression.
func (l *lowerer) lowerIndexExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerTupleExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerTupleIndexExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerArrayExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerStructExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerTernaryExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerCompareExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerAwaitExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerSpawnExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerAsyncExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerCastExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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
func (l *lowerer) lowerBlockExpr(_ ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
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

// lowerLetItem lowers a top-level let declaration.
func (l *lowerer) lowerLetItem(itemID ast.ItemID) *VarDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemLet {
		return nil
	}

	letItem := l.builder.Items.Lets.Get(uint32(item.Payload))
	if letItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	decl := &VarDecl{
		Name:     l.lookupString(letItem.Name),
		SymbolID: symID,
		IsMut:    letItem.IsMut,
		Span:     item.Span,
	}

	if letItem.Type.IsValid() {
		decl.Type = l.lookupTypeFromAST(letItem.Type)
	}

	if letItem.Value.IsValid() {
		decl.Value = l.lowerExpr(letItem.Value)
		if decl.Type == types.NoTypeID && decl.Value != nil {
			decl.Type = decl.Value.Type
		}
	}

	return decl
}

// lowerConstItem lowers a top-level const declaration.
func (l *lowerer) lowerConstItem(itemID ast.ItemID) *ConstDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemConst {
		return nil
	}

	constItem := l.builder.Items.Consts.Get(uint32(item.Payload))
	if constItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	decl := &ConstDecl{
		Name:     l.lookupString(constItem.Name),
		SymbolID: symID,
		Span:     item.Span,
	}

	if constItem.Type.IsValid() {
		decl.Type = l.lookupTypeFromAST(constItem.Type)
	}

	if constItem.Value.IsValid() {
		decl.Value = l.lowerExpr(constItem.Value)
		if decl.Type == types.NoTypeID && decl.Value != nil {
			decl.Type = decl.Value.Type
		}
	}

	return decl
}

// lowerTypeItem lowers a type declaration.
func (l *lowerer) lowerTypeItem(itemID ast.ItemID) *TypeDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemType {
		return nil
	}

	typeItem := l.builder.Items.Types.Get(uint32(item.Payload))
	if typeItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	decl := &TypeDecl{
		Name:     l.lookupString(typeItem.Name),
		SymbolID: symID,
		Span:     item.Span,
	}

	// Determine kind from AST type declaration kind
	switch typeItem.Kind {
	case ast.TypeDeclStruct:
		decl.Kind = TypeDeclStruct
	case ast.TypeDeclUnion:
		decl.Kind = TypeDeclUnion
	case ast.TypeDeclEnum:
		decl.Kind = TypeDeclEnum
	case ast.TypeDeclAlias:
		decl.Kind = TypeDeclAlias
	}

	return decl
}

// lowerTagItem lowers a tag declaration.
func (l *lowerer) lowerTagItem(itemID ast.ItemID) *TypeDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemTag {
		return nil
	}

	tagItem := l.builder.Items.Tags.Get(uint32(item.Payload))
	if tagItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	return &TypeDecl{
		Name:     l.lookupString(tagItem.Name),
		SymbolID: symID,
		Kind:     TypeDeclTag,
		Span:     item.Span,
	}
}

// lowerContractItem lowers a contract declaration.
func (l *lowerer) lowerContractItem(itemID ast.ItemID) *TypeDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemContract {
		return nil
	}

	contractItem := l.builder.Items.Contracts.Get(uint32(item.Payload))
	if contractItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	return &TypeDecl{
		Name:     l.lookupString(contractItem.Name),
		SymbolID: symID,
		Kind:     TypeDeclContract,
		Span:     item.Span,
	}
}

// ensureExplicitReturn inserts explicit return if the last expression
// in a non-void function is not a return statement.
func (l *lowerer) ensureExplicitReturn(fn *Func) {
	if fn.Body == nil || fn.Body.IsEmpty() {
		return
	}

	// Check if function returns void/nothing
	if fn.Result == types.NoTypeID {
		return
	}

	lastStmt := fn.Body.LastStmt()
	if lastStmt == nil {
		return
	}

	// If last statement is already a return, nothing to do
	if lastStmt.Kind == StmtReturn {
		return
	}

	// If last statement is an expression statement, convert to return
	if lastStmt.Kind == StmtExpr {
		exprData, ok := lastStmt.Data.(ExprStmtData)
		if ok && exprData.Expr != nil {
			// Replace the last statement with a return
			fn.Body.Stmts[len(fn.Body.Stmts)-1] = Stmt{
				Kind: StmtReturn,
				Span: lastStmt.Span,
				Data: ReturnData{Value: exprData.Expr},
			}
		}
	}
}

// Helper functions

func (l *lowerer) lookupString(id source.StringID) string {
	if l.strings == nil || id == source.NoStringID {
		return ""
	}
	s, _ := l.strings.Lookup(id)
	return s
}

func (l *lowerer) lookupTypeFromAST(astTypeID ast.TypeID) types.TypeID {
	// AST TypeID is different from types.TypeID
	// For now, we return NoTypeID - the actual type would need
	// to be resolved through the type checker's type expressions
	// This is a simplification - in a full implementation,
	// we'd need to map AST type expressions to resolved types
	return types.NoTypeID
}

func (l *lowerer) inferOwnership(ty types.TypeID) Ownership {
	if ty == types.NoTypeID || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return OwnershipNone
	}

	t, ok := l.semaRes.TypeInterner.Lookup(ty)
	if !ok {
		return OwnershipNone
	}

	switch t.Kind {
	case types.KindReference:
		if t.Mutable {
			return OwnershipRefMut
		}
		return OwnershipRef
	case types.KindPointer:
		return OwnershipPtr
	case types.KindOwn:
		return OwnershipOwn
	case types.KindInt, types.KindUint, types.KindFloat, types.KindBool:
		return OwnershipCopy
	default:
		return OwnershipNone
	}
}

func isAssignOp(op ast.ExprBinaryOp) bool {
	switch op {
	case ast.ExprBinaryAssign,
		ast.ExprBinaryAddAssign,
		ast.ExprBinarySubAssign,
		ast.ExprBinaryMulAssign,
		ast.ExprBinaryDivAssign,
		ast.ExprBinaryModAssign,
		ast.ExprBinaryBitAndAssign,
		ast.ExprBinaryBitOrAssign,
		ast.ExprBinaryBitXorAssign,
		ast.ExprBinaryShlAssign,
		ast.ExprBinaryShrAssign:
		return true
	}
	return false
}

//nolint:gocritic // ifElseChain is clearer than switch for character ranges
func parseIntLiteral(s string) int64 {
	// Simplified parsing - would need proper implementation
	var result int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int64(c-'0')
		} else if c == '_' {
			continue
		} else {
			break
		}
	}
	return result
}

//nolint:gocritic // ifElseChain is clearer than switch for character ranges
func parseFloatLiteral(s string) float64 {
	// Simplified parsing - would need proper implementation
	var result float64
	var frac float64 = 1
	var inFrac bool
	for _, c := range s {
		if c >= '0' && c <= '9' {
			if inFrac {
				frac /= 10
				result += float64(c-'0') * frac
			} else {
				result = result*10 + float64(c-'0')
			}
		} else if c == '.' {
			inFrac = true
		} else if c == '_' {
			continue
		} else {
			break
		}
	}
	return result
}
