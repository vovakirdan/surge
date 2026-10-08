package llvm

import (
	"testing"

	"surge/internal/mir"
)

func TestChannelSendPendingFrameHasOnlyItsActualOwner(t *testing.T) {
	mod, _ := lowerMIRFromSource(t, `
async fn producer(ch: Channel<string>, value: own string) -> int {
    ch.send(value);
    return 0;
}
@entrypoint
fn main() -> int {
    let ch = Channel::<string>::new(1:uint);
    let t = spawn producer(ch, own "payload");
    return compare t.await() { Success(v) => v; Cancelled() => 1; };
}`)
	checked := 0
	for _, f := range mod.Funcs {
		for _, bb := range f.Blocks {
			for _, ins := range bb.Instrs {
				if ins.Kind != mir.InstrChanSend || ins.ChanSend.Value.Kind != mir.OperandMove {
					continue
				}
				checked++
				dispatch := f.Blocks[ins.ChanSend.PendBB].Term
				if dispatch.Kind != mir.TermIf {
					t.Fatal("consuming send cannot distinguish a refused reservation from a transferred source")
				}
				for _, row := range []struct {
					block mir.BlockID
					want  bool
				}{{dispatch.If.Then, false}, {dispatch.If.Else, true}} {
					found := pendingFrameMovesLocal(&f.Blocks[row.block], ins.ChanSend.Value.Place.Local)
					if found != row.want {
						t.Fatalf("pending frame source ownership=%t, want %t", found, row.want)
					}
				}
			}
		}
	}
	if checked != 1 {
		t.Fatalf("checked %d consuming sends, want 1", checked)
	}
}

func pendingFrameMovesLocal(block *mir.Block, local mir.LocalID) bool {
	for _, pack := range block.Instrs {
		if pack.Kind != mir.InstrCall {
			continue
		}
		for _, arg := range pack.Call.Args {
			if arg.Kind == mir.OperandMove && arg.Place.Local == local {
				return true
			}
		}
	}
	return false
}
