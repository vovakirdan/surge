package mir

import "fmt"

// suspendSiteKind identifies a suspend point type.
type suspendSiteKind uint8

const (
	suspendPoll suspendSiteKind = iota
	suspendJoinAll
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
				if ins.Kind != InstrAwait {
					continue
				}
				awaitInstr := ins.Await
				prelude := append([]Instr(nil), bb.Instrs[:i]...)
				after := append([]Instr(nil), bb.Instrs[i+1:]...)
				origTerm := bb.Term

				afterBB := newBlock(f)
				f.Blocks[afterBB].Instrs = after
				f.Blocks[afterBB].Term = origTerm

				pollBB := newBlock(f)
				pollInstr := Instr{Kind: InstrPoll, Poll: PollInstr{
					Dst:     awaitInstr.Dst,
					Task:    awaitInstr.Task,
					ReadyBB: afterBB,
					PendBB:  NoBlockID,
				}}
				f.Blocks[pollBB].Instrs = []Instr{pollInstr}
				f.Blocks[pollBB].Term = Terminator{Kind: TermUnreachable}

				bb.Instrs = prelude
				bb.Term = Terminator{Kind: TermGoto, Goto: GotoTerm{Target: pollBB}}

				sites = append(sites, awaitSite{
					kind:      suspendPoll,
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
			}
		}
	}
	return sites
}

// rejectAwaitInLoops checks that no await occurs inside a loop.
func rejectAwaitInLoops(f *Func, sites []awaitSite) error {
	if f == nil || len(sites) == 0 {
		return nil
	}
	awaitBlocks := make(map[BlockID]struct{}, len(sites))
	for _, site := range sites {
		if site.kind != suspendPoll {
			continue
		}
		awaitBlocks[site.pollBB] = struct{}{}
	}
	for bbID := range awaitBlocks {
		if hasCycleFrom(f, bbID) {
			return fmt.Errorf("mir: async: await inside loop is not supported in %s", f.Name)
		}
	}
	return nil
}

// hasCycleFrom checks if there is a path from start back to itself (DFS).
func hasCycleFrom(f *Func, start BlockID) bool {
	if f == nil || start == NoBlockID {
		return false
	}
	seen := make(map[BlockID]struct{})
	var stack []BlockID
	stack = append(stack, start)
	seen[start] = struct{}{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, succ := range succBlocks(f, id, false) {
			if succ == start {
				return true
			}
			if _, ok := seen[succ]; ok {
				continue
			}
			seen[succ] = struct{}{}
			stack = append(stack, succ)
		}
	}
	return false
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
