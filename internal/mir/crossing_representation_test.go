package mir_test

import (
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/sema"
	"surge/internal/types"
)

func TestMIRCrossingRepresentationWithExplicitCapability(t *testing.T) {
	cases := []struct {
		name          string
		src           string
		kind          sema.CrossingLoweringKind
		wantDest      sema.CrossingDestinationKind
		wantPayload   string
		wantResult    string
		wantHandle    string
		wantCaptures  int
		wantRemoteOps int
		wantReceiver  bool
		wantConsumes  bool
	}{
		{
			name: "on placement",
			src: crossingMIRPrelude + `
fn run(dst: Placement, n: int) -> TaskResult<int> {
    return on dst {
        ret n;
    };
}
`,
			kind:         sema.CrossingLoweringOnPlacement,
			wantDest:     sema.CrossingDestinationPlacement,
			wantPayload:  "int",
			wantResult:   "TaskResult<int>",
			wantCaptures: 1,
		},
		{
			name: "on far handle",
			src: crossingMIRPrelude + `
fn run(conn: far TcpConn) -> TaskResult<nothing> {
    return on conn {
        conn.close();
        ret nothing;
    };
}
`,
			kind:          sema.CrossingLoweringOnFarHandle,
			wantDest:      sema.CrossingDestinationFarHandle,
			wantPayload:   "nothing",
			wantResult:    "TaskResult<nothing>",
			wantCaptures:  1,
			wantRemoteOps: 1,
		},
		{
			name: "spawn on",
			src: crossingMIRPrelude + `
fn use(m: own Movable) -> int {
    return m.id;
}

fn run(dst: Placement, m: own Movable) -> far Task<int> {
    return spawn on dst {
        ret use(own m);
    };
}
`,
			kind:         sema.CrossingLoweringSpawnOn,
			wantDest:     sema.CrossingDestinationPlacement,
			wantPayload:  "int",
			wantResult:   "far Task<int>",
			wantHandle:   "far Task<int>",
			wantCaptures: 1,
		},
		{
			name: "far task await",
			src: crossingMIRPrelude + `
fn wait_remote(t: far Task<int>) -> TaskResult<int> {
    return t.await();
}
`,
			kind:         sema.CrossingLoweringFarTaskAwait,
			wantPayload:  "int",
			wantResult:   "TaskResult<int>",
			wantReceiver: true,
			wantConsumes: true,
		},
		{
			name: "far task cancel",
			src: crossingMIRPrelude + `
fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
    return t.cancel();
}
`,
			kind:         sema.CrossingLoweringFarTaskCancel,
			wantPayload:  "nothing",
			wantResult:   "TaskResult<nothing>",
			wantReceiver: true,
			wantConsumes: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileCrossingMIR(t, tc.src, crossingForms(tc.kind))
			ins := requireMIRCrossing(t, compiled.mod, tc.kind)
			if ins.Dst.Local == mir.NoLocalID {
				t.Fatalf("crossing destination local missing")
			}
			if ins.Destination.Kind != tc.wantDest {
				t.Fatalf("destination kind = %d, want %d", ins.Destination.Kind, tc.wantDest)
			}
			if tc.wantDest != sema.CrossingDestinationNone && ins.Destination.Value.Type == types.NoTypeID {
				t.Fatalf("destination operand type missing")
			}
			if got := types.Label(compiled.types, ins.PayloadType); got != tc.wantPayload {
				t.Fatalf("payload type = %q, want %q", got, tc.wantPayload)
			}
			if got := types.Label(compiled.types, ins.ResultType); got != tc.wantResult {
				t.Fatalf("result type = %q, want %q", got, tc.wantResult)
			}
			if tc.wantHandle != "" {
				if got := types.Label(compiled.types, ins.HandleType); got != tc.wantHandle {
					t.Fatalf("handle type = %q, want %q", got, tc.wantHandle)
				}
			}
			if tc.kind == sema.CrossingLoweringSpawnOn {
				if ins.BodyFuncID == mir.NoFuncID {
					t.Fatal("spawn_on crossing missing synthetic poll function id")
				}
				if ins.Pending.Local == mir.NoLocalID {
					t.Fatal("spawn_on crossing missing persisted pending local")
				}
				// The captures, plus the frame's lifecycle word — which is written
				// only where a frame is actually built. A capture-less crossing
				// is handed a null state, and a word describing storage that does
				// not exist would be the one thing worse than no word at all.
				wantFields := tc.wantCaptures
				if wantFields > 0 {
					wantFields++
				}
				if len(ins.State.Fields) != wantFields {
					t.Fatalf("spawn_on state fields = %d, want %d", len(ins.State.Fields), wantFields)
				}
				pollFn := compiled.mod.Funcs[ins.BodyFuncID]
				if pollFn == nil || !strings.HasPrefix(pollFn.Name, "__spawn_on_block$") || !strings.HasSuffix(pollFn.Name, "$poll") {
					t.Fatalf("spawn_on synthetic poll function name = %v", pollFn)
				}
			}
			if tc.kind == sema.CrossingLoweringFarTaskAwait || tc.kind == sema.CrossingLoweringFarTaskCancel {
				if ins.Pending.Local == mir.NoLocalID {
					t.Fatal("far Task lifecycle crossing missing persisted pending local")
				}
			}
			if len(ins.Captures) != tc.wantCaptures {
				t.Fatalf("captures = %d, want %d", len(ins.Captures), tc.wantCaptures)
			}
			for i := range ins.Captures {
				if ins.Captures[i].Value.Type == types.NoTypeID {
					t.Fatalf("capture %d operand type missing", i)
				}
			}
			if len(ins.RemoteOps) != tc.wantRemoteOps {
				t.Fatalf("remote ops = %d, want %d", len(ins.RemoteOps), tc.wantRemoteOps)
			}
			if (ins.Receiver.Type != types.NoTypeID) != tc.wantReceiver {
				t.Fatalf("receiver present = %v, want %v", ins.Receiver.Type != types.NoTypeID, tc.wantReceiver)
			}
			if ins.ConsumesHandle != tc.wantConsumes {
				t.Fatalf("consumes handle = %v, want %v", ins.ConsumesHandle, tc.wantConsumes)
			}
		})
	}
}

func TestMIRCrossingValidationDefaultClosed(t *testing.T) {
	compiled := compileCrossingMIR(t, crossingMIRPrelude+`
fn run(dst: Placement) -> TaskResult<int> {
    return on dst {
        ret 1;
    };
}
`, crossingForms(sema.CrossingLoweringOnPlacement))

	err := mir.ValidateStructure(compiled.mod, compiled.types)
	if err == nil || !strings.Contains(err.Error(), "crossing on is not enabled") {
		t.Fatalf("expected default-closed crossing validation error, got %v", err)
	}
	if err := mir.ValidateStructureWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
		CrossingForms: crossingForms(sema.CrossingLoweringOnPlacement),
	}); err != nil {
		t.Fatalf("validate with explicit crossing capability: %v", err)
	}
}

func TestMIRAsyncCrossingSuspendRepresentation(t *testing.T) {
	compiled := compileCrossingMIR(t, crossingMIRPrelude+`
async fn run(dst: Placement) -> TaskResult<int> {
    return on dst {
        ret 1;
    };
}
`, crossingForms(sema.CrossingLoweringOnPlacement))

	for _, f := range compiled.mod.Funcs {
		mir.SimplifyCFG(f)
	}
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("async lowering failed: %v", err)
	}
	ins := requireMIRCrossing(t, compiled.mod, sema.CrossingLoweringOnPlacement)
	if ins.ReadyBB == mir.NoBlockID {
		t.Fatalf("async crossing missing ready block")
	}
	if ins.PendBB == mir.NoBlockID {
		t.Fatalf("async crossing missing pending block")
	}
	if ins.BodyFuncID == mir.NoFuncID {
		t.Fatalf("on crossing missing destination poll function")
	}
	if compiled.mod.Funcs[ins.BodyFuncID] == nil {
		t.Fatalf("on crossing poll function %d not materialized", ins.BodyFuncID)
	}
	if ins.Pending.Kind != mir.PlaceLocal {
		t.Fatalf("on crossing missing persisted pending slot")
	}
	if err := mir.ValidateStructureWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
		CrossingForms: crossingForms(sema.CrossingLoweringOnPlacement),
	}); err != nil {
		t.Fatalf("validate async crossing MIR: %v", err)
	}
}

func TestMIRAsyncSpawnOnCrossingKeepsPreSuspendInputsAndRetryState(t *testing.T) {
	compiled := compileCrossingMIR(t, crossingMIRPrelude+`
async fn checkpoint() -> int {
    return 0;
}

async fn run(dst: Placement, n: int) -> far Task<int> {
    let _ = checkpoint().await();
    return spawn on dst {
        ret n;
    };
}
`, crossingForms(sema.CrossingLoweringSpawnOn))

	for _, f := range compiled.mod.Funcs {
		mir.SimplifyCFG(f)
	}
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("async lowering failed: %v", err)
	}
	ins := requireMIRCrossing(t, compiled.mod, sema.CrossingLoweringSpawnOn)
	if ins.ReadyBB == mir.NoBlockID {
		t.Fatalf("async spawn_on crossing missing ready block")
	}
	if ins.PendBB == mir.NoBlockID {
		t.Fatalf("async spawn_on crossing missing pending block")
	}
	if ins.Pending.Local == mir.NoLocalID {
		t.Fatalf("async spawn_on crossing missing pending local")
	}
	if !asyncPayloadHasLabels(compiled.types, "__AsyncPayload$run", "Placement", "int") {
		t.Fatalf("pre-spawn suspend payload must preserve Placement and int inputs before first spawn_on publish")
	}
	if !asyncPayloadHasLabels(compiled.types, "__AsyncPayload$run", "*uint8") {
		t.Fatalf("spawn_on pending payload must preserve rt_remote_spawn_pending* retry state")
	}
	if err := mir.ValidateStructureWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
		CrossingForms: crossingForms(sema.CrossingLoweringSpawnOn),
	}); err != nil {
		t.Fatalf("validate async spawn_on crossing MIR: %v", err)
	}
}

func TestMIRAsyncFarTaskLifecycleCrossingKeepsRetryState(t *testing.T) {
	cases := []struct {
		name     string
		form     sema.CrossingLoweringKind
		funcName string
		src      string
	}{
		{
			name:     "await",
			form:     sema.CrossingLoweringFarTaskAwait,
			funcName: "wait_remote",
			src: crossingMIRPrelude + `
async fn wait_remote(t: far Task<int>) -> TaskResult<int> {
    return t.await();
}
`,
		},
		{
			name:     "cancel",
			form:     sema.CrossingLoweringFarTaskCancel,
			funcName: "cancel_remote",
			src: crossingMIRPrelude + `
async fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
    return t.cancel();
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileCrossingMIR(t, tc.src, crossingForms(tc.form))
			for _, f := range compiled.mod.Funcs {
				mir.SimplifyCFG(f)
			}
			if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
				t.Fatalf("async lowering failed: %v", err)
			}
			ins := requireMIRCrossing(t, compiled.mod, tc.form)
			if ins.ReadyBB == mir.NoBlockID {
				t.Fatalf("async far Task lifecycle crossing missing ready block")
			}
			if ins.PendBB == mir.NoBlockID {
				t.Fatalf("async far Task lifecycle crossing missing pending block")
			}
			if ins.Pending.Local == mir.NoLocalID {
				t.Fatalf("async far Task lifecycle crossing missing pending local")
			}
			if !asyncPayloadHasLabels(compiled.types, "__AsyncPayload$"+tc.funcName, "*uint8") {
				t.Fatalf("far Task lifecycle retry payload must preserve rt_remote_task_pending* state")
			}
			if err := mir.ValidateStructureWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
				CrossingForms: crossingForms(tc.form),
			}); err != nil {
				t.Fatalf("validate async far Task lifecycle crossing MIR: %v", err)
			}
		})
	}
}

// TestMIRAnchoredChannelBodyLowersToReentryHelpers pins the anchored-body
// shape: the crossing produces a body poll function and a pending-retry
// slot, the send lowers to the park-by-re-entry runtime helper call, and
// the receive lowers to an anchored chan_recv (no suspend targets) whose
// destination carries Option<T>.
func TestMIRAnchoredChannelBodyLowersToReentryHelpers(t *testing.T) {
	src := crossingMIRPrelude + `
fn producer(ch: far Channel<int>, n: int) -> TaskResult<nothing> {
    return on ch {
        ch.send(n);
        ret nothing;
    };
}

fn consumer(ch: far Channel<int>) -> TaskResult<Option<int>> {
    return on ch {
        ret ch.recv();
    };
}
`
	compiled := compileCrossingMIR(t, src, crossingForms(sema.CrossingLoweringOnFarHandle))
	sendCalls := 0
	anchoredRecvs := 0
	bodyFns := 0
	for _, fn := range compiled.mod.Funcs {
		if fn == nil {
			continue
		}
		if strings.HasPrefix(fn.Name, "__on_anchored_block$") && strings.HasSuffix(fn.Name, "$poll") {
			bodyFns++
		}
		for bi := range fn.Blocks {
			for ii := range fn.Blocks[bi].Instrs {
				ins := &fn.Blocks[bi].Instrs[ii]
				switch ins.Kind {
				case mir.InstrCall:
					if ins.Call.Callee.Name == "rt_anchored_channel_send" {
						sendCalls++
						if len(ins.Call.Args) != 1 {
							t.Fatalf("anchored send helper call args = %d, want 1", len(ins.Call.Args))
						}
					}
				case mir.InstrChanRecv:
					if ins.ChanRecv.Anchored {
						anchoredRecvs++
						if ins.ChanRecv.ReadyBB != mir.NoBlockID || ins.ChanRecv.PendBB != mir.NoBlockID {
							t.Fatal("anchored chan_recv must carry no suspend targets")
						}
						if got := types.Label(compiled.types, mustLocalType(t, fn, ins.ChanRecv.Dst)); got != "Option<int>" {
							t.Fatalf("anchored recv dst type = %q, want Option<int>", got)
						}
					}
				}
			}
		}
	}
	if bodyFns != 2 {
		t.Fatalf("anchored body poll functions = %d, want 2", bodyFns)
	}
	if sendCalls != 1 {
		t.Fatalf("anchored send helper calls = %d, want 1", sendCalls)
	}
	if anchoredRecvs != 1 {
		t.Fatalf("anchored chan_recv instrs = %d, want 1", anchoredRecvs)
	}
}

func mustLocalType(t *testing.T, fn *mir.Func, place mir.Place) types.TypeID {
	t.Helper()
	if place.Local == mir.NoLocalID || int(place.Local) >= len(fn.Locals) {
		t.Fatalf("place has no local")
	}
	return fn.Locals[place.Local].Type
}

func crossingForms(kinds ...sema.CrossingLoweringKind) map[sema.CrossingLoweringKind]bool {
	out := make(map[sema.CrossingLoweringKind]bool, len(kinds))
	for _, kind := range kinds {
		out[kind] = true
	}
	return out
}

func requireMIRCrossing(t *testing.T, mod *mir.Module, kind sema.CrossingLoweringKind) mir.CrossingInstr {
	t.Helper()
	for _, f := range mod.Funcs {
		if f == nil {
			continue
		}
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				if ins.Kind == mir.InstrCrossing && ins.Crossing.Kind == kind {
					return ins.Crossing
				}
			}
		}
	}
	t.Fatalf("missing MIR crossing kind %d", kind)
	return mir.CrossingInstr{}
}

func asyncPayloadHasLabels(typesIn *types.Interner, unionName string, labels ...string) bool {
	if typesIn == nil || typesIn.Strings == nil {
		return false
	}
	for id := types.TypeID(1); ; id++ {
		tt, ok := typesIn.Lookup(id)
		if !ok {
			break
		}
		if tt.Kind != types.KindUnion {
			continue
		}
		info, ok := typesIn.UnionInfo(id)
		if !ok || info == nil {
			continue
		}
		name, ok := typesIn.Strings.Lookup(info.Name)
		if !ok || name != unionName {
			continue
		}
		for _, member := range info.Members {
			got := make(map[string]bool, len(member.TagArgs))
			for _, arg := range member.TagArgs {
				got[types.Label(typesIn, arg)] = true
			}
			all := true
			for _, want := range labels {
				if !got[want] {
					all = false
					break
				}
			}
			if all {
				return true
			}
		}
		return false
	}
	return false
}
