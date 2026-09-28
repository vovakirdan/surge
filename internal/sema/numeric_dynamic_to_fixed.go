package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/types"
)

// reportDynamicToFixedNumeric explains a dynamic numeric (`int`, `uint`,
// `float`) handed to a fixed-width formal of its family: the language never
// narrows it implicitly (LANGUAGE.md §2.1), and the cast that would is the
// author's to write, since it may trap or round. Reports and returns true only
// for a by-value argument of that shape.
func (tc *typeChecker) reportDynamicToFixedNumeric(expected, actual types.TypeID, expr ast.ExprID, span source.Span) bool {
	if tc.reporter == nil || !expr.IsValid() {
		return false
	}
	aInfo, okA := tc.numericInfo(actual)
	eInfo, okE := tc.numericInfo(expected)
	if !okA || !okE || aInfo.kind != eInfo.kind || aInfo.width != types.WidthAny || eInfo.width == types.WidthAny {
		return false
	}
	target := tc.typeLabel(expected)
	msg := fmt.Sprintf("expected %s, got %s: %s never narrows to %s implicitly", target, tc.typeLabel(actual), tc.typeLabel(actual), target)
	b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, span, msg)
	if b == nil {
		return true
	}
	b.WithHelp(span, fmt.Sprintf("write `<expr> to %s` to convert it; the conversion traps if the value does not fit", target))
	if node := tc.builder.Exprs.Get(expr); node != nil {
		switch node.Kind {
		case ast.ExprIdent, ast.ExprCall, ast.ExprMember, ast.ExprIndex, ast.ExprLit:
			b.WithFixSuggestion(fix.InsertText(fmt.Sprintf("insert `to %s`", target), span.ZeroideToEnd(), " to "+target, ""))
		}
	}
	b.Emit()
	return true
}
