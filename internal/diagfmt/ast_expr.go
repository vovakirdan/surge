package diagfmt

import (
	"fmt"
	"strings"
	"surge/internal/ast"
	"surge/internal/source"
)

const exprInlineMaxDepth = 32

// formatExprSummary produces a compact diagnostic summary for the given expression ID.
// If exprID is invalid it returns "<none>". Otherwise it returns a string of the form
// "expr#<id>: <inline>" where `<inline>` is a concise inline representation; if that
// representation is empty it returns "<invalid>".
func formatExprSummary(builder *ast.Builder, exprID ast.ExprID) string {
	if !exprID.IsValid() {
		return "<none>"
	}
	inline := formatExprInlineDepth(builder, exprID, 0)
	if inline == "" {
		inline = "<invalid>"
	}
	return fmt.Sprintf("expr#%d: %s", uint32(exprID), inline)
}

// formatExprInline produces a compact, human-friendly inline representation of the expression identified by exprID.
// If the expression is invalid or cannot be resolved, it yields a placeholder such as "<none>" or "<invalid>".
func formatExprInline(builder *ast.Builder, exprID ast.ExprID) string {
	return formatExprInlineDepth(builder, exprID, 0)
}

// `"<invalid-binary>"`, etc.
func formatExprInlineDepth(builder *ast.Builder, exprID ast.ExprID, depth int) string {
	if !exprID.IsValid() {
		return "<none>"
	}
	if builder == nil || builder.Exprs == nil {
		return "<invalid>"
	}
	if depth >= exprInlineMaxDepth {
		return "..."
	}

	expr := builder.Exprs.Get(exprID)
	if expr == nil {
		return "<invalid>"
	}

	switch expr.Kind {
	case ast.ExprIdent:
		data, ok := builder.Exprs.Ident(exprID)
		if !ok {
			return "<invalid-ident>"
		}
		if builder.StringsInterner == nil || data.Name == source.NoStringID {
			return "<ident>"
		}
		return builder.StringsInterner.MustLookup(data.Name)
	case ast.ExprLit:
		data, ok := builder.Exprs.Literal(exprID)
		if !ok {
			return "<invalid-literal>"
		}
		switch data.Kind {
		case ast.ExprLitTrue:
			return "true"
		case ast.ExprLitFalse:
			return "false"
		case ast.ExprLitNothing:
			return "nothing"
		default:
			if builder.StringsInterner != nil && data.Value != source.NoStringID {
				return builder.StringsInterner.MustLookup(data.Value)
			}
			return "<literal>"
		}
	case ast.ExprUnary:
		data, ok := builder.Exprs.Unary(exprID)
		if !ok {
			return "<invalid-unary>"
		}
		operand := formatExprInlineDepth(builder, data.Operand, depth+1)
		operand = wrapExprIfNeeded(builder, data.Operand, operand)
		return formatUnaryOpString(data.Op, operand)
	case ast.ExprBinary:
		data, ok := builder.Exprs.Binary(exprID)
		if !ok {
			return "<invalid-binary>"
		}
		left := formatExprInlineDepth(builder, data.Left, depth+1)
		right := formatExprInlineDepth(builder, data.Right, depth+1)
		left = wrapExprIfNeeded(builder, data.Left, left)
		right = wrapExprIfNeeded(builder, data.Right, right)
		op := formatBinaryOpString(data.Op)
		return fmt.Sprintf("(%s %s %s)", left, op, right)
	case ast.ExprCall:
		data, ok := builder.Exprs.Call(exprID)
		if !ok {
			return "<invalid-call>"
		}
		target := formatExprInlineDepth(builder, data.Target, depth+1)
		target = wrapExprIfNeeded(builder, data.Target, target)
		args := make([]string, 0, len(data.Args))
		for _, arg := range data.Args {
			args = append(args, formatExprInlineDepth(builder, arg, depth+1))
		}
		return fmt.Sprintf("%s(%s)", target, strings.Join(args, ", "))
	case ast.ExprIndex:
		data, ok := builder.Exprs.Index(exprID)
		if !ok {
			return "<invalid-index>"
		}
		target := formatExprInlineDepth(builder, data.Target, depth+1)
		target = wrapExprIfNeeded(builder, data.Target, target)
		index := formatExprInlineDepth(builder, data.Index, depth+1)
		return fmt.Sprintf("%s[%s]", target, index)
	case ast.ExprMember:
		data, ok := builder.Exprs.Member(exprID)
		if !ok {
			return "<invalid-member>"
		}
		target := formatExprInlineDepth(builder, data.Target, depth+1)
		target = wrapExprIfNeeded(builder, data.Target, target)
		field := "<field>"
		if builder.StringsInterner != nil && data.Field != source.NoStringID {
			field = builder.StringsInterner.MustLookup(data.Field)
		}
		return fmt.Sprintf("%s.%s", target, field)
	case ast.ExprAwait:
		data, ok := builder.Exprs.Await(exprID)
		if !ok {
			return "<invalid-await>"
		}
		target := formatExprInlineDepth(builder, data.Value, depth+1)
		target = wrapExprIfNeeded(builder, data.Value, target)
		return target + ".await"
	case ast.ExprGroup:
		data, ok := builder.Exprs.Group(exprID)
		if !ok {
			return "<invalid-group>"
		}
		inner := formatExprInlineDepth(builder, data.Inner, depth+1)
		return fmt.Sprintf("(%s)", inner)
	case ast.ExprTuple:
		data, ok := builder.Exprs.Tuple(exprID)
		if !ok {
			return "<invalid-tuple>"
		}
		if len(data.Elements) == 0 {
			return "()"
		}
		elems := make([]string, 0, len(data.Elements))
		for _, elem := range data.Elements {
			elems = append(elems, formatExprInlineDepth(builder, elem, depth+1))
		}
		if len(data.Elements) == 1 {
			return fmt.Sprintf("(%s,)", elems[0])
		}
		return fmt.Sprintf("(%s)", strings.Join(elems, ", "))
	case ast.ExprCast:
		data, ok := builder.Exprs.Cast(exprID)
		if !ok {
			return "<invalid-cast>"
		}
		value := formatExprInlineDepth(builder, data.Value, depth+1)
		value = wrapExprIfNeeded(builder, data.Value, value)
		typ := formatTypeExprInline(builder, data.Type)
		return fmt.Sprintf("%s to %s", value, typ)
	case ast.ExprSpread:
		data, ok := builder.Exprs.Spread(exprID)
		if !ok {
			return "<invalid-spread>"
		}
		value := formatExprInlineDepth(builder, data.Value, depth+1)
		value = wrapExprIfNeeded(builder, data.Value, value)
		return value + "..."
	case ast.ExprSpawn:
		data, ok := builder.Exprs.Spawn(exprID)
		if !ok {
			return "<invalid-spawn>"
		}
		operand := formatExprInlineDepth(builder, data.Value, depth+1)
		operand = wrapExprIfNeeded(builder, data.Value, operand)
		return "spawn " + operand
	case ast.ExprAsync:
		data, ok := builder.Exprs.Async(exprID)
		if !ok || data == nil {
			return "<invalid-async>"
		}
		if builder.Stmts != nil && data.Body.IsValid() {
			if block := builder.Stmts.Block(data.Body); block != nil {
				return fmt.Sprintf("async { %d stmt(s) }", len(block.Stmts))
			}
		}
		return "async { ... }"
	case ast.ExprParallel:
		data, ok := builder.Exprs.Parallel(exprID)
		if !ok {
			return "<invalid-parallel>"
		}
		iterable := formatExprInlineDepth(builder, data.Iterable, depth+1)
		iterable = wrapExprIfNeeded(builder, data.Iterable, iterable)
		args := make([]string, 0, len(data.Args))
		for _, argID := range data.Args {
			arg := formatExprInlineDepth(builder, argID, depth+1)
			args = append(args, arg)
		}
		argsList := "()"
		if len(args) > 0 {
			argsList = "(" + strings.Join(args, ", ") + ")"
		}
		body := formatExprInlineDepth(builder, data.Body, depth+1)
		body = wrapExprIfNeeded(builder, data.Body, body)

		var sb strings.Builder
		switch data.Kind {
		case ast.ExprParallelMap:
			sb.WriteString("parallel map ")
		case ast.ExprParallelReduce:
			sb.WriteString("parallel reduce ")
		default:
			sb.WriteString("parallel <unknown> ")
		}
		sb.WriteString(iterable)
		sb.WriteString(" with ")
		if data.Kind == ast.ExprParallelReduce {
			init := formatExprInlineDepth(builder, data.Init, depth+1)
			init = wrapExprIfNeeded(builder, data.Init, init)
			sb.WriteString(init)
			sb.WriteString(", ")
		}
		sb.WriteString(argsList)
		sb.WriteString(" => ")
		sb.WriteString(body)
		return sb.String()
	case ast.ExprCompare:
		data, ok := builder.Exprs.Compare(exprID)
		if !ok {
			return "<invalid-compare>"
		}
		subject := formatExprInlineDepth(builder, data.Value, depth+1)
		return fmt.Sprintf("compare %s { %d arms }", subject, len(data.Arms))
	default:
		return fmt.Sprintf("<%s>", formatExprKind(expr.Kind))
	}
}

// wrapExprIfNeeded conditionally wraps a rendered expression in parentheses to preserve
// precedence for certain expression kinds.
// If the expression ID is invalid, or the builder or referenced expression is nil,
// the original rendered string is returned unchanged. Expressions of kinds
// ExprBinary, ExprTernary, ExprCompare, and ExprCast are wrapped with parentheses.
func wrapExprIfNeeded(builder *ast.Builder, exprID ast.ExprID, rendered string) string {
	if !exprID.IsValid() {
		return rendered
	}
	if builder == nil || builder.Exprs == nil {
		return rendered
	}
	expr := builder.Exprs.Get(exprID)
	if expr == nil {
		return rendered
	}

	switch expr.Kind {
	case ast.ExprBinary, ast.ExprTernary, ast.ExprCompare, ast.ExprCast:
		return "(" + rendered + ")"
	default:
		return rendered
	}
}

// formatUnaryOpString formats a unary operator and its operand into a textual representation.
// For known operators it produces the conventional prefix form (e.g. "+x", "&mut x", "await x").
// For unknown operators it returns a placeholder of the form "<unary N> operand".
func formatUnaryOpString(op ast.ExprUnaryOp, operand string) string {
	switch op {
	case ast.ExprUnaryPlus:
		return "+" + operand
	case ast.ExprUnaryMinus:
		return "-" + operand
	case ast.ExprUnaryNot:
		return "!" + operand
	case ast.ExprUnaryDeref:
		return "*" + operand
	case ast.ExprUnaryRef:
		return "&" + operand
	case ast.ExprUnaryRefMut:
		return "&mut " + operand
	case ast.ExprUnaryOwn:
		return "own " + operand
	case ast.ExprUnaryAwait:
		return "await " + operand
	default:
		return fmt.Sprintf("<unary %d> %s", op, operand)
	}
}

// formatBinaryOpString returns the textual symbol for the given binary operator.
// For known operators it yields conventional symbols (for example "+", "&&", "==", "is", ".."); for unknown operators it returns "op<value>" where <value> is the operator's numeric value.
func formatBinaryOpString(op ast.ExprBinaryOp) string {
	switch op {
	case ast.ExprBinaryAdd:
		return "+"
	case ast.ExprBinarySub:
		return "-"
	case ast.ExprBinaryMul:
		return "*"
	case ast.ExprBinaryDiv:
		return "/"
	case ast.ExprBinaryMod:
		return "%"
	case ast.ExprBinaryBitAnd:
		return "&"
	case ast.ExprBinaryBitOr:
		return "|"
	case ast.ExprBinaryBitXor:
		return "^"
	case ast.ExprBinaryShiftLeft:
		return "<<"
	case ast.ExprBinaryShiftRight:
		return ">>"
	case ast.ExprBinaryLogicalAnd:
		return "&&"
	case ast.ExprBinaryLogicalOr:
		return "||"
	case ast.ExprBinaryEq:
		return "=="
	case ast.ExprBinaryNotEq:
		return "!="
	case ast.ExprBinaryLess:
		return "<"
	case ast.ExprBinaryLessEq:
		return "<="
	case ast.ExprBinaryGreater:
		return ">"
	case ast.ExprBinaryGreaterEq:
		return ">="
	case ast.ExprBinaryAssign:
		return "="
	case ast.ExprBinaryAddAssign:
		return "+="
	case ast.ExprBinarySubAssign:
		return "-="
	case ast.ExprBinaryMulAssign:
		return "*="
	case ast.ExprBinaryDivAssign:
		return "/="
	case ast.ExprBinaryModAssign:
		return "%="
	case ast.ExprBinaryBitAndAssign:
		return "&="
	case ast.ExprBinaryBitOrAssign:
		return "|="
	case ast.ExprBinaryBitXorAssign:
		return "^="
	case ast.ExprBinaryShlAssign:
		return "<<="
	case ast.ExprBinaryShrAssign:
		return ">>="
	case ast.ExprBinaryNullCoalescing:
		return "??"
	case ast.ExprBinaryRange:
		return ".."
	case ast.ExprBinaryRangeInclusive:
		return "..="
	case ast.ExprBinaryIs:
		return "is"
	case ast.ExprBinaryHeir:
		return "heir"
	default:
		return fmt.Sprintf("op%d", op)
	}
}

// formatExprKind returns a human-friendly name for the given expression kind.
// For recognized kinds it returns names like "Ident", "Literal", "Call", etc.; for unknown kinds it returns "ExprKind(<numeric>)".
func formatExprKind(kind ast.ExprKind) string {
	switch kind {
	case ast.ExprIdent:
		return "Ident"
	case ast.ExprLit:
		return "Literal"
	case ast.ExprCall:
		return "Call"
	case ast.ExprBinary:
		return "Binary"
	case ast.ExprUnary:
		return "Unary"
	case ast.ExprCast:
		return "Cast"
	case ast.ExprGroup:
		return "Group"
	case ast.ExprTuple:
		return "Tuple"
	case ast.ExprIndex:
		return "Index"
	case ast.ExprMember:
		return "Member"
	case ast.ExprTernary:
		return "Ternary"
	case ast.ExprAwait:
		return "Await"
	case ast.ExprSpawn:
		return "Spawn"
	case ast.ExprParallel:
		return "Parallel"
	case ast.ExprSpread:
		return "Spread"
	case ast.ExprCompare:
		return "Compare"
	case ast.ExprAsync:
		return "Async"
	default:
		return fmt.Sprintf("ExprKind(%d)", kind)
	}
}
