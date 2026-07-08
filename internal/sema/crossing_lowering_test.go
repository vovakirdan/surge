package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func checkCrossingLowering(t *testing.T, src string) (*Result, *symbols.Result) {
	t.Helper()
	res, symRes, semaBag := checkCrossingLoweringDiagnostics(t, src)
	if semaBag.HasErrors() {
		t.Fatalf("sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
	return res, symRes
}

func checkCrossingLoweringDiagnostics(t *testing.T, src string) (*Result, *symbols.Result, *diag.Bag) {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, onCrossingPrelude+src)
	if parseBag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(128)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	return &res, symRes, semaBag
}

func requireCrossingLowering(t *testing.T, res *Result, kind CrossingLoweringKind) CrossingLoweringInfo {
	t.Helper()
	for _, info := range res.CrossingLowering {
		if info.Kind == kind {
			return info
		}
	}
	t.Fatalf("missing crossing lowering record kind %d; have %d records", kind, len(res.CrossingLowering))
	return CrossingLoweringInfo{}
}

func requireNoCrossingLowering(t *testing.T, res *Result, fn symbols.SymbolID) {
	t.Helper()
	for _, info := range res.CrossingLowering {
		if info.Function == fn {
			t.Fatalf("unexpected lowering record for function symbol %d: kind %d", fn, info.Kind)
		}
	}
}

func typeLabelForTest(res *Result, id types.TypeID) string {
	return types.Label(res.TypeInterner, id)
}

func symbolNameForTest(t *testing.T, symRes *symbols.Result, id symbols.SymbolID) string {
	t.Helper()
	if symRes == nil || symRes.Table == nil || symRes.Table.Symbols == nil || symRes.Table.Strings == nil {
		t.Fatal("missing symbol table")
	}
	sym := symRes.Table.Symbols.Get(id)
	if sym == nil {
		t.Fatalf("missing symbol %d", id)
	}
	name, ok := symRes.Table.Strings.Lookup(sym.Name)
	if !ok {
		t.Fatalf("missing symbol name for %d", id)
	}
	return name
}

func TestCrossingLoweringOnPlacementRecord(t *testing.T) {
	res, symRes := checkCrossingLowering(t, `
fn run(dst: Placement, n: int) -> TaskResult<int> {
	return on dst {
		ret n;
	};
}
`)
	if len(res.CrossingLowering) != 1 {
		t.Fatalf("record count = %d, want 1", len(res.CrossingLowering))
	}
	info := requireCrossingLowering(t, res, CrossingLoweringOnPlacement)
	if !info.Expr.IsValid() || info.Span.Empty() || !info.Body.IsValid() {
		t.Fatalf("missing site coordinates: expr=%d span=%s body=%d", info.Expr, info.Span, info.Body)
	}
	if got := symbolNameForTest(t, symRes, info.Function); got != "run" {
		t.Fatalf("function = %q, want run", got)
	}
	if !res.FunctionEffects[info.Function].MayCross {
		t.Fatalf("function backref does not point at MayCross effect")
	}
	if info.Destination.Kind != CrossingDestinationPlacement {
		t.Fatalf("destination kind = %d, want placement", info.Destination.Kind)
	}
	if !info.Destination.Expr.IsValid() {
		t.Fatalf("destination expr is not recorded")
	}
	if got := typeLabelForTest(res, info.Destination.Type); got != "Placement" {
		t.Fatalf("destination type = %q, want Placement", got)
	}
	if got := typeLabelForTest(res, info.PayloadType); got != "int" {
		t.Fatalf("payload type = %q, want int", got)
	}
	if got := typeLabelForTest(res, info.ResultType); got != "TaskResult<int>" {
		t.Fatalf("result type = %q, want TaskResult<int>", got)
	}
	if len(info.Captures) != 1 {
		t.Fatalf("captures = %d, want 1", len(info.Captures))
	}
	cap := info.Captures[0]
	if got := symbolNameForTest(t, symRes, cap.Symbol); got != "n" {
		t.Fatalf("capture symbol = %q, want n", got)
	}
	if got := typeLabelForTest(res, cap.Type); got != "int" {
		t.Fatalf("capture type = %q, want int", got)
	}
	if !cap.Expr.IsValid() || cap.Span.Empty() {
		t.Fatalf("capture coordinates missing: expr=%d span=%s", cap.Expr, cap.Span)
	}
	if cap.Mode != CrossingCaptureCopy || cap.Verdict != CrossingCaptureCopyValue {
		t.Fatalf("capture mode/verdict = %d/%d, want copy/copy-value", cap.Mode, cap.Verdict)
	}
}

func TestCrossingLoweringOnFarHandleRecord(t *testing.T) {
	res, symRes := checkCrossingLowering(t, `
fn run(conn: far TcpConn) -> TaskResult<nothing> {
	return on conn {
		conn.close();
		ret nothing;
	};
}
`)
	info := requireCrossingLowering(t, res, CrossingLoweringOnFarHandle)
	if !info.Expr.IsValid() || info.Span.Empty() || !info.Body.IsValid() {
		t.Fatalf("missing site coordinates: expr=%d span=%s body=%d", info.Expr, info.Span, info.Body)
	}
	if info.Destination.Kind != CrossingDestinationFarHandle {
		t.Fatalf("destination kind = %d, want far handle", info.Destination.Kind)
	}
	if !info.Destination.Expr.IsValid() {
		t.Fatalf("destination expr is not recorded")
	}
	if got := typeLabelForTest(res, info.Destination.Type); got != "far TcpConn" {
		t.Fatalf("destination type = %q, want far TcpConn", got)
	}
	if got := symbolNameForTest(t, symRes, info.Destination.AnchorSymbol); got != "conn" {
		t.Fatalf("anchor symbol = %q, want conn", got)
	}
	if !info.Destination.OwnerAnchored {
		t.Fatalf("expected owner-anchored far-handle destination")
	}
	if len(info.RemoteOps) != 1 {
		t.Fatalf("remote ops = %d, want 1", len(info.RemoteOps))
	}
	op := info.RemoteOps[0]
	if op.Method != "close" {
		t.Fatalf("remote op method = %q, want close", op.Method)
	}
	if !op.ReceiverExpr.IsValid() || op.Span.Empty() {
		t.Fatalf("remote op coordinates missing: receiver=%d span=%s", op.ReceiverExpr, op.Span)
	}
	if got := symbolNameForTest(t, symRes, op.ReceiverSymbol); got != "conn" {
		t.Fatalf("remote op receiver = %q, want conn", got)
	}
	if got := typeLabelForTest(res, op.ReceiverType); got != "far TcpConn" {
		t.Fatalf("remote op receiver type = %q, want far TcpConn", got)
	}
	if got := typeLabelForTest(res, info.ResultType); got != "TaskResult<nothing>" {
		t.Fatalf("result type = %q, want TaskResult<nothing>", got)
	}
}

func TestCrossingLoweringSpawnOnRecord(t *testing.T) {
	res, symRes := checkCrossingLowering(t, `
fn use(m: own Movable) -> int {
	return m.id;
}

fn run(dst: Placement, m: own Movable) -> far Task<int> {
	return spawn on dst {
		ret use(own m);
	};
}
`)
	info := requireCrossingLowering(t, res, CrossingLoweringSpawnOn)
	if !info.Expr.IsValid() || info.Span.Empty() || !info.Body.IsValid() {
		t.Fatalf("missing site coordinates: expr=%d span=%s body=%d", info.Expr, info.Span, info.Body)
	}
	if info.Destination.Kind != CrossingDestinationPlacement {
		t.Fatalf("destination kind = %d, want placement", info.Destination.Kind)
	}
	if !info.Destination.Expr.IsValid() {
		t.Fatalf("destination expr is not recorded")
	}
	if got := typeLabelForTest(res, info.Destination.Type); got != "Placement" {
		t.Fatalf("destination type = %q, want Placement", got)
	}
	if got := typeLabelForTest(res, info.PayloadType); got != "int" {
		t.Fatalf("payload type = %q, want int", got)
	}
	if got := typeLabelForTest(res, info.ResultType); got != "far Task<int>" {
		t.Fatalf("result type = %q, want far Task<int>", got)
	}
	if got := typeLabelForTest(res, info.HandleType); got != "far Task<int>" {
		t.Fatalf("handle type = %q, want far Task<int>", got)
	}
	if len(info.Captures) != 1 {
		t.Fatalf("captures = %d, want 1", len(info.Captures))
	}
	cap := info.Captures[0]
	if got := symbolNameForTest(t, symRes, cap.Symbol); got != "m" {
		t.Fatalf("capture symbol = %q, want m", got)
	}
	if got := typeLabelForTest(res, cap.Type); got != "own Movable" {
		t.Fatalf("capture type = %q, want own Movable", got)
	}
	if !cap.Expr.IsValid() || cap.Span.Empty() {
		t.Fatalf("capture coordinates missing: expr=%d span=%s", cap.Expr, cap.Span)
	}
	if cap.Mode != CrossingCaptureMoveOwned || cap.Verdict != CrossingCaptureOwnedShardMovable {
		t.Fatalf("capture mode/verdict = %d/%d, want move-owned/shard-movable", cap.Mode, cap.Verdict)
	}
}

func TestCrossingLoweringFarTaskAwaitRecord(t *testing.T) {
	res, symRes := checkCrossingLowering(t, `
fn wait_remote(t: far Task<int>) -> TaskResult<int> {
	return t.await();
}
`)
	info := requireCrossingLowering(t, res, CrossingLoweringFarTaskAwait)
	if !info.Expr.IsValid() || info.Span.Empty() || !info.ReceiverExpr.IsValid() {
		t.Fatalf("missing await coordinates: expr=%d receiver=%d span=%s", info.Expr, info.ReceiverExpr, info.Span)
	}
	if got := symbolNameForTest(t, symRes, info.ReceiverSymbol); got != "t" {
		t.Fatalf("receiver = %q, want t", got)
	}
	if got := typeLabelForTest(res, info.ReceiverType); got != "far Task<int>" {
		t.Fatalf("receiver type = %q, want far Task<int>", got)
	}
	if !info.ConsumesHandle {
		t.Fatalf("await must consume the far task handle")
	}
	if got := typeLabelForTest(res, info.PayloadType); got != "int" {
		t.Fatalf("payload type = %q, want int", got)
	}
	if got := typeLabelForTest(res, info.ResultType); got != "TaskResult<int>" {
		t.Fatalf("result type = %q, want TaskResult<int>", got)
	}
}

func TestCrossingLoweringFarTaskCancelRecord(t *testing.T) {
	res, symRes := checkCrossingLowering(t, `
fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
	return t.cancel();
}
`)
	info := requireCrossingLowering(t, res, CrossingLoweringFarTaskCancel)
	if !info.Expr.IsValid() || info.Span.Empty() || !info.ReceiverExpr.IsValid() {
		t.Fatalf("missing cancel coordinates: expr=%d receiver=%d span=%s", info.Expr, info.ReceiverExpr, info.Span)
	}
	if got := symbolNameForTest(t, symRes, info.ReceiverSymbol); got != "t" {
		t.Fatalf("receiver = %q, want t", got)
	}
	if got := typeLabelForTest(res, info.ReceiverType); got != "far Task<int>" {
		t.Fatalf("receiver type = %q, want far Task<int>", got)
	}
	if !info.ConsumesHandle {
		t.Fatalf("cancel must consume the far task handle")
	}
	if got := typeLabelForTest(res, info.ResultType); got != "TaskResult<nothing>" {
		t.Fatalf("result type = %q, want TaskResult<nothing>", got)
	}
}

func TestCrossingLoweringDirectCallEffectStability(t *testing.T) {
	res, symRes := checkCrossingLowering(t, `
fn direct_on() -> TaskResult<int> {
	return on pool {
		ret 1;
	};
}

fn wrapper() -> TaskResult<int> {
	return direct_on();
}
`)
	direct := requireFunctionSymbolID(t, symRes, "direct_on")
	wrapper := requireFunctionSymbolID(t, symRes, "wrapper")
	if !res.FunctionEffects[direct].MayCross {
		t.Fatalf("direct_on should be MayCross")
	}
	if !res.FunctionEffects[wrapper].MayCross {
		t.Fatalf("wrapper should inherit MayCross through direct call")
	}
	if len(res.CrossingLowering) != 1 {
		t.Fatalf("record count = %d, want exactly the real crossing site", len(res.CrossingLowering))
	}
	if res.CrossingLowering[0].Function != direct {
		t.Fatalf("lowering record belongs to symbol %d, want direct_on %d", res.CrossingLowering[0].Function, direct)
	}
	requireNoCrossingLowering(t, res, wrapper)
}

func TestCrossingLoweringRejectedOnBodyDoesNotRecord(t *testing.T) {
	res, _, bag := checkCrossingLoweringDiagnostics(t, `
fn run(dst: Placement) -> TaskResult<int> {
	return on dst {
		ret 1;
		ret "bad";
	};
}
`)
	if !hasCode(bag, diag.SemaTypeMismatch) {
		t.Fatalf("expected type mismatch, got %s", diagnosticsSummary(bag))
	}
	if len(res.CrossingLowering) != 0 {
		t.Fatalf("rejected on body produced lowering records: %#v", res.CrossingLowering)
	}
}

func TestCrossingLoweringRejectedSpawnOnBodyDoesNotRecord(t *testing.T) {
	res, _, bag := checkCrossingLoweringDiagnostics(t, `
fn run(dst: Placement) -> far Task<int> {
	return spawn on dst {
		ret 1;
		ret "bad";
	};
}
`)
	if !hasCode(bag, diag.SemaTypeMismatch) {
		t.Fatalf("expected type mismatch, got %s", diagnosticsSummary(bag))
	}
	if len(res.CrossingLowering) != 0 {
		t.Fatalf("rejected spawn-on body produced lowering records: %#v", res.CrossingLowering)
	}
}

func TestCrossingLoweringRejectedFarHandleOpDoesNotRecord(t *testing.T) {
	res, _, bag := checkCrossingLoweringDiagnostics(t, `
fn run(conn: far TcpConn) -> TaskResult<nothing> {
	return on conn {
		conn.read();
		ret nothing;
	};
}
`)
	if !hasCode(bag, diag.SemaOnTcpRemoteIO) {
		t.Fatalf("expected remote I/O diagnostic, got %s", diagnosticsSummary(bag))
	}
	if len(res.CrossingLowering) != 0 {
		t.Fatalf("rejected far-handle op produced lowering records: %#v", res.CrossingLowering)
	}
}

func TestCrossingLoweringRejectedFarTaskReuseDoesNotRecordSecondOperation(t *testing.T) {
	res, _, bag := checkCrossingLoweringDiagnostics(t, `
fn wait_twice(t: far Task<int>) -> TaskResult<int> {
	let a: TaskResult<int> = t.await();
	let b: TaskResult<int> = t.await();
	return a;
}
`)
	if !hasCode(bag, diag.SemaUseAfterMove) {
		t.Fatalf("expected use-after-move, got %s", diagnosticsSummary(bag))
	}
	if len(res.CrossingLowering) != 1 {
		t.Fatalf("record count = %d, want only first accepted await", len(res.CrossingLowering))
	}
	if res.CrossingLowering[0].Kind != CrossingLoweringFarTaskAwait {
		t.Fatalf("record kind = %d, want await", res.CrossingLowering[0].Kind)
	}
}

func TestCrossingLoweringRejectedFarTaskAwaitAfterCancelDoesNotRecordSecondOperation(t *testing.T) {
	res, _, bag := checkCrossingLoweringDiagnostics(t, `
fn cancel_then_wait(t: far Task<int>) -> TaskResult<int> {
	t.cancel();
	return t.await();
}
`)
	if !hasCode(bag, diag.SemaUseAfterMove) {
		t.Fatalf("expected use-after-move, got %s", diagnosticsSummary(bag))
	}
	if len(res.CrossingLowering) != 1 {
		t.Fatalf("record count = %d, want only first accepted cancel", len(res.CrossingLowering))
	}
	if res.CrossingLowering[0].Kind != CrossingLoweringFarTaskCancel {
		t.Fatalf("record kind = %d, want cancel", res.CrossingLowering[0].Kind)
	}
}
