package vm_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"
)

// `@drop` of a binding that holds the borrow an implicit `&self` __to took used
// to release the borrow and nothing else, so scope exit freed the binding again.
// Measured before the fix on the VM: `panic VM3301: use-after-free: local "t"
// used after drop`. Natively a leaf handle is nulled after its release, so the
// second drop is silent; a struct result runs its drop glue twice on one
// address, which valgrind reports. Every source is frozen with its sha256.

// The user-extern witness: `t = h;` converts through __to(self: &Named, …).
const runtimeV2ConvertedDropUserSource = "type Named = { name: string };\n" +
	"extern<Named> {\n" +
	"    fn __to(self: &Named, _: string) -> string {\n" +
	"        return \"converted\";\n" +
	"    }\n" +
	"}\n" +
	"\n" +
	"fn drop_assigned() -> nothing {\n" +
	"    let h: Named = Named { name = \"n\" };\n" +
	"    let mut t: string = \"\";\n" +
	"    t = h;\n" +
	"    @drop t;\n" +
	"    return nothing;\n" +
	"}\n" +
	"\n" +
	"@entrypoint\n" +
	"fn main() -> int {\n" +
	"    drop_assigned();\n" +
	"    return 0;\n" +
	"}\n"

// The core witness: `b = s;` converts through core's __to(self: &string, _:
// byte[]); a Copy source such as `int` never reaches the borrow at all.
const runtimeV2ConvertedDropCoreSource = "fn drop_core_converted() -> nothing {\n" +
	"    let s: string = \"ab\";\n" +
	"    let mut b: byte[] = [];\n" +
	"    b = s;\n" +
	"    @drop b;\n" +
	"    return nothing;\n" +
	"}\n" +
	"\n" +
	"@entrypoint\n" +
	"fn main() -> int {\n" +
	"    drop_core_converted();\n" +
	"    return 0;\n" +
	"}\n"

// The struct result: its members are built by string concatenation so they are
// heap blocks a second glue run would free twice.
const runtimeV2ConvertedDropStructSource = "type Named = { name: string };\n" +
	"type Boxed = { text: string };\n" +
	"extern<Named> {\n" +
	"    fn __to(self: &Named, _: Boxed) -> Boxed {\n" +
	"        return Boxed { text = \"box\" + \"ed\" };\n" +
	"    }\n" +
	"}\n" +
	"\n" +
	"fn drop_assigned() -> nothing {\n" +
	"    let h: Named = Named { name = \"n\" + \"amed\" };\n" +
	"    let mut b: Boxed = Boxed { text = \"\" };\n" +
	"    b = h;\n" +
	"    @drop b;\n" +
	"    return nothing;\n" +
	"}\n" +
	"\n" +
	"@entrypoint\n" +
	"fn main() -> int {\n" +
	"    drop_assigned();\n" +
	"    print(\"drop-converted-struct-witness\");\n" +
	"    return 0;\n" +
	"}\n"

// The control: the same program with a plain struct store in place of the
// conversion, which has always dropped once.
const runtimeV2ConvertedDropStructControlSource = "type Named = { name: string };\n" +
	"type Boxed = { text: string };\n" +
	"\n" +
	"fn drop_assigned() -> nothing {\n" +
	"    let h: Named = Named { name = \"n\" + \"amed\" };\n" +
	"    let mut b: Boxed = Boxed { text = \"\" };\n" +
	"    b = Boxed { text = \"box\" + \"ed\" };\n" +
	"    @drop b;\n" +
	"    return nothing;\n" +
	"}\n" +
	"\n" +
	"@entrypoint\n" +
	"fn main() -> int {\n" +
	"    drop_assigned();\n" +
	"    print(\"drop-converted-struct-witness\");\n" +
	"    return 0;\n" +
	"}\n"

type convertedDropRow struct {
	name, source, digest string
}

func requireConvertedDropSource(t *testing.T, row convertedDropRow) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(row.source))); got != row.digest {
		t.Fatalf("PRECONDITION: frozen source %s changed: %s", row.name, got)
	}
}

func TestRuntimeV2DropOfConvertedBindingDoesNotPanicOnVM(t *testing.T) {
	requireVMBackend(t)
	for _, row := range []convertedDropRow{
		{"user_extern_to", runtimeV2ConvertedDropUserSource, "1b0036bb59f848ebb388bd5131216d22ea1922a64e5f91c127067e76a767cd30"},
		{"core_string_to_bytes", runtimeV2ConvertedDropCoreSource, "2eaa39e7bc37e7b88edfb6c43559e419f6a1a465c73d26ac755f50968e65cd2d"},
	} {
		t.Run(row.name, func(t *testing.T) {
			requireConvertedDropSource(t, row)
			res := runProgramFromSource(t, row.source, runOptions{})
			if res.exitCode != 0 || strings.Contains(res.stderr, "VM3301") {
				t.Fatalf("a dropped converted binding was freed twice (exit %d)\nstderr:\n%s", res.exitCode, res.stderr)
			}
		})
	}
}

func TestRuntimeV2DropOfConvertedStructResultFreesOnce(t *testing.T) {
	for _, row := range []convertedDropRow{
		{"converted_struct_result", runtimeV2ConvertedDropStructSource, "9bd427d925306e011cf79b217f0d52ab8ffb19091361b77474db97dbba435c1c"},
		{"control_without_conversion", runtimeV2ConvertedDropStructControlSource, "7b512caf97d36f362e055e13116f0c43f5e213ee6e89202b2d015e4e8dbcdd10"},
	} {
		t.Run(row.name, func(t *testing.T) {
			requireConvertedDropSource(t, row)
			outputPath := buildRuntimeV2CrossingSource(t, row.source, nil)
			stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, envWithStdlib(repoRoot(t)), 120*time.Second)
			if hasValgrindMemcheckError(stderr) {
				t.Fatalf("drop glue ran twice over one struct\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if exitCode != 0 || !strings.Contains(stdout, "drop-converted-struct-witness") {
				t.Fatalf("program did not complete (exit %d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
			}
		})
	}
}
