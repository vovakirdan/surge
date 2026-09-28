package vm_test

import (
	"os/exec"
	"testing"
)

// Controls for refusing a function declared without a body: function types,
// function-typed parameters and function values, a forward declaration
// completed by @override, and core @intrinsic declarations keep compiling and
// running on both backends.
var bodilessControlRows = []struct{ name, source, want string }{
	{"function_type_alias_parameter", `type Foo = fn(int, int) -> int;
fn foo(a: int, b: int) -> int { return a + b; }
fn bar(a: Foo) -> int { return a(1, 2); }
@entrypoint
fn main() -> int {
    print(bar(foo) to string);
    return 0;
}
`, "3\n"},
	{"function_value_passed_and_called", `fn mul(a: int, b: int) -> int { return a * b; }
fn apply(f: fn(int, int) -> int, x: int, y: int) -> int { return f(x, y); }
@entrypoint
fn main() -> int {
    let g: fn(int, int) -> int = mul;
    print(apply(g, 3, 4) to string);
    print(g(5, 6) to string);
    return 0;
}
`, "12\n30\n"},
	{"forward_declaration_with_override", `fn encode(x: int) -> int;
@override
fn encode(x: int) -> int { return x + 40; }
@entrypoint
fn main() -> int {
    print(encode(2) to string);
    return 0;
}
`, "42\n"},
	{"extern_forward_declaration_with_override", `type Box = { value: int };
extern<Box> {
    fn get(self: &Box) -> int;
    @override fn get(self: &Box) -> int { return self.value + 6; }
}
@entrypoint
fn main() -> int {
    let b = Box { value = 1 };
    print(b.get() to string);
    return 0;
}
`, "7\n"},
	{"core_intrinsic_declarations", `@entrypoint
fn main() -> int {
    let s: string = "hello";
    let xs: int[] = [1, 2, 3];
    print(s.__len() to string);
    print(xs.__len() to string);
    return 0;
}
`, "5\n3\n"},
}

func TestBodilessRefusalKeepsControlsRunningVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range bodilessControlRows {
		t.Run(row.name, func(t *testing.T) {
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != 0 || res.stdout != row.want {
				t.Fatalf("exit=%d stdout=%q want=%q\nstderr:\n%s", res.exitCode, res.stdout, row.want, res.stderr)
			}
		})
	}
}

func TestBodilessRefusalKeepsControlsRunningLLVM(t *testing.T) {
	root := repoRoot(t)
	for _, row := range bodilessControlRows {
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
