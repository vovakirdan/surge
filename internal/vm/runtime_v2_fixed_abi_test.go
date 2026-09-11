package vm_test

import (
	"strconv"
	"testing"
	"time"

	"surge/internal/vm"
)

// The wide pair fits signed64 but cannot be represented by an inline bigint.
// Native resize parsing uses strtol into long on our Linux x86-64 ABI target.
const termResizeFixed64Program = `import stdlib/term as term;

fn read_expected(cols: int64, rows: int64) -> bool {
    let event = term.term_read_event();
    let copy = event;
    let first = compare event {
        term.Resize(c, r) => c == cols && r == rows;
        _ => false;
    };
    let second = compare copy {
        term.Resize(c, r) => c == cols && r == rows;
        _ => false;
    };
    return first && second;
}

@entrypoint
fn main() -> int {
    if !read_expected(120:int64, 30:int64) { return 11; }
    if !read_expected(0:int64, 0:int64) { return 12; }
    if !read_expected(4611686018427387911:int64, -4611686018427387913:int64) { return 13; }
    let end = term.term_read_event();
    return compare end {
        term.Eof() => 0;
        _ => 14;
    };
}
`

func TestVMTermResizeFixed64Payload(t *testing.T) {
	requireVMBackend(t)
	if strconv.IntSize != 64 {
		t.Fatal("fixed ABI terminal proof requires the Linux x86-64 target")
	}
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	mod, files, interner := compileToMIRFromSource(t, termResizeFixed64Program)
	rt := vm.NewTestRuntime(nil, "")
	rt.EnqueueTermEvents(
		vm.TermEventData{Kind: vm.TermEventResize, Cols: 120, Rows: 30},
		vm.TermEventData{Kind: vm.TermEventResize, Cols: 0, Rows: 0},
		vm.TermEventData{Kind: vm.TermEventResize, Cols: 4611686018427387911, Rows: -4611686018427387913},
		vm.TermEventData{Kind: vm.TermEventEOF},
	)
	exitCode, vmErr := runVM(mod, rt, files, interner, nil)
	if vmErr != nil {
		t.Fatalf("fixed64 Resize VM error: %s", vmErr.FormatWithFiles(files))
	}
	if exitCode != 0 {
		t.Fatalf("fixed64 Resize VM exit = %d, want 0", exitCode)
	}
	calls := rt.TermCalls()
	if len(calls) != 4 {
		t.Fatalf("terminal read census = %d, want four", len(calls))
	}
	for i, call := range calls {
		if call.Name != "term_read_event" {
			t.Fatalf("terminal call %d = %s, want term_read_event", i, call.Name)
		}
	}
}

func TestNativeTermResizeFixed64Payload(t *testing.T) {
	output := buildLLVMProgramFromSource(t, termResizeFixed64Program)
	env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_TERM_EVENTS",
		"resize:120x30;resize:0x0;resize:4611686018427387911x-4611686018427387913;eof")
	env = overrideEnvVar(env, "SURGE_TERM_DEBUG", "0")
	_, result := runBinaryWithTimeout(t, output, env, 10*time.Second)
	if result.exitCode != 0 || result.stdout != "" || result.stderr != "" {
		t.Fatalf("fixed64 Resize native exit=%d stdout=%q stderr=%q; want 0 and empty output\n%s",
			result.exitCode, result.stdout, result.stderr, result.diagnostics)
	}
}
