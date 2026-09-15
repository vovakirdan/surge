package llvm

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"surge/internal/mir"
)

func TestChannelSendOfferUsesDisposablePollStorage(t *testing.T) {
	for _, row := range []struct{ name, typ, initial string }{
		{"float", "float", "1.5"},
		{"channel-handle", "Channel<int32>", "Channel::<int32>::new(1:uint)"},
	} {
		for _, yield := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/yield-%t", row.name, yield), func(t *testing.T) {
				src := fmt.Sprintf(`
async fn producer(ch: Channel<%s>, kept: %s) -> int {
    ch.send(kept);
    return 0;
}

@entrypoint
fn main() -> int {
    let ch = Channel::<%s>::new(1:uint);
    let task = spawn producer(ch, %s);
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}`, row.typ, row.typ, row.typ, row.initial)
				mod, result := lowerMIRFromSource(t, src)
				var sender *mir.Func
				var send *mir.ChanSendInstr
				for _, fn := range mod.Funcs {
					for bi := range fn.Blocks {
						for ii := range fn.Blocks[bi].Instrs {
							ins := &fn.Blocks[bi].Instrs[ii]
							if ins.Kind != mir.InstrChanSend {
								continue
							}
							if send != nil {
								t.Fatal("fixture must contain exactly one send")
							}
							sender, send = fn, &ins.ChanSend
						}
					}
				}
				if send == nil || send.Value.Kind != mir.OperandRetain {
					t.Fatal("actual source did not lower to a counted Copy offer")
				}
				// Exercise both ABI selections on the same source-lowered poll;
				// the yield optimization is independently responsible for this bit.
				send.YieldAfterHandoff = yield
				ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
				if err != nil {
					t.Fatal(err)
				}
				body := functionBody(t, ir, sender.ID)
				callee := "rt_channel_send_offer"
				if yield {
					callee = "rt_channel_send_yield_offer"
				}
				call := regexp.MustCompile(`(%[\w.]+) = call i1 @` + callee + `\(ptr [^,]+, ptr (%[\w.]+)\)`)
				matches := call.FindAllStringSubmatchIndex(body, -1)
				if len(matches) != 1 {
					t.Fatalf("want one %s call:\n%s", callee, body)
				}
				m := matches[0]
				slot, ready := body[m[4]:m[5]], body[m[2]:m[3]]
				alloca := slot + " = alloca ptr, align 8"
				at := strings.Index(body, alloca)
				firstBlock := regexp.MustCompile(`(?m)^bb\d+:`).FindStringIndex(body)
				if at < 0 || strings.Count(body, alloca) != 1 || len(firstBlock) != 2 || at >= firstBlock[0] {
					t.Fatalf("offer must address its own aligned entry alloca %s:\n%s", slot, body)
				}
				blockStart := strings.LastIndex(body[:m[0]], "\nbb")
				if blockStart < 0 {
					t.Fatal("send call has no MIR poll block")
				}
				poll := body[blockStart:m[0]]
				store := regexp.MustCompile(`store ptr (%[\w.]+), ptr ` + regexp.QuoteMeta(slot) + `, align 8`)
				stores := store.FindAllStringSubmatch(poll, -1)
				if len(stores) != 1 {
					t.Fatalf("offer needs one typed store on this poll:\n%s", poll)
				}
				value := stores[0][1]
				if row.name == "channel-handle" {
					if strings.Count(poll, "call void @rt_channel_handle_retain(ptr "+value+")") != 1 {
						t.Fatalf("offered handle was not retained exactly once:\n%s", poll)
					}
				} else if strings.Count(poll, " = add i32 ") != 1 ||
					!strings.Contains(poll, " = icmp eq ptr "+value+", null") {
					t.Fatalf("offered float was not retained exactly once:\n%s", poll)
				}
				branch := fmt.Sprintf("br i1 %s, label %%bb%d, label %%bb%d", ready, send.ReadyBB, send.PendBB)
				if !strings.Contains(body[m[1]:], branch) {
					t.Fatalf("lost Ready/Pending branch %s", branch)
				}
			})
		}
	}
}

func TestChannelSendConsumingPollKeepsItsAPI(t *testing.T) {
	for _, row := range []struct{ name, typ, param, initial, declaration string }{
		{"plain", "int32", "int32", "17:int32", ""},
		{"owning", "string", "own string", `own "owned"`, ""},
		{"composite", "P", "P", "P{ value: 1.5 }", "@copy\ntype P = { value: float };"},
	} {
		for _, yield := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/yield-%t", row.name, yield), func(t *testing.T) {
				src := row.declaration + fmt.Sprintf(`
async fn producer(ch: Channel<%s>, kept: %s) -> int {
    ch.send(kept);
    return 0;
}
@entrypoint
fn main() -> int {
    let ch = Channel::<%s>::new(1:uint);
    let task = spawn producer(ch, %s);
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}`, row.typ, row.param, row.typ, row.initial)
				mod, result := lowerMIRFromSource(t, src)
				var sender *mir.Func
				for _, fn := range mod.Funcs {
					for bi := range fn.Blocks {
						for ii := range fn.Blocks[bi].Instrs {
							ins := &fn.Blocks[bi].Instrs[ii]
							if ins.Kind != mir.InstrChanSend {
								continue
							}
							if sender != nil {
								t.Fatal("fixture must contain exactly one send")
							}
							if ins.ChanSend.Value.Kind == mir.OperandRetain {
								t.Fatal("consuming control became an offer")
							}
							ins.ChanSend.YieldAfterHandoff = yield
							sender = fn
						}
					}
				}
				if sender == nil {
					t.Fatal("actual source lost its send")
				}
				ir, err := EmitModule(mod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
				if err != nil {
					t.Fatal(err)
				}
				body := functionBody(t, ir, sender.ID)
				callee := "rt_channel_send"
				if yield {
					callee += "_yield"
				}
				if strings.Count(body, "call i1 @"+callee+"(") != 1 ||
					strings.Contains(body, "call i1 @rt_channel_send_offer(") ||
					strings.Contains(body, "call i1 @rt_channel_send_yield_offer(") {
					t.Fatalf("consuming send changed API:\n%s", body)
				}
			})
		}
	}
}
