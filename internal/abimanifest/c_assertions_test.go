package abimanifest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const requireTypedCarrierABIToolsEnv = "SURGE_REQUIRE_TYPED_CARRIER_ABI_TOOLS"

func TestGeneratedCLayoutAndSignatureAssertionsCompile(t *testing.T) {
	root := testRepoRoot(t)
	source := filepath.Join(root, "runtime", "native", "rt_typed_carrier_abi.generated.c")
	include := filepath.Join(root, "runtime", "native")
	compilers := 0
	for _, name := range []string{"clang", "gcc"} {
		compiler, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		compilers++
		command := exec.Command(compiler, "-std=c11", "-Wall", "-Wextra", "-Wpedantic", "-Werror", "-I", include, "-fsyntax-only", source)
		if output, runErr := command.CombinedOutput(); runErr != nil {
			t.Fatalf("%s rejected generated C ABI assertions: %v\n%s", name, runErr, output)
		}
	}
	if compilers == 0 {
		t.Skip("no C compiler available")
	}
	checks, err := os.ReadFile(filepath.Join(include, "rt_typed_carrier_abi_checks.generated.h"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range [][]byte{[]byte("_Static_assert"), []byte("offsetof("), []byte("__builtin_types_compatible_p")} {
		if !bytes.Contains(checks, required) {
			t.Fatalf("generated C assertion view missing %q", required)
		}
	}
	header, err := os.ReadFile(filepath.Join(include, "rt_typed_carrier_abi.generated.h"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte("rt_cross_sidecar_shape"), []byte(" sidecars;")} {
		if bytes.Contains(header, forbidden) {
			t.Fatalf("generated cross plan retains plan-owned shape storage %q", forbidden)
		}
	}
	for _, required := range [][]byte{[]byte("size_t remaining_allocations;"), []byte("size_t sidecar_count;")} {
		if !bytes.Contains(header, required) {
			t.Fatalf("generated C ABI missing exact allocation budget %q", required)
		}
	}
}

func TestGeneratedCAssertionsRejectIndependentDeclarationDrift(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang unavailable")
	}
	root := testRepoRoot(t)
	native := filepath.Join(root, "runtime", "native")
	tests := map[string]struct {
		old string
		new string
	}{
		"handle layout": {
			old: "    uintptr_t opaque;",
			new: "    uint32_t opaque;",
		},
		"callback signature": {
			old: "typedef uint64_t (*rt_key_hash_fn)(const void* key);",
			new: "typedef uint32_t (*rt_key_hash_fn)(const void* key);",
		},
		"allocator count budget": {
			old: "    size_t remaining_allocations;",
			new: "    uint32_t remaining_allocations;",
		},
		"cross plan allocation count": {
			old: "    size_t sidecar_count;",
			new: "    uint32_t sidecar_count;",
		},
	}
	for name, mutation := range tests {
		t.Run(name, func(t *testing.T) {
			temp := t.TempDir()
			for _, file := range []string{"rt_typed_carrier_abi.generated.c", "rt_typed_carrier_abi.generated.h", "rt_typed_carrier_abi_checks.generated.h"} {
				data, readErr := os.ReadFile(filepath.Join(native, file))
				if readErr != nil {
					t.Fatal(readErr)
				}
				if file == "rt_typed_carrier_abi.generated.h" {
					updated := strings.Replace(string(data), mutation.old, mutation.new, 1)
					if updated == string(data) {
						t.Fatalf("mutation anchor missing: %q", mutation.old)
					}
					data = []byte(updated)
				}
				if writeErr := os.WriteFile(filepath.Join(temp, file), data, 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}
			}
			command := exec.Command(clang, "-std=c11", "-I", temp, "-fsyntax-only", filepath.Join(temp, "rt_typed_carrier_abi.generated.c"))
			if output, runErr := command.CombinedOutput(); runErr == nil {
				t.Fatalf("independent %s drift passed assertions:\n%s", name, output)
			}
		})
	}
}

func TestGeneratedCHeaderDirectCXXLinkageAndExactSentinel(t *testing.T) {
	clang := requireTypedCarrierABITool(t, "clang")
	clangXX := requireTypedCarrierABITool(t, "clang++")
	root := testRepoRoot(t)
	native := filepath.Join(root, "runtime", "native")
	temp := t.TempDir()
	runtimeObject := filepath.Join(temp, "runtime.o")
	runABICommand(t, clang, []string{
		"-std=c11", "-Wall", "-Wextra", "-Wpedantic", "-Werror",
		"-I", native, "-c", filepath.Join(native, "rt_typed_carrier_abi.generated.c"),
		"-o", runtimeObject,
	})

	consumerSource := filepath.Join(temp, "consumer.cc")
	consumer := `#include "rt_typed_carrier_abi.generated.h"
#include <array>
#include <atomic>
#include <cstring>
#include <thread>
#include <type_traits>

static_assert(std::is_standard_layout<rt_cross_plan>::value, "CrossPlan must be standard-layout");
static_assert(std::is_trivially_copyable<rt_cross_plan>::value, "CrossPlan must be POD-copyable");

int main() {
    rt_cross_plan original = {};
    original.sidecar_bytes = 13;
    original.total_bytes = 101;
    original.sidecar_count = 2;
    std::atomic<unsigned> failures{0};
    std::array<std::thread, 8> workers;
    for (size_t index = 0; index < workers.size(); ++index) {
        rt_cross_plan copy = original;
        workers[index] = std::thread([copy, index, &failures]() mutable {
            copy.sidecar_bytes += index;
            if (copy.total_bytes != 101 || copy.sidecar_count != 2) {
                ++failures;
            }
        });
    }
    for (std::thread& worker : workers) {
        worker.join();
    }
    if (failures != 0 || original.sidecar_bytes != 13) {
        return 2;
    }
    const uint8_t* hash = rt_typed_carrier_abi_manifest_hash();
    SURGE_TYPED_CARRIER_ABI_SENTINEL();
    return std::strcmp(reinterpret_cast<const char*>(hash), SURGE_TYPED_CARRIER_ABI_MANIFEST_HASH);
}
`
	if err := os.WriteFile(consumerSource, []byte(consumer), 0o600); err != nil {
		t.Fatal(err)
	}
	consumerObject := filepath.Join(temp, "consumer.o")
	runABICommand(t, clangXX, []string{
		"-std=c++17", "-Wall", "-Wextra", "-Wpedantic", "-Werror",
		"-pthread", "-I", native, "-c", consumerSource, "-o", consumerObject,
	})
	matchingExecutable := filepath.Join(temp, "matching")
	runABICommand(t, clangXX, []string{"-pthread", consumerObject, runtimeObject, "-o", matchingExecutable})
	runABICommand(t, matchingExecutable, nil)

	assertCXXLinkRejectsExactSentinel(t, clangXX, []string{"-pthread", consumerObject, "-o", filepath.Join(temp, "absent")}, "absent C runtime")

	wrongHash := strings.Repeat("0", 64)
	wrongSource := filepath.Join(temp, "wrong_runtime.c")
	wrongRuntime := fmt.Sprintf(`#include <stdint.h>
const uint8_t rt_typed_carrier_abi_manifest_identity[] = "%s";
const uint8_t* rt_typed_carrier_abi_manifest_hash(void) {
    return rt_typed_carrier_abi_manifest_identity;
}
void %s%s(void) {}
`, wrongHash, SentinelPrefix, wrongHash)
	if err := os.WriteFile(wrongSource, []byte(wrongRuntime), 0o600); err != nil {
		t.Fatal(err)
	}
	wrongObject := filepath.Join(temp, "wrong_runtime.o")
	runABICommand(t, clang, []string{"-std=c11", "-Wall", "-Wextra", "-Wpedantic", "-Werror", "-c", wrongSource, "-o", wrongObject})
	assertCXXLinkRejectsExactSentinel(t, clangXX, []string{"-pthread", consumerObject, wrongObject, "-o", filepath.Join(temp, "mixed")}, "mixed manifest hash")
}

func TestGeneratedCContainsNoCarrierPlaceholderImplementations(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(testRepoRoot(t), "runtime", "native", "rt_typed_carrier_abi.generated.c"))
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"move_init", "copy_init", "clone_init", "drop_in_place", "plan_cross", "cross_move_init", "cross_clone_init"} {
		if bytes.Contains(source, []byte(operation+"(")) {
			t.Fatalf("generated C source contains placeholder carrier operation %q", operation)
		}
	}
}

func requireTypedCarrierABITool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}
	if os.Getenv(requireTypedCarrierABIToolsEnv) == "1" {
		t.Fatalf("%s is required when %s=1", name, requireTypedCarrierABIToolsEnv)
	}
	t.Skipf("%s unavailable", name)
	return ""
}

func runABICommand(t *testing.T, executable string, arguments []string) {
	t.Helper()
	command := exec.Command(executable, arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", executable, strings.Join(arguments, " "), err, output)
	}
}

func assertCXXLinkRejectsExactSentinel(t *testing.T, clangXX string, arguments []string, scenario string) {
	t.Helper()
	command := exec.Command(clangXX, arguments...)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("%s unexpectedly linked without exact ABI sentinel", scenario)
	}
	if !bytes.Contains(output, []byte(GeneratedSentinelSymbol)) {
		t.Fatalf("%s failed for the wrong reason; missing exact sentinel %q:\n%s", scenario, GeneratedSentinelSymbol, output)
	}
}
