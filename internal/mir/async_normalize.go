package mir

import "fmt"

// suspendSiteKind identifies a suspend point type.
type suspendSiteKind uint8

const (
	suspendPoll suspendSiteKind = iota
	suspendJoinAll
	suspendChanSend
	suspendChanRecv
	suspendTimeout
	suspendSelect
)

// awaitSite describes a suspend point that has been split into a poll instruction.
type awaitSite struct {
	kind       suspendSiteKind
	pollBB     BlockID
	pollInstr  int
	readyBB    BlockID
	stateIndex int
	liveLocals localSet
	pendingBB  BlockID
}

// splitAsyncAwaits transforms await instructions into poll instructions,
// splitting blocks at each await point.
func splitAsyncAwaits(f *Func) ([]awaitSite, error) {
	if f == nil {
		return nil, nil
	}
	var sites []awaitSite
	for {
		split := false
		for bi := range f.Blocks {
			bb := &f.Blocks[bi]
			for i := 0; i < len(bb.Instrs); i++ {
				ins := &bb.Instrs[i]
				if ins.Kind != InstrAwait && ins.Kind != InstrChanSend && ins.Kind != InstrChanRecv && ins.Kind != InstrTimeout && ins.Kind != InstrSelect {
					continue
				}
				if ins.Kind == InstrChanSend && ins.ChanSend.ReadyBB != NoBlockID {
					continue
				}
				if ins.Kind == InstrChanRecv && ins.ChanRecv.ReadyBB != NoBlockID {
					continue
				}
				if ins.Kind == InstrTimeout && ins.Timeout.ReadyBB != NoBlockID {
					continue
				}
				if ins.Kind == InstrSelect && ins.Select.ReadyBB != NoBlockID {
					continue
				}
				prelude := append([]Instr(nil), bb.Instrs[:i]...)
				after := append([]Instr(nil), bb.Instrs[i+1:]...)
				origTerm := bb.Term

				afterBB := newBlock(f)
				f.Blocks[afterBB].Instrs = after
				f.Blocks[afterBB].Term = origTerm

				pollBB := newBlock(f)
				var pollInstr Instr
				var kind suspendSiteKind
				switch ins.Kind {
				case InstrAwait:
					awaitInstr := ins.Await
					kind = suspendPoll
					pollInstr = Instr{Kind: InstrPoll, Poll: PollInstr{
						Dst:     awaitInstr.Dst,
						Task:    awaitInstr.Task,
						ReadyBB: afterBB,
						PendBB:  NoBlockID,
					}}
				case InstrChanSend:
					kind = suspendChanSend
					pollInstr = Instr{Kind: InstrChanSend, ChanSend: ChanSendInstr{
						Channel: ins.ChanSend.Channel,
						Value:   ins.ChanSend.Value,
						ReadyBB: afterBB,
						PendBB:  NoBlockID,
					}}
				case InstrChanRecv:
					kind = suspendChanRecv
					pollInstr = Instr{Kind: InstrChanRecv, ChanRecv: ChanRecvInstr{
						Dst:     ins.ChanRecv.Dst,
						Channel: ins.ChanRecv.Channel,
						ReadyBB: afterBB,
						PendBB:  NoBlockID,
					}}
				case InstrTimeout:
					kind = suspendTimeout
					pollInstr = Instr{Kind: InstrTimeout, Timeout: TimeoutInstr{
						Dst:     ins.Timeout.Dst,
						Task:    ins.Timeout.Task,
						Ms:      ins.Timeout.Ms,
						ReadyBB: afterBB,
						PendBB:  NoBlockID,
					}}
				case InstrSelect:
					kind = suspendSelect
					pollInstr = Instr{Kind: InstrSelect, Select: SelectInstr{
						Dst:     ins.Select.Dst,
						Arms:    append([]SelectArm(nil), ins.Select.Arms...),
						ReadyBB: afterBB,
						PendBB:  NoBlockID,
					}}
				default:
					continue
				}
				f.Blocks[pollBB].Instrs = []Instr{pollInstr}
				f.Blocks[pollBB].Term = Terminator{Kind: TermUnreachable}

				bb.Instrs = prelude
				bb.Term = Terminator{Kind: TermGoto, Goto: GotoTerm{Target: pollBB}}

				sites = append(sites, awaitSite{
					kind:      kind,
					pollBB:    pollBB,
					pollInstr: 0,
					readyBB:   afterBB,
				})
				split = true
				break
			}
			if split {
				break
			}
		}
		if !split {
			break
		}
	}

	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		for ii := range bb.Instrs {
			if bb.Instrs[ii].Kind == InstrAwait {
				return sites, fmt.Errorf("mir: async: await not normalized in %s", f.Name)
			}
		}
	}

	return sites, nil
}

// collectSuspendSites scans for poll/join_all instructions in block order.
func collectSuspendSites(f *Func) []awaitSite {
	if f == nil {
		return nil
	}
	sites := make([]awaitSite, 0)
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		bbID := BlockID(bi) //nolint:gosec // G115: bounded by block count
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			switch ins.Kind {
			case InstrPoll:
				sites = append(sites, awaitSite{
					kind:      suspendPoll,
					pollBB:    bbID,
					pollInstr: ii,
					readyBB:   ins.Poll.ReadyBB,
				})
			case InstrJoinAll:
				sites = append(sites, awaitSite{
					kind:      suspendJoinAll,
					pollBB:    bbID,
					pollInstr: ii,
					readyBB:   ins.JoinAll.ReadyBB,
				})
			case InstrChanSend:
				sites = append(sites, awaitSite{
					kind:      suspendChanSend,
					pollBB:    bbID,
					pollInstr: ii,
					readyBB:   ins.ChanSend.ReadyBB,
				})
			case InstrChanRecv:
				sites = append(sites, awaitSite{
					kind:      suspendChanRecv,
					pollBB:    bbID,
					pollInstr: ii,
					readyBB:   ins.ChanRecv.ReadyBB,
				})
			case InstrTimeout:
				sites = append(sites, awaitSite{
					kind:      suspendTimeout,
					pollBB:    bbID,
					pollInstr: ii,
					readyBB:   ins.Timeout.ReadyBB,
				})
			case InstrSelect:
				sites = append(sites, awaitSite{
					kind:      suspendSelect,
					pollBB:    bbID,
					pollInstr: ii,
					readyBB:   ins.Select.ReadyBB,
				})
			}
		}
	}
	return sites
}

// succBlocks returns the successor blocks of a given block.
func succBlocks(f *Func, bbID BlockID, includePollPending bool) []BlockID {
	if f == nil || bbID == NoBlockID || int(bbID) >= len(f.Blocks) {
		return nil
	}
	bb := &f.Blocks[bbID]
	if len(bb.Instrs) > 0 {
		last := &bb.Instrs[len(bb.Instrs)-1]
		switch last.Kind {
		case InstrPoll:
			out := []BlockID{}
			if last.Poll.ReadyBB != NoBlockID {
				out = append(out, last.Poll.ReadyBB)
			}
			if includePollPending && last.Poll.PendBB != NoBlockID {
				out = append(out, last.Poll.PendBB)
			}
			return out
		case InstrJoinAll:
			out := []BlockID{}
			if last.JoinAll.ReadyBB != NoBlockID {
				out = append(out, last.JoinAll.ReadyBB)
			}
			if includePollPending && last.JoinAll.PendBB != NoBlockID {
				out = append(out, last.JoinAll.PendBB)
			}
			return out
		case InstrChanSend:
			out := []BlockID{}
			if last.ChanSend.ReadyBB != NoBlockID {
				out = append(out, last.ChanSend.ReadyBB)
			}
			if includePollPending && last.ChanSend.PendBB != NoBlockID {
				out = append(out, last.ChanSend.PendBB)
			}
			return out
		case InstrChanRecv:
			out := []BlockID{}
			if last.ChanRecv.ReadyBB != NoBlockID {
				out = append(out, last.ChanRecv.ReadyBB)
			}
			if includePollPending && last.ChanRecv.PendBB != NoBlockID {
				out = append(out, last.ChanRecv.PendBB)
			}
			return out
		case InstrTimeout:
			out := []BlockID{}
			if last.Timeout.ReadyBB != NoBlockID {
				out = append(out, last.Timeout.ReadyBB)
			}
			if includePollPending && last.Timeout.PendBB != NoBlockID {
				out = append(out, last.Timeout.PendBB)
			}
			return out
		case InstrSelect:
			out := []BlockID{}
			if last.Select.ReadyBB != NoBlockID {
				out = append(out, last.Select.ReadyBB)
			}
			if includePollPending && last.Select.PendBB != NoBlockID {
				out = append(out, last.Select.PendBB)
			}
			return out
		}
	}
	switch bb.Term.Kind {
	case TermGoto:
		return []BlockID{bb.Term.Goto.Target}
	case TermIf:
		return []BlockID{bb.Term.If.Then, bb.Term.If.Else}
	case TermSwitchTag:
		out := make([]BlockID, 0, len(bb.Term.SwitchTag.Cases)+1)
		for _, c := range bb.Term.SwitchTag.Cases {
			out = append(out, c.Target)
		}
		out = append(out, bb.Term.SwitchTag.Default)
		return out
	default:
		return nil
	}
}
