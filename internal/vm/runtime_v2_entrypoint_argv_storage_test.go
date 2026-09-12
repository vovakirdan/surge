//go:build runtime_v2_pending

package vm_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"surge/internal/mir"
)

var entrypointArgvStorageCases = []struct {
	name, source, want string
	argv               []string
}{
	{"number", `@entrypoint("argv") fn main(value: uint) {
    if value != 42:uint { panic("argv number changed"); }
    print("argv-number-ok");
}`, "argv-number-ok\n", []string{"42"}},
	{"string", `@entrypoint("argv") fn main(value: string) {
    print(value);
}`, "a string long enough to have independent heap storage\n", []string{"a string long enough to have independent heap storage"}},
	{"user-parser", `type Parsed = { text: string };
extern<Parsed> {
    pub fn from_str(text: &string) -> Erring<Parsed, Error> {
        return Success(Parsed { text = clone(text) });
    }
}
@entrypoint("argv") fn main(value: Parsed) { print(value.text); }
`, "the custom parser retains its borrowed argument\n", []string{"the custom parser retains its borrowed argument"}},
	{"default", `@entrypoint("argv") fn main(value: uint = 7:uint) {
    if value != 7:uint { panic("argv default changed"); }
    print("argv-default-ok");
}`, "argv-default-ok\n", nil},
}

func TestRuntimeV2EntrypointArgvBorrowsAndReleasesStorage(t *testing.T) {
	for _, row := range entrypointArgvStorageCases {
		t.Run(row.name, func(t *testing.T) {
			module, _, _ := compileToMIRFromSource(t, row.source)
			var start *mir.Func
			for _, function := range module.Funcs {
				if function.Name == "__surge_start" {
					start = function
				}
			}
			if start == nil {
				t.Fatal("compiled source has no startup function")
			}
			argv := mir.NoLocalID
			for index, local := range start.Locals {
				if local.Name == "argv" {
					argv = mir.LocalID(index) //nolint:gosec // bounded by the MIR local table
				}
				if strings.HasPrefix(local.Name, "arg_str") {
					t.Fatal("startup created an unowned copy of an argv string")
				}
			}
			if argv == mir.NoLocalID {
				t.Fatal("startup did not allocate the expected argv array")
			}
			parsers, mainCalls := 0, 0
			for _, block := range start.Blocks {
				dropped := false
				for _, instruction := range block.Instrs {
					if instruction.Kind == mir.InstrDrop && instruction.Drop.Place.Local == argv {
						if dropped {
							t.Fatal("startup drops argv twice on one path")
						}
						dropped = true
					}
					if instruction.Kind != mir.InstrCall {
						continue
					}
					call := instruction.Call
					if call.Callee.Name == "from_str" {
						parsers++
						if dropped || len(call.Args) != 1 || len(call.ArgContracts) != 1 || call.ArgContracts[0] != mir.ArgContractBorrow {
							t.Fatalf("parser lost its live borrowing contract: %+v", call)
						}
						argument := call.Args[0]
						if argument.Kind != mir.OperandAddrOf || argument.Place.Local != argv || len(argument.Place.Proj) != 1 || argument.Place.Proj[0].Kind != mir.PlaceProjIndex {
							t.Fatalf("parser must borrow the actual argv member: %+v", argument)
						}
					}
					if call.Callee.Name == "main" {
						mainCalls++
						if !dropped {
							t.Fatal("argv remains owned when the user entrypoint begins")
						}
					}
				}
			}
			if parsers != 1 || mainCalls != 1 {
				t.Fatalf("source did not exercise parser/main: %d/%d", parsers, mainCalls)
			}
		})
	}
}

func TestRuntimeV2EntrypointArgvStorageValgrindZero(t *testing.T) {
	if testing.Short() || os.Getenv("SURGE_SKIP_TIMEOUT_TESTS") != "0" {
		t.Fatal("argv storage proof requires non-short execution and SURGE_SKIP_TIMEOUT_TESTS=0")
	}
	for _, tool := range []string{"clang", "ar", "valgrind"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("argv storage proof requires %s: %v", tool, err)
		}
	}
	for _, row := range entrypointArgvStorageCases {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv(backendEnvVar, backendVM)
			result := runProgramFromSource(t, row.source, runOptions{argv: row.argv, captureStdout: true})
			if result.exitCode != 0 || result.stdout != row.want || result.stderr != "" {
				t.Fatalf("VM argv lifetime: code=%d stdout=%q stderr=%q", result.exitCode, result.stdout, result.stderr)
			}
			t.Setenv(backendEnvVar, backendLLVM)
			binary := buildRuntimeV2CrossingSource(t, row.source, nil)
			ctx, cancel := context.WithTimeout(t.Context(), mtScaledTimeout(t, 120*time.Second))
			defer cancel()
			args := append([]string{"--leak-check=full", "--show-leak-kinds=all", "--default-suppressions=no", "--error-exitcode=97", binary}, row.argv...)
			command := exec.CommandContext(ctx, "valgrind", args...)
			command.Dir, command.Env = repoRoot(t), asyncAllocationEnvironment(t, "1")
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Run(); err != nil || stdout.String() != row.want {
				t.Fatalf("native argv lifetime: %v stdout=%q stderr=%s", err, stdout.String(), stderr.String())
			}
			bytes, blocks := parseValgrindInUseAtExit(t, stderr.String())
			if bytes != 0 || blocks != 0 || hasValgrindMemcheckError(stderr.String()) || !strings.Contains(stderr.String(), "ERROR SUMMARY: 0 errors from 0 contexts") {
				t.Fatalf("argv storage requires physical zero: %d bytes/%d blocks\n%s", bytes, blocks, stderr.String())
			}
			t.Logf("VM/native exact stdout; physical Memcheck 0 bytes/0 blocks/0 errors")
		})
	}
}
