package mir

import "sort"

// ownershipDefSite identifies ONE definition of ONE local.
//
// An ordinary definition is the instruction that wrote it, named by
// (Block, Instr). A parameter has no such instruction — it is written by the
// call, before the first block runs — so it gets a synthetic root instead,
// which the recursion treats as terminal rather than as something to trace
// further back.
type ownershipDefSite struct {
	Block BlockID
	Instr int
	Local LocalID
}

// paramRootDef is the synthetic definition of a parameter at function entry.
func paramRootDef(local LocalID) ownershipDefSite {
	return ownershipDefSite{Block: NoBlockID, Instr: -1, Local: local}
}

// IsParamRoot reports whether this is a parameter's entry root rather than an
// instruction.
func (d ownershipDefSite) IsParamRoot() bool { return d.Block == NoBlockID }

// definedLocals reports the BARE locals an instruction defines.
//
// A write THROUGH a projection (`arr[i] = x`, `p.field = x`) deliberately does
// not count: the base local's own heap reference is unchanged by a write into
// what it points to, so such a write must not displace the definitions reaching
// past it. The value being written in is separately a STORE sink.
//
// Most instructions define at most one local. ChannelSelect is the deliberate
// exception: a success reply defines the winner destination plus every unique
// ReturnPlace whose losing payload was handed back. Keeping those definitions
// on the crossing itself makes the ownership fact visible to MIR rather than a
// backend-only store the verifier could not prove.
func definedLocals(ins *Instr) []LocalID {
	if ins == nil {
		return nil
	}
	var dst Place
	switch ins.Kind {
	case InstrAssign:
		dst = ins.Assign.Dst
	case InstrSpawn:
		dst = ins.Spawn.Dst
	default:
		p, ok := instrMintsDest(ins)
		if !ok {
			return nil
		}
		dst = p
	}
	out := make([]LocalID, 0, 1)
	if dst.Kind == PlaceLocal && len(dst.Proj) == 0 && dst.Local != NoLocalID {
		out = append(out, dst.Local)
	}
	if ins.Kind != InstrCrossing {
		return out
	}
	for i := range ins.Crossing.RemoteOps {
		place := ins.Crossing.RemoteOps[i].ReturnPlace
		if place == nil || place.Kind != PlaceLocal || len(place.Proj) != 0 || place.Local == NoLocalID {
			continue
		}
		seen := false
		for _, local := range out {
			if local == place.Local {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, place.Local)
		}
	}
	return out
}

// reachingDefs answers "which definitions of this local reach this program
// point" for one function.
//
// It is computed PER LOCAL, on demand. That is the same fixpoint as the joint
// GEN/KILL formulation, only factored: a definition of one local is killed
// only by another definition of that SAME local, so the joint lattice is the
// product of the per-local ones and solving them separately reaches the
// identical answer. Factoring it this way is what lets the guarded-drop
// recognizer ask about a plain `bool` guard — a local the ownership sinks
// never track — without a second engine.
type reachingDefs struct {
	f     *Func
	preds [][]BlockID
	cache map[LocalID]*localReaching
}

// localReaching holds one local's solved fixpoint: the definitions reaching the
// TOP of each block.
type localReaching struct {
	in [][]ownershipDefSite
}

func newReachingDefs(f *Func) *reachingDefs {
	return &reachingDefs{f: f, preds: predBlocks(f), cache: map[LocalID]*localReaching{}}
}

// predBlocks inverts the SAME successor notion async suspension analysis uses,
// pending edges included.
//
// A block's real successors are not always its terminator: one ending in a
// suspend-shaped instruction branches through that instruction's own
// ReadyBB/PendBB. And a pending edge is a REAL edge here — a definition
// arriving only along the pending path is exactly as live as one arriving along
// the ready path, so dropping it would silently discard a merge, which is a
// missing reaching definition rather than a wrong one.
func predBlocks(f *Func) [][]BlockID {
	if f == nil {
		return nil
	}
	preds := make([][]BlockID, len(f.Blocks))
	for bi := range f.Blocks {
		from := BlockID(bi)
		for _, to := range succBlocks(f, from, true) {
			if to == NoBlockID || int(to) >= len(preds) {
				continue
			}
			preds[to] = append(preds[to], from)
		}
	}
	for i := range preds {
		preds[i] = dedupBlocks(preds[i])
	}
	return preds
}

func dedupBlocks(ids []BlockID) []BlockID {
	if len(ids) < 2 {
		return ids
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	out := ids[:1]
	for _, id := range ids[1:] {
		if out[len(out)-1] != id {
			out = append(out, id)
		}
	}
	return out
}

// forLocal solves (once) the reaching-definitions fixpoint for one local.
func (r *reachingDefs) forLocal(local LocalID) *localReaching {
	if r == nil || r.f == nil {
		return &localReaching{}
	}
	if cached, ok := r.cache[local]; ok {
		return cached
	}
	solved := r.solve(local)
	r.cache[local] = solved
	return solved
}

func (r *reachingDefs) solve(local LocalID) *localReaching {
	n := len(r.f.Blocks)
	gen := make([]ownershipDefSite, n)
	kills := make([]bool, n)
	for bi := range r.f.Blocks {
		instrs := r.f.Blocks[bi].Instrs
		for ii := range instrs {
			for _, got := range definedLocals(&instrs[ii]) {
				if got == local {
					// A later definition in the same block locally kills an earlier
					// one: a MIR local is one storage slot, not an SSA name.
					gen[bi] = ownershipDefSite{Block: BlockID(bi), Instr: ii, Local: local}
					kills[bi] = true
				}
			}
		}
	}

	// The parameter root is a PERSISTENT term unioned into the entry block's IN
	// on every iteration, not a one-time seed. It matters when the entry block
	// is itself a loop target: seeding once and then applying the ordinary
	// equation would lose the root the moment the entry is revisited.
	var entryRoot []ownershipDefSite
	if r.isParam(local) {
		entryRoot = []ownershipDefSite{paramRootDef(local)}
	}
	entry := r.f.Entry

	in := make([]map[ownershipDefSite]struct{}, n)
	for i := range in {
		in[i] = map[ownershipDefSite]struct{}{}
	}
	out := func(b int) []ownershipDefSite {
		if kills[b] {
			return []ownershipDefSite{gen[b]}
		}
		return setToSlice(in[b])
	}

	for changed := true; changed; {
		changed = false
		for bi := range n {
			next := map[ownershipDefSite]struct{}{}
			if BlockID(bi) == entry {
				for _, d := range entryRoot {
					next[d] = struct{}{}
				}
			}
			for _, p := range r.preds[bi] {
				for _, d := range out(int(p)) {
					next[d] = struct{}{}
				}
			}
			if !sameSet(in[bi], next) {
				in[bi] = next
				changed = true
			}
		}
	}

	res := &localReaching{in: make([][]ownershipDefSite, n)}
	for i := range in {
		res.in[i] = setToSlice(in[i])
	}
	return res
}

func (r *reachingDefs) isParam(local LocalID) bool {
	return r.f != nil && local >= 0 && int(local) < r.f.ParamCount
}

// ReachingAt returns the definitions of `local` that reach the point just
// BEFORE instruction `idx` of block `b` — the block-level fixpoint refined by
// that block's own instructions 0..idx-1.
func (r *reachingDefs) ReachingAt(local LocalID, b BlockID, idx int) []ownershipDefSite {
	if r == nil || r.f == nil || b == NoBlockID || int(b) >= len(r.f.Blocks) {
		return nil
	}
	solved := r.forLocal(local)
	if int(b) >= len(solved.in) {
		return nil
	}
	cur := solved.in[b]
	instrs := r.f.Blocks[b].Instrs
	limit := idx
	if limit > len(instrs) {
		limit = len(instrs)
	}
	for i := range limit {
		for _, got := range definedLocals(&instrs[i]) {
			if got == local {
				cur = []ownershipDefSite{{Block: b, Instr: i, Local: local}}
			}
		}
	}
	return cur
}

func setToSlice(set map[ownershipDefSite]struct{}) []ownershipDefSite {
	if len(set) == 0 {
		return nil
	}
	out := make([]ownershipDefSite, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Block != out[b].Block {
			return out[a].Block < out[b].Block
		}
		return out[a].Instr < out[b].Instr
	})
	return out
}

func sameSet(a, b map[ownershipDefSite]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for d := range a {
		if _, ok := b[d]; !ok {
			return false
		}
	}
	return true
}
