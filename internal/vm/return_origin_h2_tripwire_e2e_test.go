package vm_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/source"
	"surge/internal/types"
	"surge/internal/vm"
)

// RV2-DEBT-365 tripwire, runtime rows. The leaking programs are refused by the task check
// (TestH2TripwireTaskCheckRefusesLeakedRuns). Each row below runs the same runner over a twin whose
// task borrows nothing: it builds with no refusal and its runner works on no backend. A row
// pins that exact outcome on the backend SURGE_BACKEND names; anything else - the task ran, a
// different fault, a compile-time refusal - is red. The frozen sources and digests are in
// return_origin_h2_tripwire_sources_test.go.

type h2TripwireOutcome struct {
	refused     string // a compile-time refusal; a runtime row must never see one
	vmError     string // "VM<code>: <message>" on the VM
	buildFailed bool   // native: `surge build` exited nonzero
	buildOutput string
	exit        int
	stdout      string
	stderr      string
}

// h2TripwireLower is compileToMIR without its Fatal calls: a refusal is an answer here.
func h2TripwireLower(path string) (*mir.Module, *source.FileSet, *types.Interner, string) {
	opts := driver.DiagnoseOptions{Stage: driver.DiagnoseStageSema, EmitHIR: true, EmitInstantiations: true}
	result, err := driver.DiagnoseWithOptions(context.Background(), path, &opts)
	if err != nil {
		return nil, nil, nil, "diagnose: " + err.Error()
	}
	if result.Bag.HasErrors() || result.HIR == nil || result.Instantiations == nil || result.Sema == nil {
		return nil, nil, nil, fmt.Sprintf("diagnostics: %+v", result.Bag.Items())
	}
	hirModule, err := driver.CombineHIRWithModules(context.Background(), result)
	if err != nil {
		return nil, nil, nil, "merge: " + err.Error()
	}
	if hirModule == nil {
		hirModule = result.HIR
	}
	mm, err := mono.MonomorphizeModule(hirModule, result.Instantiations, result.Sema, mono.Options{MaxDepth: 64})
	if err != nil {
		return nil, nil, nil, "mono: " + err.Error()
	}
	mod, err := mir.LowerModule(mm, result.Sema)
	if err != nil {
		return nil, nil, nil, "mir: " + err.Error()
	}
	for _, f := range mod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}
	if err := mir.LowerAsyncStateMachine(mod, result.Sema, result.Symbols.Table); err != nil {
		return nil, nil, nil, "async: " + err.Error()
	}
	for _, f := range mod.Funcs {
		mir.SimplifyCFG(f)
	}
	if err := mir.FinalizeModuleMeta(mod, result.Sema.TypeInterner, layout.X86_64LinuxGNU(), mir.NewOperationPlanInput(result.Sema, mm)); err != nil {
		return nil, nil, nil, "layout: " + err.Error()
	}
	if err := mir.Validate(mod, result.Sema.TypeInterner); err != nil {
		return nil, nil, nil, "validate: " + err.Error()
	}
	return mod, result.FileSet, result.Sema.TypeInterner, ""
}

// h2TripwireRun builds and runs one frozen probe without failing the test on a refusal, a
// failed build or a panic: the row's matcher decides.
func h2TripwireRun(t *testing.T, name, text, digest string) h2TripwireOutcome {
	t.Helper()
	skipTimeoutTests(t)
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != digest {
		t.Fatalf("PRECONDITION: frozen probe changed: %s", got)
	}
	root := repoRoot(t)
	src := filepath.Join(t.TempDir(), "h2tw_"+name+".sg")
	if err := os.WriteFile(src, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	var out h2TripwireOutcome
	if testBackend(t) == backendVM {
		t.Setenv("SURGE_STDLIB", root)
		mod, files, interner, refused := h2TripwireLower(src)
		if refused != "" {
			out.refused = refused
			return out
		}
		code, vmErr := runVM(mod, vm.NewTestRuntime(nil, ""), files, interner, nil)
		out.exit = code
		if vmErr != nil {
			out.vmError = vmErr.Code.String() + ": " + vmErr.Message
		}
		t.Logf("H2_TRIPWIRE %s vm outcome=%+v", name, out)
		return out
	}
	ensureLLVMToolchain(t)
	surge := buildSurgeBinary(t, root)
	stdout, stderr, code := runSurgeWithInputEnv(t, root, surge, "", envWithStdlib(root), "build", src)
	out.buildOutput = stdout + stderr
	out.buildFailed = code != 0
	if strings.Contains(out.buildOutput, "return-origin") || strings.Contains(out.buildOutput, "error SEM") {
		out.refused = out.buildOutput
	}
	if out.buildFailed {
		t.Logf("H2_TRIPWIRE %s llvm build output=%s", name, out.buildOutput)
		return out
	}
	binary := llvmOutputPath(root, src)
	t.Cleanup(func() { _ = os.Remove(binary) })
	out.stdout, out.stderr, out.exit = runBinary(t, binary)
	t.Logf("H2_TRIPWIRE %s llvm outcome=%+v", name, out)
	return out
}

// A barrier is the one outcome a row accepts on each backend.
type h2TripwireBarrier struct {
	vmError  string // the VM error must start with this
	emitText string // native: the build must fail and say this; "" means the build succeeds,
	exit     int    // and the binary must exit with this code and print nothing
}

var (
	h2BarrierOwnAwait   = h2TripwireBarrier{vmError: "VM1005: unsupported intrinsic: await", emitText: `unknown external function "await::<int>"`}
	h2BarrierScopeJoin  = h2TripwireBarrier{vmError: "VM1999: rt_scope_enter without current task", emitText: `unknown external function "rt_scope_register_child::<int>"`}
	h2BarrierAsyncEntry = h2TripwireBarrier{vmError: "VM1003: expected int, got resource"}
	h2HarnessExit       = h2TripwireBarrier{exit: 46}
)

// holds answers "" when out is exactly this barrier's outcome on the given backend.
func (b h2TripwireBarrier) holds(backend string, out h2TripwireOutcome) string {
	switch {
	case out.refused != "":
		return "refused at compile time, so the runtime barrier is not what stops it: " + out.refused
	case backend == backendVM && b.vmError == "":
		if out.vmError != "" || out.exit != b.exit {
			return fmt.Sprintf("vm exit %d error %q, want exit %d", out.exit, out.vmError, b.exit)
		}
	case backend == backendVM:
		if !strings.HasPrefix(out.vmError, b.vmError) {
			return fmt.Sprintf("vm error %q (exit %d), want %q", out.vmError, out.exit, b.vmError)
		}
	case b.emitText != "":
		if !out.buildFailed || !strings.Contains(out.buildOutput, b.emitText) {
			return fmt.Sprintf("native build failed=%v, want an emit failure saying %s: %s", out.buildFailed, b.emitText, out.buildOutput)
		}
	case out.buildFailed || out.exit != b.exit || out.stdout != "" || out.stderr != "":
		return fmt.Sprintf("native build failed=%v exit %d stdout %q stderr %q, want exit %d and no output", out.buildFailed, out.exit, out.stdout, out.stderr, b.exit)
	}
	return ""
}

func checkH2TripwireBarrier(t *testing.T, name, text, digest string, barrier h2TripwireBarrier) {
	t.Helper()
	if verdict := barrier.holds(testBackend(t), h2TripwireRun(t, name, text, digest)); verdict != "" {
		t.Errorf("RV2-DEBT-365 barrier moved: %s", verdict)
	}
}

// RV2-DEBT-365: `.await()` on an `own Task` receiver is supported on no backend.
func TestH2TripwireNotRunnableG1OwnBinding(t *testing.T) {
	checkH2TripwireBarrier(t, "g1_own_binding", h2TripwireG1OwnBindingTwin, h2TripwireG1OwnBindingTwinDigest, h2BarrierOwnAwait)
}

// RV2-DEBT-365: the same receiver as a unary `own` expression.
func TestH2TripwireNotRunnableG1bOwnExpr(t *testing.T) {
	checkH2TripwireBarrier(t, "g1b_own_expr", h2TripwireG1bOwnExprTwin, h2TripwireG1bOwnExprTwinDigest, h2BarrierOwnAwait)
}

// RV2-DEBT-365: a scope join from a sync main has no current task.
func TestH2TripwireNotRunnableG3ScopeJoin(t *testing.T) {
	checkH2TripwireBarrier(t, "g3_scope_join", h2TripwireG3ScopeJoinTwin, h2TripwireG3ScopeJoinTwinDigest, h2BarrierScopeJoin)
}

// RV2-DEBT-365: an async entrypoint's body does not run (RV2-DEBT-050). The arms end the
// process with rt_exit, so a body that runs is seen even while the entry drops its result.
func TestH2TripwireNotRunnableG4xAsyncEntry(t *testing.T) {
	checkH2TripwireBarrier(t, "g4x_async_entry", h2TripwireG4xAsyncEntryTwin, h2TripwireG4xAsyncEntryTwinDigest, h2BarrierAsyncEntry)
}

// RV2-DEBT-365: the same runner over a task forwarded through a by-value formal.
func TestH2TripwireNotRunnableRo6xAsyncEntry(t *testing.T) {
	checkH2TripwireBarrier(t, "ro6x_async_entry", h2TripwireRo6xAsyncEntryTwin, h2TripwireRo6xAsyncEntryTwinDigest, h2BarrierAsyncEntry)
}

// RV2-DEBT-365: the same runner over a task stored through a `&mut` formal.
func TestH2TripwireNotRunnableRo7xAsyncEntry(t *testing.T) {
	checkH2TripwireBarrier(t, "ro7x_async_entry", h2TripwireRo7xAsyncEntryTwin, h2TripwireRo7xAsyncEntryTwinDigest, h2BarrierAsyncEntry)
}

// The same entry and arms over a task that borrows nothing. While RV2-DEBT-050 stands it is
// stopped like the rows above; once the body runs it must exit 46, next to those rows going red.
func TestH2TripwireWitnessControl(t *testing.T) {
	checkH2TripwireBarrier(t, "g4c_witness_control", h2TripwireG4cWitnessControl, h2TripwireG4cWitnessControlDigest, h2BarrierAsyncEntry)
}

// The positive control: rt_exit(46) from a sync main must be seen as exit 46 on this backend.
// A harness that ran nothing, or lost the exit code, would otherwise look like a barrier.
func TestH2TripwireHarnessObservesExit(t *testing.T) {
	checkH2TripwireBarrier(t, "sync_rt_exit", h2TripwireSyncRtExit, h2TripwireSyncRtExitDigest, h2HarnessExit)
}

// Every barrier must refuse the recorded outcomes of a leaked task that ran, and of a program
// that was refused at compile time; and must accept its own recorded outcome.
func TestH2TripwireMatcherRejectsRecordedFaults(t *testing.T) {
	faults := map[string]h2TripwireOutcome{
		"vm_use_after_free":   {vmError: `VM3301: use-after-free: local "l" used after drop`, exit: 1},
		"vm_storage_fault":    {vmError: "VM1999: storage: type#4 is not an inline aggregate", exit: 1},
		"vm_task_ran":         {exit: 46},
		"native_silent_wrong": {exit: 40},
		"native_cancelled":    {exit: 41},
		"native_task_ran":     {exit: 46},
		"native_printed":      {stdout: "TASK_RAN len=0\n"},
		"native_other_build":  {buildFailed: true, buildOutput: "LLVM emit failed: something else"},
		"refused_by_origins":  {refused: "return-origin analysis unfinished", buildFailed: true},
	}
	barriers := map[string]h2TripwireBarrier{"own_await": h2BarrierOwnAwait, "scope_join": h2BarrierScopeJoin, "async_entry": h2BarrierAsyncEntry}
	for bname, barrier := range barriers {
		for fname, out := range faults {
			for _, backend := range []string{backendVM, backendLLVM} {
				if fname != "refused_by_origins" && (backend == backendVM) != strings.HasPrefix(fname, "vm_") {
					continue
				}
				t.Run(bname+"/"+backend+"/"+fname, func(t *testing.T) {
					if barrier.holds(backend, out) == "" {
						t.Errorf("the %s barrier accepted %+v", bname, out)
					}
				})
			}
		}
	}
	recorded := []struct {
		name    string
		barrier h2TripwireBarrier
		backend string
		out     h2TripwireOutcome
	}{
		{"own_await_vm", h2BarrierOwnAwait, backendVM, h2TripwireOutcome{vmError: "VM1005: unsupported intrinsic: await", exit: 1}},
		{"own_await_native", h2BarrierOwnAwait, backendLLVM, h2TripwireOutcome{buildFailed: true, buildOutput: `Error: LLVM emit failed: llvm emit main bb0 instr[4] (Call): unknown external function "await::<int>" (callee Sym sym 2415919110)`}},
		{"scope_join_vm", h2BarrierScopeJoin, backendVM, h2TripwireOutcome{vmError: "VM1999: rt_scope_enter without current task", exit: 1}},
		{"async_entry_vm", h2BarrierAsyncEntry, backendVM, h2TripwireOutcome{vmError: "VM1003: expected int, got resource", exit: 1}},
		{"async_entry_native", h2BarrierAsyncEntry, backendLLVM, h2TripwireOutcome{}},
		{"harness_exit_vm", h2HarnessExit, backendVM, h2TripwireOutcome{exit: 46}},
		{"harness_exit_native", h2HarnessExit, backendLLVM, h2TripwireOutcome{exit: 46}},
	}
	for _, tc := range recorded {
		t.Run("accepts/"+tc.name, func(t *testing.T) {
			if verdict := tc.barrier.holds(tc.backend, tc.out); verdict != "" {
				t.Errorf("the barrier refused its own recorded outcome: %s", verdict)
			}
		})
	}
	// The positive control must not read a lost exit code as success.
	for _, backend := range []string{backendVM, backendLLVM} {
		if h2HarnessExit.holds(backend, h2TripwireOutcome{}) == "" {
			t.Errorf("the harness control accepted exit 0 on %s", backend)
		}
	}
}

// h2TripwireTaskCheckVerdict answers "" when the frozen leaking program is refused by the task
// check and by nothing else: exactly one error, with this code, whose primary span reads at.
func h2TripwireTaskCheckVerdict(t *testing.T, name, text, digest, code, at string) string {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != digest {
		t.Fatalf("PRECONDITION: frozen probe changed: %s", got)
	}
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	src := filepath.Join(t.TempDir(), "h2tw_"+name+".sg")
	if err := os.WriteFile(src, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := driver.DiagnoseOptions{Stage: driver.DiagnoseStageSema}
	result, err := driver.DiagnoseWithOptions(context.Background(), src, &opts)
	if err != nil || result == nil || result.Bag == nil {
		t.Fatalf("PRECONDITION: the probe did not reach the task check: %v", err)
	}
	var verdicts []string
	for _, d := range result.Bag.Items() {
		if d.Severity < diag.SevError {
			continue
		}
		got := ""
		if int(d.Primary.End) <= len(text) {
			got = text[d.Primary.Start:d.Primary.End]
		}
		verdicts = append(verdicts, d.Code.ID()+"@"+got)
	}
	if want := code + "@" + at; len(verdicts) != 1 || verdicts[0] != want {
		return fmt.Sprintf("the leaked task is not refused by the task check alone: errors %q, want exactly [%q]", verdicts, want)
	}
	return ""
}

// RV2-DEBT-365, the first line: each leaking program the rows above are twins of is refused by
// the task check, on either backend, before any runner is reached. A handle that leaves by
// `return t` is SEM3139 at the handle; one that leaves through `pass(t)` or `*out =` is SEM3021
// at the borrow the task took.
func TestH2TripwireTaskCheckRefusesLeakedRuns(t *testing.T) {
	for _, row := range []struct{ name, text, digest, code, at string }{
		{"g1_own_binding", h2TripwireG1OwnBinding, h2TripwireG1OwnBindingDigest, "SEM3139", "t"},
		{"g1b_own_expr", h2TripwireG1bOwnExpr, h2TripwireG1bOwnExprDigest, "SEM3139", "t"},
		{"g3_scope_join", h2TripwireG3ScopeJoin, h2TripwireG3ScopeJoinDigest, "SEM3139", "t"},
		{"g4x_async_entry", h2TripwireG4xAsyncEntry, h2TripwireG4xAsyncEntryDigest, "SEM3139", "t"},
		{"ro6x_async_entry", h2TripwireRo6xAsyncEntry, h2TripwireRo6xAsyncEntryDigest, "SEM3021", "&l"},
		{"ro7x_async_entry", h2TripwireRo7xAsyncEntry, h2TripwireRo7xAsyncEntryDigest, "SEM3021", "&l"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if verdict := h2TripwireTaskCheckVerdict(t, row.name, row.text, row.digest, row.code, row.at); verdict != "" {
				t.Error(verdict)
			}
		})
	}
}
