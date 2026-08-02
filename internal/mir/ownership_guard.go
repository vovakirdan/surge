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
	guardDefs, ok := v.canonicalGuardDefs(guard, guardBlock)
	if !ok {
		return false
	}

	dropped, ok := v.droppedLocalAt(at)
	if !ok {
		return false
	}
	// lowerOwnedTempExpr materializes the choice result into the local that is
	// eventually dropped. Real lowering therefore has one pure move between the
	// arm-local definitions and this sink. Peel only an unambiguous chain of
	// those moves; the first multi-definition frontier is kept whole for the arm
	// selection below.
	reaching, ok := v.guardArmDefFrontier(dropped, at)
	if !ok {
		return false
	}

	return v.guardFrontierCorrelates(guard, reaching, guardDefs)
}

// guardFrontierCorrelates proves BOTH directions. Every frontier definition
// must see exactly one guard definition that reaches the final branch, and
// every true guard write must select exactly one frontier definition. This is
// stricter than matching blocks: a later overwrite after a true write cannot
// hide behind the earlier, owned definition on that path.
func (v *ownershipFuncVerifier) guardFrontierCorrelates(
	guard LocalID,
	frontier []ownershipDefSite,
	guardDefs canonicalGuardDefSet,
) bool {
	state := guardCorrelationState{
		guard:       guard,
		guardDefs:   guardDefs,
		resolve:     newOwnershipResolveState(),
		trueMatches: map[ownershipDefSite]int{},
		active:      map[ownershipResolveKey]bool{},
	}
	for _, valueDef := range frontier {
		if !v.guardValueDefCorrelates(valueDef, &state) {
			return false
		}
	}
	return everyTrueGuardMatched(guardDefs.values, state.trueMatches)
}

type guardCorrelationState struct {
	guard       LocalID
	guardDefs   canonicalGuardDefSet
	resolve     *ownershipResolveState
	trueMatches map[ownershipDefSite]int
	active      map[ownershipResolveKey]bool
}

// guardValueDefCorrelates accepts a definition only at an exact guard leaf.
// A mixed guard frontier may be opened solely through the canonical bare-local
// move emitted by nested choice lowering; every source definition is then
// checked recursively, preserving the verifier's EVERY rule.
func (v *ownershipFuncVerifier) guardValueDefCorrelates(
	valueDef ownershipDefSite,
	state *guardCorrelationState,
) bool {
	if valueDef.IsParamRoot() {
		return false
	}
	at := ownershipPoint{Block: valueDef.Block, Instr: valueDef.Instr}
	guardDefs := v.defs.ReachingAt(state.guard, at.Block, at.Instr)
	if len(guardDefs) == 1 {
		return v.guardLeafCorrelates(valueDef, guardDefs[0], state)
	}
	if len(guardDefs) == 0 {
		return false
	}

	source, sourceAt, ok := v.canonicalGuardTransferSource(valueDef)
	if !ok {
		return false
	}
	return v.guardSourceDefsCorrelate(source, sourceAt, state)
}

func (v *ownershipFuncVerifier) guardLeafCorrelates(
	valueDef, guardDef ownershipDefSite,
	state *guardCorrelationState,
) bool {
	value, belongs := state.guardDefs.values[guardDef]
	if !belongs || (!value && guardDef != state.guardDefs.falseInit) {
		return false
	}
	if !value {
		return true
	}
	state.trueMatches[guardDef]++
	return state.trueMatches[guardDef] == 1 && v.resolveDef(valueDef, state.resolve)
}

func (v *ownershipFuncVerifier) guardSourceDefsCorrelate(
	source LocalID,
	at ownershipPoint,
	state *guardCorrelationState,
) bool {
	key := ownershipResolveKey{Local: source, Point: at}
	if state.active[key] {
		return false
	}
	defs := v.defs.ReachingAt(source, at.Block, at.Instr)
	if len(defs) == 0 {
		return false
	}

	state.active[key] = true
	defer delete(state.active, key)
	for _, sourceDef := range defs {
		if !v.guardValueDefCorrelates(sourceDef, state) {
			return false
		}
	}
	return true
}

func everyTrueGuardMatched(guardValues map[ownershipDefSite]bool, matches map[ownershipDefSite]int) bool {
	for def, value := range guardValues {
		if value && matches[def] != 1 {
			return false
		}
	}
	return true
}

// guardArmDefFrontier follows the exact pure transfer lowerOwnedTempExpr puts
// between a choice's arm-local result and its guarded drop.
//
// A hop is allowed only while ONE definition reaches the current point. Once
// multiple definitions reach it, they are the arm frontier and every member is
// preserved: choosing one here would turn the verifier's EVERY rule into ANY.
// Repeated (local, point) pairs reject the shortcut conservatively rather than
// letting a loop-carried move cycle license a drop.
func (v *ownershipFuncVerifier) guardArmDefFrontier(local LocalID, at ownershipPoint) ([]ownershipDefSite, bool) {
	seen := map[ownershipResolveKey]bool{}
	for {
		key := ownershipResolveKey{Local: local, Point: at}
		if seen[key] {
			return nil, false
		}
		seen[key] = true

		defs := v.defs.ReachingAt(local, at.Block, at.Instr)
		if len(defs) == 0 {
			return nil, false
		}
		if len(defs) != 1 {
			return defs, true
		}

		source, sourceAt, ok := v.canonicalGuardTransferSource(defs[0])
		if !ok {
			return defs, true
		}
		local, at = source, sourceAt
	}
}

// canonicalGuardTransferSource matches the representation observed from real
// lowering: `bare dst = move bare source`, classified as TRANSFERS. Field or
// payload moves, unary pass-throughs, spawn transfers, and projected places
// remain ordinary resolver work; widening to them would make this a general
// transfer prover rather than the canonical guarded-temp recognizer.
func (v *ownershipFuncVerifier) canonicalGuardTransferSource(d ownershipDefSite) (LocalID, ownershipPoint, bool) {
	if d.IsParamRoot() {
		return NoLocalID, ownershipPoint{}, false
	}
	ins := v.instrAt(d)
	if ins == nil || ins.Kind != InstrAssign ||
		ins.Assign.Dst.Kind != PlaceLocal || len(ins.Assign.Dst.Proj) != 0 ||
		ins.Assign.Dst.Local != d.Local {
		return NoLocalID, ownershipPoint{}, false
	}

	src := v.effectiveRValue(&ins.Assign.Src)
	if src.Kind != RValueUse || src.Use.Kind != OperandMove ||
		src.Use.Place.Kind != PlaceLocal || len(src.Use.Place.Proj) != 0 ||
		src.Use.Place.Local == NoLocalID {
		return NoLocalID, ownershipPoint{}, false
	}
	if classifyRValue(&src, v.localType(d.Local), v.typesIn, v.semaRes) != ownershipTransfers {
		return NoLocalID, ownershipPoint{}, false
	}
	return src.Use.Place.Local, ownershipPoint{Block: d.Block, Instr: d.Instr}, true
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
	if term.Kind != TermIf || term.If.Then != at.Block || term.If.Else == at.Block {
		return NoLocalID, NoBlockID, false
	}
	cond := term.If.Cond
	if cond.Kind != OperandCopy || cond.Place.Kind != PlaceLocal ||
		len(cond.Place.Proj) != 0 || cond.Place.Local == NoLocalID {
		return NoLocalID, NoBlockID, false
	}
	return cond.Place.Local, pred, true
}

type canonicalGuardDefSet struct {
	falseInit ownershipDefSite
	values    map[ownershipDefSite]bool
}

// canonicalGuardDefs proves the guard's complete initialization shape.
//
// Every definition reaching the branch must be a literal bool const write —
// not a copy, call result, retain, or parameter root. Exactly one false write
// initializes the guard before the choice, and it must dominate both the final
// read and every true write. May-reaching definitions alone cannot prove that:
// a path with no definition contributes no element to their union.
func (v *ownershipFuncVerifier) canonicalGuardDefs(guard LocalID, guardBlock BlockID) (canonicalGuardDefSet, bool) {
	point := ownershipPoint{Block: guardBlock, Instr: len(v.f.Blocks[guardBlock].Instrs)}
	defs := v.defs.ReachingAt(guard, point.Block, point.Instr)
	if len(defs) == 0 {
		return canonicalGuardDefSet{}, false
	}
	set := canonicalGuardDefSet{values: make(map[ownershipDefSite]bool, len(defs))}
	haveFalse := false
	trueCount := 0
	for _, d := range defs {
		value, ok := v.boolConstDef(d)
		if !ok {
			return canonicalGuardDefSet{}, false
		}
		set.values[d] = value
		if !value {
			if haveFalse {
				return canonicalGuardDefSet{}, false
			}
			haveFalse = true
			set.falseInit = d
		} else {
			trueCount++
		}
	}
	if !haveFalse || trueCount == 0 || !v.guardDefDominatesPoint(set.falseInit, point) {
		return canonicalGuardDefSet{}, false
	}
	for d, value := range set.values {
		if value && !v.guardDefDominatesPoint(set.falseInit, ownershipPoint{Block: d.Block, Instr: d.Instr}) {
			return canonicalGuardDefSet{}, false
		}
	}
	return set, true
}

// guardDefDominatesPoint answers the narrow instruction-level dominance query
// the canonical false initialization needs. Blocks have no internal jumps, so
// a definition earlier in the same block dominates the point. Across blocks,
// walking predecessors from the target must encounter the definition's block
// before function entry on every path. The predecessor graph is the same one
// reaching definitions use, including pending async edges.
func (v *ownershipFuncVerifier) guardDefDominatesPoint(d ownershipDefSite, at ownershipPoint) bool {
	if v == nil || v.f == nil || v.defs == nil || d.IsParamRoot() ||
		d.Block == NoBlockID || at.Block == NoBlockID ||
		int(d.Block) >= len(v.f.Blocks) || int(at.Block) >= len(v.f.Blocks) {
		return false
	}
	if d.Block == at.Block {
		return d.Instr < at.Instr
	}

	seen := map[BlockID]bool{at.Block: true}
	work := []BlockID{at.Block}
	for len(work) > 0 {
		block := work[len(work)-1]
		work = work[:len(work)-1]
		if block == d.Block {
			continue
		}
		if block == v.f.Entry {
			return false
		}
		for _, pred := range v.defs.preds[block] {
			if !seen[pred] {
				seen[pred] = true
				work = append(work, pred)
			}
		}
	}
	return true
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
