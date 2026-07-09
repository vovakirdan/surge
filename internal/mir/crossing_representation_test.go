package mir_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/lexer"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
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
				if len(ins.State.Fields) != tc.wantCaptures {
					t.Fatalf("spawn_on state fields = %d, want %d", len(ins.State.Fields), tc.wantCaptures)
				}
				pollFn := compiled.mod.Funcs[ins.BodyFuncID]
				if pollFn == nil || !strings.HasPrefix(pollFn.Name, "__spawn_on_block$") || !strings.HasSuffix(pollFn.Name, "$poll") {
					t.Fatalf("spawn_on synthetic poll function name = %v", pollFn)
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

	err := mir.Validate(compiled.mod, compiled.types)
	if err == nil || !strings.Contains(err.Error(), "crossing on is not enabled") {
		t.Fatalf("expected default-closed crossing validation error, got %v", err)
	}
	if err := mir.ValidateWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
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
	if err := mir.ValidateWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
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
	if err := mir.ValidateWithOptions(compiled.mod, compiled.types, mir.ValidateOptions{
		CrossingForms: crossingForms(sema.CrossingLoweringSpawnOn),
	}); err != nil {
		t.Fatalf("validate async spawn_on crossing MIR: %v", err)
	}
}

const crossingMIRPrelude = `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
type Task<T> = { __opaque: int };
extern<Task<T>> {
    fn await(self: own Task<T>) -> TaskResult<T>;
}
@intrinsic @copy
type Placement = { __opaque: int };
type TcpConn = { fd: int };
type Channel<T> = { id: int };
@shard_movable
type Movable = { id: int };
`

type crossingMIRCompileResult struct {
	mod     *mir.Module
	types   *types.Interner
	sema    *sema.Result
	symbols *symbols.Result
}

func crossingForms(kinds ...sema.CrossingLoweringKind) map[sema.CrossingLoweringKind]bool {
	out := make(map[sema.CrossingLoweringKind]bool, len(kinds))
	for _, kind := range kinds {
		out[kind] = true
	}
	return out
}

func compileCrossingMIR(t *testing.T, src string, forms map[sema.CrossingLoweringKind]bool) crossingMIRCompileResult {
	t.Helper()
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)

	stringsIn := source.NewInterner()
	typesIn := types.NewInterner()
	instMap := mono.NewInstantiationMap()
	bag := diag.NewBag(200)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, stringsIn)
	parsed := parser.ParseFile(context.Background(), fs, lx, builder, parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: 200,
	})
	if bag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", crossingDiagSummary(bag))
	}

	symbolsRes := symbols.ResolveFile(builder, parsed.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "core",
		FilePath:   "test.sg",
	})
	if bag.HasErrors() {
		t.Fatalf("symbol diagnostics: %s", crossingDiagSummary(bag))
	}

	semaRes := sema.Check(context.Background(), builder, parsed.File, sema.Options{
		Reporter:       &diag.BagReporter{Bag: bag},
		Symbols:        &symbolsRes,
		Types:          typesIn,
		ModulePath:     builder.StringsInterner.Intern("core"),
		Instantiations: mono.NewInstantiationMapRecorder(instMap),
	})
	if bag.HasErrors() {
		t.Fatalf("sema diagnostics: %s", crossingDiagSummary(bag))
	}

	hirMod, err := hir.LowerWithOptions(context.Background(), builder, parsed.File, &semaRes, &symbolsRes, hir.LowerOptions{
		CrossingForms: forms,
	})
	if err != nil {
		t.Fatalf("HIR lowering failed: %v", err)
	}
	monoMod, err := mono.MonomorphizeModule(hirMod, instMap, &semaRes, mono.Options{})
	if err != nil {
		t.Fatalf("monomorphization failed: %v", err)
	}
	mirMod, err := mir.LowerModuleWithOptions(monoMod, &semaRes, mir.LowerOptions{
		CrossingForms: forms,
	})
	if err != nil {
		t.Fatalf("MIR lowering failed: %v", err)
	}
	return crossingMIRCompileResult{mod: mirMod, types: typesIn, sema: &semaRes, symbols: &symbolsRes}
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

func crossingDiagSummary(bag *diag.Bag) string {
	if bag == nil || bag.Len() == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, d := range bag.Items() {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s: %s", d.Code.ID(), d.Message)
	}
	return b.String()
}
