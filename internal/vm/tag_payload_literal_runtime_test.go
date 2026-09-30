package vm_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// An array literal handed to a tag constructor where a union is expected is
// built as the payload the union holds: `return Some([6, 7])` from a function
// returning `Option<int[]>` builds an `int[]`. Typed on its own the literal
// was an `int[2]`, the call a `Some<int[2]>`, and the widening cast to
// `Option<int[]>` that followed had no conversion for a fixed array's inline
// bytes: the VM panicked (VM1999 "composite is not handle-backed") and the
// native build failed ("union cast payload type mismatch for tag Some").
//
// Each row reaches the constructor through a different context that carries
// the expected union -- a return, a typed let, a compare arm, a ternary
// branch, a nested constructor, an `Erring`, user tags -- and every owned
// payload is dropped on the arm it takes. Both backends run every row and
// print the same thing; the native rows run under valgrind and must free
// every block.
var tagPayloadLiteralRows = []struct{ name, source, want string }{
	{"int_literal_returned", `fn f() -> Option<int[]> { return Some([6, 7]); }
@entrypoint
fn main() -> int {
    compare f() { Some(xs) => print(xs[1] to string); nothing => print("none"); };
    return 0;
}
`, "7\n"},
	{"empty_literal_returned", `fn f() -> Option<int[]> { return Some([]); }
@entrypoint
fn main() -> int {
    compare f() { Some(xs) => print((xs.__len() to int) to string); nothing => print("none"); };
    return 0;
}
`, "0\n"},
	{"string_elements_returned_and_let", `fn f() -> Option<string[]> { return Some(["alpha", "beta"]); }
@entrypoint
fn main() -> int {
    let o: Option<string[]> = Some(["gamma"]);
    compare f() { Some(xs) => print(xs[1]); nothing => print("none"); };
    compare o { Some(xs) => print(xs[0]); nothing => print("none"); };
    return 0;
}
`, "beta\ngamma\n"},
	{"record_elements", `type R = { label: string, n: int };
fn f() -> Option<R[]> { return Some([R { label = "one", n = 1 }, R { label = "two", n = 2 }]); }
fn total(xs: &R[]) -> int { return xs[0].n + xs[1].n; }
@entrypoint
fn main() -> int {
    compare f() { Some(xs) => print(total(&xs) to string); nothing => print("none"); };
    return 0;
}
`, "3\n"},
	{"widened_elements", `fn f() -> Option<int64[]> { let a: int32 = 3; let b: int32 = 4; return Some([a, b]); }
@entrypoint
fn main() -> int {
    compare f() { Some(xs) => print((xs[0] + xs[1]) to string); nothing => print("none"); };
    return 0;
}
`, "7\n"},
	{"nested_option", `fn f() -> Option<Option<int[]>> { return Some(Some([6, 7])); }
@entrypoint
fn main() -> int {
    compare f() {
        Some(inner) => { compare inner { Some(xs) => print(xs[1] to string); nothing => print("inner-none"); }; }
        nothing => print("none");
    };
    return 0;
}
`, "7\n"},
	{"erring_success", `fn f(ok: bool) -> Erring<string[], Error> {
    if ok { return Success(["x", "y"]); }
    return Error { message = "bad", code = 1:uint };
}
@entrypoint
fn main() -> int {
    compare f(true) { Success(xs) => print(xs[1]); err => print(err.message); };
    compare f(false) { Success(xs) => print(xs[0]); err => print(err.message); };
    return 0;
}
`, "y\nbad\n"},
	// A plain tag's payload is concrete in its signature; before, the call's
	// strict argument match refused the literal outright.
	{"user_tags", `tag Bag(string[]);
tag Held<T>(T);
type U = Bag | Held<int[]> | nothing;
fn f(k: int) -> U {
    if k == 0 { return Held([1, 2]); }
    if k == 1 { return Bag(["b1", "b2"]); }
    return nothing;
}
fn show(u: U) -> nothing {
    compare u {
        Held(xs) => print(xs[1] to string);
        Bag(ys) => print(ys[1]);
        nothing => print("none");
    };
    return nothing;
}
@entrypoint
fn main() -> int {
    show(f(0));
    show(f(1));
    show(f(2));
    return 0;
}
`, "2\nb2\nnone\n"},
	{"ternary_branches", `fn f(c: bool) -> Option<string[]> { return c ? Some(["kept", "other"]) : nothing; }
@entrypoint
fn main() -> int {
    compare f(true) { Some(xs) => print(xs[0]); nothing => print("none"); };
    compare f(false) { Some(xs) => print(xs[0]); nothing => print("none"); };
    let t: Option<string[]> = true ? Some(["let"]) : nothing;
    compare t { Some(xs) => print(xs[0]); nothing => print("none"); };
    return 0;
}
`, "kept\nnone\nlet\n"},
	{"identity_payloads_unchanged", `fn f() -> Option<string> { return Some("plain"); }
fn g() -> Option<int> { return Some(5); }
@entrypoint
fn main() -> int {
    compare f() { Some(s) => print(s); nothing => print("none"); };
    compare g() { Some(v) => print(v to string); nothing => print("none"); };
    return 0;
}
`, "plain\n5\n"},
	{"drop_on_each_arm", `fn g(c: bool) -> Option<string[]> {
    if c { return Some(["a", "b"]); }
    return nothing;
}
@entrypoint
fn main() -> int {
    let unused: Option<string[]> = Some(["never", "read"]);
    let a = g(true);
    let b = g(false);
    let t: Option<string[]> = compare 1 { 1 => Some(["arm"]); _ => nothing; };
    compare t { Some(xs) => print(xs[0]); nothing => print("none"); };
    compare b { Some(xs) => print(xs[0]); nothing => print("none"); };
    compare a { Some(_) => print("some"); nothing => print("none"); };
    return 0;
}
`, "arm\nnone\nsome\n"},
}

func TestTagPayloadLiteralRunsVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range tagPayloadLiteralRows {
		t.Run(row.name, func(t *testing.T) {
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != 0 || res.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, row.want, res.stderr)
			}
		})
	}
}

func TestTagPayloadLiteralRunsLLVM(t *testing.T) {
	root := repoRoot(t)
	for _, row := range tagPayloadLiteralRows {
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
