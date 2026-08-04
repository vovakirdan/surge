package abimanifest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
