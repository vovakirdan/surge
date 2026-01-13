package lsp

import (
	"encoding/json"
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/token"
	"surge/internal/types"
)

func (s *Server) handleHover(msg *rpcMessage) error {
	var params hoverParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.sendError(msg.ID, -32602, "invalid params")
		}
	}
	snapshot := s.currentSnapshot()
	if snapshot == nil {
		return s.sendResponse(msg.ID, nil)
	}
	result := buildHover(snapshot, params.TextDocument.URI, params.Position)
	return s.sendResponse(msg.ID, result)
}

func buildHover(snapshot *diagnose.AnalysisSnapshot, uri string, pos position) *hover {
	af, file := snapshotFile(snapshot, uri)
	if af == nil || file == nil {
		return nil
	}
	offset := offsetForPositionInFile(file, pos)
	tok, tokOK := tokenAtOffset(af.Tokens, offset)

	var targetSpan source.Span
	if tokOK && tok.Kind == token.Ident {
		targetSpan = tok.Span
	}

	resolved := resolvedSymbol{}
	if tokOK && tok.Kind == token.Ident {
		resolved = resolveSymbolAt(af, file, offset, tok)
	}

	exprID, expr := findExprAtOffset(af.Builder, file.ID, offset, false)
	nonIdentID, nonIdentExpr := findExprAtOffset(af.Builder, file.ID, offset, true)

	lines := make([]string, 0, 3)
	if resolved.Sym != nil {
		if signature := formatSymbolSignature(af, resolved); signature != "" {
			lines = append(lines, "```surge\n"+signature+"\n```")
		}
		if loc := symbolLocation(snapshot, resolved.Sym); loc != "" {
			lines = append(lines, loc)
		}
	}

	if resolved.Sym == nil || (nonIdentExpr != nil && nonIdentExpr.Kind == ast.ExprCall) {
		if label := exprTypeLabel(af, exprID, expr, nonIdentID, nonIdentExpr); label != "" {
			lines = append(lines, "Type: `"+label+"`")
		}
	}

	if len(lines) == 0 {
		return nil
	}
	if targetSpan == (source.Span{}) && expr != nil {
		targetSpan = expr.Span
	}
	hoverRange := rangeForSpan(file, targetSpan)
	return &hover{
		Contents: markupContent{
			Kind:  "markdown",
			Value: strings.Join(lines, "\n"),
		},
		Range: &hoverRange,
	}
}

func formatSymbolSignature(af *diagnose.AnalysisFile, resolved resolvedSymbol) string {
	sym := resolved.Sym
	if sym == nil {
		return ""
	}
	name := lookupName(af, sym.Name)
	switch sym.Kind {
	case symbols.SymbolFunction, symbols.SymbolTag:
		return formatFunctionSignature(af, sym, name)
	case symbols.SymbolLet, symbols.SymbolConst, symbols.SymbolParam:
		label := "let"
		switch sym.Kind {
		case symbols.SymbolConst:
			label = "const"
		case symbols.SymbolParam:
			label = "param"
		}
		out := label + " " + name
		if ty := symbolTypeLabel(af, resolved.ID, sym); ty != "" {
			out += ": " + ty
		}
		return out
	case symbols.SymbolType:
		if name != "" {
			return "type " + name
		}
	case symbols.SymbolContract:
		if name != "" {
			return "contract " + name
		}
	}
	if name != "" {
		return name
	}
	return ""
}

func formatFunctionSignature(af *diagnose.AnalysisFile, sym *symbols.Symbol, name string) string {
	if sym == nil {
		return ""
	}
	if name == "" {
		name = lookupName(af, sym.Name)
	}
	if sym.Signature == nil {
		if name != "" {
			return "fn " + name
		}
		return ""
	}
	params := make([]string, 0, len(sym.Signature.Params))
	for i, param := range sym.Signature.Params {
		paramLabel := string(param)
		if i < len(sym.Signature.ParamNames) {
			if pname := lookupName(af, sym.Signature.ParamNames[i]); pname != "" {
				paramLabel = pname + ": " + paramLabel
			}
		}
		if i < len(sym.Signature.Variadic) && sym.Signature.Variadic[i] {
			paramLabel = "[" + paramLabel + "]"
		}
		params = append(params, paramLabel)
	}
	out := "fn " + name + "(" + strings.Join(params, ", ") + ")"
	if res := string(sym.Signature.Result); res != "" {
		out += " -> " + res
	}
	return out
}

func symbolTypeLabel(af *diagnose.AnalysisFile, symID symbols.SymbolID, sym *symbols.Symbol) string {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return ""
	}
	if af.Sema.BindingTypes != nil {
		if ty := af.Sema.BindingTypes[symID]; ty != types.NoTypeID {
			return types.Label(af.Sema.TypeInterner, ty)
		}
	}
	if sym != nil && sym.Type != types.NoTypeID {
		return types.Label(af.Sema.TypeInterner, sym.Type)
	}
	return ""
}

func exprTypeLabel(af *diagnose.AnalysisFile, exprID ast.ExprID, expr *ast.Expr, callID ast.ExprID, callExpr *ast.Expr) string {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil || af.Sema.ExprTypes == nil {
		return ""
	}
	if callExpr != nil && callExpr.Kind == ast.ExprCall {
		if ty := af.Sema.ExprTypes[callID]; ty != types.NoTypeID {
			return types.Label(af.Sema.TypeInterner, ty)
		}
	}
	if expr == nil {
		return ""
	}
	if ty := af.Sema.ExprTypes[exprID]; ty != types.NoTypeID {
		return types.Label(af.Sema.TypeInterner, ty)
	}
	return ""
}

func symbolLocation(snapshot *diagnose.AnalysisSnapshot, sym *symbols.Symbol) string {
	if snapshot == nil || snapshot.FileSet == nil || sym == nil {
		return ""
	}
	span := sym.Span
	if span == (source.Span{}) {
		return ""
	}
	if !snapshot.FileSet.HasFile(span.File) {
		return ""
	}
	file := snapshot.FileSet.Get(span.File)
	if file == nil {
		return ""
	}
	start, _ := snapshot.FileSet.Resolve(span)
	path := file.Path
	if snapshot.ProjectRoot != "" {
		if rel, err := source.RelativePath(path, snapshot.ProjectRoot); err == nil && rel != "" {
			path = rel
		}
	}
	return fmt.Sprintf("Defined in %s:%d", path, start.Line)
}

func lookupName(af *diagnose.AnalysisFile, id source.StringID) string {
	if id == source.NoStringID {
		return ""
	}
	if af != nil && af.Builder != nil && af.Builder.StringsInterner != nil {
		if name, ok := af.Builder.StringsInterner.Lookup(id); ok {
			return name
		}
	}
	if af != nil && af.Symbols != nil && af.Symbols.Table != nil && af.Symbols.Table.Strings != nil {
		if name, ok := af.Symbols.Table.Strings.Lookup(id); ok {
			return name
		}
	}
	return ""
}
