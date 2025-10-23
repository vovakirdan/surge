package diagfmt

import (
	"fmt"
	"io"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
)

// formatStmtKind returns a human-readable label for the provided statement kind.
// Known kinds map to "Block", "Let", "Expr", or "Return"; unknown kinds are formatted as "StmtKind(n)" using the numeric kind.
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

// formatStmtPretty writes a tree-like, human-readable representation of the statement identified by stmtID to w.
// It prints a header with the statement kind and span, then renders kind-specific details:
// - Block: lists child statements recursively with ASCII branch markers.
// - Let: prints Name, Mutable, Type, and Value fields.
// - Expr: prints a single-line expression summary.
// - Return: prints the return expression summary or "<none>" when absent.
// If builder or builder.Stmts is nil it writes "<no statements arena>" and returns nil; if the statement is missing it writes "<nil>" and returns nil.
// Any error produced while formatting child statements is returned.
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

// buildStmtTreeNode builds a treeNode representing the statement identified by stmtID and its nested structure for diagnostic output.
// 
// When the statements arena is unavailable (builder or builder.Stmts is nil) the node label will be "Stmt[idx]: <no statements arena>". If the statement is not found the label will be "Stmt[idx]: <nil>".
// 
// For recognized statement kinds the node includes children as follows:
//   - Block: one child node per nested statement (recursively built).
//   - Let: children for "Name", "Mutable", "Type", and "Value".
//   - Expr: single child labelled with the expression summary.
//   - Return: single child labelled with the return expression summary or "<none>" when no expression is present.
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

// formatStmtJSON converts the statement identified by stmtID into an ASTNodeOutput suitable for JSON-like serialization.
// 
// The returned node always contains Type set to "Stmt", Kind set via formatStmtKind, and Span copied from the statement.
// For block statements, Children contains recursively formatted child statements. For Let, Expr, and Return statements,
// Fields contains kind-specific entries such as resolved names, mutation flag, inline-formatted type/value/expr strings and
// optional numeric IDs (omitted when not applicable). Nil-valued fields are removed by cleanupNilFields.
// 
// An error is returned if the builder or its statements arena is nil, or if the requested statement ID does not exist.
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

// returns "<anon>".
func lookupStringOr(builder *ast.Builder, id source.StringID, fallback string) string {
	if builder == nil || builder.StringsInterner == nil || id == source.NoStringID {
		if fallback != "" {
			return fallback
		}
		return "<anon>"
	}
	return builder.StringsInterner.MustLookup(id)
}

// formatFnParamsInline formats a function's parameters inline as a parenthesized,
// comma-separated list.
//
// If the builder or fn is nil, or the function has no parameters, it returns "()".
//
// Each parameter is rendered as "name: type". If a parameter has a default value,
// " = <expr>" is appended. When a parameter name is missing, "_" is used as the
// fallback name.
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
		typ := formatTypeExprInline(builder, param.Type)
		piece := fmt.Sprintf("%s: %s", name, typ)
		if param.Default.IsValid() {
			piece = fmt.Sprintf("%s = %s", piece, formatExprInline(builder, param.Default))
		}
		parts = append(parts, piece)
	}
	return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
}

// cleanupNilFields removes entries with nil values from the given map.
// If the map becomes empty after removal, it returns nil; otherwise it returns the cleaned map.
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