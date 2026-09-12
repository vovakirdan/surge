//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"testing"

	"surge/internal/mir"
)

// Follow every startup branch with its own ownership state, including joins.
// Counting drops separately inside each basic block cannot establish this.
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
	pending := []state{{block: start.Entry}}
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
			case "main", "exit", "rt_exit":
				if current.drops != 1 {
					t.Fatalf("startup reaches %s with argv drop count %d at block %d", call.Callee.Name, current.drops, current.block)
				}
				if call.Callee.Name == "main" {
					mains[at] = true
				} else {
					exits[at] = true
				}
			}
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
		name, want string
		argv       []string
	}{
		{"invalid-second", "failed to parse \"abc\" as int: invalid numeric format: \"abc\"\n", []string{"first", "abc"}},
		{"missing-second", "missing argv argument \"second\"\n", []string{"first"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			var result runResult
			if testBackend(t) == backendVM {
				// Successful exit(ErrorLike) writes directly to os.Stderr and
				// returns no VMError. Capture that stream for this serial test.
				result = runArgvFailureVM(t, source, row.argv)
			} else {
				result = runProgramFromSource(t, source, runOptions{argv: row.argv, captureStdout: true})
			}
			if result.exitCode != 1 || result.stdout != "" || result.stderr != row.want {
				t.Fatalf("argv failure path: code=%d stdout=%q stderr=%q", result.exitCode, result.stdout, result.stderr)
			}
		})
	}
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
