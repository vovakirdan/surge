package diagfmt

import (
	"fmt"
	"io"

	"surge/internal/ast"
	"surge/internal/source"
)

// formatStmtKind returns a human-friendly label for the given AST statement kind.
// For known kinds it returns "Block", "Let", "Expr", or "Return"; for unknown kinds
// it returns "StmtKind(<numeric>)" where <numeric> is the numeric value of kind.
func formatStmtKind(kind ast.StmtKind) string {
	switch kind {
	case ast.StmtBlock:
		return "Block"
	case ast.StmtLet:
		return "Let"
	case ast.StmtExpr:
		return "Expr"
	case ast.StmtSignal:
		return "Signal"
	case ast.StmtReturn:
		return "Return"
	case ast.StmtBreak:
		return "Break"
	case ast.StmtContinue:
		return "Continue"
	case ast.StmtIf:
		return "If"
	case ast.StmtWhile:
		return "While"
	case ast.StmtForClassic:
		return "ForClassic"
	case ast.StmtForIn:
		return "ForIn"
	default:
		return fmt.Sprintf("StmtKind(%d)", kind)
	}
}

// formatStmtPretty writes a tree-like, human-readable representation of the statement identified by
// stmtID to w, including the statement kind and its source span.
//
// If builder or its statements arena is nil, it writes "<no statements arena>". If the referenced
// statement is nil, it writes "<nil>".
//
// The output handles block, let, expr, and return statements with nested ASCII tree markers and
// summaries for nested expressions and types. It returns any write or recursive formatting error.
func formatStmtPretty(w io.Writer, builder *ast.Builder, stmtID ast.StmtID, fs *source.FileSet, prefix string) error {
	if builder == nil || builder.Stmts == nil {
		fmt.Fprintf(w, "<no statements arena>\n") //nolint:errcheck
		return nil
	}
	stmt := builder.Stmts.Get(stmtID)
	if stmt == nil {
		fmt.Fprintf(w, "<nil>\n") //nolint:errcheck
		return nil
	}

	fmt.Fprintf(w, "%s (span: %s)\n", formatStmtKind(stmt.Kind), formatSpan(stmt.Span, fs)) //nolint:errcheck

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
			fmt.Fprintf(w, "%s%s Stmt[%d]: ", prefix, marker, idx) //nolint:errcheck
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
			fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, field.label, field.value) //nolint:errcheck
		}

	case ast.StmtExpr:
		exprStmt := builder.Stmts.Expr(stmtID)
		if exprStmt == nil {
			return nil
		}
		fmt.Fprintf(w, "%s└─ Expr: %s\n", prefix, formatExprSummary(builder, exprStmt.Expr)) //nolint:errcheck

	case ast.StmtSignal:
		signalStmt := builder.Stmts.Signal(stmtID)
		if signalStmt == nil {
			return nil
		}
		fields := []struct {
			label string
			value string
		}{
			{"Name", lookupStringOr(builder, signalStmt.Name, "<anon>")},
			{"Value", formatExprSummary(builder, signalStmt.Value)},
		}
		for i, field := range fields {
			marker := "├─"
			if i == len(fields)-1 {
				marker = "└─"
			}
			fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, field.label, field.value) //nolint:errcheck
		}

	case ast.StmtReturn:
		retStmt := builder.Stmts.Return(stmtID)
		if retStmt == nil {
			return nil
		}
		value := "<none>"
		if retStmt.Expr.IsValid() {
			value = formatExprSummary(builder, retStmt.Expr)
		}
		fmt.Fprintf(w, "%s└─ Expr: %s\n", prefix, value) //nolint:errcheck

	case ast.StmtBreak, ast.StmtContinue:
		fmt.Fprintf(w, "%s└─ (no additional data)\n", prefix) //nolint:errcheck

	case ast.StmtIf:
		ifStmt := builder.Stmts.If(stmtID)
		if ifStmt == nil {
			return nil
		}
		entries := []struct {
			label string
			kind  string
			expr  ast.ExprID
			stmt  ast.StmtID
			text  string
		}{
			{label: "Cond", kind: "expr", expr: ifStmt.Cond},
			{label: "Then", kind: "stmt", stmt: ifStmt.Then},
		}
		if ifStmt.Else.IsValid() {
			entries = append(entries, struct {
				label string
				kind  string
				expr  ast.ExprID
				stmt  ast.StmtID
				text  string
			}{label: "Else", kind: "stmt", stmt: ifStmt.Else})
		} else {
			entries = append(entries, struct {
				label string
				kind  string
				expr  ast.ExprID
				stmt  ast.StmtID
				text  string
			}{label: "Else", kind: "text", text: "<none>"})
		}
		for idx, entry := range entries {
			marker := "├─"
			childPrefix := prefix + "│  "
			if idx == len(entries)-1 {
				marker = "└─"
				childPrefix = prefix + "   "
			}
			switch entry.kind {
			case "expr":
				fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, entry.label, formatExprSummary(builder, entry.expr)) //nolint:errcheck
			case "stmt":
				fmt.Fprintf(w, "%s%s %s:\n", prefix, marker, entry.label) //nolint:errcheck
				if err := formatStmtPretty(w, builder, entry.stmt, fs, childPrefix); err != nil {
					return err
				}
			case "text":
				fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, entry.label, entry.text) //nolint:errcheck
			}
		}

	case ast.StmtWhile:
		whileStmt := builder.Stmts.While(stmtID)
		if whileStmt == nil {
			return nil
		}
		fmt.Fprintf(w, "%s├─ Cond: %s\n", prefix, formatExprSummary(builder, whileStmt.Cond)) //nolint:errcheck
		fmt.Fprintf(w, "%s└─ Body:\n", prefix)                                                //nolint:errcheck
		if err := formatStmtPretty(w, builder, whileStmt.Body, fs, prefix+"   "); err != nil {
			return err
		}

	case ast.StmtForClassic:
		forStmt := builder.Stmts.ForClassic(stmtID)
		if forStmt == nil {
			return nil
		}
		type entry struct {
			label string
			kind  string
			expr  ast.ExprID
			stmt  ast.StmtID
		}
		var entries []entry
		if forStmt.Init.IsValid() {
			entries = append(entries, entry{label: "Init", kind: "stmt", stmt: forStmt.Init})
		}
		entries = append(entries,
			entry{label: "Cond", kind: "expr", expr: forStmt.Cond},
			entry{label: "Post", kind: "expr", expr: forStmt.Post},
			entry{label: "Body", kind: "stmt", stmt: forStmt.Body},
		)
		for idx, e := range entries {
			marker := "├─"
			childPrefix := prefix + "│  "
			if idx == len(entries)-1 {
				marker = "└─"
				childPrefix = prefix + "   "
			}
			switch e.kind {
			case "expr":
				fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, e.label, formatExprSummary(builder, e.expr)) //nolint:errcheck
			case "stmt":
				fmt.Fprintf(w, "%s%s %s:\n", prefix, marker, e.label) //nolint:errcheck
				if e.stmt.IsValid() {
					if err := formatStmtPretty(w, builder, e.stmt, fs, childPrefix); err != nil {
						return err
					}
				} else {
					fmt.Fprintf(w, "%s<none>\n", childPrefix) //nolint:errcheck
				}
			}
		}

	case ast.StmtForIn:
		forIn := builder.Stmts.ForIn(stmtID)
		if forIn == nil {
			return nil
		}
		patternName := lookupStringOr(builder, forIn.Pattern, "<anon>")
		fmt.Fprintf(w, "%s├─ Pattern: %s\n", prefix, patternName) //nolint:errcheck
		if forIn.Type.IsValid() {
			fmt.Fprintf(w, "%s├─ Type: %s\n", prefix, formatTypeExprInline(builder, forIn.Type)) //nolint:errcheck
		}
		fmt.Fprintf(w, "%s├─ Iterable: %s\n", prefix, formatExprSummary(builder, forIn.Iterable)) //nolint:errcheck
		fmt.Fprintf(w, "%s└─ Body:\n", prefix)                                                    //nolint:errcheck
		if err := formatStmtPretty(w, builder, forIn.Body, fs, prefix+"   "); err != nil {
			return err
		}
	}

	return nil
}
