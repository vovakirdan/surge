package sema

import (
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type constEvalState uint8

const (
	constStateUnvisited constEvalState = iota
	constStateVisiting
	constStateDone
)

func (tc *typeChecker) ensureConstEvaluated(symID symbols.SymbolID) types.TypeID {
	if !symID.IsValid() {
		return types.NoTypeID
	}
	switch tc.constState[symID] {
	case constStateDone:
		return tc.bindingType(symID)
	case constStateVisiting:
		tc.reportConstCycle(symID)
		return types.NoTypeID
	}
	tc.constState[symID] = constStateVisiting

	typeID, valueID, scope, span := tc.constBinding(symID)
	scope = tc.scopeOrFile(scope)
	declaredType := types.NoTypeID
	if typeID.IsValid() {
		declaredType = tc.resolveTypeExprWithScope(typeID, scope)
		if declaredType != types.NoTypeID {
			tc.setBindingType(symID, declaredType)
		}
	}

	valueType := types.NoTypeID
	if valueID.IsValid() {
		valueType = tc.typeExpr(valueID)
	}
	if declaredType != types.NoTypeID && valueType != types.NoTypeID {
		tc.ensureBindingTypeMatch(typeID, declaredType, valueType, valueID)
	}
	if declaredType == types.NoTypeID {
		tc.setBindingType(symID, valueType)
	}
	if valueID.IsValid() {
		tc.requireConstExpr(valueID, symID, span)
	}
	tc.constState[symID] = constStateDone
	return tc.bindingType(symID)
}

func (tc *typeChecker) constBinding(symID symbols.SymbolID) (ast.TypeID, ast.ExprID, symbols.ScopeID, source.Span) {
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Kind != symbols.SymbolConst {
		return ast.NoTypeID, ast.NoExprID, symbols.NoScopeID, source.Span{}
	}
	if tc.builder == nil {
		return ast.NoTypeID, ast.NoExprID, symbols.NoScopeID, source.Span{}
	}
	if sym.Decl.Item.IsValid() {
		if constItem, ok := tc.builder.Items.Const(sym.Decl.Item); ok && constItem != nil {
			return constItem.Type, constItem.Value, tc.scopeForItem(sym.Decl.Item), constItem.Span
		}
	}
	if sym.Decl.Stmt.IsValid() {
		if constStmt := tc.builder.Stmts.Const(sym.Decl.Stmt); constStmt != nil {
			span := source.Span{}
			if stmt := tc.builder.Stmts.Get(sym.Decl.Stmt); stmt != nil {
				span = stmt.Span
			}
			return constStmt.Type, constStmt.Value, tc.scopeForStmt(sym.Decl.Stmt), span
		}
	}
	return ast.NoTypeID, ast.NoExprID, symbols.NoScopeID, sym.Span
}

func (tc *typeChecker) reportConstCycle(symID symbols.SymbolID) {
	_, _, _, span := tc.constBinding(symID)
	if span == (source.Span{}) {
		if sym := tc.symbolFromID(symID); sym != nil {
			span = sym.Span
		}
	}
	name := tc.constSymbolName(symID)
	msg := "cyclic const evaluation"
	if name != "" {
		msg = fmt.Sprintf("cyclic const evaluation of %s", name)
	}
	tc.report(diag.SemaConstCycle, span, "%s", msg)
	tc.constState[symID] = constStateDone
}

func (tc *typeChecker) requireConstExpr(expr ast.ExprID, symID symbols.SymbolID, fallback source.Span) {
	if tc.isConstExpr(expr) {
		return
	}
	span := fallback
	if expr.IsValid() && tc.builder != nil {
		if node := tc.builder.Exprs.Get(expr); node != nil {
			span = node.Span
		}
	}
	name := tc.constSymbolName(symID)
	msg := "const initializer must be a compile-time constant"
	if name != "" {
		msg = fmt.Sprintf("const '%s' initializer must be a compile-time constant", name)
	}
	tc.report(diag.SemaConstNotConstant, span, "%s", msg)
}

func (tc *typeChecker) constSymbolName(symID symbols.SymbolID) string {
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return ""
	}
	return tc.lookupName(sym.Name)
}

func (tc *typeChecker) isConstExpr(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprLit:
		return true
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(expr); ok && group != nil {
			return tc.isConstExpr(group.Inner)
		}
	case ast.ExprUnary:
		if data, ok := tc.builder.Exprs.Unary(expr); ok && data != nil {
			switch data.Op {
			case ast.ExprUnaryPlus, ast.ExprUnaryMinus, ast.ExprUnaryNot:
				return tc.isConstExpr(data.Operand)
			default:
				return false
			}
		}
	case ast.ExprBinary:
		if data, ok := tc.builder.Exprs.Binary(expr); ok && data != nil {
			if !tc.isConstBinaryOp(data.Op) {
				return false
			}
			return tc.isConstExpr(data.Left) && tc.isConstExpr(data.Right)
		}
	case ast.ExprIdent:
		if symID := tc.symbolForExpr(expr); symID.IsValid() {
			if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolConst {
				tc.ensureConstEvaluated(symID)
				return true
			}
		}
	case ast.ExprMember:
		if member, ok := tc.builder.Exprs.Member(expr); ok && member != nil {
			if module := tc.moduleSymbolForExpr(member.Target); module != nil {
				if exp := tc.lookupModuleExport(module, member.Field, node.Span); exp != nil && exp.Kind == symbols.SymbolConst {
					return true
				}
			}
		}
	}
	return false
}

func (tc *typeChecker) isConstBinaryOp(op ast.ExprBinaryOp) bool {
	switch op {
	case ast.ExprBinaryAdd,
		ast.ExprBinarySub,
		ast.ExprBinaryMul,
		ast.ExprBinaryDiv,
		ast.ExprBinaryMod,
		ast.ExprBinaryLogicalAnd,
		ast.ExprBinaryLogicalOr,
		ast.ExprBinaryEq,
		ast.ExprBinaryNotEq,
		ast.ExprBinaryLess,
		ast.ExprBinaryLessEq,
		ast.ExprBinaryGreater,
		ast.ExprBinaryGreaterEq:
		return true
	default:
		return false
	}
}

func (tc *typeChecker) constUintValue(expr ast.ExprID, visited map[symbols.SymbolID]bool) (uint64, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return 0, false
	}
	if visited == nil {
		visited = make(map[symbols.SymbolID]bool)
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return 0, false
	}
	switch node.Kind {
	case ast.ExprLit:
		lit, _ := tc.builder.Exprs.Literal(expr)
		if lit == nil {
			return 0, false
		}
		switch lit.Kind {
		case ast.ExprLitInt, ast.ExprLitUint:
			return tc.parseConstUintLiteral(lit.Value)
		default:
			return 0, false
		}
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(expr); ok && group != nil {
			return tc.constUintValue(group.Inner, visited)
		}
	case ast.ExprUnary:
		if data, ok := tc.builder.Exprs.Unary(expr); ok && data != nil {
			switch data.Op {
			case ast.ExprUnaryPlus:
				return tc.constUintValue(data.Operand, visited)
			case ast.ExprUnaryMinus:
				value, ok := tc.constUintValue(data.Operand, visited)
				if !ok || value > uint64(math.MaxInt64) {
					return 0, false
				}
				neg := -int64(value)
				if neg < 0 {
					return 0, false
				}
				return uint64(neg), true
			default:
				return 0, false
			}
		}
	case ast.ExprBinary:
		if data, ok := tc.builder.Exprs.Binary(expr); ok && data != nil {
			left, okLeft := tc.constUintValue(data.Left, visited)
			if !okLeft {
				return 0, false
			}
			right, okRight := tc.constUintValue(data.Right, visited)
			if !okRight {
				return 0, false
			}
			switch data.Op {
			case ast.ExprBinaryAdd:
				result, carry := bits.Add64(left, right, 0)
				if carry != 0 {
					return 0, false
				}
				return result, true
			case ast.ExprBinarySub:
				if left < right {
					return 0, false
				}
				return left - right, true
			case ast.ExprBinaryMul:
				hi, lo := bits.Mul64(left, right)
				if hi != 0 {
					return 0, false
				}
				return lo, true
			case ast.ExprBinaryDiv:
				if right == 0 {
					return 0, false
				}
				return left / right, true
			case ast.ExprBinaryMod:
				if right == 0 {
					return 0, false
				}
				return left % right, true
			default:
				return 0, false
			}
		}
	case ast.ExprIdent:
		if symID := tc.symbolForExpr(expr); symID.IsValid() {
			sym := tc.symbolFromID(symID)
			if sym == nil || sym.Kind != symbols.SymbolConst {
				return 0, false
			}
			if visited[symID] {
				return 0, false
			}
			visited[symID] = true
			defer delete(visited, symID)
			tc.ensureConstEvaluated(symID)
			_, valueExpr, _, _ := tc.constBinding(symID)
			if !valueExpr.IsValid() {
				return 0, false
			}
			return tc.constUintValue(valueExpr, visited)
		}
		if ident, ok := tc.builder.Exprs.Ident(expr); ok && ident != nil {
			if param := tc.lookupTypeParam(ident.Name); param != types.NoTypeID {
				if val, okVal := tc.constValueFromType(param); okVal {
					return val, true
				}
			}
		}
	}
	return 0, false
}

func (tc *typeChecker) parseConstUintLiteral(id source.StringID) (uint64, bool) {
	if tc.builder == nil || tc.builder.StringsInterner == nil {
		return 0, false
	}
	raw := tc.builder.StringsInterner.MustLookup(id)
	clean := strings.ReplaceAll(raw, "_", "")
	if clean == "" {
		return 0, false
	}
	if strings.HasPrefix(clean, "+") || strings.HasPrefix(clean, "-") {
		return 0, false
	}
	body, suffix, err := splitNumericLiteral(clean)
	if err != nil {
		return 0, false
	}
	if suffix != "" && !isValidIntegerSuffix(suffix) {
		return 0, false
	}
	value, err := strconv.ParseUint(body, 0, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func splitNumericLiteral(lit string) (prefix, suffix string, err error) {
	if lit == "" {
		return "", "", fmt.Errorf("empty literal")
	}
	base := 10
	start := 0
	if len(lit) >= 2 && lit[0] == '0' {
		switch lit[1] {
		case 'x', 'X':
			base = 16
			start = 2
		case 'b', 'B':
			base = 2
			start = 2
		case 'o', 'O':
			base = 8
			start = 2
		}
	}

	end := start
	for end < len(lit) && isDigitForBase(lit[end], base) {
		end++
	}

	if end == start && start != 0 {
		return "", "", fmt.Errorf("missing digits after base prefix")
	}
	if end == 0 {
		return "", "", fmt.Errorf("missing digits in literal")
	}
	if end < len(lit) {
		switch lit[end] {
		case '.':
			return "", "", fmt.Errorf("fractional literals are not allowed in constant integers")
		case 'e', 'E':
			if base == 10 {
				return "", "", fmt.Errorf("fractional literals are not allowed in constant integers")
			}
		case 'p', 'P':
			if base == 16 {
				return "", "", fmt.Errorf("fractional literals are not allowed in constant integers")
			}
		}
	}

	return lit[:end], lit[end:], nil
}

func isDigitForBase(b byte, base int) bool {
	switch base {
	case 2:
		return b == '0' || b == '1'
	case 8:
		return b >= '0' && b <= '7'
	case 10:
		return b >= '0' && b <= '9'
	case 16:
		return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
	default:
		return false
	}
}

func isValidIntegerSuffix(s string) bool {
	if s == "" {
		return true
	}
	for i := range s {
		ch := s[i]
		if i == 0 {
			if !isLetter(ch) {
				return false
			}
			continue
		}
		if !isLetter(ch) && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
