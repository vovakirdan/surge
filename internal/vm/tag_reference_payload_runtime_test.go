package vm_test

import (
	"os/exec"
	"testing"
)

// A reference handed to a by-value parameter instantiated with a reference type
// is carried as the address it is (`Some::<&mut int>(&mut r[0])`), and a
// compare over a value typed by its tag constructor keeps its `nothing` arm
// without asking the constructor's union for a case it lacks. Both backends run
// every row and print the same thing.
var tagReferencePayloadRows = []struct{ name, source, want string }{
	{"some_ref_from_helper", `fn oa(r: &mut int[]) -> Option<&mut int> { return Some::<&mut int>(&mut r[0]); }
fn g(k: &mut int[]) -> nothing {
    let e = oa(k);
    compare e { Some(v) => { print((*v) to string); *v = 5; } nothing => { print("none"); } };
    return nothing;
}
@entrypoint
fn main() -> int {
    let mut x: int[] = [1, 2, 3];
    g(&mut x);
    print(x[0] to string);
    return 0;
}
`, "1\n5\n"},
	{"bare_some_ref", `fn g(k: &mut int[]) -> nothing {
    let e = Some::<&mut int>(&mut k[0]);
    compare e { Some(v) => { print((*v) to string); *v = 5; } nothing => { print("none"); } };
    return nothing;
}
@entrypoint
fn main() -> int {
    let mut x: int[] = [1, 2, 3];
    g(&mut x);
    print(x[0] to string);
    return 0;
}
`, "1\n5\n"},
	{"async_poll_body", `async fn g(k: &mut int[]) -> int {
    let e = Some::<&mut int>(&mut k[0]);
    compare e { Some(v) => { print((*v) to string); *v = 5; } nothing => { print("none"); } };
    return 0;
}
@entrypoint
fn main() -> int {
    let mut x: int[] = [1, 2, 3];
    let r = g(&mut x).await();
    print(x[0] to string);
    return 0;
}
`, "1\n5\n"},
	{"bare_some_value", `@entrypoint
fn main() -> int {
    let f = Some(7);
    compare f { Some(v) => { print(v to string); } nothing => { print("none"); } };
    let g = Some(8);
    compare g { nothing => { print("none"); } _ => { print("some"); } };
    return 0;
}
`, "7\nsome\n"},
	{"choice_of_some_and_nothing", `fn show(e: Option<int>) -> nothing { compare e { Some(v) => { print("S" + (v to string)); } nothing => { print("N"); } }; return nothing; }
fn mk(c: bool) -> Option<int> { return c ? Some(1) : nothing; }
fn pick(c: bool) -> nothing {
    let e = c ? Some(1) : nothing;
    compare e { Some(v) => { print("S" + (v to string)); } nothing => { print("N"); } };
    let a: Option<int> = c ? nothing : Some(2);
    show(a);
    show(c ? Some(3) : nothing);
    show(mk(c));
    let k = c ? 1 : 2;
    let d = compare k { 1 => Some(4); _ => nothing; };
    show(d);
    return nothing;
}
@entrypoint
fn main() -> int {
    pick(true);
    pick(false);
    return 0;
}
`, "S1\nN\nS3\nS1\nS4\nN\nS2\nN\nN\nN\n"},
	{"generic_identity_of_ref", `fn idg<T>(x: T) -> T { return x; }
fn g(k: &mut int[]) -> nothing {
    let p: &mut int = &mut k[0];
    let q = idg::<&mut int>(p);
    *q = 9;
    return nothing;
}
@entrypoint
fn main() -> int {
    let mut x: int[] = [1, 2, 3];
    g(&mut x);
    print(x[0] to string);
    return 0;
}
`, "9\n"},
}

func TestTagReferencePayloadRunsVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range tagReferencePayloadRows {
		t.Run(row.name, func(t *testing.T) {
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != 0 || res.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, row.want, res.stderr)
			}
		})
	}
}

func TestTagReferencePayloadRunsLLVM(t *testing.T) {
	root := repoRoot(t)
	for _, row := range tagReferencePayloadRows {
		t.Run(row.name, func(t *testing.T) {
			outputPath := buildLLVMProgramFromSource(t, row.source)
			cmd := exec.Command(outputPath)
			cmd.Dir = root
			stdout, stderr, exitCode := runCommand(t, cmd, "")
			if exitCode != 0 || stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", exitCode, stdout, row.want, stderr)
			}
		})
	}
}
