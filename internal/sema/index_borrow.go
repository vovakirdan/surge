package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// typeExplicitBorrow types `&operand` / `&mut operand`. An explicit borrow can
// escape the statement (into a binding, field, or argument that stores it), so
// its operand must not drop at statement end: leak over dangle.
func (tc *typeChecker) typeExplicitBorrow(exprID ast.ExprID, span source.Span, data *ast.ExprUnaryData, operandType types.TypeID) types.TypeID {
	tc.consumeTempCandidate(data.Operand)
	if operandType == types.NoTypeID {
		// Preserve the existing place/borrow recovery even when typing the
		// operand failed; later statements may still need conflict diagnostics.
		tc.handleBorrow(exprID, span, data.Op, data.Operand)
		return types.NoTypeID
	}
	mutable := data.Op == ast.ExprUnaryRefMut
	indexInfo, isIndex := tc.indexBorrowInfo(data.Operand, operandType)
	if isIndex {
		if !indexInfo.hasReferenceCarrier {
			tc.reportBorrowNonAddressable(data.Operand, mutable)
			return types.NoTypeID
		}
		if mutable && !indexInfo.physicalArray && !indexInfo.carrierMutable {
			tc.reportSharedIndexMutableBorrow(span, indexInfo.expr, operandType)
			// Recover with the requested shape so binding checks do not add a
			// misleading "temporary value" diagnostic. The emitted SEM3022
			// still stops every normal backend before lowering.
			return tc.types.Intern(types.MakeReference(indexInfo.elem, true))
		}
	}
	tc.handleBorrow(exprID, span, data.Op, data.Operand)
	if isIndex {
		return tc.types.Intern(types.MakeReference(indexInfo.elem, mutable))
	}
	return tc.types.Intern(types.MakeReference(operandType, mutable))
}

type indexBorrowDetails struct {
	expr                ast.ExprID
	elem                types.TypeID
	carrierMutable      bool
	hasReferenceCarrier bool
	physicalArray       bool
}

// indexBorrowInfo describes the selected `__index` carrier under an explicit
// borrow. At the language surface `a[i]` is an element place when `__index`
// returns a reference, so `&a[i]` reborrows its pointee rather than creating
// `&&T`. Only the built-in Array/ArrayFixed storage may bypass that callable
// later and lower as a physical projection.
func (tc *typeChecker) indexBorrowInfo(exprID ast.ExprID, operandType types.TypeID) (indexBorrowDetails, bool) {
	for exprID.IsValid() {
		expr := tc.builder.Exprs.Get(exprID)
		if expr == nil {
			return indexBorrowDetails{}, false
		}
		if expr.Kind != ast.ExprGroup {
			if expr.Kind != ast.ExprIndex || tc.types == nil {
				return indexBorrowDetails{}, false
			}
			info := indexBorrowDetails{expr: exprID}
			selectedCallable := false
			if index, ok := tc.builder.Exprs.Index(exprID); ok && index != nil && tc.result != nil {
				targetType := tc.resolveAlias(tc.result.ExprTypes[index.Target])
				if target, found := tc.types.Lookup(targetType); found && target.Kind == types.KindReference {
					targetType = target.Elem
				}
				_, dynamicArray := tc.types.ArrayInfo(targetType)
				_, _, fixedArray := tc.types.ArrayFixedInfo(targetType)
				info.physicalArray = dynamicArray || fixedArray || tc.isArrayType(targetType)
				if symID, found := tc.result.IndexSymbols[exprID]; found && symID.IsValid() {
					selectedCallable = true
					sym := tc.symbolFromID(symID)
					intrinsicArrayIndex := sym != nil && sym.Flags&symbols.SymbolFlagBuiltin != 0 &&
						(sym.Signature == nil || !sym.Signature.HasBody)
					info.physicalArray = info.physicalArray && intrinsicArrayIndex
				}
			}
			tt, ok := tc.types.Lookup(tc.resolveAlias(operandType))
			if ok && tt.Kind == types.KindReference {
				info.elem = tt.Elem
				info.carrierMutable = tt.Mutable
				info.hasReferenceCarrier = true
			}
			if info.physicalArray && !info.hasReferenceCarrier {
				// Direct fixed-array types in no-stdlib/compiler-unit contexts
				// expose their element value, but the syntax is still a physical
				// place whose address may be taken.
				info.elem = operandType
				info.hasReferenceCarrier = true
			}
			if !info.physicalArray && !selectedCallable {
				// Preserve legacy/raw index handling when sema did not select a
				// callable (including recovery from an already-invalid target).
				return indexBorrowDetails{}, false
			}
			return info, true
		}
		group, ok := tc.builder.Exprs.Group(exprID)
		if !ok || group == nil {
			return indexBorrowDetails{}, false
		}
		exprID = group.Inner
	}
	return indexBorrowDetails{}, false
}

func (tc *typeChecker) reportSharedIndexMutableBorrow(span source.Span, indexExpr ast.ExprID, carrierType types.TypeID) {
	message := fmt.Sprintf("cannot take a mutable reference through indexed access: selected `__index` returns shared %s", tc.typeLabel(carrierType))
	if tc.reporter == nil {
		tc.report(diag.SemaBorrowImmutable, span, "%s", message)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaBorrowImmutable, span, message)
	if b == nil {
		return
	}
	b.WithNote(tc.exprSpan(indexExpr), "the selected `__index` result is read-only; use a mutable accessor or an `__index` implementation that returns `&mut T`")
	b.Emit()
}
