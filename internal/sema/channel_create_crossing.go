package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// maybeRecordChannelCreateCrossing records the crossing-lowering info for a
// `channel_on(dst, capacity)` producer call. The recognition is deliberately
// strict: the callee must be the core-module intrinsic (a user function that
// happens to share the name never records a crossing), and the result must
// have resolved to `far Channel<T>`.
func (tc *typeChecker) maybeRecordChannelCreateCrossing(
	id ast.ExprID,
	call *ast.ExprCallData,
	resultType types.TypeID,
	span source.Span,
) {
	if tc == nil || call == nil || resultType == types.NoTypeID {
		return
	}
	ident, ok := tc.builder.Exprs.Ident(call.Target)
	if !ok || ident == nil || tc.lookupName(ident.Name) != "channel_on" {
		return
	}
	sym := tc.symbolFromID(tc.symbolForExpr(call.Target))
	if !isCoreIntrinsicFunction(sym) {
		return
	}
	if !tc.isFarType(resultType) {
		return
	}
	inner := tc.farInner(resultType)
	if !tc.isChannelType(inner) {
		return
	}
	if len(call.Args) < 1 {
		return
	}
	dest := call.Args[0].Value
	destType := types.NoTypeID
	if tc.result != nil {
		destType = tc.result.ExprTypes[dest]
	}
	if !tc.isPlacementType(destType) {
		tc.report(diag.SemaOnDestNotPlacement, tc.exprSpan(dest),
			"`channel_on` destination must be a `Placement` value")
		return
	}
	capacity := ast.NoExprID
	capacityType := types.NoTypeID
	if len(call.Args) >= 2 {
		capacity = call.Args[1].Value
		if tc.result != nil {
			capacityType = tc.result.ExprTypes[capacity]
		}
	}
	tc.markCurrentFunctionMayCross()
	tc.recordCrossingLowering(&CrossingLoweringInfo{
		Kind:     CrossingLoweringChannelCreate,
		Expr:     id,
		Span:     span,
		Function: tc.currentFnSym(),
		// The receiver slot carries the capacity operand: the producer has
		// no method receiver, and the slot's lowering plumbing (HIR value,
		// MIR operand, liveness, validation) is exactly what a scalar
		// argument needs.
		ReceiverExpr: capacity,
		ReceiverType: capacityType,
		// The producer suspends on its reply like every crossing; the
		// synchronous-context cause gets its own sema diagnostic in the
		// crossing-family precision pass.
		SuspendCapable: tc.awaitDepth > 0,
		Destination: CrossingDestinationInfo{
			Expr: dest,
			Kind: CrossingDestinationPlacement,
			Type: destType,
		},
		PayloadType: tc.channelPayloadType(inner),
		ResultType:  resultType,
	})
}

// channelPayloadType extracts T from Channel<T>.
func (tc *typeChecker) channelPayloadType(channelType types.TypeID) types.TypeID {
	if channelType == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	resolved := tc.valueType(channelType)
	if resolved == types.NoTypeID {
		return types.NoTypeID
	}
	if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
		if tc.lookupTypeName(resolved, info.Name) == "Channel" {
			args := tc.types.StructArgs(resolved)
			if len(args) == 1 {
				return args[0]
			}
		}
	}
	if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
		if tc.lookupTypeName(resolved, info.Name) == "Channel" && len(info.TypeArgs) == 1 {
			return info.TypeArgs[0]
		}
	}
	return types.NoTypeID
}

// isCoreIntrinsicFunction mirrors the backend's core-runtime-symbol
// discriminator: a builtin-flagged function from the core module (or a
// module-less builtin, which is how single-file checks declare intrinsics).
func isCoreIntrinsicFunction(sym *symbols.Symbol) bool {
	if sym == nil || sym.Kind != symbols.SymbolFunction ||
		sym.Flags&symbols.SymbolFlagBuiltin == 0 {
		return false
	}
	return sym.ModulePath == "" || isCoreModulePath(strings.Trim(sym.ModulePath, "/"))
}
