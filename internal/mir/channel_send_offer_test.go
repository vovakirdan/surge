package mir_test

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/mir"
)

func TestChannelSendPollPreservesTheCountedCopyOwner(t *testing.T) {
	for _, row := range []struct {
		name, valueType, declarations string
		want                          mir.OperandKind
		original                      bool
	}{
		{"float-offer", "float", "", mir.OperandRetain, true},
		{"plain-copy", "int32", "", mir.OperandCopy, true},
		{"owning-string", "own string", "", mir.OperandMove, true},
		{"composite-prelude", "P", "@copy\ntype P = { value: float };", mir.OperandMove, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			element := strings.TrimPrefix(row.valueType, "own ")
			src := crossingMIRPrelude + row.declarations + fmt.Sprintf(`
extern<Channel<T>> {
    @intrinsic fn send(self: &Channel<T>, value: T) -> nothing;
}
async fn producer(ch: Channel<%s>, kept: %s) -> int {
    ch.send(kept);
    return 0;
}`, element, row.valueType)
			compiled := compileCrossingMIR(t, src, nil)
			count := 0
			for _, fn := range compiled.mod.Funcs {
				if fn == nil || fn.Name != "producer" {
					continue
				}
				for _, block := range fn.Blocks {
					for _, ins := range block.Instrs {
						if row.want == mir.OperandRetain && ins.Kind == mir.InstrAssign &&
							ins.Assign.Src.Kind == mir.RValueUse && ins.Assign.Src.Use.Kind == mir.OperandRetain &&
							fn.Locals[ins.Assign.Src.Use.Place.Local].Name == "kept" {
							t.Fatal("counted Copy also acquired a prelude reference outside the offer poll")
						}
						if ins.Kind != mir.InstrChanSend {
							continue
						}
						count++
						value := ins.ChanSend.Value
						if value.Kind != row.want {
							t.Fatalf("send operand=%v, want %v", value.Kind, row.want)
						}
						name := fn.Locals[value.Place.Local].Name
						if row.original && name != "kept" {
							t.Fatalf("poll reads %q instead of the original kept owner", name)
						}
						if !row.original && !strings.Contains(name, "send") {
							t.Fatalf("composite lost its existing transfer temp: %q", name)
						}
					}
				}
			}
			if count != 1 {
				t.Fatalf("producer send count=%d, want 1", count)
			}
		})
	}
}
