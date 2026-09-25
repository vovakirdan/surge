package sema

import (
	"cmp"
	"slices"
	"strconv"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A struct literal without a type name (`{ x: 1 }`) and an empty array literal
// (`[]`) have no type of their own: typeExprStruct and typeExprArray leave them
// untyped, and a consumer that expects a struct or array type gives them one
// through applyExpectedType. LANGUAGE.md §2.5 requires that expected type, and
// there is no anonymous record type (owner ruling 2026-09-25). A literal that
// no consumer typed is refused here with SemaLiteralNeedsType, once, at the
// literal. The checker reads NoTypeID as an error already reported, so what
// reads the literal stays quiet; the consumers that report on an unknown value
// anyway (a call's argument, a method's receiver or argument) report the
// literal instead of their own mismatch.

// untypedLiteralState is the typeChecker's record of those literals.
type untypedLiteralState struct {
	roots      map[ast.ExprID]struct{}
	errorSpans []source.Span // primary spans of the errors the checker reported
}

// noteUntypedLiteral records a literal its own typing left untyped.
func (tc *typeChecker) noteUntypedLiteral(id ast.ExprID) {
	if tc.untyped.roots == nil {
		tc.untyped.roots = make(map[ast.ExprID]struct{})
	}
	tc.untyped.roots[id] = struct{}{}
}

// errorSpanRecorder forwards every diagnostic and keeps the primary span of each error.
type errorSpanRecorder struct {
	inner diag.Reporter
	spans *[]source.Span
}

func (r *errorSpanRecorder) Report(code diag.Code, sev diag.Severity, primary source.Span, msg string, notes []diag.Note, fixes []*diag.Fix) {
	r.ReportDiagnostic(&diag.Diagnostic{Severity: sev, Code: code, Message: msg, Primary: primary, Notes: notes, Fixes: fixes})
}

func (r *errorSpanRecorder) ReportDiagnostic(d *diag.Diagnostic) {
	if r == nil || d == nil {
		return
	}
	if d.Severity >= diag.SevError && r.spans != nil {
		*r.spans = append(*r.spans, d.Primary)
	}
	diag.ForwardDiagnostic(r.inner, d)
}

// untypedLiteralRoot answers the untyped literal an untyped expression reads:
// the literal itself, the unannotated `let` bound to it, or a group, field,
// operator, array, tuple or ternary arm over one.
func (tc *typeChecker) untypedLiteralRoot(id ast.ExprID) (ast.ExprID, bool) {
	for depth := 0; depth < 64 && id.IsValid() && tc.result.ExprTypes[id] == types.NoTypeID; depth++ {
		if _, root := tc.untyped.roots[id]; root {
			return id, true
		}
		node := tc.builder.Exprs.Get(id)
		if node == nil {
			return ast.NoExprID, false
		}
		var next []ast.ExprID
		switch node.Kind {
		case ast.ExprGroup:
			if data, ok := tc.builder.Exprs.Group(id); ok && data != nil {
				next = []ast.ExprID{data.Inner}
			}
		case ast.ExprMember:
			if data, ok := tc.builder.Exprs.Member(id); ok && data != nil {
				next = []ast.ExprID{data.Target}
			}
		case ast.ExprUnary:
			if data, ok := tc.builder.Exprs.Unary(id); ok && data != nil {
				next = []ast.ExprID{data.Operand}
			}
		case ast.ExprBinary:
			if data, ok := tc.builder.Exprs.Binary(id); ok && data != nil {
				next = []ast.ExprID{data.Left, data.Right}
			}
		case ast.ExprTernary:
			if data, ok := tc.builder.Exprs.Ternary(id); ok && data != nil {
				next = []ast.ExprID{data.TrueExpr, data.FalseExpr}
			}
		case ast.ExprArray:
			if data, ok := tc.builder.Exprs.Array(id); ok && data != nil {
				next = data.Elements
			}
		case ast.ExprTuple:
			if data, ok := tc.builder.Exprs.Tuple(id); ok && data != nil {
				next = data.Elements
			}
		case ast.ExprIdent:
			next = []ast.ExprID{tc.untypedLetValue(tc.symbolForExpr(id))}
		}
		if len(next) == 0 {
			return ast.NoExprID, false
		}
		for _, child := range next[1:] {
			if root, ok := tc.untypedLiteralRoot(child); ok {
				return root, true
			}
		}
		id = next[0]
	}
	return ast.NoExprID, false
}

// untypedLetValue answers the value of an unannotated, untyped `let` binding.
func (tc *typeChecker) untypedLetValue(symID symbols.SymbolID) ast.ExprID {
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Kind != symbols.SymbolLet || tc.bindingType(symID) != types.NoTypeID || !sym.Decl.Stmt.IsValid() {
		return ast.NoExprID
	}
	if stmt := tc.builder.Stmts.Get(sym.Decl.Stmt); stmt == nil || stmt.Kind != ast.StmtLet {
		return ast.NoExprID
	}
	decl := tc.builder.Stmts.Let(sym.Decl.Stmt)
	if decl == nil || decl.Type.IsValid() || decl.Pattern.IsValid() {
		return ast.NoExprID
	}
	return decl.Value
}

// reportUntypedArguments reports the untyped literal behind each untyped
// argument, in place of the call's own mismatch. expected answers the type a
// parameter would have given, when one is known, for the help.
func (tc *typeChecker) reportUntypedArguments(exprs []ast.ExprID, argTypes []types.TypeID, expected func(int) types.TypeID) bool {
	found := false
	for i, expr := range exprs {
		if i >= len(argTypes) || argTypes[i] != types.NoTypeID {
			continue
		}
		if root, ok := tc.untypedLiteralRoot(expr); ok {
			if tc.unwrapGroupExpr(expr) != root {
				tc.reportUntypedLiteral(root, false, types.NoTypeID)
			} else if expected != nil {
				tc.reportUntypedLiteral(root, true, expected(i))
			} else {
				tc.reportUntypedLiteral(root, true, types.NoTypeID)
			}
			found = true
		}
	}
	return found
}

// reportUntypedReceiver reports the untyped literal a method's receiver reads,
// in place of "has no method".
func (tc *typeChecker) reportUntypedReceiver(recv types.TypeID, recvExpr ast.ExprID) bool {
	if recv != types.NoTypeID {
		return false
	}
	root, ok := tc.untypedLiteralRoot(recvExpr)
	if ok {
		tc.reportUntypedLiteral(root, false, types.NoTypeID)
	}
	return ok
}

// reportUntypedCallArguments is reportUntypedArguments for a free call; a
// single non-generic candidate names the parameter types for the help.
func (tc *typeChecker) reportUntypedCallArguments(candidates []symbols.SymbolID, args []callArg) bool {
	exprs := make([]ast.ExprID, len(args))
	named := false
	for i, arg := range args {
		exprs[i] = arg.expr
		named = named || arg.name != source.NoStringID
	}
	var sig *symbols.FunctionSignature
	if sym := tc.singleCandidate(candidates); sym != nil && len(sym.TypeParams) == 0 && !named {
		sig = sym.Signature
	}
	return tc.reportUntypedArguments(exprs, tc.collectArgTypes(args), func(i int) types.TypeID {
		if sig == nil || i >= len(sig.Params) {
			return types.NoTypeID
		}
		return tc.typeFromKey(sig.Params[i])
	})
}

func (tc *typeChecker) singleCandidate(candidates []symbols.SymbolID) *symbols.Symbol {
	if len(candidates) != 1 {
		return nil
	}
	sym := tc.symbolFromID(candidates[0])
	if sym == nil || sym.Signature == nil {
		return nil
	}
	return sym
}

// reportUntypedLiterals refuses every recorded literal still untyped once the
// walk is done, outermost first, so a literal inside another is not reported
// twice.
func (tc *typeChecker) reportUntypedLiterals() {
	roots := make([]ast.ExprID, 0, len(tc.untyped.roots))
	for id := range tc.untyped.roots {
		if tc.result.ExprTypes[id] == types.NoTypeID {
			roots = append(roots, id)
		}
	}
	slices.SortFunc(roots, func(a, b ast.ExprID) int {
		sa, sb := tc.exprSpan(a), tc.exprSpan(b)
		return cmp.Or(cmp.Compare(sa.File, sb.File), cmp.Compare(sa.Start, sb.Start), cmp.Compare(sb.End, sa.End))
	})
	for _, id := range roots {
		tc.reportUntypedLiteral(id, false, types.NoTypeID)
	}
}

// reportUntypedLiteral refuses one literal unless an error already covers it:
// one reported at, inside or around the literal, including its own. argument
// says the literal is itself an argument; expected is its parameter's type
// when the call names one.
func (tc *typeChecker) reportUntypedLiteral(root ast.ExprID, argument bool, expected types.TypeID) {
	span := tc.exprSpan(root)
	for _, reported := range tc.untyped.errorSpans {
		if reported.File == span.File && reported.Start < span.End && span.Start < reported.End {
			return
		}
	}
	array := false
	if node := tc.builder.Exprs.Get(root); node != nil && node.Kind == ast.ExprArray {
		array = true
	}
	message, help := "struct literal without a type name gets no type here",
		"name the struct, as in `Point { ... }`, or annotate the binding, as in `let p: Point = { ... };`"
	note := "a parameter's type does not give a struct literal its type"
	if array {
		message, help = "empty array literal gets no element type here",
			"annotate the binding, as in `let items: int[] = [];`"
		note = "a parameter's type does not give an empty array literal its element type"
	}
	if info, _ := tc.structInfoForType(expected); info != nil && !array {
		help = "name the struct: `" + tc.typeLabel(expected) + " { ... }`"
	} else if _, _, fixed, ok := tc.arrayInfo(expected); ok && !fixed && array {
		help = "bind it with its type first, as in `let items: " + tc.arrayTypeSyntax(expected) + " = [];`, and pass `items`"
	}
	b := diag.ReportError(tc.reporter, diag.SemaLiteralNeedsType, span, message)
	if b == nil {
		return
	}
	if argument {
		b.WithNote(span, note)
	}
	b.WithHelp(span, help).Emit()
}

// noteIfUntypedLiteral records an untyped struct literal without a type name
// or an untyped empty array literal, as typeExpr leaves them.
func (tc *typeChecker) noteIfUntypedLiteral(id ast.ExprID, kind ast.ExprKind) {
	switch kind {
	case ast.ExprStruct:
		if data, ok := tc.builder.Exprs.Struct(id); ok && data != nil && !data.Type.IsValid() {
			tc.noteUntypedLiteral(id)
		}
	case ast.ExprArray:
		if data, ok := tc.builder.Exprs.Array(id); ok && data != nil && len(data.Elements) == 0 {
			tc.noteUntypedLiteral(id)
		}
	}
}

// arrayTypeSyntax spells an array type as it is written (`int[]`), not as its label (`[int]`).
func (tc *typeChecker) arrayTypeSyntax(ty types.TypeID) string {
	elem, length, fixed, ok := tc.arrayInfo(ty)
	if !ok {
		return tc.typeLabel(ty)
	}
	if fixed {
		return tc.arrayTypeSyntax(elem) + "[" + strconv.FormatUint(uint64(length), 10) + "]"
	}
	return tc.arrayTypeSyntax(elem) + "[]"
}
