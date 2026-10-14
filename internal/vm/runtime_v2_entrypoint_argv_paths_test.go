//go:build runtime_v2_pending

package vm_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"surge/internal/mir"
)

// Follow every parsing branch until the user body is invoked or a parsing
// error is reported. At both boundaries argv must already be released. The
// coroutine's later resume paths no longer own argv and are checked at run time.
func requireEntrypointArgvPaths(t *testing.T, start *mir.Func, argv mir.LocalID, wantParsers int) {
	t.Helper()
	type state struct {
		block mir.BlockID
		drops int
	}
	type site struct {
		block mir.BlockID
		index int
	}
	seen := make(map[state]bool)
	parsers, mains, exits := make(map[site]bool), make(map[site]bool), make(map[site]bool)
	entry := mir.NoBlockID
	for _, block := range start.Blocks {
		for _, instruction := range block.Instrs {
			if instruction.Kind == mir.InstrCall && instruction.Call.Callee.Name == "rt_argv" {
				if entry != mir.NoBlockID {
					t.Fatal("multiple argv acquisition sites")
				}
				entry = block.ID
			}
		}
	}
	if entry == mir.NoBlockID {
		t.Fatal("root never acquires argv")
	}
	pending := []state{{block: entry}}
	for len(pending) != 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[current] {
			continue
		}
		seen[current] = true
		if current.block < 0 || int(current.block) >= len(start.Blocks) {
			t.Fatalf("startup reaches invalid block %d", current.block)
		}
		block := &start.Blocks[current.block]
		boundary := false
		for index, instruction := range block.Instrs {
			if instruction.Kind == mir.InstrDrop && instruction.Drop.Place.Local == argv {
				current.drops++
				if current.drops != 1 {
					t.Fatalf("startup drops argv twice on a reachable path at block %d", current.block)
				}
			}
			if instruction.Kind != mir.InstrCall {
				continue
			}
			call, at := instruction.Call, site{current.block, index}
			switch call.Callee.Name {
			case "from_str":
				parsers[at] = true
				if current.drops != 0 || len(call.Args) != 1 || len(call.ArgContracts) != 1 || call.ArgContracts[0] != mir.ArgContractBorrow {
					t.Fatalf("parser lost its live borrowing contract at block %d: %+v", current.block, call)
				}
				argument := call.Args[0]
				if argument.Kind != mir.OperandAddrOf || argument.Place.Local != argv || len(argument.Place.Proj) != 1 || argument.Place.Proj[0].Kind != mir.PlaceProjIndex {
					t.Fatalf("parser must borrow the actual argv member: %+v", argument)
				}
			case "__surge_root$entry", "__entry_error_code":
				if current.drops != 1 {
					t.Fatalf("startup reaches %s with argv drop count %d at block %d", call.Callee.Name, current.drops, current.block)
				}
				if call.Callee.Name == "__surge_root$entry" {
					mains[at] = true
				} else {
					exits[at] = true
				}
				boundary = true
			}
			if boundary {
				break
			}
		}
		if boundary {
			continue
		}
		switch block.Term.Kind {
		case mir.TermGoto:
			pending = append(pending, state{block.Term.Goto.Target, current.drops})
		case mir.TermIf:
			pending = append(pending, state{block.Term.If.Then, current.drops}, state{block.Term.If.Else, current.drops})
		case mir.TermReturn:
			if current.drops != 1 {
				t.Fatalf("startup returns with argv drop count %d at block %d", current.drops, current.block)
			}
		default:
			t.Fatalf("unproved startup terminator %s at block %d", block.Term.Kind, current.block)
		}
	}
	if len(parsers) != wantParsers || len(mains) != 1 || len(exits) < wantParsers {
		t.Fatalf("startup path census: parsers=%d want=%d main=%d exits=%d", len(parsers), wantParsers, len(mains), len(exits))
	}
}

func TestRuntimeV2EntrypointArgvFailurePaths(t *testing.T) {
	const source = `@entrypoint("argv") fn main(first: string, second: int) {
    print("unexpected-main-entry");
}`
	for _, row := range []struct {
		name, kind, want string
		argv             []string
	}{
		{"invalid-second", "int", "failed to parse \"abc\" as int: invalid numeric format: \"abc\"\n", []string{"first", "abc"}},
		{"invalid-unsigned", "uint64", "failed to parse \"abc\" as uint: invalid numeric format: \"abc\"\n", []string{"first", "abc"}},
		{"invalid-float", "float", "failed to parse \"abc\" as float: invalid numeric format: \"abc\"\n", []string{"first", "abc"}},
		{"missing-second", "int", "missing argv argument \"second\"\n", []string{"first"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			sourceCode := strings.Replace(source, "second: int", "second: "+row.kind, 1)
			var result runResult
			if testBackend(t) == backendVM {
				// Successful exit(ErrorLike) writes directly to os.Stderr and
				// returns no VMError. Capture that stream for this serial test.
				result = runArgvFailureVM(t, sourceCode, row.argv)
			} else {
				result = runArgvFailureNative(t, sourceCode, row.argv)
			}
			if result.exitCode != 1 || result.stdout != "" || result.stderr != row.want {
				t.Fatalf("argv failure path: code=%d stdout=%q stderr=%q", result.exitCode, result.stdout, result.stderr)
			}
		})
	}
}

func runArgvFailureNative(t *testing.T, sourceCode string, argv []string) runResult {
	t.Helper()
	binary := buildLLVMProgramFromSource(t, sourceCode)
	logPath := filepath.Join(t.TempDir(), "memcheck.log")
	args := []string{"--leak-check=full", "--show-leak-kinds=all", "--error-exitcode=97", "--log-file=" + logPath, binary}
	args = append(args, argv...)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "valgrind", args...)
	command.Env = envWithStdlib(repoRoot(t))
	stdout, stderr, code := runCommand(t, command, "")
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	bytes, blocks := parseValgrindInUseAtExit(t, string(log))
	if code != 1 || bytes != 0 || blocks != 0 || hasValgrindMemcheckError(string(log)) ||
		!strings.Contains(string(log), "ERROR SUMMARY: 0 errors from 0 contexts") {
		t.Fatalf("startup failure must release all storage: code=%d bytes=%d blocks=%d\n%s", code, bytes, blocks, log)
	}
	return runResult{exitCode: code, stdout: stdout, stderr: stderr}
}

func runArgvFailureVM(t *testing.T, source string, argv []string) runResult {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "argv-stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = file
	defer func() {
		os.Stderr = previous
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	result := runProgramFromSource(t, source, runOptions{argv: argv, captureStdout: true})
	if result.stderr != "" {
		t.Fatalf("startup produced a VM error instead of its known exit: %s", result.stderr)
	}
	output, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	result.stderr = string(output)
	return result
}
