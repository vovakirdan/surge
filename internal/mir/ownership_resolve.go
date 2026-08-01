package mir

import (
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

// ownershipResolveState is one TOP-LEVEL sink query's bookkeeping. It is not
// shared across sinks: a resolution is only ever valid relative to the query
// that started it.
type ownershipResolveState struct {
	active map[ownershipResolveKey]bool
	memo   map[ownershipResolveKey]bool
	// blame is the innermost definition that failed to resolve, kept so a
	// finding can name where the chain broke rather than only where it was
	// consumed. Recorded once, on the first terminal failure.
	blame    ownershipDefSite
	hasBlame bool
}

func (st *ownershipResolveState) note(d ownershipDefSite) {
	if st == nil || st.hasBlame {
		return
	}
	st.blame = d
	st.hasBlame = true
}

func newOwnershipResolveState() *ownershipResolveState {
	return &ownershipResolveState{
		active: map[ownershipResolveKey]bool{},
		memo:   map[ownershipResolveKey]bool{},
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
	if ty, ok := placeTypeIn(v.typesIn, v.f, v.globals, out.Place); ok {
		out.Type = ty
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
	switch classifyRValue(rv, resultTy, v.typesIn, v.semaRes) {
	case ownershipMints, ownershipOwnedAtEntry:
		return true
	case ownershipTransfers:
		src, ok := transferSourceOperand(rv)
		if !ok {
			return false
		}
		eff := v.effectiveOperand(src)
		return v.resolvePlace(eff.Place, at, st)
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
	if answer, ok := st.memo[key]; ok {
		return answer
	}
	if st.active[key] {
		// A cycle that never reached a terminal root. Deliberately UNRESOLVED,
		// which counts as a violation, never as MINTS: this costs a false
		// positive on a rooted loop-carried transfer chain in exchange for a
		// termination rule with no SCC computation behind it.
		return false
	}
	st.active[key] = true
	answer := v.resolvePlaceDefs(p.Local, at, st)
	delete(st.active, key)
	st.memo[key] = answer
	return answer
}

func (v *ownershipFuncVerifier) resolvePlaceDefs(local LocalID, at ownershipPoint, st *ownershipResolveState) bool {
	defs := v.defs.ReachingAt(local, at.Block, at.Instr)
	if len(defs) == 0 {
		return false
	}
	for _, d := range defs {
		if !v.resolveDef(d, st) {
			return false
		}
	}
	return true
}

// resolveDef resolves ONE definition: a parameter root terminally, and an
// ordinary definition by the same use-resolution the sink itself went through,
// applied to whatever that definition assigns.
func (v *ownershipFuncVerifier) resolveDef(d ownershipDefSite, st *ownershipResolveState) bool {
	if d.IsParamRoot() {
		ty := v.localType(d.Local)
		switch classifyParamAtEntry(ty, v.typesIn, v.semaRes) {
		case ownershipMints, ownershipOwnedAtEntry:
			return true
		default:
			st.note(d)
			return false
		}
	}
	ok := v.resolveDefInstr(d, st)
	if !ok {
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
		switch classifySpawnDest(ins, v.typesIn, v.semaRes) {
		case ownershipMints, ownershipOwnedAtEntry:
			return true
		case ownershipTransfers:
			eff := v.effectiveOperand(&ins.Spawn.Value)
			return v.resolvePlace(eff.Place, at, st)
		default:
			return false
		}
	}
	if _, ok := instrMintsDest(ins); ok {
		// Call-shaped: the operation produced a value nothing else holds.
		return true
	}
	return false
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
