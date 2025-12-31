package hir

import (
	"surge/internal/ast"
	"surge/internal/types"
)

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
			Data: ReturnData{Value: value, IsTail: false},
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
		Name:     l.lookupString(letStmt.Name),
		SymbolID: l.symbolForStmt(stmtID),
		IsMut:    letStmt.IsMut,
		IsConst:  false,
	}

	if letStmt.Type.IsValid() {
		data.Type = l.lookupTypeFromAST(letStmt.Type)
	}

	if letStmt.Value.IsValid() {
		data.Value = l.lowerExpr(letStmt.Value)
	}

	if letStmt.Pattern.IsValid() {
		data.Pattern = l.lowerExpr(letStmt.Pattern)
	}

	// Prefer the binding type from sema (covers explicit annotations and pattern bindings).
	if data.SymbolID.IsValid() && l.semaRes != nil && l.semaRes.BindingTypes != nil {
		if ty := l.semaRes.BindingTypes[data.SymbolID]; ty != types.NoTypeID {
			data.Type = ty
		}
	}

	// Fallback to initializer type if sema type is not available.
	if data.Type == types.NoTypeID && data.Value != nil {
		data.Type = data.Value.Type
	}

	if data.Value == nil && data.Pattern == nil && data.Type != types.NoTypeID {
		data.Value = l.defaultValueExpr(stmt.Span, data.Type)
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
		Name:     l.lookupString(constStmt.Name),
		SymbolID: l.symbolForStmt(stmtID),
		IsMut:    false,
		IsConst:  true,
	}

	if constStmt.Type.IsValid() {
		data.Type = l.lookupTypeFromAST(constStmt.Type)
	}

	if constStmt.Value.IsValid() {
		data.Value = l.lowerExpr(constStmt.Value)
	}

	if data.SymbolID.IsValid() && l.semaRes != nil && l.semaRes.BindingTypes != nil {
		if ty := l.semaRes.BindingTypes[data.SymbolID]; ty != types.NoTypeID {
			data.Type = ty
		}
	}
	if data.Type == types.NoTypeID && data.Value != nil {
		data.Type = data.Value.Type
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

	data.VarSym = l.symbolForStmt(stmtID)

	if forStmt.Type.IsValid() {
		data.VarType = l.lookupTypeFromAST(forStmt.Type)
	}
	if data.VarSym.IsValid() && l.semaRes != nil && l.semaRes.BindingTypes != nil {
		if ty := l.semaRes.BindingTypes[data.VarSym]; ty != types.NoTypeID {
			data.VarType = ty
		}
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

// ensureExplicitReturn handles function return semantics:
// - For functions returning nothing: adds implicit "return" (no value) if missing
// - For functions returning a value: converts last expression to return (Rust-style tail return)
//
// Surge semantics: missing return means "return nothing", NOT "return last expression".
// Tail return conversion only applies to non-nothing functions.
func (l *lowerer) ensureExplicitReturn(fn *Func) {
	if fn.Body == nil || fn.Body.IsEmpty() {
		return
	}

	// Check if function returns void/nothing
	isNothingReturn := fn.Result == types.NoTypeID || l.isNothingType(fn.Result)

	lastStmt := fn.Body.LastStmt()

	// If last statement is already a return, nothing to do
	if lastStmt != nil && lastStmt.Kind == StmtReturn {
		data, ok := lastStmt.Data.(ReturnData)
		if ok && !data.IsTail {
			data.IsTail = true
			fn.Body.Stmts[len(fn.Body.Stmts)-1].Data = data
		}
		return
	}

	if isNothingReturn {
		// For nothing-returning functions: add implicit "return" (no value)
		// Expression statements stay as statements, we just add return at end
		fn.Body.Stmts = append(fn.Body.Stmts, Stmt{
			Kind: StmtReturn,
			Span: fn.Body.Span.ZeroideToEnd(),
			Data: ReturnData{Value: nil, IsTail: true},
		})
		return
	}

	// For non-nothing functions: convert last expression to return (tail return)
	if lastStmt != nil && lastStmt.Kind == StmtExpr {
		exprData, ok := lastStmt.Data.(ExprStmtData)
		if ok && exprData.Expr != nil {
			// Replace the last statement with a return
			fn.Body.Stmts[len(fn.Body.Stmts)-1] = Stmt{
				Kind: StmtReturn,
				Span: lastStmt.Span,
				Data: ReturnData{Value: exprData.Expr, IsTail: true},
			}
		}
	}
}

func (l *lowerer) markTailReturn(block *Block) {
	if block == nil || len(block.Stmts) == 0 {
		return
	}
	last := block.Stmts[len(block.Stmts)-1]
	if last.Kind != StmtReturn {
		return
	}
	data, ok := last.Data.(ReturnData)
	if !ok || data.IsTail {
		return
	}
	data.IsTail = true
	block.Stmts[len(block.Stmts)-1].Data = data
}

// isNothingType checks if the given type is the "nothing" type.
func (l *lowerer) isNothingType(ty types.TypeID) bool {
	if ty == types.NoTypeID {
		return false
	}
	if l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return false
	}
	t, ok := l.semaRes.TypeInterner.Lookup(ty)
	if !ok {
		return false
	}
	return t.Kind == types.KindNothing
}
