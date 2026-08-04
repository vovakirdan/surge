package llvm

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"surge/internal/abimanifest"
)

const requireTypedCarrierABIToolsEnv = "SURGE_REQUIRE_TYPED_CARRIER_ABI_TOOLS"

func TestTypedCarrierGeneratedLLVMViewParity(t *testing.T) {
	if typedCarrierManifestHash != abimanifest.GeneratedManifestHash {
		t.Fatalf("LLVM manifest hash = %q, Go = %q", typedCarrierManifestHash, abimanifest.GeneratedManifestHash)
	}
	if typedCarrierSentinelSymbol != abimanifest.GeneratedSentinelSymbol {
		t.Fatalf("LLVM sentinel = %q, Go = %q", typedCarrierSentinelSymbol, abimanifest.GeneratedSentinelSymbol)
	}
	if !reflect.DeepEqual(typedCarrierABISchema, abimanifest.GeneratedSchema) {
		t.Fatal("LLVM logical schema lost generated ownership, attributes, or status semantics")
	}
	plan := typedCarrierCallback(t, "rt_value_plan_cross_fn")
	if plan.result != "i32" || !reflect.DeepEqual(plan.resultAttributes, []string{"zeroext"}) {
		t.Fatalf("plan result ABI = %s %#v", plan.result, plan.resultAttributes)
	}
	if len(plan.parameters) != 3 || !reflect.DeepEqual(plan.parameters[0].attributes, []string{"nonnull", "nocapture", "readonly"}) {
		t.Fatalf("plan source attributes drifted: %#v", plan.parameters)
	}
	for _, name := range []string{"rt_value_cross_move_init_fn", "rt_value_cross_clone_init_fn"} {
		crossInit := typedCarrierCallback(t, name)
		if len(crossInit.parameters) == 0 || crossInit.parameters[0].name != "dst" || slices.Contains(crossInit.parameters[0].attributes, "writeonly") {
			t.Fatalf("%s destination forbids rollback reads: %#v", name, crossInit.parameters)
		}
	}
	identity := typedCarrierLLVMRuntimeFunctions[0]
	if identity.resultOwnership != "whole_program_borrow" || !reflect.DeepEqual(identity.resultAttributes, []string{"nonnull"}) {
		t.Fatalf("identity is acceptance-capable or underspecified: %#v", identity)
	}
}

func TestTypedCarrierRequirementIRIsStrongAndExact(t *testing.T) {
	var ir strings.Builder
	emitTypedCarrierABI(&ir)
	text := ir.String()
	for _, required := range []string{
		"@llvm.used = appending global [2 x ptr]",
		"@llvm.global_ctors = appending global",
		"define internal void @__surge_require_typed_carrier_abi() noinline",
		"call void @" + typedCarrierSentinelSymbol + "()",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("strong ABI requirement missing %q:\n%s", required, text)
		}
	}
	if strings.Count(text, typedCarrierSentinelSymbol) != 2 {
		t.Fatalf("requirement must contain used anchor plus call for exact symbol:\n%s", text)
	}
	for _, decl := range runtimeDecls() {
		if decl.name == typedCarrierSentinelSymbol {
			if got := formatRuntimeDecl(&decl); got != "declare void @"+typedCarrierSentinelSymbol+"()" {
				t.Fatalf("sentinel declaration = %q", got)
			}
			return
		}
	}
	t.Fatal("exact typed-carrier sentinel declaration missing")
}

func TestTypedCarrierSentinelSurvivesOptimizationGCAndRejectsMixedHash(t *testing.T) {
	clang := typedCarrierABITool(t, "clang")
	nm := typedCarrierABITool(t, "llvm-nm", "nm")
	root := llvmTestRepoRoot(t)
	modes := []struct {
		name       string
		compile    []string
		link       []string
		requireLTO bool
	}{
		{name: "ordinary"},
		{name: "o3-gc", compile: []string{"-O3", "-ffunction-sections", "-fdata-sections"}, link: []string{"-O3", "-Wl,--gc-sections"}},
		{name: "o3-lto-gc", compile: []string{"-O3", "-flto", "-ffunction-sections", "-fdata-sections"}, link: []string{"-O3", "-flto", "-Wl,--gc-sections"}, requireLTO: true},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			temp := t.TempDir()
			irPath := filepath.Join(temp, "require.ll")
			irObject := filepath.Join(temp, "require.o")
			runtimeObject := filepath.Join(temp, "runtime.o")
			wrongObject := filepath.Join(temp, "wrong.o")
			binary := filepath.Join(temp, "ok")
			writeRequirementModule(t, irPath)
			compileArgs := append([]string{}, mode.compile...)
			compileIR := append(append([]string{}, compileArgs...), "-c", "-x", "ir", irPath, "-o", irObject)
			if output, runErr := exec.Command(clang, compileIR...).CombinedOutput(); runErr != nil {
				if mode.requireLTO {
					typedCarrierABICapabilityUnavailable(t, "LTO unavailable: %v: %s", runErr, output)
				}
				t.Fatalf("compile requirement IR: %v: %s", runErr, output)
			}
			undefined := runTool(t, nm, "--undefined-only", irObject)
			if !strings.Contains(undefined, typedCarrierSentinelSymbol) {
				t.Fatalf("native object has no strong exact-hash undefined reference:\n%s", undefined)
			}
			compileC := append(append([]string{}, compileArgs...), "-std=c11", "-I", filepath.Join(root, "runtime", "native"), "-c", filepath.Join(root, "runtime", "native", "rt_typed_carrier_abi.generated.c"), "-o", runtimeObject)
			if output, runErr := exec.Command(clang, compileC...).CombinedOutput(); runErr != nil {
				if mode.requireLTO {
					typedCarrierABICapabilityUnavailable(t, "LTO runtime compile unavailable: %v: %s", runErr, output)
				}
				t.Fatalf("compile matching runtime: %v: %s", runErr, output)
			}
			linkArgs := append(append([]string{}, mode.link...), irObject, runtimeObject, "-o", binary)
			if output, runErr := exec.Command(clang, linkArgs...).CombinedOutput(); runErr != nil {
				if mode.requireLTO {
					typedCarrierABICapabilityUnavailable(t, "LTO link unavailable: %v: %s", runErr, output)
				}
				t.Fatalf("link matching runtime: %v: %s", runErr, output)
			}
			defined := runTool(t, nm, "--defined-only", binary)
			if countDefinedSymbol(defined, typedCarrierSentinelSymbol) != 1 {
				t.Fatalf("matching runtime must export exactly one retained sentinel:\n%s", defined)
			}

			wrongPath := filepath.Join(temp, "wrong.c")
			wrongHash := strings.Repeat("0", 64)
			wrongSource := fmt.Sprintf("#include <stdint.h>\n__attribute__((used, visibility(\"default\"), noinline)) void %s%s(void) { static volatile uint8_t anchor; (void)anchor; }\n", abimanifest.SentinelPrefix, wrongHash)
			if err := os.WriteFile(wrongPath, []byte(wrongSource), 0o600); err != nil {
				t.Fatal(err)
			}
			compileWrong := append(append([]string{}, compileArgs...), "-std=c11", "-c", wrongPath, "-o", wrongObject)
			if output, runErr := exec.Command(clang, compileWrong...).CombinedOutput(); runErr != nil {
				t.Fatalf("compile mixed runtime: %v: %s", runErr, output)
			}
			badArgs := append(append([]string{}, mode.link...), irObject, wrongObject, "-o", filepath.Join(temp, "bad"))
			output, runErr := exec.Command(clang, badArgs...).CombinedOutput()
			if runErr == nil {
				t.Fatal("mixed manifest hashes linked successfully")
			}
			if !bytes.Contains(output, []byte(typedCarrierSentinelSymbol)) {
				t.Fatalf("mixed-hash link did not name exact missing sentinel: %s", output)
			}
		})
	}
}

func TestTypedCarrierStrictToolModeRejectsMissingTools(t *testing.T) {
	const probeEnv = "SURGE_TYPED_CARRIER_ABI_MISSING_TOOL_PROBE"
	if os.Getenv(probeEnv) == "1" {
		typedCarrierABITool(t, "surge-deliberately-missing-typed-carrier-tool")
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestTypedCarrierStrictToolModeRejectsMissingTools$")
	command.Env = []string{
		requireTypedCarrierABIToolsEnv + "=1",
		probeEnv + "=1",
		"PATH=" + t.TempDir(),
	}
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("strict typed-carrier ABI mode accepted a missing required tool: %s", output)
	}
	if !bytes.Contains(output, []byte("required typed-carrier ABI proof tool unavailable")) {
		t.Fatalf("strict missing-tool failure is not actionable: %s", output)
	}
}

func typedCarrierABITool(t *testing.T, names ...string) string {
	t.Helper()
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	message := fmt.Sprintf("required typed-carrier ABI proof tool unavailable: %s", strings.Join(names, " or "))
	if os.Getenv(requireTypedCarrierABIToolsEnv) == "1" {
		t.Fatal(message)
	}
	t.Skip(message)
	return ""
}

func typedCarrierABICapabilityUnavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv(requireTypedCarrierABIToolsEnv) == "1" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func writeRequirementModule(t *testing.T, path string) {
	t.Helper()
	var ir strings.Builder
	ir.WriteString("target triple = \"x86_64-linux-gnu\"\n\n")
	emitTypedCarrierABI(&ir)
	fmt.Fprintf(&ir, "declare void @%s()\n\n", typedCarrierSentinelSymbol)
	ir.WriteString("define i32 @main() {\nentry:\n  ret i32 0\n}\n")
	if err := os.WriteFile(path, []byte(ir.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func typedCarrierCallback(t *testing.T, name string) typedCarrierLLVMFunctionDecl {
	t.Helper()
	for _, callback := range typedCarrierLLVMCallbacks {
		if callback.name == name {
			return callback
		}
	}
	t.Fatalf("callback %q missing", name)
	return typedCarrierLLVMFunctionDecl{}
}

func runTool(t *testing.T, name string, args ...string) string {
	t.Helper()
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, output)
	}
	return string(output)
}

func countDefinedSymbol(output, symbol string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[len(fields)-1] == symbol {
			count++
		}
	}
	return count
}

func llvmTestRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
