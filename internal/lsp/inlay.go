package lsp

import (
	"encoding/json"
	"sort"
	"strconv"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/token"
	"surge/internal/types"
)

const inlayHintKindType = 1

func (s *Server) handleInlayHint(msg *rpcMessage) error {
	var params inlayHintParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.sendError(msg.ID, -32602, "invalid params")
		}
	}
	snapshot := s.currentSnapshot()
	if snapshot == nil {
		if s.currentTrace() {
			s.logf("inlayHint: snapshot=%d nil=true hints=0", s.currentSnapshotVersion())
		}
		return s.sendResponse(msg.ID, []inlayHint{})
	}
	hints := buildInlayHints(snapshot, params.TextDocument.URI, params.Range, s.currentInlayConfig())
	if s.currentTrace() {
		s.logf("inlayHint: snapshot=%d nil=false hints=%d", s.currentSnapshotVersion(), len(hints))
	}
	return s.sendResponse(msg.ID, hints)
}

func buildInlayHints(snapshot *diagnose.AnalysisSnapshot, uri string, rng lspRange, cfg inlayHintConfig) []inlayHint {
	if !cfg.letTypes && !cfg.defaultInit && !cfg.enumImplicitValues {
		return nil
	}
	af, file := snapshotFile(snapshot, uri)
	if af == nil || file == nil || af.Builder == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return nil
	}
	startOff := offsetForPositionInFile(file, rng.Start)
	endOff := offsetForPositionInFile(file, rng.End)
	if endOff < startOff {
		endOff = startOff
	}

	hints := make([]inlayHint, 0)
	fileNode := af.Builder.Files.Get(af.ASTFile)
	if fileNode != nil {
		for _, itemID := range fileNode.Items {
			item := af.Builder.Items.Get(itemID)
			if item == nil || item.Kind != ast.ItemLet {
				continue
			}
			letItem, ok := af.Builder.Items.Let(itemID)
			if !ok || letItem == nil {
				continue
			}
			if letItem.Name == source.NoStringID {
				continue
			}
			name := lookupName(af, letItem.Name)
			if name == "" {
				continue
			}
			if cfg.letTypes && !letItem.Type.IsValid() {
				nameSpan := letItem.NameSpan
				if nameSpan == (source.Span{}) || nameSpan.File != file.ID {
					continue
				}
				hintOff := nameSpan.End
				if hintOff < startOff || hintOff > endOff {
					continue
				}
				symID := symbolForLetItem(af, itemID)
				ty := bindingOrInitType(af, symID, letItem.Value)
				if ty == types.NoTypeID {
					continue
				}
				if cfg.hideObvious && isObviousLiteral(af, letItem.Value, ty) {
					continue
				}
				hints = append(hints, inlayHint{
					Position: positionForOffsetInFile(file, nameSpan.End),
					Label:    ": " + types.Label(af.Sema.TypeInterner, ty),
					Kind:     inlayHintKindType,
				})
			}
			if cfg.defaultInit && letItem.Type.IsValid() && !letItem.Value.IsValid() {
				symID := symbolForLetItem(af, itemID)
				ty := bindingOrInitType(af, symID, letItem.Value)
				if ty == types.NoTypeID {
					continue
				}
				hintOff := letItem.Span.End
				if letItem.SemicolonSpan != (source.Span{}) && letItem.SemicolonSpan.File == file.ID {
					hintOff = letItem.SemicolonSpan.Start
				}
				if hintOff < startOff || hintOff > endOff {
					continue
				}
				hints = append(hints, inlayHint{
					Position: positionForOffsetInFile(file, hintOff),
					Label:    " = default::<" + types.Label(af.Sema.TypeInterner, ty) + ">();",
					Kind:     inlayHintKindType,
				})
			}
		}
	}

	for i := uint32(1); i <= af.Builder.Stmts.Arena.Len(); i++ {
		stmtID := ast.StmtID(i)
		stmt := af.Builder.Stmts.Get(stmtID)
		if stmt == nil || stmt.Kind != ast.StmtLet {
			continue
		}
		if stmt.Span.File != file.ID {
			continue
		}
		letStmt := af.Builder.Stmts.Let(stmtID)
		if letStmt == nil {
			continue
		}
		if letStmt.Pattern.IsValid() || letStmt.Name == source.NoStringID {
			continue
		}
		name := lookupName(af, letStmt.Name)
		if name == "" {
			continue
		}
		if cfg.letTypes && !letStmt.Type.IsValid() {
			nameSpan := letStmtNameSpan(af, stmt.Span, name)
			if nameSpan == (source.Span{}) {
				continue
			}
			hintOff := nameSpan.End
			if hintOff < startOff || hintOff > endOff {
				continue
			}
			symID := symbolForLetStmt(af, stmtID)
			ty := bindingOrInitType(af, symID, letStmt.Value)
			if ty == types.NoTypeID {
				continue
			}
			if cfg.hideObvious && isObviousLiteral(af, letStmt.Value, ty) {
				continue
			}
			hints = append(hints, inlayHint{
				Position: positionForOffsetInFile(file, nameSpan.End),
				Label:    ": " + types.Label(af.Sema.TypeInterner, ty),
				Kind:     inlayHintKindType,
			})
		}
		if cfg.defaultInit && letStmt.Type.IsValid() && !letStmt.Value.IsValid() {
			symID := symbolForLetStmt(af, stmtID)
			ty := bindingOrInitType(af, symID, letStmt.Value)
			if ty == types.NoTypeID {
				continue
			}
			hintOff := stmt.Span.End
			if semiSpan := letStmtSemicolonSpan(af, stmt.Span); semiSpan != (source.Span{}) {
				hintOff = semiSpan.Start
			}
			if hintOff < startOff || hintOff > endOff {
				continue
			}
			hints = append(hints, inlayHint{
				Position: positionForOffsetInFile(file, hintOff),
				Label:    " = default::<" + types.Label(af.Sema.TypeInterner, ty) + ">();",
				Kind:     inlayHintKindType,
			})
		}
	}

	if cfg.enumImplicitValues {
		hints = append(hints, enumImplicitValueHints(af, file, startOff, endOff)...)
	}

	sort.Slice(hints, func(i, j int) bool {
		if hints[i].Position.Line == hints[j].Position.Line {
			return hints[i].Position.Character < hints[j].Position.Character
		}
		return hints[i].Position.Line < hints[j].Position.Line
	})
	return hints
}

func enumImplicitValueHints(af *diagnose.AnalysisFile, file *source.File, startOff, endOff uint32) []inlayHint {
	if af == nil || file == nil || af.Builder == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return nil
	}
	fileNode := af.Builder.Files.Get(af.ASTFile)
	if fileNode == nil {
		return nil
	}
	hints := make([]inlayHint, 0)
	for _, itemID := range fileNode.Items {
		item := af.Builder.Items.Get(itemID)
		if item == nil || item.Kind != ast.ItemType {
			continue
		}
		typeItem, ok := af.Builder.Items.Type(itemID)
		if !ok || typeItem == nil || typeItem.Kind != ast.TypeDeclEnum {
			continue
		}
		enumDecl := af.Builder.Items.TypeEnum(typeItem)
		if enumDecl == nil {
			continue
		}
		enumType := enumTypeForItem(af, itemID)
		if enumType == types.NoTypeID {
			continue
		}
		enumInfo, ok := af.Sema.TypeInterner.EnumInfo(enumType)
		if !ok || enumInfo == nil || len(enumInfo.Variants) == 0 {
			continue
		}
		variantsByName := make(map[source.StringID]types.EnumVariantInfo, len(enumInfo.Variants))
		for _, variant := range enumInfo.Variants {
			variantsByName[variant.Name] = variant
		}
		start := uint32(enumDecl.VariantsStart)
		for offset := range enumDecl.VariantsCount {
			variantID := ast.EnumVariantID(start + uint32(offset))
			variant := af.Builder.Items.EnumVariant(variantID)
			if variant == nil || variant.Name == source.NoStringID {
				continue
			}
			if variant.Value.IsValid() {
				continue
			}
			info, ok := variantsByName[variant.Name]
			if !ok || info.IsString {
				continue
			}
			hintOff := variant.NameSpan.End
			if hintOff < startOff || hintOff > endOff {
				continue
			}
			hints = append(hints, inlayHint{
				Position: positionForOffsetInFile(file, hintOff),
				Label:    " = " + strconv.FormatInt(info.IntValue, 10),
				Kind:     inlayHintKindType,
			})
		}
	}
	return hints
}

func enumTypeForItem(af *diagnose.AnalysisFile, itemID ast.ItemID) types.TypeID {
	if af == nil || af.Symbols == nil || af.Symbols.Table == nil {
		return types.NoTypeID
	}
	if ids := af.Symbols.ItemSymbols[itemID]; len(ids) > 0 {
		for _, id := range ids {
			sym := af.Symbols.Table.Symbols.Get(id)
			if sym != nil && sym.Kind == symbols.SymbolType && sym.Type != types.NoTypeID {
				return sym.Type
			}
		}
	}
	symCount := af.Symbols.Table.Symbols.Len()
	for i := 1; i <= symCount; i++ {
		id, err := safecast.Conv[symbols.SymbolID](i)
		if err != nil {
			continue
		}
		sym := af.Symbols.Table.Symbols.Get(id)
		if sym == nil || sym.Kind != symbols.SymbolType {
			continue
		}
		if sym.Decl.Item == itemID && sym.Type != types.NoTypeID {
			return sym.Type
		}
	}
	return types.NoTypeID
}

func symbolForLetItem(af *diagnose.AnalysisFile, itemID ast.ItemID) symbols.SymbolID {
	if af == nil || af.Symbols == nil || af.Symbols.Table == nil {
		return symbols.NoSymbolID
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
		if sym.Kind == symbols.SymbolLet && sym.Decl.Item == itemID {
			return id
		}
	}
	return symbols.NoSymbolID
}

func symbolForLetStmt(af *diagnose.AnalysisFile, stmtID ast.StmtID) symbols.SymbolID {
	if af == nil || af.Symbols == nil || af.Symbols.Table == nil {
		return symbols.NoSymbolID
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
		if sym.Kind == symbols.SymbolLet && sym.Decl.Stmt == stmtID {
			return id
		}
	}
	return symbols.NoSymbolID
}

func bindingOrInitType(af *diagnose.AnalysisFile, symID symbols.SymbolID, init ast.ExprID) types.TypeID {
	if af == nil || af.Sema == nil {
		return types.NoTypeID
	}
	if af.Sema.BindingTypes != nil && symID.IsValid() {
		if ty := af.Sema.BindingTypes[symID]; ty != types.NoTypeID {
			return ty
		}
	}
	if init.IsValid() && af.Sema.ExprTypes != nil {
		if ty := af.Sema.ExprTypes[init]; ty != types.NoTypeID {
			return ty
		}
	}
	return types.NoTypeID
}

func letStmtNameSpan(af *diagnose.AnalysisFile, stmtSpan source.Span, name string) source.Span {
	if af == nil || len(af.Tokens) == 0 || name == "" {
		return source.Span{}
	}
	foundLet := false
	for _, tok := range af.Tokens {
		if tok.Span.File != stmtSpan.File {
			continue
		}
		if tok.Span.End <= stmtSpan.Start {
			continue
		}
		if tok.Span.Start >= stmtSpan.End {
			break
		}
		if tok.Kind == token.KwLet {
			foundLet = true
			continue
		}
		if !foundLet {
			continue
		}
		if tok.Kind == token.KwMut {
			continue
		}
		if tok.Kind == token.Ident && tok.Text == name {
			return tok.Span
		}
	}
	return source.Span{}
}

func letStmtSemicolonSpan(af *diagnose.AnalysisFile, stmtSpan source.Span) source.Span {
	if af == nil || len(af.Tokens) == 0 {
		return source.Span{}
	}
	var semi source.Span
	for _, tok := range af.Tokens {
		if tok.Span.File != stmtSpan.File {
			continue
		}
		if tok.Span.End <= stmtSpan.Start {
			continue
		}
		if tok.Span.Start >= stmtSpan.End {
			break
		}
		if tok.Kind == token.Semicolon {
			semi = tok.Span
		}
	}
	return semi
}

func isObviousLiteral(af *diagnose.AnalysisFile, exprID ast.ExprID, typeID types.TypeID) bool {
	if af == nil || af.Builder == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return false
	}
	if exprID == ast.NoExprID {
		return false
	}
	expr := af.Builder.Exprs.Get(exprID)
	if expr == nil || expr.Kind != ast.ExprLit {
		return false
	}
	lit, _ := af.Builder.Exprs.Literal(exprID)
	if lit == nil {
		return false
	}
	switch lit.Kind {
	case ast.ExprLitInt, ast.ExprLitUint, ast.ExprLitFloat, ast.ExprLitString, ast.ExprLitTrue, ast.ExprLitFalse:
	default:
		return false
	}
	tt, ok := af.Sema.TypeInterner.Lookup(typeID)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindInt, types.KindUint, types.KindFloat, types.KindBool, types.KindString:
		return true
	default:
		return false
	}
}
