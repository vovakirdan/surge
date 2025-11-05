package symbols

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

// ResolveOptions controls a resolve pass for a single AST file.
type ResolveOptions struct {
	Table    *Table
	Hints    Hints
	Prelude  []PreludeEntry
	Reporter diag.Reporter
	Validate bool
}

// Result captures resolve artefacts for one file.
type Result struct {
	Table       *Table
	File        ast.FileID
	FileScope   ScopeID
	ItemSymbols map[ast.ItemID][]SymbolID
	ExprSymbols map[ast.ExprID]SymbolID
}

// ResolveFile walks the AST file and populates the symbol table.
func ResolveFile(builder *ast.Builder, fileID ast.FileID, opts ResolveOptions) Result {
	var table *Table
	if opts.Table != nil {
		table = opts.Table
	} else {
		table = NewTable(opts.Hints, builder.StringsInterner)
	}

	result := Result{
		Table:       table,
		File:        fileID,
		ItemSymbols: make(map[ast.ItemID][]SymbolID),
		ExprSymbols: make(map[ast.ExprID]SymbolID),
	}

	file := builder.Files.Get(fileID)
	if file == nil {
		return result
	}

	sourceFile := file.Span.File
	fileScope := table.FileRoot(sourceFile, file.Span)
	result.FileScope = fileScope

	resolver := NewResolver(table, fileScope, ResolverOptions{
		Reporter: opts.Reporter,
		Prelude:  opts.Prelude,
	})

	fr := fileResolver{
		builder:    builder,
		result:     &result,
		resolver:   resolver,
		fileID:     fileID,
		sourceFile: sourceFile,
	}
	for _, itemID := range file.Items {
		fr.handleItem(itemID)
	}

	if opts.Validate {
		if err := table.Validate(); err != nil {
			if opts.Reporter != nil {
				msg := fmt.Sprintf("symbol table invariant violation: %v", err)
				diag.ReportError(opts.Reporter, diag.SemaError, file.Span, msg).Emit()
			} else {
				panic(err)
			}
		}
	}

	return result
}

type fileResolver struct {
	builder    *ast.Builder
	result     *Result
	resolver   *Resolver
	fileID     ast.FileID
	sourceFile source.FileID
}

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
	case ast.StmtReturn:
		returnStmt := fr.builder.Stmts.Return(stmtID)
		if returnStmt != nil {
			fr.walkExpr(returnStmt.Expr)
		}
	case ast.StmtBreak, ast.StmtContinue:
		// no payload
	default:
		// future statement kinds
	}
}

func (fr *fileResolver) handleExtern(itemID ast.ItemID, block *ast.ExternBlock) {
	if block.MembersCount == 0 || !block.MembersStart.IsValid() {
		return
	}
	start := uint32(block.MembersStart)
	for offset := range block.MembersCount {
		memberID := ast.ExternMemberID(start + offset)
		member := fr.builder.Items.ExternMember(memberID)
		if member == nil || member.Kind != ast.ExternMemberFn {
			continue
		}
		fn := fr.builder.Items.FnByPayload(member.Fn)
		if fn == nil {
			continue
		}
		fr.declareExternFn(itemID, member, fn)
	}
}

func (fr *fileResolver) reportMissingOverload(name source.StringID, span source.Span, existing []SymbolID) {
	reporter := fr.resolver.reporter
	if reporter == nil {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	msg := fmt.Sprintf("function '%s' redeclared without @overload or @override", nameStr)
	b := diag.ReportError(reporter, diag.SemaFnOverride, span, msg)
	if b == nil {
		return
	}
	fr.attachPreviousNotes(b, existing)
	b.Emit()
}

func (fr *fileResolver) reportInvalidOverride(name source.StringID, span source.Span, message string, existing []SymbolID) {
	reporter := fr.resolver.reporter
	if reporter == nil {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	msg := fmt.Sprintf("invalid override for '%s': %s", nameStr, message)
	b := diag.ReportError(reporter, diag.SemaFnOverride, span, msg)
	if b == nil {
		return
	}
	fr.attachPreviousNotes(b, existing)
	b.Emit()
}

func (fr *fileResolver) attachPreviousNotes(b *diag.ReportBuilder, existing []SymbolID) {
	if b == nil {
		return
	}
	for _, id := range existing {
		sym := fr.result.Table.Symbols.Get(id)
		if sym == nil || sym.Span == (source.Span{}) {
			continue
		}
		b.WithNote(sym.Span, "previous declaration here")
	}
}

func (fr *fileResolver) appendItemSymbol(item ast.ItemID, id SymbolID) {
	if !id.IsValid() {
		return
	}
	fr.result.ItemSymbols[item] = append(fr.result.ItemSymbols[item], id)
}

func preferSpan(primary, fallback source.Span) source.Span {
	if primary != (source.Span{}) {
		return primary
	}
	return fallback
}

func fnNameSpan(fn *ast.FnItem) source.Span {
	if fn == nil {
		return source.Span{}
	}
	if fn.FnKeywordSpan != (source.Span{}) && fn.ParamsSpan != (source.Span{}) && fn.FnKeywordSpan.File == fn.ParamsSpan.File {
		if fn.ParamsSpan.Start >= fn.FnKeywordSpan.End {
			return source.Span{
				File:  fn.FnKeywordSpan.File,
				Start: fn.FnKeywordSpan.End,
				End:   fn.ParamsSpan.Start,
			}
		}
	}
	return fn.Span
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
		// nothing to do
	default:
		// future expression kinds
	}
}

func (fr *fileResolver) resolveIdent(exprID ast.ExprID, span source.Span, name source.StringID) {
	if name == source.NoStringID {
		return
	}
	if fr.resolver == nil {
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
	b := diag.ReportError(fr.resolver.reporter, diag.SemaUnresolvedSymbol, span, msg)
	if b == nil {
		return
	}
	b.Emit()
}
