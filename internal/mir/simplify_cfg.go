package mir

// SimplifyCFG performs control flow graph simplification on a function.
// Transformations:
// 1. Remove trivial goto blocks (0 instructions + goto terminator)
// 2. Collapse goto chains
// 3. Remove unreachable blocks
// 4. Renumber blocks deterministically
func SimplifyCFG(f *Func) {
	if f == nil || len(f.Blocks) == 0 {
		return
	}

	// Phase 1: Build redirect map for trivial goto blocks
	redirects := buildRedirectMap(f)

	// Phase 2: Apply redirects to all terminators
	applyRedirects(f, redirects)

	// Phase 3: Compute reachability and remove dead blocks
	reachable := computeReachability(f)

	// Phase 4: Compact and renumber blocks
	compactBlocks(f, reachable)
}

// buildRedirectMap finds all trivial goto blocks and builds a mapping
// from their IDs to their final targets (following chains).
func buildRedirectMap(f *Func) map[BlockID]BlockID {
	redirects := make(map[BlockID]BlockID)

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		// Trivial goto: 0 instructions + TermGoto
		if len(bb.Instrs) == 0 && bb.Term.Kind == TermGoto {
			target := bb.Term.Goto.Target
			// Follow chain to final target
			visited := make(map[BlockID]bool)
			for !visited[target] {
				visited[target] = true

				if next, ok := redirects[target]; ok {
					target = next
					continue
				}
				if isTrivialGotoBlock(f, target) {
					target = f.Blocks[target].Term.Goto.Target
					continue
				}
				break
			}
			redirects[bb.ID] = target
		}
	}
	return redirects
}

// isTrivialGotoBlock checks if a block is a trivial goto block
// (0 instructions and a goto terminator).
func isTrivialGotoBlock(f *Func, id BlockID) bool {
	if id < 0 || int(id) >= len(f.Blocks) {
		return false
	}
	bb := &f.Blocks[id]
	return len(bb.Instrs) == 0 && bb.Term.Kind == TermGoto
}

// applyRedirects updates all terminators to use the redirected targets.
func applyRedirects(f *Func, redirects map[BlockID]BlockID) {
	if len(redirects) == 0 {
		return
	}

	redirect := func(id BlockID) BlockID {
		if newID, ok := redirects[id]; ok {
			return newID
		}
		return id
	}

	for i := range f.Blocks {
		term := &f.Blocks[i].Term
		switch term.Kind {
		case TermGoto:
			term.Goto.Target = redirect(term.Goto.Target)
		case TermIf:
			term.If.Then = redirect(term.If.Then)
			term.If.Else = redirect(term.If.Else)
		case TermSwitchTag:
			if len(term.SwitchTag.Cases) > 0 {
				term.SwitchTag.Cases = append([]SwitchTagCase(nil), term.SwitchTag.Cases...)
			}
			for j := range term.SwitchTag.Cases {
				term.SwitchTag.Cases[j].Target = redirect(term.SwitchTag.Cases[j].Target)
			}
			term.SwitchTag.Default = redirect(term.SwitchTag.Default)
		}
		if len(f.Blocks[i].Instrs) > 0 {
			last := &f.Blocks[i].Instrs[len(f.Blocks[i].Instrs)-1]
			switch last.Kind {
			case InstrPoll:
				last.Poll.ReadyBB = redirect(last.Poll.ReadyBB)
				last.Poll.PendBB = redirect(last.Poll.PendBB)
			case InstrJoinAll:
				last.JoinAll.ReadyBB = redirect(last.JoinAll.ReadyBB)
				last.JoinAll.PendBB = redirect(last.JoinAll.PendBB)
			case InstrChanSend:
				last.ChanSend.ReadyBB = redirect(last.ChanSend.ReadyBB)
				last.ChanSend.PendBB = redirect(last.ChanSend.PendBB)
			case InstrChanRecv:
				last.ChanRecv.ReadyBB = redirect(last.ChanRecv.ReadyBB)
				last.ChanRecv.PendBB = redirect(last.ChanRecv.PendBB)
			case InstrTimeout:
				last.Timeout.ReadyBB = redirect(last.Timeout.ReadyBB)
				last.Timeout.PendBB = redirect(last.Timeout.PendBB)
			case InstrSelect:
				last.Select.ReadyBB = redirect(last.Select.ReadyBB)
				last.Select.PendBB = redirect(last.Select.PendBB)
			}
		}
	}

	// Also redirect entry if needed
	f.Entry = redirect(f.Entry)
}

// computeReachability performs a DFS from the entry block to find
// all reachable blocks.
func computeReachability(f *Func) []bool {
	reachable := make([]bool, len(f.Blocks))

	var visit func(id BlockID)
	visit = func(id BlockID) {
		if id < 0 || int(id) >= len(f.Blocks) || reachable[id] {
			return
		}
		reachable[id] = true

		term := &f.Blocks[id].Term
		if len(f.Blocks[id].Instrs) > 0 {
			last := &f.Blocks[id].Instrs[len(f.Blocks[id].Instrs)-1]
			switch last.Kind {
			case InstrPoll:
				visit(last.Poll.ReadyBB)
				visit(last.Poll.PendBB)
				return
			case InstrJoinAll:
				visit(last.JoinAll.ReadyBB)
				visit(last.JoinAll.PendBB)
				return
			case InstrChanSend:
				visit(last.ChanSend.ReadyBB)
				visit(last.ChanSend.PendBB)
				return
			case InstrChanRecv:
				visit(last.ChanRecv.ReadyBB)
				visit(last.ChanRecv.PendBB)
				return
			case InstrTimeout:
				visit(last.Timeout.ReadyBB)
				visit(last.Timeout.PendBB)
				return
			case InstrSelect:
				visit(last.Select.ReadyBB)
				visit(last.Select.PendBB)
				return
			}
		}
		switch term.Kind {
		case TermGoto:
			visit(term.Goto.Target)
		case TermIf:
			visit(term.If.Then)
			visit(term.If.Else)
		case TermSwitchTag:
			for _, c := range term.SwitchTag.Cases {
				visit(c.Target)
			}
			visit(term.SwitchTag.Default)
		}
		// TermReturn, TermUnreachable, TermNone have no successors
	}

	visit(f.Entry)
	return reachable
}

// compactBlocks removes unreachable blocks and renumbers the remaining ones.
func compactBlocks(f *Func, reachable []bool) {
	// Count reachable blocks
	count := 0
	for _, r := range reachable {
		if r {
			count++
		}
	}

	// If all blocks are reachable, just update IDs
	if count == len(f.Blocks) {
		for i := range f.Blocks {
			f.Blocks[i].ID = BlockID(i) //nolint:gosec // G115: bounded by existing block count
		}
		return
	}

	// Build old→new ID mapping
	oldToNew := make(map[BlockID]BlockID)
	newBlocks := make([]Block, 0, count)

	for i, keep := range reachable {
		if keep {
			//nolint:gosec // G115: bounded by existing block count
			oldToNew[BlockID(i)] = BlockID(len(newBlocks))
			newBlocks = append(newBlocks, f.Blocks[i])
		}
	}

	// Update all block references
	remap := func(id BlockID) BlockID {
		if newID, ok := oldToNew[id]; ok {
			return newID
		}
		return id // Should not happen if reachability is correct
	}

	for i := range newBlocks {
		newBlocks[i].ID = BlockID(i) //nolint:gosec // G115: bounded by newBlocks length
		term := &newBlocks[i].Term
		switch term.Kind {
		case TermGoto:
			term.Goto.Target = remap(term.Goto.Target)
		case TermIf:
			term.If.Then = remap(term.If.Then)
			term.If.Else = remap(term.If.Else)
		case TermSwitchTag:
			if len(term.SwitchTag.Cases) > 0 {
				term.SwitchTag.Cases = append([]SwitchTagCase(nil), term.SwitchTag.Cases...)
			}
			for j := range term.SwitchTag.Cases {
				term.SwitchTag.Cases[j].Target = remap(term.SwitchTag.Cases[j].Target)
			}
			term.SwitchTag.Default = remap(term.SwitchTag.Default)
		}
		if len(newBlocks[i].Instrs) > 0 {
			last := &newBlocks[i].Instrs[len(newBlocks[i].Instrs)-1]
			switch last.Kind {
			case InstrPoll:
				last.Poll.ReadyBB = remap(last.Poll.ReadyBB)
				last.Poll.PendBB = remap(last.Poll.PendBB)
			case InstrJoinAll:
				last.JoinAll.ReadyBB = remap(last.JoinAll.ReadyBB)
				last.JoinAll.PendBB = remap(last.JoinAll.PendBB)
			case InstrChanSend:
				last.ChanSend.ReadyBB = remap(last.ChanSend.ReadyBB)
				last.ChanSend.PendBB = remap(last.ChanSend.PendBB)
			case InstrChanRecv:
				last.ChanRecv.ReadyBB = remap(last.ChanRecv.ReadyBB)
				last.ChanRecv.PendBB = remap(last.ChanRecv.PendBB)
			case InstrTimeout:
				last.Timeout.ReadyBB = remap(last.Timeout.ReadyBB)
				last.Timeout.PendBB = remap(last.Timeout.PendBB)
			case InstrSelect:
				last.Select.ReadyBB = remap(last.Select.ReadyBB)
				last.Select.PendBB = remap(last.Select.PendBB)
			}
		}
	}

	f.Blocks = newBlocks
	f.Entry = remap(f.Entry)
}
