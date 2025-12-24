package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
	"surge/internal/vm"
)

func runHeapReleaseTrace(t *testing.T, mirMod *mir.Module, files *source.FileSet, typesInterner *types.Interner) string {
	t.Helper()
	var buf bytes.Buffer
	tracer := vm.NewTracer(&buf, files)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, tracer)
	if vmErr != nil {
		t.Fatalf("unexpected VM error: %v", vmErr.Error())
	}
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
	var out strings.Builder
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "[heap] release") || strings.HasPrefix(line, "[heap] free") {
			out.WriteString(line)
			out.WriteString("\n")
		}
	}
	return out.String()
}

func TestVMDropOrderDeterministic(t *testing.T) {
	sourceCode := `type Foo = { s: string, n: int }

@entrypoint
fn main() -> int {
    let mut a: Foo[] = [Foo { s = "a", n = 1 }, Foo { s = "b", n = 2 }];
    let _view = a[[0..2]];

    let mut b: int[][] = [[1, 2], [3, 4]];
    let _inner = b[0];

    let mut s: string = "";
    let mut i: int = 0;
    while i < 5 {
        s = s + "x";
        i = i + 1;
    }
    let _strings: string[] = [s, s + "y"];
    return 0;
}
`
	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	trace1 := runHeapReleaseTrace(t, mirMod, files, typesInterner)
	trace2 := runHeapReleaseTrace(t, mirMod, files, typesInterner)
	if trace1 == "" {
		t.Fatalf("expected heap release trace output")
	}
	if trace1 != trace2 {
		t.Fatalf("heap release trace mismatch:\nfirst:\n%s\nsecond:\n%s", trace1, trace2)
	}
}
