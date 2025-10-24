package diagfmt

import (
	"fmt"
	"io"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
)

func formatStmtKind(kind ast.StmtKind) string {
	switch kind {
	case ast.StmtBlock:
		return "Block"
	case ast.StmtLet:
		return "Let"
	case ast.StmtExpr:
		return "Expr"
	case ast.StmtReturn:
		return "Return"
	default:
		return fmt.Sprintf("StmtKind(%d)", kind)
	}
}

func formatStmtPretty(w io.Writer, builder *ast.Builder, stmtID ast.StmtID, fs *source.FileSet, prefix string) error {
	if builder == nil || builder.Stmts == nil {
		fmt.Fprintf(w, "<no statements arena>\n")
		return nil
	}
	stmt := builder.Stmts.Get(stmtID)
	if stmt == nil {
		fmt.Fprintf(w, "<nil>\n")
		return nil
	}

	fmt.Fprintf(w, "%s (span: %s)\n", formatStmtKind(stmt.Kind), formatSpan(stmt.Span, fs))

	switch stmt.Kind {
	case ast.StmtBlock:
		block := builder.Stmts.Block(stmtID)
		if block == nil {
			return nil
		}
		for idx, childID := range block.Stmts {
			isLast := idx == len(block.Stmts)-1
			marker := "├─"
			childPrefix := prefix + "│  "
			if isLast {
				marker = "└─"
				childPrefix = prefix + "   "
			}
			fmt.Fprintf(w, "%s%s Stmt[%d]: ", prefix, marker, idx)
			if err := formatStmtPretty(w, builder, childID, fs, childPrefix); err != nil {
				return err
			}
		}

	case ast.StmtLet:
		letStmt := builder.Stmts.Let(stmtID)
		if letStmt == nil {
			return nil
		}
		fields := []struct {
			label string
			value string
		}{
			{"Name", lookupStringOr(builder, letStmt.Name, "<anon>")},
			{"Mutable", fmt.Sprintf("%v", letStmt.IsMut)},
			{"Type", formatTypeExprInline(builder, letStmt.Type)},
			{"Value", formatExprSummary(builder, letStmt.Value)},
		}
		for i, field := range fields {
			marker := "├─"
			if i == len(fields)-1 {
				marker = "└─"
			}
			fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, field.label, field.value)
		}

	case ast.StmtExpr:
		exprStmt := builder.Stmts.Expr(stmtID)
		if exprStmt == nil {
			return nil
		}
		fmt.Fprintf(w, "%s└─ Expr: %s\n", prefix, formatExprSummary(builder, exprStmt.Expr))

	case ast.StmtReturn:
		retStmt := builder.Stmts.Return(stmtID)
		if retStmt == nil {
			return nil
		}
		value := "<none>"
		if retStmt.Expr.IsValid() {
			value = formatExprSummary(builder, retStmt.Expr)
		}
		fmt.Fprintf(w, "%s└─ Expr: %s\n", prefix, value)
	}

	return nil
}

func buildStmtTreeNode(builder *ast.Builder, stmtID ast.StmtID, fs *source.FileSet, idx int) *treeNode {
	if builder == nil || builder.Stmts == nil {
		return &treeNode{label: fmt.Sprintf("Stmt[%d]: <no statements arena>", idx)}
	}

	stmt := builder.Stmts.Get(stmtID)
	if stmt == nil {
		return &treeNode{label: fmt.Sprintf("Stmt[%d]: <nil>", idx)}
	}

	node := &treeNode{
		label: fmt.Sprintf("Stmt[%d]: %s (span: %s)", idx, formatStmtKind(stmt.Kind), formatSpan(stmt.Span, fs)),
	}

	switch stmt.Kind {
	case ast.StmtBlock:
		block := builder.Stmts.Block(stmtID)
		if block == nil {
			return node
		}
		for i, childID := range block.Stmts {
			node.children = append(node.children, buildStmtTreeNode(builder, childID, fs, i))
		}

	case ast.StmtLet:
		letStmt := builder.Stmts.Let(stmtID)
		if letStmt == nil {
			return node
		}
		node.children = append(node.children,
			&treeNode{label: fmt.Sprintf("Name: %s", lookupStringOr(builder, letStmt.Name, "<anon>"))},
			&treeNode{label: fmt.Sprintf("Mutable: %v", letStmt.IsMut)},
			&treeNode{label: fmt.Sprintf("Type: %s", formatTypeExprInline(builder, letStmt.Type))},
			&treeNode{label: fmt.Sprintf("Value: %s", formatExprSummary(builder, letStmt.Value))},
		)

	case ast.StmtExpr:
		exprStmt := builder.Stmts.Expr(stmtID)
		if exprStmt == nil {
			return node
		}
		node.children = append(node.children, &treeNode{
			label: fmt.Sprintf("Expr: %s", formatExprSummary(builder, exprStmt.Expr)),
		})

	case ast.StmtReturn:
		retStmt := builder.Stmts.Return(stmtID)
		if retStmt == nil {
			return node
		}
		value := "<none>"
		if retStmt.Expr.IsValid() {
			value = formatExprSummary(builder, retStmt.Expr)
		}
		node.children = append(node.children, &treeNode{
			label: fmt.Sprintf("Expr: %s", value),
		})
	}

	return node
}

func formatStmtJSON(builder *ast.Builder, stmtID ast.StmtID) (ASTNodeOutput, error) {
	if builder == nil || builder.Stmts == nil {
		return ASTNodeOutput{}, fmt.Errorf("statements arena is nil")
	}

	stmt := builder.Stmts.Get(stmtID)
	if stmt == nil {
		return ASTNodeOutput{}, fmt.Errorf("statement %d not found", stmtID)
	}

	output := ASTNodeOutput{
		Type: "Stmt",
		Kind: formatStmtKind(stmt.Kind),
		Span: stmt.Span,
	}

	switch stmt.Kind {
	case ast.StmtBlock:
		block := builder.Stmts.Block(stmtID)
		if block != nil {
			for _, childID := range block.Stmts {
				childNode, err := formatStmtJSON(builder, childID)
				if err != nil {
					return ASTNodeOutput{}, err
				}
				output.Children = append(output.Children, childNode)
			}
		}

	case ast.StmtLet:
		letStmt := builder.Stmts.Let(stmtID)
		if letStmt != nil {
			fields := map[string]any{
				"name":  lookupStringOr(builder, letStmt.Name, "<anon>"),
				"isMut": letStmt.IsMut,
				"type":  formatTypeExprInline(builder, letStmt.Type),
				"value": formatExprInline(builder, letStmt.Value),
				"valueID": func() any {
					if letStmt.Value.IsValid() {
						return uint32(letStmt.Value)
					}
					return nil
				}(),
			}
			output.Fields = cleanupNilFields(fields)
		}

	case ast.StmtExpr:
		exprStmt := builder.Stmts.Expr(stmtID)
		if exprStmt != nil {
			output.Fields = cleanupNilFields(map[string]any{
				"expr":   formatExprInline(builder, exprStmt.Expr),
				"exprID": uint32(exprStmt.Expr),
			})
		}

	case ast.StmtReturn:
		retStmt := builder.Stmts.Return(stmtID)
		if retStmt != nil {
			fields := map[string]any{
				"expr": formatExprInline(builder, retStmt.Expr),
				"exprID": func() any {
					if retStmt.Expr.IsValid() {
						return uint32(retStmt.Expr)
					}
					return nil
				}(),
			}
			output.Fields = cleanupNilFields(fields)
		}
	}

	return output, nil
}

func lookupStringOr(builder *ast.Builder, id source.StringID, fallback string) string {
	if builder == nil || builder.StringsInterner == nil || id == source.NoStringID {
		if fallback != "" {
			return fallback
		}
		return "<anon>"
	}
	return builder.StringsInterner.MustLookup(id)
}

func formatFnParamsInline(builder *ast.Builder, fn *ast.FnItem) string {
	if builder == nil || fn == nil {
		return "()"
	}
	paramIDs := builder.Items.GetFnParamIDs(fn)
	if len(paramIDs) == 0 {
		return "()"
	}
	parts := make([]string, 0, len(paramIDs))
	for _, paramID := range paramIDs {
		param := builder.Items.FnParam(paramID)
		if param == nil {
			continue
		}
		name := lookupStringOr(builder, param.Name, "_")
		if param.Variadic {
			name = "..." + name
		}
		typ := formatTypeExprInline(builder, param.Type)
		piece := fmt.Sprintf("%s: %s", name, typ)
		if param.Default.IsValid() {
			piece = fmt.Sprintf("%s = %s", piece, formatExprInline(builder, param.Default))
		}
		parts = append(parts, piece)
	}
	return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
}

func cleanupNilFields(fields map[string]any) map[string]any {
	for key, value := range fields {
		if value == nil {
			delete(fields, key)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}
