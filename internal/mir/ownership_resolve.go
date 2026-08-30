package mir

import (
	"sort"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/types"
)

// ownershipPoint is a program point: the instant just BEFORE instruction Instr
// of block Block runs. A terminator's own point is Instr == len(block.Instrs).
type ownershipPoint struct {
	Block BlockID
	Instr int
}

// ownershipResolveKey is one (place, program-point) pair, the unit both the
// cycle-breaking visited stack and the memo are keyed by.
type ownershipResolveKey struct {
	Local LocalID
	Point ownershipPoint
}

type ownershipResolveMemo struct {
	owned  bool
	blames []ownershipDefSite
}

// ownershipResolveState is one TOP-LEVEL sink query's bookkeeping. It is not
// shared across sinks: a resolution is only ever valid relative to the query
// that started it.
type ownershipResolveState struct {
	active map[ownershipResolveKey]bool
	memo   map[ownershipResolveKey]ownershipResolveMemo
	// blameEvents records each terminal failure observation, including repeats
	// replayed from memoized subqueries. The event boundary lets a failed
	// wrapper definition tell whether its child already supplied a more exact
	// cause; blames is the unique set eventually emitted as findings.
	blameEvents []ownershipDefSite
	blames      map[ownershipDefSite]struct{}
}

func (st *ownershipResolveState) note(d ownershipDefSite) {
	if st == nil {
		return
	}
	st.blameEvents = append(st.blameEvents, d)
	st.blames[d] = struct{}{}
}

func (st *ownershipResolveState) replay(blames []ownershipDefSite) {
	for _, blame := range blames {
		st.note(blame)
	}
}

func (st *ownershipResolveState) sortedBlames() []ownershipDefSite {
	if st == nil || len(st.blames) == 0 {
		return nil
	}
	out := make([]ownershipDefSite, 0, len(st.blames))
	for blame := range st.blames {
		out = append(out, blame)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Block != out[j].Block {
			return out[i].Block < out[j].Block
		}
		if out[i].Instr != out[j].Instr {
			return out[i].Instr < out[j].Instr
		}
		return out[i].Local < out[j].Local
	})
	return out
}

func newOwnershipResolveState() *ownershipResolveState {
	return &ownershipResolveState{
		active: map[ownershipResolveKey]bool{},
		memo:   map[ownershipResolveKey]ownershipResolveMemo{},
		blames: map[ownershipDefSite]struct{}{},
	}
}

// ownershipFuncVerifier carries everything one function's checks need.
type ownershipFuncVerifier struct {
	f       *Func
	globals []Global
	typesIn *types.Interner
	semaRes *sema.Result
	defs    *reachingDefs
}

// effectiveOperand fills in an operand's type when the construction site left
// it blank.
//
// Several sites (operandForLocal, and so AsyncYieldTerm.State and
// AsyncReturnCancelledTerm.State among others) build an operand naming a local
// without repeating the local's type on it, because the caller already knew it.
// Step 0's classifier only ever reads Operand.Type, so an untyped operand reads
// as non-owning and its sink is skipped ENTIRELY — a missing check, which is
// worse than a false one. Resolved by copy, never by writing back: this pass
// may not touch the MIR it reads.
func (v *ownershipFuncVerifier) effectiveOperand(op *Operand) Operand {
	out := *op
	if out.Type != types.NoTypeID || out.Kind == OperandConst {
		return out
	}
	if out.Place.Kind == PlaceLocal && len(out.Place.Proj) == 0 {
		if idx := int(out.Place.Local); idx >= 0 && v.f != nil && idx < len(v.f.Locals) {
			out.Type = v.f.Locals[idx].Type
		}
		return out
	}
	if ty, ok := placeTypeWithMapElems(v.typesIn, v.f, v.globals, out.Place); ok {
		out.Type = ty
	}
	return out
}

// effectiveRValue is the same normalization one level up, applied to every
// operand an RValue's own classification reads.
//
// Filling in only the operand a TRANSFERS answer later recurses into is not
// enough, because the classification itself consults operand types BEFORE
// producing an answer: castIsIdentity compares the cast's source type against
// its target, derefsABorrow asks whether a unary's operand is a reference, and
// indexIsView asks whether an index operand is a Range. An untyped source makes
// castIsIdentity report "not identity", which classifies MINTS and accepts an
// alias without ever tracing it — a false NEGATIVE, the one direction this pass
// may not err in.
//
// Copied, like every other normalization here: the MIR is read, never written.
func (v *ownershipFuncVerifier) effectiveRValue(rv *RValue) RValue {
	out := *rv
	switch out.Kind {
	case RValueUse:
		out.Use = v.effectiveOperand(&out.Use)
	case RValueUnaryOp:
		out.Unary.Operand = v.effectiveOperand(&out.Unary.Operand)
	case RValueCast:
		out.Cast.Value = v.effectiveOperand(&out.Cast.Value)
	case RValueIndex:
		out.Index.Object = v.effectiveOperand(&out.Index.Object)
		out.Index.Index = v.effectiveOperand(&out.Index.Index)
	case RValueField:
		out.Field.Object = v.effectiveOperand(&out.Field.Object)
	case RValueTagPayload:
		out.TagPayload.Value = v.effectiveOperand(&out.TagPayload.Value)
	}
	return out
}

// operandOwnsHeap gates a position on its EFFECTIVE type owning heap. A
// non-owning position can never violate the invariant, so it is filtered out
// before the classifier is consulted at all.
func (v *ownershipFuncVerifier) operandOwnsHeap(op *Operand) bool {
	eff := v.effectiveOperand(op)
	return ownsHeapFor(v.typesIn, v.semaRes, operandType(&eff))
}

// resolveOperandUse answers whether the value an operand delivers to a sink is
// one that sink is entitled to own.
//
// The USE itself is classified FIRST, before anything recurses. An
// OperandCopy of an owning place sitting at a sink is this session's five
// defects' shape and must fail HERE — not be waved through because some
// definition of the underlying place elsewhere happens to mint.
func (v *ownershipFuncVerifier) resolveOperandUse(op *Operand, at ownershipPoint, st *ownershipResolveState) bool {
	eff := v.effectiveOperand(op)
	switch classifyOperand(&eff, v.typesIn, v.semaRes) {
	case ownershipMints, ownershipOwnedAtEntry:
		return true
	case ownershipTransfers:
		// The source is read as of right before this use executes, so the
		// recursion queries THIS program point, not the source's own.
		return v.resolvePlace(eff.Place, at, st)
	default:
		return false
	}
}

// resolveRValueUse is the same judgment for a sink that names an RValue rather
// than an operand — a projected assignment's right-hand side, and every
// definition the recursion walks back through.
func (v *ownershipFuncVerifier) resolveRValueUse(rv *RValue, resultTy types.TypeID, at ownershipPoint, st *ownershipResolveState) bool {
	eff := v.effectiveRValue(rv)
	switch classifyRValue(&eff, resultTy, v.typesIn, v.semaRes) {
	case ownershipMints, ownershipOwnedAtEntry:
		return true
	case ownershipTransfers:
		src, ok := transferSourceOperand(&eff)
		if !ok {
			return false
		}
		// Unary `+` and `own` pass the evaluated operand through unchanged.
		// Preserve minting constants, retains, and value clones as such. The
		// one deliberate exception is `own <place>`: real lowering represents
		// its inner bare place as COPY because the unary operator itself is the
		// explicit transfer boundary. Trace that place's definitions instead
		// of rejecting every legitimate `take(own value)` as an alias. A place
		// whose own definition is merely aliasing still fails, so `own` cannot
		// launder an unowned chain.
		if eff.Kind == RValueUnaryOp &&
			(eff.Unary.Op == ast.ExprUnaryPlus || eff.Unary.Op == ast.ExprUnaryOwn) {
			effSrc := v.effectiveOperand(src)
			if eff.Unary.Op == ast.ExprUnaryOwn &&
				effSrc.Kind == OperandCopy &&
				classifyOperand(&effSrc, v.typesIn, v.semaRes) == ownershipAliases &&
				effSrc.Place.Kind == PlaceLocal && effSrc.Place.Local != NoLocalID &&
				len(effSrc.Place.Proj) == 0 {
				return v.resolvePlace(effSrc.Place, at, st)
			}
			return v.resolveOperandUse(&effSrc, at, st)
		}
		return v.resolvePlace(src.Place, at, st)
	default:
		return false
	}
}

// transferSourceOperand names the operand a TRANSFERS answer inherits from.
//
// Every TRANSFERS row has exactly one: OperandMove's own place, a moved-out
// field's container, a moved-out tag payload's subject, and a deref-of-own (or
// `+`/`own`) unary's operand. The flag on a field or payload read decides only
// whether the read ALIASES or TRANSFERS — once it says TRANSFERS, ordinary
// recursion applies, because the flag records what HIR decided the extraction
// DOES, not a promise MIR built the subject to match.
func transferSourceOperand(rv *RValue) (*Operand, bool) {
	if rv == nil {
		return nil, false
	}
	switch rv.Kind {
	case RValueUse:
		return &rv.Use, true
	case RValueField:
		return &rv.Field.Object, true
	case RValueTagPayload:
		return &rv.TagPayload.Value, true
	case RValueUnaryOp:
		return &rv.Unary.Operand, true
	}
	return nil, false
}

// resolvePlace answers whether EVERY definition of a place reaching a program
// point resolves to an owned root.
//
// Not "at least one": an unconditional release downstream of a branch where
// only one arm minted is exactly the shape a one-path rule would be blind to
// by construction. And an EMPTY reaching set is a violation, not a vacuous
// pass — it means the pass could not establish where the value came from.
func (v *ownershipFuncVerifier) resolvePlace(p Place, at ownershipPoint, st *ownershipResolveState) bool {
	if p.Kind != PlaceLocal || p.Local == NoLocalID {
		// A global, or nothing at all. Neither has a definition this dataflow
		// can trace, so neither can be established as owned.
		return false
	}
	key := ownershipResolveKey{Local: p.Local, Point: at}
	// Memo first, active stack second — the same pair reached again by a second
	// path through a diamond has a real answer already, and only a pair still
	// being resolved is a cycle.
	if memo, ok := st.memo[key]; ok {
		if !memo.owned {
			st.replay(memo.blames)
		}
		return memo.owned
	}
	if st.active[key] {
		// A cycle that never reached a terminal root. Deliberately UNRESOLVED,
		// which counts as a violation, never as MINTS: this costs a false
		// positive on a rooted loop-carried transfer chain in exchange for a
		// termination rule with no SCC computation behind it.
		return false
	}
	st.active[key] = true
	mark := len(st.blameEvents)
	answer := v.resolvePlaceDefs(p.Local, at, st)
	delete(st.active, key)
	st.memo[key] = ownershipResolveMemo{
		owned:  answer,
		blames: append([]ownershipDefSite(nil), st.blameEvents[mark:]...),
	}
	return answer
}

func (v *ownershipFuncVerifier) resolvePlaceDefs(local LocalID, at ownershipPoint, st *ownershipResolveState) bool {
	defs := v.defs.ReachingAt(local, at.Block, at.Instr)
	if len(defs) == 0 {
		return false
	}
	allOwned := true
	for _, d := range defs {
		if !v.resolveDef(d, st) {
			allOwned = false
		}
	}
	return allOwned
}

// resolveDef resolves ONE definition: a parameter root terminally, and an
// ordinary definition by the same use-resolution the sink itself went through,
// applied to whatever that definition assigns.
func (v *ownershipFuncVerifier) resolveDef(d ownershipDefSite, st *ownershipResolveState) bool {
	if d.IsParamRoot() {
		ty := v.localType(d.Local)
		switch classifyParamAtEntry(ty, v.typesIn, v.semaRes, v.f != nil && v.f.CapturesArriveOwned) {
		case ownershipMints, ownershipOwnedAtEntry:
			return true
		default:
			st.note(d)
			return false
		}
	}
	mark := len(st.blameEvents)
	ok := v.resolveDefInstr(d, st)
	if !ok && len(st.blameEvents) == mark {
		st.note(d)
	}
	return ok
}

func (v *ownershipFuncVerifier) resolveDefInstr(d ownershipDefSite, st *ownershipResolveState) bool {
	ins := v.instrAt(d)
	if ins == nil {
		return false
	}
	at := ownershipPoint{Block: d.Block, Instr: d.Instr}
	switch ins.Kind {
	case InstrAssign:
		return v.resolveRValueUse(&ins.Assign.Src, v.localType(d.Local), at, st)
	case InstrSpawn:
		// Not unconditional MINTS: the backend stores the handle Value already
		// names straight into Dst, so Dst's ownership is Value's own answer.
		// Asked against a normalized COPY of the instruction, so that an
		// untyped handle operand is classified on the same terms as any other.
		eff := *ins
		eff.Spawn.Value = v.effectiveOperand(&ins.Spawn.Value)
		switch classifySpawnDest(&eff, v.typesIn, v.semaRes) {
		case ownershipMints, ownershipOwnedAtEntry:
			return true
		case ownershipTransfers:
			return v.resolvePlace(eff.Spawn.Value.Place, at, st)
		default:
			return false
		}
	case InstrCrossing:
		if v.validCrossingReturnDefinition(&ins.Crossing, d.Local) {
			// The runtime pending was the sole owner while suspended. A genuine
			// winner reply returned this non-committed payload before ReadyBB,
			// so this instruction is a fresh owned root on every path that may
			// subsequently consume the local. The exact MOVE/same-place/unique
			// shape is rechecked here rather than trusting arbitrary metadata.
			return true
		}
	}
	if _, ok := instrMintsDest(ins); ok {
		// Call-shaped: the operation produced a value nothing else holds.
		return true
	}
	return false
}

func (v *ownershipFuncVerifier) validCrossingReturnDefinition(cr *CrossingInstr, local LocalID) bool {
	if v == nil || v.f == nil || cr == nil || cr.Kind != sema.CrossingLoweringChannelSelect ||
		local == NoLocalID || int(local) < 0 || int(local) >= len(v.f.Locals) {
		return false
	}
	matches := 0
	for i := range cr.RemoteOps {
		op := &cr.RemoteOps[i]
		if op.ReturnPlace == nil || op.ReturnPlace.Kind != PlaceLocal ||
			len(op.ReturnPlace.Proj) != 0 || op.ReturnPlace.Local != local {
			continue
		}
		matches++
		if op.Method != "send" || op.Value.Kind != OperandMove ||
			op.Value.Place.Kind != PlaceLocal || len(op.Value.Place.Proj) != 0 ||
			op.Value.Place.Local != local || op.Value.Type != v.f.Locals[local].Type {
			return false
		}
	}
	return matches == 1
}

func (v *ownershipFuncVerifier) instrAt(d ownershipDefSite) *Instr {
	if v.f == nil || d.Block == NoBlockID || int(d.Block) >= len(v.f.Blocks) {
		return nil
	}
	instrs := v.f.Blocks[d.Block].Instrs
	if d.Instr < 0 || d.Instr >= len(instrs) {
		return nil
	}
	return &instrs[d.Instr]
}

func (v *ownershipFuncVerifier) localType(local LocalID) types.TypeID {
	if v.f == nil || local < 0 || int(local) >= len(v.f.Locals) {
		return types.NoTypeID
	}
	return v.f.Locals[local].Type
}

func (v *ownershipFuncVerifier) localOwnsHeap(local LocalID) bool {
	if v.f == nil || local < 0 || int(local) >= len(v.f.Locals) {
		return false
	}
	return v.f.Locals[local].Flags&LocalFlagOwnsHeap != 0
}
