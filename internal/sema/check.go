package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Options configure a semantic pass over a file.
type Options struct {
	Reporter diag.Reporter
	Symbols  *symbols.Result
	Types    *types.Interner
}

// Result stores semantic artefacts produced by the checker.
type Result struct {
	TypeInterner *types.Interner
	ExprTypes    map[ast.ExprID]types.TypeID
}

// Check performs semantic analysis (type inference, borrow checks, etc.).
// At this stage it handles literal typing and basic operator validation.
func Check(builder *ast.Builder, fileID ast.FileID, opts Options) Result {
	res := Result{
		ExprTypes: make(map[ast.ExprID]types.TypeID),
	}
	if opts.Types != nil {
		res.TypeInterner = opts.Types
	} else {
		res.TypeInterner = types.NewInterner()
	}
	if builder == nil || fileID == ast.NoFileID {
		return res
	}

	checker := typeChecker{
		builder:  builder,
		fileID:   fileID,
		reporter: opts.Reporter,
		symbols:  opts.Symbols,
		result:   &res,
		types:    res.TypeInterner,
	}
	checker.run()
	return res
}

type typeChecker struct {
	builder  *ast.Builder
	fileID   ast.FileID
	reporter diag.Reporter
	symbols  *symbols.Result
	result   *Result
	types    *types.Interner
}

func (tc *typeChecker) run() {
	if tc.builder == nil || tc.result == nil || tc.types == nil {
		return
	}
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}
	for _, itemID := range file.Items {
		tc.walkItem(itemID)
	}
}

func (tc *typeChecker) walkItem(id ast.ItemID) {
	item := tc.builder.Items.Get(id)
	if item == nil {
		return
	}
	switch item.Kind {
	case ast.ItemLet:
		letItem, ok := tc.builder.Items.Let(id)
		if !ok || letItem == nil || !letItem.Value.IsValid() {
			return
		}
		tc.typeExpr(letItem.Value)
	case ast.ItemFn:
		fnItem, ok := tc.builder.Items.Fn(id)
		if !ok || fnItem == nil || !fnItem.Body.IsValid() {
			return
		}
		tc.walkStmt(fnItem.Body)
	default:
		// Other item kinds are currently ignored.
	}
}

func (tc *typeChecker) walkStmt(id ast.StmtID) {
	stmt := tc.builder.Stmts.Get(id)
	if stmt == nil {
		return
	}
	switch stmt.Kind {
	case ast.StmtBlock:
		if block := tc.builder.Stmts.Block(id); block != nil {
			for _, child := range block.Stmts {
				tc.walkStmt(child)
			}
		}
	case ast.StmtLet:
		if letStmt := tc.builder.Stmts.Let(id); letStmt != nil && letStmt.Value.IsValid() {
			tc.typeExpr(letStmt.Value)
		}
	case ast.StmtExpr:
		if exprStmt := tc.builder.Stmts.Expr(id); exprStmt != nil {
			tc.typeExpr(exprStmt.Expr)
		}
	case ast.StmtReturn:
		if ret := tc.builder.Stmts.Return(id); ret != nil {
			tc.typeExpr(ret.Expr)
		}
	case ast.StmtIf:
		if ifStmt := tc.builder.Stmts.If(id); ifStmt != nil {
			tc.typeExpr(ifStmt.Cond)
			tc.walkStmt(ifStmt.Then)
			if ifStmt.Else.IsValid() {
				tc.walkStmt(ifStmt.Else)
			}
		}
	case ast.StmtWhile:
		if whileStmt := tc.builder.Stmts.While(id); whileStmt != nil {
			tc.typeExpr(whileStmt.Cond)
			tc.walkStmt(whileStmt.Body)
		}
	case ast.StmtForClassic:
		if forStmt := tc.builder.Stmts.ForClassic(id); forStmt != nil {
			if forStmt.Init.IsValid() {
				tc.walkStmt(forStmt.Init)
			}
			tc.typeExpr(forStmt.Cond)
			tc.typeExpr(forStmt.Post)
			tc.walkStmt(forStmt.Body)
		}
	case ast.StmtForIn:
		if forIn := tc.builder.Stmts.ForIn(id); forIn != nil {
			tc.typeExpr(forIn.Iterable)
			tc.walkStmt(forIn.Body)
		}
	case ast.StmtSignal:
		if signal := tc.builder.Stmts.Signal(id); signal != nil {
			tc.typeExpr(signal.Value)
		}
	default:
		// StmtBreak / StmtContinue and others have no expressions to type.
	}
}

func (tc *typeChecker) typeExpr(id ast.ExprID) types.TypeID {
	if !id.IsValid() {
		return types.NoTypeID
	}
	if ty, ok := tc.result.ExprTypes[id]; ok {
		return ty
	}
	expr := tc.builder.Exprs.Get(id)
	if expr == nil {
		return types.NoTypeID
	}
	var ty types.TypeID
	switch expr.Kind {
	case ast.ExprLit:
		if lit, ok := tc.builder.Exprs.Literal(id); ok && lit != nil {
			ty = tc.literalType(lit.Kind)
		}
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(id); ok && group != nil {
			ty = tc.typeExpr(group.Inner)
		}
	case ast.ExprUnary:
		if data, ok := tc.builder.Exprs.Unary(id); ok && data != nil {
			ty = tc.typeUnary(expr.Span, data)
		}
	case ast.ExprBinary:
		if data, ok := tc.builder.Exprs.Binary(id); ok && data != nil {
			ty = tc.typeBinary(expr.Span, data)
		}
	case ast.ExprCall:
		if call, ok := tc.builder.Exprs.Call(id); ok && call != nil {
			tc.typeExpr(call.Target)
			for _, arg := range call.Args {
				tc.typeExpr(arg)
			}
		}
	case ast.ExprArray:
		if arr, ok := tc.builder.Exprs.Array(id); ok && arr != nil {
			for _, elem := range arr.Elements {
				tc.typeExpr(elem)
			}
		}
	case ast.ExprTuple:
		if tuple, ok := tc.builder.Exprs.Tuple(id); ok && tuple != nil {
			for _, elem := range tuple.Elements {
				tc.typeExpr(elem)
			}
		}
	case ast.ExprIndex:
		if idx, ok := tc.builder.Exprs.Index(id); ok && idx != nil {
			tc.typeExpr(idx.Target)
			tc.typeExpr(idx.Index)
		}
	case ast.ExprMember:
		if member, ok := tc.builder.Exprs.Member(id); ok && member != nil {
			tc.typeExpr(member.Target)
		}
	case ast.ExprAwait:
		if awaitData, ok := tc.builder.Exprs.Await(id); ok && awaitData != nil {
			ty = tc.typeExpr(awaitData.Value)
		}
	case ast.ExprCast:
		if cast, ok := tc.builder.Exprs.Cast(id); ok && cast != nil {
			ty = tc.typeExpr(cast.Value)
		}
	case ast.ExprCompare:
		if cmp, ok := tc.builder.Exprs.Compare(id); ok && cmp != nil {
			tc.typeExpr(cmp.Value)
			for _, arm := range cmp.Arms {
				tc.typeExpr(arm.Pattern)
				tc.typeExpr(arm.Guard)
				tc.typeExpr(arm.Result)
			}
		}
	case ast.ExprParallel:
		if par, ok := tc.builder.Exprs.Parallel(id); ok && par != nil {
			tc.typeExpr(par.Iterable)
			tc.typeExpr(par.Init)
			for _, arg := range par.Args {
				tc.typeExpr(arg)
			}
			tc.typeExpr(par.Body)
		}
	case ast.ExprSpawn:
		if spawn, ok := tc.builder.Exprs.Spawn(id); ok && spawn != nil {
			ty = tc.typeExpr(spawn.Value)
		}
	case ast.ExprSpread:
		if spread, ok := tc.builder.Exprs.Spread(id); ok && spread != nil {
			tc.typeExpr(spread.Value)
		}
	default:
		// ExprIdent and other unhandled kinds default to unknown.
	}
	tc.result.ExprTypes[id] = ty
	return ty
}

func (tc *typeChecker) literalType(kind ast.ExprLitKind) types.TypeID {
	b := tc.types.Builtins()
	switch kind {
	case ast.ExprLitInt:
		return b.Int
	case ast.ExprLitUint:
		return b.Uint
	case ast.ExprLitFloat:
		return b.Float
	case ast.ExprLitString:
		return b.String
	case ast.ExprLitTrue, ast.ExprLitFalse:
		return b.Bool
	case ast.ExprLitNothing:
		return b.Nothing
	default:
		return types.NoTypeID
	}
}

func (tc *typeChecker) typeUnary(span source.Span, data *ast.ExprUnaryData) types.TypeID {
	operandType := tc.typeExpr(data.Operand)
	spec, ok := types.UnarySpecFor(data.Op)
	if !ok {
		return operandType
	}
	if operandType == types.NoTypeID {
		return types.NoTypeID
	}
	family := tc.familyOf(operandType)
	if !tc.familyMatches(family, spec.Operand) {
		tc.report(diag.SemaInvalidUnaryOperand, span, "operator %s cannot be applied to %s", tc.unaryOpLabel(data.Op), tc.typeLabel(operandType))
		return types.NoTypeID
	}
	switch spec.Result {
	case types.UnaryResultNumeric, types.UnaryResultSame:
		return operandType
	case types.UnaryResultBool:
		return tc.types.Builtins().Bool
	case types.UnaryResultReference:
		if operandType == types.NoTypeID {
			return types.NoTypeID
		}
		mutable := data.Op == ast.ExprUnaryRefMut
		return tc.types.Intern(types.MakeReference(operandType, mutable))
	case types.UnaryResultDeref:
		elem, ok := tc.elementType(operandType)
		if !ok {
			tc.report(diag.SemaInvalidUnaryOperand, span, "cannot dereference %s", tc.typeLabel(operandType))
			return types.NoTypeID
		}
		return elem
	case types.UnaryResultAwait:
		return operandType
	default:
		return types.NoTypeID
	}
}

func (tc *typeChecker) typeBinary(span source.Span, data *ast.ExprBinaryData) types.TypeID {
	leftType := tc.typeExpr(data.Left)
	rightType := tc.typeExpr(data.Right)
	specs := types.BinarySpecs(data.Op)
	if len(specs) == 0 || leftType == types.NoTypeID || rightType == types.NoTypeID {
		return types.NoTypeID
	}
	leftFamily := tc.familyOf(leftType)
	rightFamily := tc.familyOf(rightType)
	for _, spec := range specs {
		if !tc.familyMatches(leftFamily, spec.Left) || !tc.familyMatches(rightFamily, spec.Right) {
			continue
		}
		if spec.Flags&types.BinaryFlagSameFamily != 0 && leftFamily != rightFamily {
			continue
		}
		switch spec.Result {
		case types.BinaryResultLeft:
			return leftType
		case types.BinaryResultRight:
			return rightType
		case types.BinaryResultBool:
			return tc.types.Builtins().Bool
		case types.BinaryResultNumeric:
			return tc.pickNumericResult(leftType, rightType)
		case types.BinaryResultRange:
			return types.NoTypeID
		default:
			return types.NoTypeID
		}
	}
	tc.report(diag.SemaInvalidBinaryOperands, span, "operator %s is not defined for %s and %s", tc.binaryOpLabel(data.Op), tc.typeLabel(leftType), tc.typeLabel(rightType))
	return types.NoTypeID
}

func (tc *typeChecker) pickNumericResult(left, right types.TypeID) types.TypeID {
	if left != types.NoTypeID {
		return left
	}
	if right != types.NoTypeID {
		return right
	}
	return tc.types.Builtins().Int
}

func (tc *typeChecker) elementType(id types.TypeID) (types.TypeID, bool) {
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return types.NoTypeID, false
	}
	switch tt.Kind {
	case types.KindPointer, types.KindReference, types.KindOwn, types.KindArray:
		return tt.Elem, true
	default:
		return types.NoTypeID, false
	}
}

func (tc *typeChecker) familyOf(id types.TypeID) types.FamilyMask {
	if id == types.NoTypeID || tc.types == nil {
		return types.FamilyNone
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return types.FamilyNone
	}
	switch tt.Kind {
	case types.KindBool:
		return types.FamilyBool
	case types.KindInt:
		return types.FamilySignedInt
	case types.KindUint:
		return types.FamilyUnsignedInt
	case types.KindFloat:
		return types.FamilyFloat
	case types.KindString:
		return types.FamilyString
	case types.KindArray:
		return types.FamilyArray
	case types.KindPointer:
		return types.FamilyPointer
	case types.KindReference:
		return types.FamilyReference
	default:
		return types.FamilyAny
	}
}

func (tc *typeChecker) familyMatches(actual, expected types.FamilyMask) bool {
	if expected == types.FamilyAny {
		return actual != types.FamilyNone
	}
	return actual&expected != 0
}

func (tc *typeChecker) typeLabel(id types.TypeID) string {
	if id == types.NoTypeID || tc.types == nil {
		return "unknown"
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return "unknown"
	}
	switch tt.Kind {
	case types.KindBool:
		return "bool"
	case types.KindInt:
		return "int"
	case types.KindUint:
		return "uint"
	case types.KindFloat:
		return "float"
	case types.KindString:
		return "string"
	case types.KindNothing:
		return "nothing"
	case types.KindUnit:
		return "unit"
	case types.KindReference:
		prefix := "&"
		if tt.Mutable {
			prefix = "&mut "
		}
		return prefix + tc.typeLabel(tt.Elem)
	case types.KindPointer:
		return fmt.Sprintf("*%s", tc.typeLabel(tt.Elem))
	case types.KindArray:
		return fmt.Sprintf("[%s]", tc.typeLabel(tt.Elem))
	case types.KindOwn:
		return fmt.Sprintf("own %s", tc.typeLabel(tt.Elem))
	default:
		return tt.Kind.String()
	}
}

func (tc *typeChecker) report(code diag.Code, span source.Span, format string, args ...interface{}) {
	if tc.reporter == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if b := diag.ReportError(tc.reporter, code, span, msg); b != nil {
		b.Emit()
	}
}

func (tc *typeChecker) binaryOpLabel(op ast.ExprBinaryOp) string {
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
	case ast.ExprBinaryLogicalAnd:
		return "&&"
	case ast.ExprBinaryLogicalOr:
		return "||"
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
	default:
		return fmt.Sprintf("op#%d", op)
	}
}

func (tc *typeChecker) unaryOpLabel(op ast.ExprUnaryOp) string {
	switch op {
	case ast.ExprUnaryPlus:
		return "+"
	case ast.ExprUnaryMinus:
		return "-"
	case ast.ExprUnaryNot:
		return "!"
	case ast.ExprUnaryDeref:
		return "*"
	case ast.ExprUnaryRef:
		return "&"
	case ast.ExprUnaryRefMut:
		return "&mut"
	default:
		return fmt.Sprintf("op#%d", op)
	}
}
