package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A fixed array `T[N]` never becomes a dynamic `T[]` implicitly; the copy is
// written, `x.to_array()` (core ArrayFixed<T, N>.to_array). These rows pin that
// the method the SEM3015 help names builds a dynamic array on both backends:
// the copy grows without touching its source, lands in a field, a payload and
// a function result, and an array literal written where a
// `T[]` is expected -- directly, nested, in a tuple -- builds the dynamic array
// itself. Before the rule, `let b: int[] = a` with `a: int[2]` moved the fixed
// array's inline bytes into a dynamic array's slot: the VM printed 7 and the
// native binary crashed. The native rows run under valgrind and must free every
// block. The rows copy Copy elements: on this tip a `string[N]`'s to_array
// leaves the return-origin analysis unfinished (its deferred clone of a
// non-Copy element), and a record without __clone is refused inside core.
var fixedArrayToArrayRows = []struct{ name, source, want string }{
	{"int_copy_grows_alone", `@entrypoint
fn main() -> int {
    let a: int[2] = [6, 7];
    let mut b: int[] = a.to_array();
    b.push(8);
    b[0] = 1;
    print(b[1] to string);
    print((b.__len() to int) to string);
    print(a[0] to string);
    print((a.__len() to int) to string);
    return 0;
}
`, "7\n3\n6\n2\n"},
	{"field_payload_and_result", `type S = { xs: int[] }

fn widen(a: &int[2]) -> int[] {
    return a.to_array();
}

@entrypoint
fn main() -> int {
    let a: int[2] = [6, 7];
    let c = widen(&a);
    let s = S { xs: a.to_array() };
    let o: Option<int[]> = Some(a.to_array());
    print(c[0] to string);
    print(s.xs[1] to string);
    compare o { Some(v) => print(v[1] to string); nothing => print("none"); };
    return 0;
}
`, "6\n7\n7\n"},
	{"literals_typed_as_dynamic", `fn r() -> int[] {
    return [6, 7];
}

@entrypoint
fn main() -> int {
    let mut b: int[] = [1, 2];
    b.push(3);
    let xs: int[][] = [[4, 5], [6, 7]];
    let t: (int[], int) = ([8, 9], 1);
    print(b[2] to string);
    let rr = r();
    print(rr[1] to string);
    print(xs[1][0] to string);
    print(t.0[1] to string);
    return 0;
}
`, "3\n7\n6\n9\n"},
}

func TestFixedArrayToArrayRunsVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range fixedArrayToArrayRows {
		t.Run(row.name, func(t *testing.T) {
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != 0 || res.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, row.want, res.stderr)
			}
		})
	}
}

func TestFixedArrayToArrayRunsLLVM(t *testing.T) {
	root := repoRoot(t)
	for _, row := range fixedArrayToArrayRows {
		t.Run(row.name, func(t *testing.T) {
			outputPath := buildLLVMProgramFromSource(t, row.source)
			cmd := exec.Command(outputPath)
			cmd.Dir = root
			stdout, stderr, exitCode := runCommand(t, cmd, "")
			if exitCode != 0 || stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, row.want, stderr)
			}
			vgOut, vgErr, vgCode := runBinaryUnderValgrind(t, outputPath, envWithStdlib(root), 120*time.Second)
			if vgCode != 0 || vgOut != row.want || hasValgrindMemcheckError(vgErr) {
				t.Fatalf("under valgrind: exit=%d stdout=%q want=%q\nstderr:\n%s", vgCode, vgOut, row.want, vgErr)
			}
			if !strings.Contains(vgErr, "All heap blocks were freed") {
				t.Fatalf("under valgrind: a block outlived the program\nstderr:\n%s", vgErr)
			}
		})
	}
}
