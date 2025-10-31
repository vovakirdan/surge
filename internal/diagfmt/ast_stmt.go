package diagfmt

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
)

// buildStmtTreeNode constructs a treeNode that represents the statement identified by stmtID.
// If builder or its Stmts arena is unavailable the returned node's label indicates the missing arena; if the statement is nil the label indicates nil.
// The node's label contains "Stmt[idx]: <Kind> (span: <span>)". For a block statement each child statement is appended as a child node. For a let statement the node includes children for the name, mutability, type, and value. For expression and return statements the node includes a child summarizing the expression (returns "<none>" when a return has no expression).
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

	case ast.StmtSignal:
		signalStmt := builder.Stmts.Signal(stmtID)
		if signalStmt == nil {
			return node
		}
		node.children = append(node.children,
			&treeNode{label: fmt.Sprintf("Name: %s", lookupStringOr(builder, signalStmt.Name, "<anon>"))},
			&treeNode{label: fmt.Sprintf("Value: %s", formatExprSummary(builder, signalStmt.Value))},
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

	case ast.StmtBreak:
		node.children = append(node.children, &treeNode{label: "(no additional data)"})

	case ast.StmtContinue:
		node.children = append(node.children, &treeNode{label: "(no additional data)"})

	case ast.StmtIf:
		ifStmt := builder.Stmts.If(stmtID)
		if ifStmt == nil {
			return node
		}
		node.children = append(node.children,
			&treeNode{label: fmt.Sprintf("Cond: %s", formatExprSummary(builder, ifStmt.Cond))},
		)
		if ifStmt.Then.IsValid() {
			thenNode := buildStmtTreeNode(builder, ifStmt.Then, fs, 0)
			if thenNode != nil {
				thenNode.label = "Then → " + thenNode.label
				node.children = append(node.children, thenNode)
			}
		} else {
			node.children = append(node.children, &treeNode{label: "Then → <none>"})
		}
		if ifStmt.Else.IsValid() {
			elseNode := buildStmtTreeNode(builder, ifStmt.Else, fs, 0)
			if elseNode != nil {
				elseNode.label = "Else → " + elseNode.label
				node.children = append(node.children, elseNode)
			}
		} else {
			node.children = append(node.children, &treeNode{label: "Else → <none>"})
		}

	case ast.StmtWhile:
		whileStmt := builder.Stmts.While(stmtID)
		if whileStmt == nil {
			return node
		}
		node.children = append(node.children,
			&treeNode{label: fmt.Sprintf("Cond: %s", formatExprSummary(builder, whileStmt.Cond))},
		)
		if whileStmt.Body.IsValid() {
			bodyNode := buildStmtTreeNode(builder, whileStmt.Body, fs, 0)
			if bodyNode != nil {
				bodyNode.label = "Body → " + bodyNode.label
				node.children = append(node.children, bodyNode)
			}
		}

	case ast.StmtForClassic:
		forStmt := builder.Stmts.ForClassic(stmtID)
		if forStmt == nil {
			return node
		}
		if forStmt.Init.IsValid() {
			initNode := buildStmtTreeNode(builder, forStmt.Init, fs, 0)
			if initNode != nil {
				initNode.label = "Init → " + initNode.label
				node.children = append(node.children, initNode)
			}
		}
		node.children = append(node.children,
			&treeNode{label: fmt.Sprintf("Cond: %s", formatExprSummary(builder, forStmt.Cond))},
			&treeNode{label: fmt.Sprintf("Post: %s", formatExprSummary(builder, forStmt.Post))},
		)
		if forStmt.Body.IsValid() {
			bodyNode := buildStmtTreeNode(builder, forStmt.Body, fs, 0)
			if bodyNode != nil {
				bodyNode.label = "Body → " + bodyNode.label
				node.children = append(node.children, bodyNode)
			}
		}

	case ast.StmtForIn:
		forIn := builder.Stmts.ForIn(stmtID)
		if forIn == nil {
			return node
		}
		pattern := lookupStringOr(builder, forIn.Pattern, "<anon>")
		node.children = append(node.children, &treeNode{label: fmt.Sprintf("Pattern: %s", pattern)})
		if forIn.Type.IsValid() {
			node.children = append(node.children, &treeNode{label: fmt.Sprintf("Type: %s", formatTypeExprInline(builder, forIn.Type))})
		}
		node.children = append(node.children, &treeNode{label: fmt.Sprintf("Iterable: %s", formatExprSummary(builder, forIn.Iterable))})
		if forIn.Body.IsValid() {
			bodyNode := buildStmtTreeNode(builder, forIn.Body, fs, 0)
			if bodyNode != nil {
				bodyNode.label = "Body → " + bodyNode.label
				node.children = append(node.children, bodyNode)
			}
		}
	}

	return node
}

// formatStmtJSON converts the statement identified by stmtID into an ASTNodeOutput suitable for JSON-like serialization.
// It returns the constructed ASTNodeOutput and an error if the builder or its statements arena is nil or if the statement is not found.
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
				"expr": formatExprInline(builder, exprStmt.Expr),
				"exprID": func() any {
					if exprStmt.Expr.IsValid() {
						return uint32(exprStmt.Expr)
					}
					return nil
				}(),
			})
		}

	case ast.StmtSignal:
		signalStmt := builder.Stmts.Signal(stmtID)
		if signalStmt != nil {
			output.Fields = cleanupNilFields(map[string]any{
				"name":  lookupStringOr(builder, signalStmt.Name, "<anon>"),
				"value": formatExprInline(builder, signalStmt.Value),
				"valueID": func() any {
					if signalStmt.Value.IsValid() {
						return uint32(signalStmt.Value)
					}
					return nil
				}(),
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

	case ast.StmtBreak, ast.StmtContinue:
		// no extra fields

	case ast.StmtIf:
		ifStmt := builder.Stmts.If(stmtID)
		if ifStmt != nil {
			output.Fields = cleanupNilFields(map[string]any{
				"cond": formatExprInline(builder, ifStmt.Cond),
				"condID": func() any {
					if ifStmt.Cond.IsValid() {
						return uint32(ifStmt.Cond)
					}
					return nil
				}(),
			})
			if ifStmt.Then.IsValid() {
				thenNode, err := formatStmtJSON(builder, ifStmt.Then)
				if err != nil {
					return ASTNodeOutput{}, err
				}
				if thenNode.Fields == nil {
					thenNode.Fields = map[string]any{}
				}
				thenNode.Fields["role"] = "then"
				output.Children = append(output.Children, thenNode)
			}
			if ifStmt.Else.IsValid() {
				elseNode, err := formatStmtJSON(builder, ifStmt.Else)
				if err != nil {
					return ASTNodeOutput{}, err
				}
				if elseNode.Fields == nil {
					elseNode.Fields = map[string]any{}
				}
				elseNode.Fields["role"] = "else"
				output.Children = append(output.Children, elseNode)
			}
		}

	case ast.StmtWhile:
		whileStmt := builder.Stmts.While(stmtID)
		if whileStmt != nil {
			output.Fields = cleanupNilFields(map[string]any{
				"cond": formatExprInline(builder, whileStmt.Cond),
				"condID": func() any {
					if whileStmt.Cond.IsValid() {
						return uint32(whileStmt.Cond)
					}
					return nil
				}(),
			})
			if whileStmt.Body.IsValid() {
				bodyNode, err := formatStmtJSON(builder, whileStmt.Body)
				if err != nil {
					return ASTNodeOutput{}, err
				}
				if bodyNode.Fields == nil {
					bodyNode.Fields = map[string]any{}
				}
				bodyNode.Fields["role"] = "body"
				output.Children = append(output.Children, bodyNode)
			}
		}

	case ast.StmtForClassic:
		forStmt := builder.Stmts.ForClassic(stmtID)
		if forStmt != nil {
			output.Fields = cleanupNilFields(map[string]any{
				"cond": formatExprInline(builder, forStmt.Cond),
				"condID": func() any {
					if forStmt.Cond.IsValid() {
						return uint32(forStmt.Cond)
					}
					return nil
				}(),
				"post": formatExprInline(builder, forStmt.Post),
				"postID": func() any {
					if forStmt.Post.IsValid() {
						return uint32(forStmt.Post)
					}
					return nil
				}(),
			})
			if forStmt.Init.IsValid() {
				initNode, err := formatStmtJSON(builder, forStmt.Init)
				if err != nil {
					return ASTNodeOutput{}, err
				}
				if initNode.Fields == nil {
					initNode.Fields = map[string]any{}
				}
				initNode.Fields["role"] = "init"
				output.Children = append(output.Children, initNode)
			}
			if forStmt.Body.IsValid() {
				bodyNode, err := formatStmtJSON(builder, forStmt.Body)
				if err != nil {
					return ASTNodeOutput{}, err
				}
				if bodyNode.Fields == nil {
					bodyNode.Fields = map[string]any{}
				}
				bodyNode.Fields["role"] = "body"
				output.Children = append(output.Children, bodyNode)
			}
		}

	case ast.StmtForIn:
		forIn := builder.Stmts.ForIn(stmtID)
		if forIn != nil {
			fields := map[string]any{
				"pattern":  lookupStringOr(builder, forIn.Pattern, "<anon>"),
				"iterable": formatExprInline(builder, forIn.Iterable),
				"iterableID": func() any {
					if forIn.Iterable.IsValid() {
						return uint32(forIn.Iterable)
					}
					return nil
				}(),
			}
			if forIn.Type.IsValid() {
				fields["type"] = formatTypeExprInline(builder, forIn.Type)
			}
			output.Fields = cleanupNilFields(fields)
			if forIn.Body.IsValid() {
				bodyNode, err := formatStmtJSON(builder, forIn.Body)
				if err != nil {
					return ASTNodeOutput{}, err
				}
				if bodyNode.Fields == nil {
					bodyNode.Fields = map[string]any{}
				}
				bodyNode.Fields["role"] = "body"
				output.Children = append(output.Children, bodyNode)
			}
		}
	}

	return output, nil
}

// lookupStringOr resolves the interned string for the given StringID, falling back to the provided fallback or "<anon>" when unavailable.
// If builder or its StringsInterner is nil, or id is source.NoStringID, the fallback is returned when non-empty; otherwise "<anon>" is returned.
func lookupStringOr(builder *ast.Builder, id source.StringID, fallback string) string {
	if builder == nil || builder.StringsInterner == nil || id == source.NoStringID {
		if fallback != "" {
			return fallback
		}
		return "<anon>"
	}
	return builder.StringsInterner.MustLookup(id)
}

// formatFnParamsInline formats a function's parameters as a parenthesized, comma-separated inline string.
// It resolves parameter names, types, and default values using the provided builder.
// If builder or fn is nil, or the function has no parameters, it returns "()".
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

// cleanupNilFields removes any key/value pairs from fields whose value is nil and returns the resulting map.
// The input map is modified in-place; if all entries are removed the function returns nil.
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
