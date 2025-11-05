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

func (fr *fileResolver) declareLet(itemID ast.ItemID, letItem *ast.LetItem) {
	if letItem.Name == source.NoStringID {
		return
	}
	flags := SymbolFlags(0)
	if letItem.Visibility == ast.VisPublic {
		flags |= SymbolFlagPublic
	}
	if letItem.IsMut {
		flags |= SymbolFlagMutable
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(letItem.NameSpan, letItem.Span)
	if symID, ok := fr.resolver.Declare(letItem.Name, span, SymbolLet, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareFn(itemID ast.ItemID, fnItem *ast.FnItem) {
	if fnItem.Name == source.NoStringID {
		return
	}
	flags := SymbolFlags(0)
	if fnItem.Flags&ast.FnModifierPublic != 0 {
		flags |= SymbolFlagPublic
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := fnNameSpan(fnItem)
	if symID, ok := fr.resolver.Declare(fnItem.Name, span, SymbolFunction, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
	}
	fr.walkFn(itemID, fnItem)
}

func (fr *fileResolver) declareType(itemID ast.ItemID, typeItem *ast.TypeItem) {
	if typeItem.Name == source.NoStringID {
		return
	}
	flags := SymbolFlags(0)
	if typeItem.Visibility == ast.VisPublic {
		flags |= SymbolFlagPublic
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(typeItem.TypeKeywordSpan, typeItem.Span)
	if symID, ok := fr.resolver.Declare(typeItem.Name, span, SymbolType, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareTag(itemID ast.ItemID, tagItem *ast.TagItem) {
	if tagItem.Name == source.NoStringID {
		return
	}
	flags := SymbolFlags(0)
	if tagItem.Visibility == ast.VisPublic {
		flags |= SymbolFlagPublic
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	span := preferSpan(tagItem.TagKeywordSpan, tagItem.Span)
	if symID, ok := fr.resolver.Declare(tagItem.Name, span, SymbolTag, flags, decl); ok {
		fr.appendItemSymbol(itemID, symID)
	}
}

func (fr *fileResolver) declareImport(itemID ast.ItemID, importItem *ast.ImportItem, itemSpan source.Span) {
	if importItem.ModuleAlias != source.NoStringID {
		fr.declareImportName(itemID, importItem.ModuleAlias, source.NoStringID, importItem.Module, itemSpan)
	}
	if importItem.HasOne {
		name := importItem.One.Alias
		if name == source.NoStringID {
			name = importItem.One.Name
		}
		fr.declareImportName(itemID, name, importItem.One.Name, importItem.Module, itemSpan)
	}
	for _, pair := range importItem.Group {
		name := pair.Alias
		if name == source.NoStringID {
			name = pair.Name
		}
		fr.declareImportName(itemID, name, pair.Name, importItem.Module, itemSpan)
	}
}

func (fr *fileResolver) declareImportName(itemID ast.ItemID, name, original source.StringID, module []source.StringID, span source.Span) {
	if name == source.NoStringID {
		return
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       itemID,
	}
	if symID, ok := fr.resolver.Declare(name, span, SymbolImport, SymbolFlagImported, decl); ok {
		if sym := fr.result.Table.Symbols.Get(symID); sym != nil {
			if len(module) > 0 {
				path := append([]source.StringID(nil), module...)
				sym.Aliases = append(sym.Aliases, path...)
			}
			if original != source.NoStringID && original != name {
				sym.Aliases = append(sym.Aliases, original)
			}
		}
		fr.appendItemSymbol(itemID, symID)
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
		fr.walkStmt(ifStmt.Then)
		if ifStmt.Else.IsValid() {
			fr.walkStmt(ifStmt.Else)
		}
	case ast.StmtWhile:
		whileStmt := fr.builder.Stmts.While(stmtID)
		if whileStmt == nil {
			return
		}
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
		fr.walkStmt(forIn.Body)
		fr.resolver.Leave(scopeID)
	case ast.StmtExpr, ast.StmtSignal, ast.StmtReturn, ast.StmtBreak, ast.StmtContinue:
		// no declarations to track yet
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

func (fr *fileResolver) declareExternFn(container ast.ItemID, member *ast.ExternMember, fnItem *ast.FnItem) {
	if fnItem.Name == source.NoStringID {
		return
	}
	flags := SymbolFlagImported
	if fnItem.Flags&ast.FnModifierPublic != 0 {
		flags |= SymbolFlagPublic
	}
	decl := SymbolDecl{
		SourceFile: fr.sourceFile,
		ASTFile:    fr.fileID,
		Item:       container,
	}
	span := fnNameSpan(fnItem)
	if symID, ok := fr.resolver.Declare(fnItem.Name, span, SymbolFunction, flags, decl); ok {
		fr.appendItemSymbol(container, symID)
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
