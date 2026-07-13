package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// CrossingLoweringKind identifies the source crossing form that a future
// transport/lowering backend must handle.
type CrossingLoweringKind uint8

const (
	// CrossingLoweringOnPlacement records `on placement { ... }`.
	CrossingLoweringOnPlacement CrossingLoweringKind = iota
	// CrossingLoweringOnFarHandle records `on far_handle { ... }`.
	CrossingLoweringOnFarHandle
	// CrossingLoweringSpawnOn records `spawn on placement { ... }`.
	CrossingLoweringSpawnOn
	// CrossingLoweringFarTaskAwait records `far Task<T>.await()`.
	CrossingLoweringFarTaskAwait
	// CrossingLoweringFarTaskCancel records `far Task<T>.cancel()`.
	CrossingLoweringFarTaskCancel
	// CrossingLoweringChannelCreate records a `channel_on(dst, cap)` producer
	// call: create a channel owned by the destination shard and return the
	// remote handle.
	CrossingLoweringChannelCreate
	// CrossingLoweringChannelShare records `far Channel<T>.share()` minting
	// a sibling lease on the same channel.
	CrossingLoweringChannelShare
	// CrossingLoweringChannelSelect records a `select` whose arms are far
	// channel operations: the whole select ships to the arms' owner shard as
	// one anchored block and replies with the winner index.
	CrossingLoweringChannelSelect
)

// CrossingDestinationKind classifies a checked crossing destination.
type CrossingDestinationKind uint8

const (
	// CrossingDestinationNone means the destination failed to resolve.
	CrossingDestinationNone CrossingDestinationKind = iota
	// CrossingDestinationPlacement means the destination is a Placement value.
	CrossingDestinationPlacement
	// CrossingDestinationFarHandle means the destination is an owner-anchored far handle.
	CrossingDestinationFarHandle
)

// CrossingCaptureMode records how an accepted capture crosses the boundary.
type CrossingCaptureMode uint8

const (
	// CrossingCaptureCopy records a copy capture.
	CrossingCaptureCopy CrossingCaptureMode = iota
	// CrossingCaptureMoveOwned records an owned value move.
	CrossingCaptureMoveOwned
	// CrossingCaptureMoveFarHandle records a far-handle move.
	CrossingCaptureMoveFarHandle
)

// CrossingCaptureVerdict records the exact sema rule that accepted a capture.
type CrossingCaptureVerdict uint8

const (
	// CrossingCaptureCopyValue records a regular Copy capture verdict.
	CrossingCaptureCopyValue CrossingCaptureVerdict = iota
	// CrossingCapturePlacementCopy records a Placement copy verdict.
	CrossingCapturePlacementCopy
	// CrossingCaptureFarHandle records an affine far-handle capture verdict.
	CrossingCaptureFarHandle
	// CrossingCaptureOwnedShardMovable records an owned @shard_movable verdict.
	CrossingCaptureOwnedShardMovable
	// CrossingCaptureOwnedBuiltinCopy records an owned builtin Copy verdict.
	CrossingCaptureOwnedBuiltinCopy
)

// CrossingDestinationInfo is the sema-checked destination for an `on` or
// `spawn on` crossing.
type CrossingDestinationInfo struct {
	Kind          CrossingDestinationKind
	Expr          ast.ExprID
	Type          types.TypeID
	AnchorSymbol  symbols.SymbolID
	OwnerAnchored bool
}

// CrossingCaptureInfo is an accepted cross-boundary capture and its movement
// judgment. Rejected captures are reported as diagnostics and are not recorded.
type CrossingCaptureInfo struct {
	Symbol symbols.SymbolID
	// Name is the capture's source-level binding name, recorded at check
	// time so later pipeline stages can diagnose captures without a
	// symbol-table round trip.
	Name    string
	Expr    ast.ExprID
	Span    source.Span
	Type    types.TypeID
	Mode    CrossingCaptureMode
	Verdict CrossingCaptureVerdict
}

// CrossingRemoteOpInfo records an accepted operation through an anchored
// `far T` handle inside `on far_handle { ... }`, or one arm of a remote
// select (where ValueExpr carries a send arm's payload expression).
type CrossingRemoteOpInfo struct {
	Method         string
	Span           source.Span
	ReceiverExpr   ast.ExprID
	ReceiverSymbol symbols.SymbolID
	ReceiverType   types.TypeID
	ValueExpr      ast.ExprID
}

// CrossingLoweringInfo is the guard-before-HIR handoff record for accepted
// crossing forms. It deliberately stays at sema/source level until the Phase 4
// transport design introduces real lowering nodes.
type CrossingLoweringInfo struct {
	Kind     CrossingLoweringKind
	Expr     ast.ExprID
	Span     source.Span
	Body     ast.StmtID
	Function symbols.SymbolID
	// SuspendCapable is true when lowering occurs inside an async function or
	// async block. The first executable transport vertical may suspend
	// only at those sites; synchronous forms remain backend-guarded.
	SuspendCapable bool

	Destination CrossingDestinationInfo
	Captures    []CrossingCaptureInfo
	RemoteOps   []CrossingRemoteOpInfo

	ReceiverExpr   ast.ExprID
	ReceiverSymbol symbols.SymbolID
	ReceiverType   types.TypeID
	ConsumesHandle bool

	PayloadType types.TypeID
	ResultType  types.TypeID
	HandleType  types.TypeID
}

func (tc *typeChecker) recordCrossingLowering(info *CrossingLoweringInfo) {
	if tc == nil || tc.result == nil || info == nil {
		return
	}
	tc.result.CrossingLowering = append(tc.result.CrossingLowering, *info)
}

func cloneCrossingCaptures(in []CrossingCaptureInfo) []CrossingCaptureInfo {
	if len(in) == 0 {
		return nil
	}
	return append([]CrossingCaptureInfo(nil), in...)
}

func cloneCrossingRemoteOps(in []CrossingRemoteOpInfo) []CrossingRemoteOpInfo {
	if len(in) == 0 {
		return nil
	}
	return append([]CrossingRemoteOpInfo(nil), in...)
}
