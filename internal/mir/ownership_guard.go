package mir

// The guarded-drop recognizer: narrow, exact, and deliberately not a general
// boolean-guard prover.
//
// The compiler already encodes "release only on the paths that minted" as a
// bool raised at the minting site and tested at the drop (emitGuardedTempDrop,
// ChoiceReleaseGuards). Flagging every such drop would drown a corpus run in
// noise from correct code, so this trusts that ONE construction — recognized
// structurally, end to end — rather than re-proving it.
//
// It applies only to the WHOLE, non-residual drop row. A residual (multi-step)
// guarded drop is out of scope and falls through to ordinary resolution.

// guardedDropAccepted reports whether a drop is reached through the canonical
// guarded-release shape AND every path that raises the guard independently
// resolves as owned.
//
// A false answer is never an accusation on its own: it means the shortcut does
// not apply, and the ordinary reaching-definition query runs as it would for
// any other drop.
func (v *ownershipFuncVerifier) guardedDropAccepted(at ownershipPoint) bool {
	guard, guardBlock, ok := v.recognizeGuardBranch(at)
	if !ok {
		return false
	}
	trueBlocks, ok := v.guardTrueBlocks(guard, guardBlock)
	if !ok {
		return false
	}

	dropped, ok := v.droppedLocalAt(at)
	if !ok {
		return false
	}
	// Computed ONCE, the ordinary way — the same query the unguarded drop row
	// runs. The recognizer only SELECTS from it. That is what makes a decoy
	// assignment powerless: a decoy defines some other local, so it is never a
	// member of this set no matter where it sits textually.
	reaching := v.defs.ReachingAt(dropped, at.Block, at.Instr)

	st := newOwnershipResolveState()
	for _, trueBlock := range trueBlocks {
		def, ok := defInBlock(reaching, trueBlock)
		if !ok {
			return false
		}
		if !v.resolveDef(def, st) {
			return false
		}
	}
	return true
}

// recognizeGuardBranch matches emitGuardedTempDrop's own emitted shape: a
// freshly allocated block holding nothing but the drop, entered from exactly
// one predecessor, on the true edge of an `if` reading a bare bool local.
//
// Anything else at all in the block, or more than one way in, and this does not
// fire — which is exactly what makes a hand-built non-canonical "guard" surface
// as a genuine finding instead of being waved through.
func (v *ownershipFuncVerifier) recognizeGuardBranch(at ownershipPoint) (LocalID, BlockID, bool) {
	if v.f == nil || at.Block == NoBlockID || int(at.Block) >= len(v.f.Blocks) {
		return NoLocalID, NoBlockID, false
	}
	block := &v.f.Blocks[at.Block]
	if len(block.Instrs) != 1 || at.Instr != 0 {
		return NoLocalID, NoBlockID, false
	}
	ins := &block.Instrs[0]
	if ins.Kind != InstrDrop || ins.Drop.Shallow || len(ins.Drop.Place.Proj) != 0 {
		return NoLocalID, NoBlockID, false
	}
	if int(at.Block) >= len(v.defs.preds) {
		return NoLocalID, NoBlockID, false
	}
	preds := v.defs.preds[at.Block]
	if len(preds) != 1 {
		return NoLocalID, NoBlockID, false
	}
	pred := preds[0]
	term := &v.f.Blocks[pred].Term
	if term.Kind != TermIf || term.If.Then != at.Block {
		return NoLocalID, NoBlockID, false
	}
	cond := term.If.Cond
	if cond.Kind != OperandCopy || cond.Place.Kind != PlaceLocal ||
		len(cond.Place.Proj) != 0 || cond.Place.Local == NoLocalID {
		return NoLocalID, NoBlockID, false
	}
	return cond.Place.Local, pred, true
}

// guardTrueBlocks answers which blocks raise the guard.
//
// Every definition of the guard reaching the branch must be a literal bool
// const write — not a copy, not a call result, not a retain. This is the
// deliberate narrow exception where the dataflow is asked about a plain `bool`
// local rather than a heap-owning one.
func (v *ownershipFuncVerifier) guardTrueBlocks(guard LocalID, guardBlock BlockID) ([]BlockID, bool) {
	point := ownershipPoint{Block: guardBlock, Instr: len(v.f.Blocks[guardBlock].Instrs)}
	defs := v.defs.ReachingAt(guard, point.Block, point.Instr)
	if len(defs) == 0 {
		return nil, false
	}
	var trueBlocks []BlockID
	for _, d := range defs {
		value, ok := v.boolConstDef(d)
		if !ok {
			return nil, false
		}
		if value {
			trueBlocks = append(trueBlocks, d.Block)
		}
		// A `false` write contributes nothing to check: it is the "nobody
		// minted on this path" case the guard exists to skip.
	}
	return trueBlocks, true
}

// boolConstDef reports the literal bool a definition writes, and whether it is
// one at all. A parameter root never is.
func (v *ownershipFuncVerifier) boolConstDef(d ownershipDefSite) (value, isBoolConst bool) {
	if d.IsParamRoot() {
		return false, false
	}
	ins := v.instrAt(d)
	if ins == nil || ins.Kind != InstrAssign {
		return false, false
	}
	src := &ins.Assign.Src
	if src.Kind != RValueUse || src.Use.Kind != OperandConst || src.Use.Const.Kind != ConstBool {
		return false, false
	}
	return src.Use.Const.BoolValue, true
}

func (v *ownershipFuncVerifier) droppedLocalAt(at ownershipPoint) (LocalID, bool) {
	block := &v.f.Blocks[at.Block]
	place := block.Instrs[at.Instr].Drop.Place
	if place.Kind != PlaceLocal || place.Local == NoLocalID {
		return NoLocalID, false
	}
	return place.Local, true
}

// defInBlock picks the member of an already-computed reaching-definition set
// that lives in one block. There is at most one: a block's later definition of
// a local locally kills its earlier ones, so only the last can reach out.
func defInBlock(defs []ownershipDefSite, block BlockID) (ownershipDefSite, bool) {
	for _, d := range defs {
		if !d.IsParamRoot() && d.Block == block {
			return d, true
		}
	}
	return ownershipDefSite{}, false
}
