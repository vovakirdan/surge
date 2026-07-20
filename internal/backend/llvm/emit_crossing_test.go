package llvm

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"surge/internal/driver"
	"surge/internal/hir"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/sema"
)

func TestEmitSpawnOnCrossingUsesRemotePublicationPath(t *testing.T) {
	sourceCode := `
async fn run(dst: Placement, n: int) -> far Task<int> {
    return spawn on dst {
        ret n + 1;
    };
}
`

	mirMod, result := lowerCrossingMIRFromSource(t, sourceCode, sema.CrossingLoweringSpawnOn)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	runPoll := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mirMod, "run$poll").ID))
	spawnPoll := findSpawnOnPollFunc(t, mirMod)
	spawnPollBody := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(spawnPoll.ID))

	for _, want := range []string{
		"declare i32 @rt_remote_spawn_publish_placement(i64, i64, i64, i64, ptr, ptr, ptr)",
		"declare i32 @rt_far_task_handle_alloc(ptr)",
		"call i32 @rt_far_task_handle_alloc(ptr ",
		"call i32 @rt_remote_spawn_publish_placement(",
		"call i32 @rt_remote_spawn_publish_placement(i64 0,",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("spawn_on LLVM IR missing %q:\n%s", want, ir)
		}
	}
	if strings.Contains(runPoll, "call ptr @rt_alloc(i64 24, i64 8)") {
		t.Fatalf("spawn_on must allocate far-task handles through the runtime ABI:\n%s", runPoll)
	}
	retryBranch := strings.Index(runPoll, "br i1 ")
	handleAlloc := strings.Index(runPoll, "call i32 @rt_far_task_handle_alloc(ptr ")
	if retryBranch < 0 || handleAlloc < retryBranch {
		t.Fatalf("far-task handle allocation must live on the initial branch, after retry selection:\n%s", runPoll)
	}
	allocCall := runPoll[handleAlloc:]
	allocBranch := strings.Index(allocCall, "br i1 ")
	publishCall := strings.Index(allocCall, "call i32 @rt_remote_spawn_publish_placement(")
	if allocBranch < 0 || publishCall < allocBranch {
		t.Fatalf("far-task allocation status must branch before publication:\n%s", runPoll)
	}
	for _, want := range []string{
		"i32 2, label",
		"i32 3, label",
		"i32 4, label",
		"i32 5, label",
		"call void @rt_panic(",
	} {
		if !strings.Contains(runPoll, want) {
			t.Fatalf("spawn_on status/error path missing %q:\n%s", want, runPoll)
		}
	}
	if !regexp.MustCompile(`br label %bb\d+`).MatchString(runPoll) {
		t.Fatalf("spawn_on poll path must branch back to MIR ready/pending blocks:\n%s", runPoll)
	}
	if strings.Contains(runPoll, "call void @rt_task_wake") {
		t.Fatalf("spawn_on lowering must not use local spawn wake fallback:\n%s", runPoll)
	}
	if !strings.Contains(spawnPollBody, "call ptr @__task_state()") ||
		!strings.Contains(spawnPollBody, "call void @rt_async_return(") {
		t.Fatalf("synthetic remote child poll must read task state and async-return result:\n%s", spawnPollBody)
	}
}

func TestEmitFarTaskLifecycleCrossingUsesRemoteTaskPath(t *testing.T) {
	sourceCode := `
async fn wait_remote(t: far Task<int>) -> TaskResult<int> {
    return t.await();
}

async fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
    return t.cancel();
}
`

	mirMod, result := lowerCrossingMIRFromSource(
		t,
		sourceCode,
		sema.CrossingLoweringFarTaskAwait,
		sema.CrossingLoweringFarTaskCancel,
	)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	waitPoll := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mirMod, "wait_remote$poll").ID))
	cancelPoll := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(findMIRFunc(t, mirMod, "cancel_remote$poll").ID))
	for _, want := range []string{
		"declare i32 @rt_far_task_await(ptr, i64, ptr, ptr, ptr)",
		"declare i32 @rt_far_task_cancel(ptr, i64, ptr, ptr, ptr)",
		"call i32 @rt_far_task_await(",
		"call i32 @rt_far_task_cancel(",
		"call i32 @rt_far_task_await(ptr null, i64 0,",
		"call i32 @rt_far_task_cancel(ptr null, i64 0,",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("far Task lifecycle LLVM IR missing %q:\n%s", want, ir)
		}
	}
	for _, body := range []struct {
		name string
		text string
	}{
		{name: "await", text: waitPoll},
		{name: "cancel", text: cancelPoll},
	} {
		for _, want := range []string{
			"i32 2, label",
			"i32 3, label",
			"i32 4, label",
			"i32 5, label",
			"i32 6, label",
			"i32 7, label",
			"call void @rt_panic(",
		} {
			if !strings.Contains(body.text, want) {
				t.Fatalf("far Task %s status/error path missing %q:\n%s", body.name, want, body.text)
			}
		}
		if strings.Contains(body.text, "call void @rt_task_await") ||
			strings.Contains(body.text, "call void @rt_task_cancel") {
			t.Fatalf("far Task %s lowering must not use local task lifecycle fallback:\n%s", body.name, body.text)
		}
		runtimeCall := "call i32 @rt_far_task_" + body.name + "("
		initialCalls := strings.Count(body.text, runtimeCall) - strings.Count(body.text, runtimeCall+"ptr null,")
		if got := strings.Count(body.text, "call void @rt_far_task_handle_free("); got != initialCalls {
			t.Fatalf("far Task %s must free every initial consumed handle, got %d frees for %d initial calls:\n%s", body.name, got, initialCalls, body.text)
		}
	}
}

func TestEmitFarTaskAsyncOwnershipTransferHooks(t *testing.T) {
	sourceCode := `
async fn forward_far_task_lease(t: far Task<int>) -> far Task<int> {
    return t;
}
`

	mirMod, result := lowerCrossingMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}

	constructor := findMIRFunc(t, mirMod, "forward_far_task_lease")
	poll := llvmFunctionContaining(t, ir, "call void @rt_far_task_prepare_return(")
	calls := make([]string, 0, 4)
	for _, block := range constructor.Blocks {
		for _, ins := range block.Instrs {
			if ins.Kind == mir.InstrCall {
				calls = append(calls, ins.Call.Callee.Name)
			}
		}
	}
	begin := slices.Index(calls, "rt_far_task_begin_transfer")
	create := slices.Index(calls, "__task_create")
	finish := slices.Index(calls, "rt_far_task_finish_transfer")
	if begin < 0 || create < 0 || finish < 0 || begin >= create || create >= finish {
		t.Fatalf("far Task parameter ownership must bracket task creation, calls=%v", calls)
	}
	if !strings.Contains(poll, "call void @rt_far_task_prepare_return(") {
		t.Fatalf("successful direct far Task async return must prepare lease transfer:\n%s", poll)
	}
}

func llvmFunctionContaining(t *testing.T, ir, needle string) string {
	t.Helper()
	for _, part := range strings.Split(ir, "\ndefine ") {
		if strings.Contains(part, needle) {
			return "define " + part
		}
	}
	t.Fatalf("missing LLVM function containing %q", needle)
	return ""
}

func lowerCrossingMIRFromSource(t *testing.T, sourceCode string, forms ...sema.CrossingLoweringKind) (*mir.Module, *driver.DiagnoseResult) {
	t.Helper()
	withRepoStdlib(t)
	tmpFile := filepath.Join(t.TempDir(), "crossing.sg")
	if err := os.WriteFile(tmpFile, []byte(sourceCode), 0o600); err != nil {
		t.Fatalf("write temp source: %v", err)
	}
	enabled := make(map[sema.CrossingLoweringKind]bool, len(forms))
	for _, form := range forms {
		enabled[form] = true
	}
	opts := driver.DiagnoseOptions{
		Stage:              driver.DiagnoseStageSema,
		EmitInstantiations: true,
		MaxDiagnostics:     200,
	}
	res, err := driver.DiagnoseWithOptions(context.Background(), tmpFile, &opts)
	if err != nil {
		t.Fatalf("diagnose crossing source: %v", err)
	}
	if res == nil || res.Builder == nil || res.Sema == nil || res.Symbols == nil || res.Instantiations == nil {
		t.Fatalf("missing crossing diagnose artifacts")
	}
	if res.Bag != nil && res.Bag.HasErrors() {
		t.Fatalf("unexpected crossing diagnostics: %v", res.Bag.Items())
	}
	hirMod, err := hir.LowerWithOptions(context.Background(), res.Builder, res.FileID, res.Sema, res.Symbols, hir.LowerOptions{
		CrossingForms: enabled,
	})
	if err != nil {
		t.Fatalf("lower crossing HIR: %v", err)
	}
	res.HIR = hirMod
	combined, err := driver.CombineHIRWithModulesWithOptions(context.Background(), res, driver.HIRCombineOptions{
		CrossingForms: enabled,
	})
	if err != nil {
		t.Fatalf("combine crossing HIR modules: %v", err)
	}
	if combined == nil {
		combined = hirMod
	}
	mm, err := mono.MonomorphizeModule(combined, res.Instantiations, res.Sema, mono.Options{MaxDepth: 64})
	if err != nil {
		t.Fatalf("monomorphize crossing source: %v", err)
	}
	mirMod, err := mir.LowerModuleWithOptions(mm, res.Sema, mir.LowerOptions{CrossingForms: enabled})
	if err != nil {
		t.Fatalf("lower crossing MIR: %v", err)
	}
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}
	if err := mir.LowerAsyncStateMachine(mirMod, res.Sema, res.Symbols.Table); err != nil {
		t.Fatalf("lower crossing async state machine: %v", err)
	}
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
	}
	if err := mir.ValidateWithOptions(mirMod, res.Sema.TypeInterner, mir.ValidateOptions{CrossingForms: enabled}); err != nil {
		t.Fatalf("validate crossing MIR: %v", err)
	}
	return mirMod, res
}

func findSpawnOnPollFunc(t *testing.T, mod *mir.Module) *mir.Func {
	t.Helper()
	for _, fn := range mod.Funcs {
		if fn != nil && strings.HasPrefix(fn.Name, "__spawn_on_block$") && strings.HasSuffix(fn.Name, "$poll") {
			return fn
		}
	}
	t.Fatalf("missing synthetic spawn_on poll function")
	return nil
}

func itoaMIRFuncID(id mir.FuncID) string {
	return strconv.Itoa(int(id))
}
