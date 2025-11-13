package symbols

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
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
			fr.declareLet(id, letItem)
		}
	case ast.ItemFn:
		if fnItem, ok := fr.builder.Items.Fn(id); ok && fnItem != nil {
			fr.declareFn(id, fnItem)
		}
	case ast.ItemType:
		if typeItem, ok := fr.builder.Items.Type(id); ok && typeItem != nil {
			fr.declareType(id, typeItem)
		}
	case ast.ItemTag:
		if tagItem, ok := fr.builder.Items.Tag(id); ok && tagItem != nil {
			fr.declareTag(id, tagItem)
		}
	case ast.ItemImport:
		if importItem, ok := fr.builder.Items.Import(id); ok && importItem != nil {
			fr.declareImport(id, importItem, item.Span)
		}
	case ast.ItemExtern:
		if externItem, ok := fr.builder.Items.Extern(id); ok && externItem != nil {
			fr.handleExtern(id, externItem)
		}
	}
}

func (fr *fileResolver) walkFn(itemID ast.ItemID, fnItem *ast.FnItem) {
	if fnItem == nil {
		return
	}
	owner := ScopeOwner{
		Kind:       ScopeOwnerItem,
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	scopeSpan := preferSpan(fnItem.ParamsSpan, fnItem.Span)
	scopeID := fr.resolver.Enter(ScopeFunction, owner, scopeSpan)
	paramIDs := fr.builder.Items.GetFnParamIDs(fnItem)
	for _, pid := range paramIDs {
		param := fr.builder.Items.FnParam(pid)
		if param == nil || param.Name == source.NoStringID {
			continue
		}
		span := param.Span
		if span == (source.Span{}) {
			span = fnItem.ParamsSpan
		}
		decl := SymbolDecl{
			SourceFile: fr.sourceFile,
			ASTFile:    fr.fileID,
			Item:       itemID,
		}
		fr.resolver.Declare(param.Name, span, SymbolParam, 0, decl)
	}
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
		for _, child := range block.Stmts {
			fr.walkStmt(child)
		}
		fr.resolver.Leave(scopeID)
	case ast.StmtLet:
		letStmt := fr.builder.Stmts.Let(stmtID)
		if letStmt == nil || letStmt.Name == source.NoStringID {
			return
		}
		if letStmt.Value.IsValid() {
			fr.walkExpr(letStmt.Value)
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
		if forIn.Pattern != source.NoStringID {
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
			fr.walkExpr(arg)
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
			fr.walkExpr(arm.Pattern)
			fr.walkExpr(arm.Guard)
			fr.walkExpr(arm.Result)
		}
	case ast.ExprLit:
	}
}

func (fr *fileResolver) resolveIdent(exprID ast.ExprID, span source.Span, name source.StringID) {
	if name == source.NoStringID || fr.resolver == nil {
		return
	}
	if symID, ok := fr.resolver.Lookup(name); ok {
		fr.result.ExprSymbols[exprID] = symID
		return
	}
	fr.reportUnresolved(name, span)
}

func (fr *fileResolver) reportUnresolved(name source.StringID, span source.Span) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	if nameStr == "_" {
		return
	}
	msg := fmt.Sprintf("cannot resolve '%s'", nameStr)
	if b := diag.ReportError(fr.resolver.reporter, diag.SemaUnresolvedSymbol, span, msg); b != nil {
		b.Emit()
	}
}

func (fr *fileResolver) checkAmbiguousCall(target ast.ExprID) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	targetExpr := fr.builder.Exprs.Get(target)
	if targetExpr == nil || targetExpr.Kind != ast.ExprIdent {
		return
	}
	data, _ := fr.builder.Exprs.Ident(target)
	if data == nil || data.Name == source.NoStringID {
		return
	}
	fnSyms := fr.collectFileScopeSymbols(data.Name, SymbolFunction)
	if len(fnSyms) == 0 {
		return
	}
	tagSyms := fr.collectFileScopeSymbols(data.Name, SymbolTag)
	if len(tagSyms) == 0 {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(data.Name)
	msg := fmt.Sprintf("identifier '%s' matches both a function and a tag constructor", nameStr)
	if b := diag.ReportError(fr.resolver.reporter, diag.SemaAmbiguousCtorOrFn, targetExpr.Span, msg); b != nil {
		combined := append(append([]SymbolID(nil), fnSyms...), tagSyms...)
		fr.attachPreviousNotes(b, combined)
		b.Emit()
	}
}

func (fr *fileResolver) collectFileScopeSymbols(name source.StringID, kinds ...SymbolKind) []SymbolID {
	if fr.result == nil || fr.result.Table == nil || name == source.NoStringID {
		return nil
	}
	scope := fr.result.Table.Scopes.Get(fr.result.FileScope)
	if scope == nil {
		return nil
	}
	ids := scope.NameIndex[name]
	if len(ids) == 0 || len(kinds) == 0 {
		return nil
	}
	want := make(map[SymbolKind]struct{}, len(kinds))
	for _, kind := range kinds {
		want[kind] = struct{}{}
	}
	out := make([]SymbolID, 0, len(ids))
	for _, id := range ids {
		sym := fr.result.Table.Symbols.Get(id)
		if sym == nil {
			continue
		}
		if _, ok := want[sym.Kind]; ok {
			out = append(out, id)
		}
	}
	return out
}
