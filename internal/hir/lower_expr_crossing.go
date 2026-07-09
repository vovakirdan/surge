package hir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/types"
)

func (l *lowerer) crossingInfo(exprID ast.ExprID) (*sema.CrossingLoweringInfo, bool) {
	if l == nil || l.crossingByExpr == nil || !exprID.IsValid() {
		return nil, false
	}
	info, ok := l.crossingByExpr[exprID]
	return info, ok
}

func crossingConstructName(kind sema.CrossingLoweringKind) string {
	switch kind {
	case sema.CrossingLoweringOnPlacement:
		return "`on`"
	case sema.CrossingLoweringOnFarHandle:
		return "`on far_handle`"
	case sema.CrossingLoweringSpawnOn:
		return "`spawn on`"
	case sema.CrossingLoweringFarTaskAwait:
		return "`far Task<T>.await()`"
	case sema.CrossingLoweringFarTaskCancel:
		return "`far Task<T>.cancel()`"
	default:
		return fmt.Sprintf("crossing kind %d", kind)
	}
}

func (l *lowerer) lowerCrossingExpr(exprID ast.ExprID, info *sema.CrossingLoweringInfo) *Expr {
	if l == nil || info == nil {
		return nil
	}
	if !l.opts.crossingEnabled(info.Kind) {
		l.setErrorf("internal compiler error: %s crossing reached HIR lowering without explicit lowering capability; backend-unavailable guard should have stopped production compile", crossingConstructName(info.Kind))
		return nil
	}

	data := CrossingData{
		Kind: info.Kind,
		Destination: CrossingDestination{
			Kind:          info.Destination.Kind,
			Type:          info.Destination.Type,
			AnchorSymbol:  info.Destination.AnchorSymbol,
			OwnerAnchored: info.Destination.OwnerAnchored,
		},
		ReceiverSymbol: info.ReceiverSymbol,
		ReceiverType:   info.ReceiverType,
		ConsumesHandle: info.ConsumesHandle,
		PayloadType:    info.PayloadType,
		ResultType:     info.ResultType,
		HandleType:     info.HandleType,
	}
	if info.Destination.Expr.IsValid() {
		data.Destination.Value = l.lowerExpr(info.Destination.Expr)
	}
	if info.Body.IsValid() {
		data.Body = l.lowerBlockOrWrap(info.Body)
	}
	if info.ReceiverExpr.IsValid() {
		data.Receiver = l.lowerExpr(info.ReceiverExpr)
	}
	data.Captures = l.lowerCrossingCaptures(info.Captures)
	data.RemoteOps = l.lowerCrossingRemoteOps(info.RemoteOps)

	span := info.Span
	ty := info.ResultType
	if ty == types.NoTypeID {
		if l.semaRes != nil {
			ty = l.semaRes.ExprTypes[exprID]
		}
	}
	return &Expr{
		Kind: ExprCrossing,
		Type: ty,
		Span: span,
		Data: data,
	}
}

func (l *lowerer) lowerCrossingCaptures(in []sema.CrossingCaptureInfo) []CrossingCapture {
	if len(in) == 0 {
		return nil
	}
	out := make([]CrossingCapture, 0, len(in))
	for _, cap := range in {
		out = append(out, CrossingCapture{
			Symbol:  cap.Symbol,
			Value:   l.varRefForSymbol(cap.Symbol, cap.Span),
			Span:    cap.Span,
			Type:    cap.Type,
			Mode:    cap.Mode,
			Verdict: cap.Verdict,
		})
	}
	return out
}

func (l *lowerer) lowerCrossingRemoteOps(in []sema.CrossingRemoteOpInfo) []CrossingRemoteOp {
	if len(in) == 0 {
		return nil
	}
	out := make([]CrossingRemoteOp, 0, len(in))
	for _, op := range in {
		out = append(out, CrossingRemoteOp{
			Method:         op.Method,
			Span:           op.Span,
			Receiver:       l.lowerExpr(op.ReceiverExpr),
			ReceiverSymbol: op.ReceiverSymbol,
			ReceiverType:   op.ReceiverType,
		})
	}
	return out
}
