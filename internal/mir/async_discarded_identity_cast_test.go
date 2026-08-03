package mir_test

import (
	"strings"
	"testing"

	"surge/internal/mir"
)

func TestAsyncSuspendAfterDiscardedIdentityCastKeepsLoopLocals(t *testing.T) {
	compiled := compileCrossingMIR(t, crossingMIRPrelude+`
tag Ready();
type Problem = { code: int };
tag Wait(Problem);
type Decision = Ready | Wait(Problem);

fn rt_net_wait_writable(conn: &TcpConn) -> bool {
    let _ = conn;
    return true;
}

async fn run(conn: TcpConn, data: int[], length: uint) -> int64 {
    let mut written: int64 = 0:int64;
    while written < (length to int64) {
		let problem: Problem = { code: conn.fd };
		let decision: Decision = Wait(own problem);
        compare decision {
            Ready() => {
                written = written + 1:int64;
                0:int;
            }
            Wait(problem) => {
                if problem.code != 0 {
                    rt_net_wait_writable(&conn);
                    0:int;
                } else {
                    return written;
                }
            }
        };
    }
    return written + (data[0] to int64);
}
`, nil)

	for _, fn := range compiled.mod.Funcs {
		mir.SimplifyCFG(fn)
		mir.RecognizeSwitchTag(fn)
		mir.SimplifyCFG(fn)
	}
	fn := findNamedMIRFunc(t, compiled.mod, "run")
	waitBlock := requireUnnormalizedNetWaitBlock(t, fn)
	if target := waitBlock.Term.Goto.Target; target < 0 || int(target) >= len(fn.Blocks) {
		t.Fatalf("discarded identity-cast tail left net-wait continuation bb%d outside %d-block CFG", target, len(fn.Blocks))
	}

	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("async lowering failed: %v", err)
	}
	pollFn := findNamedMIRFunc(t, compiled.mod, "run$poll")
	packed := netWaitPendingLocalNames(t, pollFn)
	for _, want := range []string{"conn", "data", "length", "written"} {
		if !packed[want] {
			t.Fatalf("suspend payload locals = %v, missing %q used after discarded identity-cast tail", packed, want)
		}
	}
}

func requireUnnormalizedNetWaitBlock(t *testing.T, fn *mir.Func) *mir.Block {
	t.Helper()
	for bi := range fn.Blocks {
		bb := &fn.Blocks[bi]
		for ii := range bb.Instrs {
			if bb.Instrs[ii].Kind != mir.InstrNetWait {
				continue
			}
			if ii != len(bb.Instrs)-1 {
				t.Fatalf("net wait is not the final instruction in bb%d", bi)
			}
			if bb.Instrs[ii].NetWait.ReadyBB != mir.NoBlockID || bb.Instrs[ii].NetWait.PendBB != mir.NoBlockID {
				t.Fatalf("pre-async net wait already has suspend targets")
			}
			if bb.Term.Kind != mir.TermGoto {
				t.Fatalf("pre-async net wait continuation = %s, want goto", bb.Term.Kind)
			}
			return bb
		}
	}
	t.Fatal("missing pre-async net wait")
	return nil
}

func netWaitPendingLocalNames(t *testing.T, fn *mir.Func) map[string]bool {
	t.Helper()
	for bi := range fn.Blocks {
		bb := &fn.Blocks[bi]
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			if ins.Kind != mir.InstrNetWait {
				continue
			}
			pending := ins.NetWait.PendBB
			if pending < 0 || int(pending) >= len(fn.Blocks) {
				t.Fatalf("net-wait pending target bb%d outside CFG", pending)
			}
			for pi := range fn.Blocks[pending].Instrs {
				call := &fn.Blocks[pending].Instrs[pi]
				if call.Kind != mir.InstrCall || !strings.HasPrefix(call.Call.Callee.Name, "Pc") {
					continue
				}
				names := make(map[string]bool, len(call.Call.Args))
				for ai := range call.Call.Args {
					arg := &call.Call.Args[ai]
					switch arg.Kind {
					case mir.OperandCopy, mir.OperandCopyValue, mir.OperandRetain,
						mir.OperandMove, mir.OperandAddrOf, mir.OperandAddrOfMut:
					default:
						continue
					}
					if arg.Place.Kind != mir.PlaceLocal {
						continue
					}
					local := arg.Place.Local
					if local >= 0 && int(local) < len(fn.Locals) {
						names[fn.Locals[local].Name] = true
					}
				}
				return names
			}
			t.Fatalf("pending bb%d has no async payload constructor", pending)
		}
	}
	t.Fatal("missing lowered net wait")
	return nil
}
