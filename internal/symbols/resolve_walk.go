package symbols

import (
	"surge/internal/ast"
	"surge/internal/source"
)

func (fr *fileResolver) handleItem(id ast.ItemID) {
	item := fr.builder.Items.Get(id)
	if item == nil {
		return
	}
	switch item.Kind {
	case ast.ItemLet:
		if letItem, ok := fr.builder.Items.Let(id); ok && letItem != nil {
			if !fr.declareOnly {
				fr.walkTypeExpr(letItem.Type)
			}
			fr.declareLet(id, letItem)
		}
	case ast.ItemConst:
		if constItem, ok := fr.builder.Items.Const(id); ok && constItem != nil {
			if !fr.declareOnly {
				fr.walkTypeExpr(constItem.Type)
			}
			if syms := fr.result.ItemSymbols[id]; len(syms) == 0 {
				fr.declareConstItem(id, constItem)
			}
			if !fr.declareOnly && constItem.Value.IsValid() {
				fr.walkExpr(constItem.Value)
			}
		}
	case ast.ItemFn:
		if fnItem, ok := fr.builder.Items.Fn(id); ok && fnItem != nil {
			fr.declareFn(id, fnItem)
		}
	case ast.ItemType:
		if typeItem, ok := fr.builder.Items.Type(id); ok && typeItem != nil {
			fr.declareType(id, typeItem)
		}
	case ast.ItemContract:
		if contractItem, ok := fr.builder.Items.Contract(id); ok && contractItem != nil {
			fr.declareContract(id, contractItem)
		}
	case ast.ItemTag:
		if tagItem, ok := fr.builder.Items.Tag(id); ok && tagItem != nil {
			fr.declareTag(id, tagItem)
		}
	case ast.ItemImport:
		if importItem, ok := fr.builder.Items.Import(id); ok && importItem != nil {
			if fr.declareOnly {
				return
			}
			fr.declareImport(id, importItem, item.Span)
		}
	case ast.ItemExtern:
		if externItem, ok := fr.builder.Items.Extern(id); ok && externItem != nil {
			fr.handleExtern(id, externItem)
		}
	}
}

func (fr *fileResolver) walkFn(owner ScopeOwner, fnItem *ast.FnItem) {
	if fnItem == nil {
		return
	}
	fr.pushTypeParams(fnItem.Generics)
	defer fr.popTypeParams()
	if owner.SourceFile == 0 {
		owner.SourceFile = fr.sourceFile
	}
	if owner.ASTFile == 0 {
		owner.ASTFile = fr.fileID
	}
	scopeSpan := preferSpan(fnItem.ParamsSpan, fnItem.Span)
	scopeID := fr.resolver.Enter(ScopeFunction, owner, scopeSpan)
	paramIDs := fr.builder.Items.GetFnParamIDs(fnItem)
	for _, pid := range paramIDs {
		param := fr.builder.Items.FnParam(pid)
		if param == nil {
			continue
		}
		fr.walkTypeExpr(param.Type)
		if param.Name == source.NoStringID || fr.isWildcard(param.Name) {
			continue
		}
		span := param.Span
		if span == (source.Span{}) {
			span = fnItem.ParamsSpan
		}
		decl := SymbolDecl{
			SourceFile: fr.sourceFile,
			ASTFile:    fr.fileID,
			Item:       owner.Item,
		}
		fr.resolver.Declare(param.Name, span, SymbolParam, 0, decl)
	}
	fr.walkTypeExpr(fnItem.ReturnType)
	if fnItem.Body.IsValid() {
		fr.walkStmt(fnItem.Body)
	}
	fr.resolver.Leave(scopeID)
}

func (fr *fileResolver) walkStmt(stmtID ast.StmtID) {
	if !stmtID.IsValid() {
		return
	}
	stmt := fr.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return
	}
	switch stmt.Kind {
	case ast.StmtBlock:
		block := fr.builder.Stmts.Block(stmtID)
		if block == nil {
			return
		}
		owner := ScopeOwner{
			Kind:       ScopeOwnerStmt,
			SourceFile: fr.sourceFile,
			ASTFile:    fr.fileID,
			Stmt:       stmtID,
		}
		scopeID := fr.resolver.Enter(ScopeBlock, owner, stmt.Span)
		fr.predeclareConstStmts(block.Stmts)
		for _, child := range block.Stmts {
			fr.walkStmt(child)
		}
		fr.resolver.Leave(scopeID)
	case ast.StmtLet:
		letStmt := fr.builder.Stmts.Let(stmtID)
		if letStmt == nil {
			return
		}
		fr.walkTypeExpr(letStmt.Type)
		if letStmt.Value.IsValid() {
			fr.walkExpr(letStmt.Value)
		}
		if letStmt.Pattern.IsValid() {
			fr.bindLetPattern(letStmt.Pattern, letStmt.IsMut, stmt.Span, stmtID)
			return
		}
		if letStmt.Name == source.NoStringID || fr.isWildcard(letStmt.Name) {
			if letStmt.IsMut {
				fr.reportWildcardMut(stmt.Span)
			}
			return
		}
		flags := SymbolFlags(0)
		if letStmt.IsMut {
			flags |= SymbolFlagMutable
		}
		decl := SymbolDecl{
			SourceFile: fr.sourceFile,
			ASTFile:    fr.fileID,
			Stmt:       stmtID,
		}
		fr.resolver.Declare(letStmt.Name, stmt.Span, SymbolLet, flags, decl)
	case ast.StmtConst:
		constStmt := fr.builder.Stmts.Const(stmtID)
		if constStmt == nil || constStmt.Name == source.NoStringID {
			return
		}
		fr.walkTypeExpr(constStmt.Type)
		if constStmt.Value.IsValid() {
			fr.walkExpr(constStmt.Value)
		}
	case ast.StmtIf:
		ifStmt := fr.builder.Stmts.If(stmtID)
		if ifStmt == nil {
			return
		}
		fr.walkExpr(ifStmt.Cond)
		fr.walkStmt(ifStmt.Then)
		if ifStmt.Else.IsValid() {
			fr.walkStmt(ifStmt.Else)
		}
	case ast.StmtWhile:
		whileStmt := fr.builder.Stmts.While(stmtID)
		if whileStmt == nil {
			return
		}
		fr.walkExpr(whileStmt.Cond)
		fr.walkStmt(whileStmt.Body)
	case ast.StmtForClassic:
		forStmt := fr.builder.Stmts.ForClassic(stmtID)
		if forStmt == nil {
			return
		}
		owner := ScopeOwner{
			Kind:       ScopeOwnerStmt,
			SourceFile: fr.sourceFile,
			ASTFile:    fr.fileID,
			Stmt:       stmtID,
		}
		scopeID := fr.resolver.Enter(ScopeBlock, owner, stmt.Span)
		fr.predeclareConstStmt(forStmt.Init)
		if forStmt.Init.IsValid() {
			fr.walkStmt(forStmt.Init)
		}
		fr.walkExpr(forStmt.Cond)
		fr.walkExpr(forStmt.Post)
		fr.walkStmt(forStmt.Body)
		fr.resolver.Leave(scopeID)
	case ast.StmtForIn:
		forIn := fr.builder.Stmts.ForIn(stmtID)
		if forIn == nil {
			return
		}
		owner := ScopeOwner{
			Kind:       ScopeOwnerStmt,
			SourceFile: fr.sourceFile,
			ASTFile:    fr.fileID,
			Stmt:       stmtID,
		}
		scopeID := fr.resolver.Enter(ScopeBlock, owner, stmt.Span)
		fr.walkTypeExpr(forIn.Type)
		if forIn.Pattern != source.NoStringID && !fr.isWildcard(forIn.Pattern) {
			decl := SymbolDecl{
				SourceFile: fr.sourceFile,
				ASTFile:    fr.fileID,
				Stmt:       stmtID,
			}
			span := preferSpan(forIn.PatternSpan, stmt.Span)
			fr.resolver.Declare(forIn.Pattern, span, SymbolLet, 0, decl)
		}
		fr.walkExpr(forIn.Iterable)
		fr.walkStmt(forIn.Body)
		fr.resolver.Leave(scopeID)
	case ast.StmtExpr:
		exprStmt := fr.builder.Stmts.Expr(stmtID)
		if exprStmt != nil {
			fr.walkExpr(exprStmt.Expr)
		}
	case ast.StmtSignal:
		signalStmt := fr.builder.Stmts.Signal(stmtID)
		if signalStmt != nil {
			fr.walkExpr(signalStmt.Value)
		}
	case ast.StmtDrop:
		if dropStmt := fr.builder.Stmts.Drop(stmtID); dropStmt != nil {
			fr.walkExpr(dropStmt.Expr)
		}
	case ast.StmtReturn:
		returnStmt := fr.builder.Stmts.Return(stmtID)
		if returnStmt != nil {
			fr.walkExpr(returnStmt.Expr)
		}
	case ast.StmtBreak, ast.StmtContinue:
	default:
	}
}

func (fr *fileResolver) walkExpr(exprID ast.ExprID) {
	if !exprID.IsValid() {
		return
	}
	expr := fr.builder.Exprs.Get(exprID)
	if expr == nil {
		return
	}
	switch expr.Kind {
	case ast.ExprIdent:
		data, _ := fr.builder.Exprs.Ident(exprID)
		if data == nil {
			return
		}
		fr.resolveIdent(exprID, expr.Span, data.Name)
	case ast.ExprBinary:
		data, _ := fr.builder.Exprs.Binary(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Left)
		fr.walkExpr(data.Right)
	case ast.ExprUnary:
		data, _ := fr.builder.Exprs.Unary(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Operand)
	case ast.ExprCast:
		data, _ := fr.builder.Exprs.Cast(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Value)
	case ast.ExprCall:
		data, _ := fr.builder.Exprs.Call(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Target)
		for _, arg := range data.Args {
			fr.walkExpr(arg.Value)
		}
		fr.checkAmbiguousCall(data.Target)
	case ast.ExprIndex:
		data, _ := fr.builder.Exprs.Index(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Target)
		fr.walkExpr(data.Index)
	case ast.ExprMember:
		data, _ := fr.builder.Exprs.Member(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Target)
		fr.resolveMember(exprID, data)
	case ast.ExprAwait:
		data, _ := fr.builder.Exprs.Await(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Value)
	case ast.ExprGroup:
		data, _ := fr.builder.Exprs.Group(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Inner)
	case ast.ExprTuple:
		data, _ := fr.builder.Exprs.Tuple(exprID)
		if data == nil {
			return
		}
		for _, elem := range data.Elements {
			fr.walkExpr(elem)
		}
	case ast.ExprArray:
		data, _ := fr.builder.Exprs.Array(exprID)
		if data == nil {
			return
		}
		for _, elem := range data.Elements {
			fr.walkExpr(elem)
		}
	case ast.ExprSpread:
		data, _ := fr.builder.Exprs.Spread(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Value)
	case ast.ExprSpawn:
		data, _ := fr.builder.Exprs.Spawn(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Value)
	case ast.ExprAsync:
		data, _ := fr.builder.Exprs.Async(exprID)
		if data == nil || !data.Body.IsValid() {
			return
		}
		fr.walkStmt(data.Body)
	case ast.ExprParallel:
		data, _ := fr.builder.Exprs.Parallel(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Iterable)
		fr.walkExpr(data.Init)
		for _, arg := range data.Args {
			fr.walkExpr(arg)
		}
		fr.walkExpr(data.Body)
	case ast.ExprCompare:
		data, _ := fr.builder.Exprs.Compare(exprID)
		if data == nil {
			return
		}
		fr.walkExpr(data.Value)
		for _, arm := range data.Arms {
			scope := fr.resolver.Enter(ScopeBlock, ScopeOwner{
				Kind:       ScopeOwnerExpr,
				SourceFile: fr.sourceFile,
				ASTFile:    fr.fileID,
				Expr:       exprID,
			}, arm.PatternSpan)
			fr.bindComparePattern(arm.Pattern)
			fr.walkExpr(arm.Guard)
			fr.walkExpr(arm.Result)
			fr.resolver.Leave(scope)
		}
	case ast.ExprLit:
	}
}
func (fr *fileResolver) walkTypeExpr(typeID ast.TypeID) {
	if !typeID.IsValid() {
		return
	}
	typ := fr.builder.Types.Get(typeID)
	if typ == nil {
		return
	}
	switch typ.Kind {
	case ast.TypeExprUnary:
		if unary, ok := fr.builder.Types.UnaryType(typeID); ok && unary != nil {
			fr.walkTypeExpr(unary.Inner)
		}
	case ast.TypeExprArray:
		if arr, ok := fr.builder.Types.Array(typeID); ok && arr != nil {
			fr.walkTypeExpr(arr.Elem)
			if arr.Length.IsValid() {
				fr.walkExpr(arr.Length)
			}
		}
	case ast.TypeExprTuple:
		if tuple, ok := fr.builder.Types.Tuple(typeID); ok && tuple != nil {
			for _, elem := range tuple.Elems {
				fr.walkTypeExpr(elem)
			}
		}
	case ast.TypeExprFn:
		if fn, ok := fr.builder.Types.Fn(typeID); ok && fn != nil {
			for _, param := range fn.Params {
				fr.walkTypeExpr(param.Type)
			}
			fr.walkTypeExpr(fn.Return)
		}
	case ast.TypeExprOptional:
		if opt, ok := fr.builder.Types.Optional(typeID); ok && opt != nil {
			fr.walkTypeExpr(opt.Inner)
		}
	case ast.TypeExprErrorable:
		if errable, ok := fr.builder.Types.Errorable(typeID); ok && errable != nil {
			fr.walkTypeExpr(errable.Inner)
			fr.walkTypeExpr(errable.Error)
		}
	case ast.TypeExprPath:
		if path, ok := fr.builder.Types.Path(typeID); ok && path != nil {
			for _, seg := range path.Segments {
				for _, gen := range seg.Generics {
					fr.walkTypeExpr(gen)
				}
			}
		}
	}
}

func (fr *fileResolver) predeclareConstStmts(stmts []ast.StmtID) {
	for _, stmtID := range stmts {
		fr.predeclareConstStmt(stmtID)
	}
}

func (fr *fileResolver) predeclareConstStmt(stmtID ast.StmtID) {
	if !stmtID.IsValid() {
		return
	}
	stmt := fr.builder.Stmts.Get(stmtID)
	if stmt == nil || stmt.Kind != ast.StmtConst {
		return
	}
	constStmt := fr.builder.Stmts.Const(stmtID)
	if constStmt == nil || constStmt.Name == source.NoStringID {
		return
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Stmt:       stmtID,
	}
	fr.resolver.Declare(constStmt.Name, stmt.Span, SymbolConst, 0, decl)
}
