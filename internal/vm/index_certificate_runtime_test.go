package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// N-INDEXCERT: indexes the return-origin analysis now answers by the callee
// their selection certifies run on both backends and print the same thing. A
// Map read borrows the indexed map's storage, a Map store is insert, an `own`
// container owns its elements, and a nominal `__index` with an int index and
// an erased result is its call. Each row was refused as unfinished before.
// The native rows run under valgrind and must free every block.
var indexCertificateRows = []struct{ name, source, want string }{
	{"map_read_through_a_formal", `type Holder = { m: Map<string, int> };
fn get(m: &Map<string, int>) -> &int { return m["x"]; }
fn deref(pm: &Map<string, int>) -> &int { return (*pm)["x"]; }
fn member(h: &Holder) -> &int { return h.m["x"]; }
@entrypoint
fn main() -> int {
    let m = { "x" => 41 };
    let h = Holder { m = { "x" => 7 } };
    print((*get(&m) + 1) to string);
    print((*deref(&m)) to string);
    print((*member(&h)) to string);
    return 0;
}
`, "42\n41\n7\n"},
	{"map_store_local_and_through_a_formal", `fn put(m: &mut Map<string, int>, v: int) -> nothing {
    m["y"] = v;
    return nothing;
}
@entrypoint
fn main() -> int {
    let mut m = Map::<string, int>.new();
    m["x"] = 10;
    m["x"] = 11;
    put(&mut m, 5);
    print((m["x"] + m["y"]) to string);
    print((m.length()) to string);
    return 0;
}
`, "16\n2\n"},
	{"map_int_key_record_value", `type P = { a: int, b: int };
fn pick(m: &Map<int, P>, k: int) -> &P { return m[k]; }
@entrypoint
fn main() -> int {
    let mut m = Map::<int, P>.new();
    m[1] = P { a = 10, b = 11 };
    m[2] = P { a = 20, b = 21 };
    m[1] = P { a = 30, b = 31 };
    let one = 1;
    let two = 2;
    let p: &P = pick(&m, one);
    let q: &P = m[two];
    print((p.a + q.b) to string);
    return 0;
}
`, "51\n"},
	{"map_growth_by_index", `fn sum(m: &Map<int, int>, n: int) -> int {
    let mut s = 0;
    for i in 0..n {
        let k = i;
        s = s + m[k];
    }
    return s;
}
@entrypoint
fn main() -> int {
    let mut m = Map::<int, int>.new();
    for i in 0..40 {
        let k = i;
        m[k] = i * 2;
    }
    print((sum(&m, 40)) to string);
    return 0;
}
`, "1560\n"},
	{"map_string_value", `fn name(m: &Map<int, string>, k: int) -> &string { return m[k]; }
@entrypoint
fn main() -> int {
    let mut m = Map::<int, string>.new();
    m[1] = "one";
    m[1] = "uno";
    let k = 1;
    let s: &string = name(&m, k);
    print(s.__len() to string);
    let t: string = m[k] + "!";
    print(t);
    return 0;
}
`, "3\nuno!\n"},
	{"own_container_read", `fn total(xs: own int[]) -> int { return xs[0] + xs[1]; }
@entrypoint
fn main() -> int {
    let xs: int[] = [1, 2];
    print((total(own xs)) to string);
    return 0;
}
`, "3\n"},
	{"nominal_int_index", `type Bag = { values: int[] };
extern<Bag> {
    fn __index(self: &Bag, index: int) -> int { return self.values[index]; }
}
@entrypoint
fn main() -> int {
    let b = Bag { values = [4, 5, 6] };
    print((b[1] + b[2]) to string);
    return 0;
}
`, "11\n"},
	{"mutable_method_on_indexed_array_element", `fn grow(xs: &mut int[][]) -> nothing {
    xs[1].push(9);
    return nothing;
}
@entrypoint
fn main() -> int {
    let base: int[] = [1, 2, 3, 4];
    let win: int[] = base[[1..3]];
    let owned: int[] = [7, 8];
    let mut xs: int[][] = [win, owned];
    grow(&mut xs);
    print((xs[1].__len() to int) to string);
    print(base[1] to string);
    return 0;
}
`, "3\n2\n"},
}

func TestIndexCertificateRunsVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range indexCertificateRows {
		t.Run(row.name, func(t *testing.T) {
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != 0 || res.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, row.want, res.stderr)
			}
		})
	}
}

func TestIndexCertificateRunsLLVM(t *testing.T) {
	root := repoRoot(t)
	for _, row := range indexCertificateRows {
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
