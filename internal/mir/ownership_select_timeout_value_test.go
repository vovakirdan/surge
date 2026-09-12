package mir_test

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// A timeout is evaluated once before polling. Its saved uint must own a
// reference while the select is suspended, independently of the source value.
func TestOwnershipSelectTimeoutValueSurvivesPending(t *testing.T) {
	for _, row := range []struct{ name, parameter, value string }{
		{"inline", "", "1:uint"},
		{"parameter", ", ms: uint", "ms"},
		{"projected", ", config: Config", "config.ms"},
		{"wide", "", "18446744073709551617:uint"},
		{"call", "", "duration()"},
	} {
		t.Run(row.name, func(t *testing.T) {
			src := ownershipTaskPrelude + fmt.Sprintf(`
@intrinsic fn timeout<T>(task: Task<T>, ms: uint) -> TaskResult<T>;
type Config = { ms: uint };
fn duration() -> uint { return 1:uint; }
async fn selected(first: Task<int>, second: Task<int>%s) -> int {
    return select {
        timeout(first, %s) => 1;
        second.await() => 2;
    };
}
`, row.parameter, row.value)
			compiled := compileCrossingMIR(t, src, nil)
			if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
				t.Fatal(err)
			}
			var poll *mir.Func
			for _, fn := range compiled.mod.Funcs {
				if fn != nil && baseName(fn.Name) == "selected$poll" {
					if poll != nil {
						t.Fatal("duplicate selected poll")
					}
					poll = fn
				}
			}
			if poll == nil {
				t.Fatal("missing selected poll")
			}
			ms := mir.NoLocalID
			for id, local := range poll.Locals {
				if !strings.HasPrefix(local.Name, "tmp_select_ms") {
					continue
				}
				if ms != mir.NoLocalID || types.Label(compiled.types, local.Type) != "uint" ||
					local.Flags&mir.LocalFlagOwnsHeap == 0 || !compiled.types.IsRefCountedScalar(local.Type) {
					t.Fatalf("timeout must have one counted uint owner: %+v", local)
				}
				ms = mir.LocalID(id)
			}
			if ms == mir.NoLocalID {
				t.Fatal("missing evaluate-once timeout local")
			}
			selects, calls := 0, 0
			for _, block := range poll.Blocks {
				for _, ins := range block.Instrs {
					if ins.Kind == mir.InstrCall && baseName(ins.Call.Callee.Name) == "duration" {
						calls++
					}
					if ins.Kind != mir.InstrSelect {
						continue
					}
					selects++
					assertSelectTaskPendingTransfers(t, poll, &ins.Select)
					assertTimeoutPendingOwner(t, poll, &ins.Select, ms)
				}
			}
			wantCalls := 0
			if row.name == "call" {
				wantCalls = 1
			}
			if selects != 1 || calls != wantCalls {
				t.Fatalf("selects=%d duration calls=%d, want 1/%d", selects, calls, wantCalls)
			}
			if got := findingsIn(mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema), "selected$poll"); len(got) != 0 {
				t.Fatalf("timeout pending owner is an alias:\n%s", joinLines(got))
			}
		})
	}
}

func assertTimeoutPendingOwner(t *testing.T, poll *mir.Func, sel *mir.SelectInstr, ms mir.LocalID) {
	t.Helper()
	timeouts := 0
	for _, arm := range sel.Arms {
		if arm.Kind != mir.SelectArmTimeout {
			continue
		}
		timeouts++
		if arm.Ms.Kind != mir.OperandCopy || arm.Ms.Place.Kind != mir.PlaceLocal || arm.Ms.Place.Local != ms || len(arm.Ms.Place.Proj) != 0 {
			t.Fatalf("timeout poll must borrow the stable local: %+v", arm.Ms)
		}
	}
	stores := 0
	for _, ins := range poll.Blocks[sel.PendBB].Instrs {
		if ins.Kind != mir.InstrCall {
			continue
		}
		for index, arg := range ins.Call.Args {
			if arg.Place.Kind != mir.PlaceLocal || arg.Place.Local != ms {
				continue
			}
			if arg.Kind != mir.OperandMove || len(arg.Place.Proj) != 0 || index >= len(ins.Call.ArgContracts) || ins.Call.ArgContracts[index] != mir.ArgContractStore {
				t.Fatalf("pending timeout must transfer one owner into STORE: %+v", arg)
			}
			stores++
		}
	}
	if timeouts != 1 || stores != 1 {
		t.Fatalf("timeout arms=%d pending stores=%d, want 1/1", timeouts, stores)
	}
}
