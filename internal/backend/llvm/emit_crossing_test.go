package llvm

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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
		"declare i32 @rt_remote_spawn_publish_placement(i64, i64, ptr, ptr, ptr)",
		"call ptr @rt_alloc(i64 24, i64 8)",
		"call i32 @rt_remote_spawn_publish_placement(",
		"call i32 @rt_remote_spawn_publish_placement(i64 0,",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("spawn_on LLVM IR missing %q:\n%s", want, ir)
		}
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
