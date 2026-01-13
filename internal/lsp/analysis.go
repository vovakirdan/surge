package lsp

import (
	"path/filepath"
	"sort"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/token"
)

type resolvedSymbol struct {
	ID  symbols.SymbolID
	Sym *symbols.Symbol
}

func snapshotFile(snapshot *diagnose.AnalysisSnapshot, uri string) (*diagnose.AnalysisFile, *source.File) {
	if snapshot == nil || snapshot.FileSet == nil {
		return nil, nil
	}
	path := uriToPath(uri)
	if path == "" {
		return nil, nil
	}
	key := snapshotPathKey(path)
	if key == "" {
		return nil, nil
	}
	af := snapshot.Files[key]
	var file *source.File
	if af != nil && af.FileID != 0 && snapshot.FileSet.HasFile(af.FileID) {
		file = snapshot.FileSet.Get(af.FileID)
	}
	if file == nil {
		if id, ok := snapshot.FileSet.GetLatest(path); ok {
			file = snapshot.FileSet.Get(id)
		}
	}
	return af, file
}

func snapshotPathKey(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := source.AbsolutePath(path); err == nil {
		return abs
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func tokenAtOffset(tokens []token.Token, offset uint32) (token.Token, bool) {
	if len(tokens) == 0 {
		return token.Token{}, false
	}
	idx := sort.Search(len(tokens), func(i int) bool { return tokens[i].Span.End > offset })
	if idx < len(tokens) {
		tok := tokens[idx]
		if tok.Span.Start <= offset && offset < tok.Span.End {
			return tok, true
		}
	}
	if idx > 0 {
		prev := tokens[idx-1]
		if prev.Span.Start <= offset && offset == prev.Span.End {
			return prev, true
		}
	}
	return token.Token{}, false
}

func findIdentExprBySpan(builder *ast.Builder, span source.Span) ast.ExprID {
	if builder == nil || builder.Exprs == nil || span == (source.Span{}) {
		return ast.NoExprID
	}
	for i := uint32(1); i <= builder.Exprs.Arena.Len(); i++ {
		exprID := ast.ExprID(i)
		expr := builder.Exprs.Get(exprID)
		if expr == nil || expr.Kind != ast.ExprIdent {
			continue
		}
		if expr.Span == span {
			return exprID
		}
	}
	return ast.NoExprID
}

func findExprAtOffset(builder *ast.Builder, fileID source.FileID, offset uint32, skipIdent bool) (ast.ExprID, *ast.Expr) {
	if builder == nil || builder.Exprs == nil {
		return ast.NoExprID, nil
	}
	var (
		bestID    ast.ExprID
		bestExpr  *ast.Expr
		bestWidth uint32
	)
	for i := uint32(1); i <= builder.Exprs.Arena.Len(); i++ {
		exprID := ast.ExprID(i)
		expr := builder.Exprs.Get(exprID)
		if expr == nil {
			continue
		}
		if skipIdent && expr.Kind == ast.ExprIdent {
			continue
		}
		if expr.Span.File != fileID {
			continue
		}
		if offset < expr.Span.Start || offset >= expr.Span.End {
			continue
		}
		width := expr.Span.End - expr.Span.Start
		if bestExpr == nil || width < bestWidth {
			bestID = exprID
			bestExpr = expr
			bestWidth = width
		}
	}
	return bestID, bestExpr
}

func resolveSymbolAt(af *diagnose.AnalysisFile, file *source.File, offset uint32, tok token.Token) resolvedSymbol {
	if af == nil || af.Symbols == nil || af.Symbols.Table == nil {
		return resolvedSymbol{}
	}
	if af.Builder != nil && tok.Span != (source.Span{}) {
		if identID := findIdentExprBySpan(af.Builder, tok.Span); identID.IsValid() {
			if symID, ok := af.Symbols.ExprSymbols[identID]; ok && symID.IsValid() {
				if sym := af.Symbols.Table.Symbols.Get(symID); sym != nil {
					return resolvedSymbol{ID: symID, Sym: sym}
				}
			}
		}
	}
	if af.Builder != nil && af.ASTFile.IsValid() {
		if symID := symbolForItemAtOffset(af, file, offset); symID.IsValid() {
			if sym := af.Symbols.Table.Symbols.Get(symID); sym != nil {
				return resolvedSymbol{ID: symID, Sym: sym}
			}
		}
	}
	if tok.Kind == token.Ident && tok.Text != "" && file != nil {
		if symID := lookupSymbolInScope(af, file, offset, tok.Text); symID.IsValid() {
			if sym := af.Symbols.Table.Symbols.Get(symID); sym != nil {
				return resolvedSymbol{ID: symID, Sym: sym}
			}
		}
	}
	return resolvedSymbol{}
}

func symbolForItemAtOffset(af *diagnose.AnalysisFile, file *source.File, offset uint32) symbols.SymbolID {
	if af == nil || af.Builder == nil || af.Symbols == nil || af.Symbols.Table == nil {
		return symbols.NoSymbolID
	}
	if file == nil {
		return symbols.NoSymbolID
	}
	fileNode := af.Builder.Files.Get(af.ASTFile)
	if fileNode == nil {
		return symbols.NoSymbolID
	}
	for _, itemID := range fileNode.Items {
		item := af.Builder.Items.Get(itemID)
		if item == nil {
			continue
		}
		nameSpan := itemNameSpan(af.Builder, itemID, item.Kind)
		if nameSpan == (source.Span{}) || nameSpan.File != file.ID {
			continue
		}
		if offset < nameSpan.Start || offset >= nameSpan.End {
			continue
		}
		if ids := af.Symbols.ItemSymbols[itemID]; len(ids) > 0 {
			return ids[0]
		}
		symCount := af.Symbols.Table.Symbols.Len()
		for i := 1; i <= symCount; i++ {
			id, err := safecast.Conv[symbols.SymbolID](i)
			if err != nil {
				continue
			}
			sym := af.Symbols.Table.Symbols.Get(id)
			if sym == nil {
				continue
			}
			if sym.Decl.Item == itemID {
				return id
			}
		}
	}
	return symbols.NoSymbolID
}

func itemNameSpan(builder *ast.Builder, itemID ast.ItemID, kind ast.ItemKind) source.Span {
	if builder == nil {
		return source.Span{}
	}
	switch kind {
	case ast.ItemFn:
		if fn, ok := builder.Items.Fn(itemID); ok && fn != nil {
			return fn.NameSpan
		}
	case ast.ItemLet:
		if letItem, ok := builder.Items.Let(itemID); ok && letItem != nil {
			return letItem.NameSpan
		}
	case ast.ItemConst:
		if c, ok := builder.Items.Const(itemID); ok && c != nil {
			return c.NameSpan
		}
	case ast.ItemTag:
		if tag, ok := builder.Items.Tag(itemID); ok && tag != nil {
			return tag.NameSpan
		}
	case ast.ItemContract:
		if contract, ok := builder.Items.Contract(itemID); ok && contract != nil {
			return contract.NameSpan
		}
	}
	return source.Span{}
}

func lookupSymbolInScope(af *diagnose.AnalysisFile, file *source.File, offset uint32, name string) symbols.SymbolID {
	if af == nil || af.Symbols == nil || af.Symbols.Table == nil || file == nil {
		return symbols.NoSymbolID
	}
	scope := scopeAtOffset(af.Symbols.Table.Scopes, af.Symbols.FileScope, file.ID, offset)
	if !scope.IsValid() {
		return symbols.NoSymbolID
	}
	nameID := internName(af, name)
	if nameID == source.NoStringID {
		return symbols.NoSymbolID
	}
	return lookupSymbolInScopeChain(af.Symbols.Table, scope, nameID)
}

func internName(af *diagnose.AnalysisFile, name string) source.StringID {
	if name == "" {
		return source.NoStringID
	}
	if af != nil && af.Builder != nil && af.Builder.StringsInterner != nil {
		return af.Builder.StringsInterner.Intern(name)
	}
	if af != nil && af.Symbols != nil && af.Symbols.Table != nil && af.Symbols.Table.Strings != nil {
		return af.Symbols.Table.Strings.Intern(name)
	}
	return source.NoStringID
}

func lookupSymbolInScopeChain(table *symbols.Table, scopeID symbols.ScopeID, name source.StringID) symbols.SymbolID {
	if table == nil || name == source.NoStringID {
		return symbols.NoSymbolID
	}
	for scopeID.IsValid() {
		scope := table.Scopes.Get(scopeID)
		if scope == nil {
			break
		}
		if ids := scope.NameIndex[name]; len(ids) > 0 {
			return ids[len(ids)-1]
		}
		scopeID = scope.Parent
	}
	return symbols.NoSymbolID
}

func scopeAtOffset(scopes *symbols.Scopes, scopeID symbols.ScopeID, fileID source.FileID, offset uint32) symbols.ScopeID {
	if scopes == nil || !scopeID.IsValid() {
		return symbols.NoScopeID
	}
	scope := scopes.Get(scopeID)
	if scope == nil {
		return symbols.NoScopeID
	}
	if scope.Span != (source.Span{}) {
		if scope.Span.File != fileID {
			return symbols.NoScopeID
		}
		if offset < scope.Span.Start || offset > scope.Span.End {
			return symbols.NoScopeID
		}
	}
	best := scopeID
	for _, child := range scope.Children {
		if nested := scopeAtOffset(scopes, child, fileID, offset); nested.IsValid() {
			best = nested
		}
	}
	return best
}
