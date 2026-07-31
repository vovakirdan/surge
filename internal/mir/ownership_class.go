package mir

import (
	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/types"
)

// ownershipClass names what a definition DOES to heap ownership, which is the
// vocabulary an ownership verifier's dataflow runs on: it asks whether every
// definition reaching a release resolves to a value that release is entitled
// to free.
//
// Four of the five are terminal answers. TRANSFERS deliberately is not — see
// ownershipTransfers.
type ownershipClass uint8

const (
	// ownershipUnclassified is the zero value on purpose: it is never a
	// legitimate answer, so a kind no table row covers reads as a gap rather
	// than as whichever real class happened to sort first. Every caller must
	// treat it as a finding.
	ownershipUnclassified ownershipClass = iota
	// ownershipMints materializes a fresh, independently-releasable value.
	// Whoever receives it owes exactly one release.
	ownershipMints
	// ownershipAliases reads THROUGH to storage someone else owns without
	// creating a new reference. Nothing may release it until something bridges
	// it — a retain, which is its own ownershipMints definition of the same
	// local.
	ownershipAliases
	// ownershipTransfers is NOT a terminal answer. It is a PASS-THROUGH: the
	// definition inherits the ownership status of whatever was moved into it,
	// resolved recursively through this same judgment. A move of an alias is
	// still an alias, only relocated, so "this classifies TRANSFERS" says
	// nothing whatsoever about whether a release of it is legitimate — only
	// resolving it back to its ultimate source does.
	//
	// This distinction is load-bearing, not stylistic. RV2-DEBT-100's pre-fix
	// shape was one unretained borrowed read handed onward through a chain of
	// ordinary moves before the drop; a flat table treating TRANSFERS as
	// self-sufficient would have waved every link through.
	ownershipTransfers
	// ownershipOwnedAtEntry is a function parameter the callee owns on the way
	// in. It stands exactly where ownershipMints stands for everything
	// downstream, and is separate only because its origin is different: a
	// parameter has no earlier MIR definition, so it is a terminal ROOT and
	// must not be labeled ownershipTransfers, which would leave the recursion
	// with nothing to inherit from.
	ownershipOwnedAtEntry
	// ownershipNotApplicable is a definition with no heap ownership in it at
	// all — a bool test result, an arithmetic temp, a reference.
	ownershipNotApplicable
)

func (c ownershipClass) String() string {
	switch c {
	case ownershipMints:
		return "mints"
	case ownershipAliases:
		return "aliases"
	case ownershipTransfers:
		return "transfers"
	case ownershipOwnedAtEntry:
		return "owned_at_entry"
	case ownershipNotApplicable:
		return "n/a"
	case ownershipUnclassified:
		return "unclassified"
	default:
		return "unknown"
	}
}

// classifyRValue answers what an InstrAssign's right-hand side does to heap
// ownership. resultTy is the DESTINATION place's type, which several rows turn
// on and which the RValue itself does not always carry.
func classifyRValue(rv *RValue, resultTy types.TypeID, typesIn *types.Interner, semaRes *sema.Result) ownershipClass {
	if rv == nil {
		return ownershipUnclassified
	}
	switch rv.Kind {
	case RValueUse:
		// A pure pass-through: the wrapped operand IS the answer (Table B), and
		// it asks the ownership question against its own type rather than the
		// destination's, so it is answered before the gate below.
		return classifyOperand(&rv.Use, typesIn, semaRes)
	case RValueTagTest, RValueTypeTest, RValueHeirTest:
		// A test yields a bool. There is nothing to own whatever it looked at.
		return ownershipNotApplicable
	case RValueIterInit:
		// The iterator state box is allocated outright (emitIterInit's own
		// rt_alloc), independent of what is being iterated, and freed by an
		// InstrEnvelopeRelease later.
		return ownershipMints
	case RValueIterNext:
		// A fresh Option<T> envelope per step, likewise independent of the
		// element type — HIR releases each one explicitly (StmtEnvelopeRelease
		// in normalize_for.go).
		return ownershipMints
	}

	// Everything below owes an answer only where a value owns heap at all.
	if !ownsHeapFor(typesIn, semaRes, resultTy) {
		return ownershipNotApplicable
	}

	switch rv.Kind {
	case RValueUnaryOp:
		return classifyUnaryOp(&rv.Unary, typesIn)
	case RValueBinaryOp:
		// Reached only for an ordinary binary operator: `is`, `heir`,
		// assignment, compound assignment and the short-circuiting logicals
		// are each peeled off into their own RValue kind before this one is
		// built (lowerBinaryOpExpr). What is left is either a primitive
		// arithmetic/comparison op or a MAGIC operator that resolves to a real
		// call underneath, and BinaryOp carries no marker separating them.
		//
		// The result type is the discriminant, and not merely as a proxy: a
		// primitive op on machine words produces a machine word, so reaching
		// here at all with an owning result type means a real call built a
		// fresh value. Either way the answer is the same one.
		return ownershipMints
	case RValueCast:
		if castIsIdentity(typesIn, &rv.Cast) {
			// Converts nothing and builds nothing — every backend hands the
			// source value straight back, so this reads the source's storage
			// (RV2-DEBT-097's exact shape).
			return ownershipAliases
		}
		// A representation change builds a new value: a tag cast allocates a
		// fresh box, an int-to-float cast allocates a fresh block.
		return ownershipMints
	case RValueStructLit, RValueArrayLit, RValueTupleLit:
		// The CONTAINER is fresh. Each owns-heap field position is separately a
		// consuming sink, which is a different check against the operand
		// occupying it, not a second classification of this RValue.
		return ownershipMints
	case RValueField:
		if rv.Field.MoveOut {
			// The field left the container, whose own drop has been narrowed
			// around it. Whether the result is owned is whatever the CONTAINER's
			// definition resolves to.
			return ownershipTransfers
		}
		// A plain read counts no new holder: the container still owns it.
		return ownershipAliases
	case RValueIndex:
		// A plain element read, which is the only shape that reaches here — an
		// index expression whose result type is a reference never becomes an
		// RValueIndex at all (lowerIndexExpr returns an AddrOf operand for it),
		// and IndexAccess has no other form to distinguish.
		return ownershipAliases
	case RValueTagPayload:
		// Reads its OWN flag, exactly like RValueField, because the question
		// cannot be re-derived from the subject: compareScrutineeOwnership has
		// three outcomes and a MINTS-resolving subject covers two of them, one
		// needing a retain here and one not. Once the flag says TRANSFERS,
		// ordinary recursion into the subject still applies — the flag records
		// what HIR decided the extraction DOES, not a promise that MIR built
		// the subject to match.
		if rv.TagPayload.MoveOut {
			return ownershipTransfers
		}
		return ownershipAliases
	}
	return ownershipUnclassified
}

// classifyUnaryOp answers per OPERATOR, because the unary kinds do not share
// one answer the way the binary ones do — two of them hand their operand
// straight back. It is reached only for an owning result type; the caller
// answered N/A for everything else.
func classifyUnaryOp(op *UnaryOp, typesIn *types.Interner) ownershipClass {
	if op == nil {
		return ownershipUnclassified
	}
	switch op.Op {
	case ast.ExprUnaryDeref:
		if derefsABorrow(typesIn, op.Operand.Type) {
			// Reading through a reference copies the bare handle; the owner on
			// the other side keeps its own and knows nothing about this one.
			return ownershipAliases
		}
		// Through an OWNING pointer the single reference transfers, so this
		// inherits whatever the pointer's own definition resolves to.
		return ownershipTransfers
	case ast.ExprUnaryPlus, ast.ExprUnaryOwn:
		// Both backends return the operand untouched (emitUnary, evalUnary), so
		// nothing is built and nothing is converted — whatever the operand held
		// is what this holds, which is what TRANSFERS means. Deliberately NOT
		// MINTS: `own s` on an aliasing read would then look like a fresh value
		// and license a release the alias never earned.
		return ownershipTransfers
	case ast.ExprUnaryMinus:
		// Negating an owning type is a real call that allocates its result
		// (rt_bigint_neg/rt_bigfloat_neg); the machine-word forms produce a
		// machine word and never reach here.
		return ownershipMints
	case ast.ExprUnaryNot:
		// A bool, which the caller's owns-heap gate already answered for. Kept
		// explicit so the operator is covered rather than merely unreachable.
		return ownershipNotApplicable
	case ast.ExprUnaryRef, ast.ExprUnaryRefMut:
		// Never built as an RValueUnaryOp at all: lowerUnaryOpExpr returns an
		// AddrOf operand for these before any RValue exists.
		return ownershipNotApplicable
	}
	// ExprUnaryFar/ExprUnaryAwait reach no backend's emitUnary, so MIR carrying
	// one is already a gap. Report it rather than guess an ownership answer for
	// an operator nothing can execute.
	return ownershipUnclassified
}

// classifyOperand answers what reading an operand does to heap ownership. It is
// the whole of Table B: RValueUse carries no ownership of its own, so its
// destination's status IS this answer.
func classifyOperand(op *Operand, typesIn *types.Interner, semaRes *sema.Result) ownershipClass {
	if op == nil {
		return ownershipUnclassified
	}
	switch op.Kind {
	case OperandAddrOf, OperandAddrOfMut:
		// A reference, not a value. Nothing here ever owes a release, whatever
		// the referent's type is.
		return ownershipNotApplicable
	case OperandRetain:
		// A reference of this destination's OWN, independently releasable. This
		// is what bridges an aliasing read: `L = tag_payload ...; L = retain L`
		// is two ordinary definitions, the second of which mints.
		return ownershipMints
	case OperandCopyValue:
		// An independent clone of a value composite. Cloning MINTS — the source
		// is untouched and keeps its own release, which is precisely what
		// RV2-DEBT-052's @copy residual needed said out loud.
		return ownershipMints
	}

	if !ownsHeapFor(typesIn, semaRes, operandType(op)) {
		return ownershipNotApplicable
	}

	switch op.Kind {
	case OperandConst:
		// A constant of an owning type is materialized fresh for its use: a
		// float/bignum through materializeOwnedConst's temp, a STRING through
		// the backend's own allocation at the use site (emitStringConst), which
		// materializeOwnedConst skips entirely because IsRefCountedScalar
		// excludes strings. Different mechanisms, one ownership outcome — which
		// is why this asks whether the TYPE owns heap and not whether it is a
		// reference-counted scalar.
		return ownershipMints
	case OperandCopy:
		// A bare copy of an owning place, with no retain: two holders, one
		// reference. Every one of this session's five ownership defects was
		// this shape.
		return ownershipAliases
	case OperandMove:
		// Relocates whatever the source held, which is what it inherits.
		return ownershipTransfers
	}
	return ownershipUnclassified
}

// classifyParamAtEntry seeds a function parameter, which has no RValue to
// classify and is a terminal root rather than something recursing further back.
//
// Mirrors sema's paramTransfersOwnership predicate exactly — the same one
// byValueArgContract mirrors on the caller's side of the same call — because
// the callee's view of what it owns on entry and the caller's view of what it
// handed over have to be the same fact. The exclusion is written as
// "reference-counted scalar" rather than "Copy" for the reason it is written
// that way there: a @copy value composite is CLONED at the call site and the
// callee genuinely owns that clone.
func classifyParamAtEntry(ty types.TypeID, typesIn *types.Interner, semaRes *sema.Result) ownershipClass {
	if !ownsHeapFor(typesIn, semaRes, ty) {
		return ownershipNotApplicable
	}
	if typesIn != nil && typesIn.IsRefCountedScalar(resolveAlias(typesIn, ty)) {
		// Not owned at entry: the caller keeps its binding and its reference
		// for the whole call, so this parameter is a borrow and needs the same
		// explicit retain any other borrowed read does before anything may
		// release it. Deliberately not a sixth class — it behaves as an alias.
		return ownershipAliases
	}
	return ownershipOwnedAtEntry
}

// instrMintsDest reports the destination place an instruction defines WITHOUT
// going through an RValue, all of which are call-shaped operations producing a
// value nothing else holds, and so classify ownershipMints.
//
// InstrAssign is excluded rather than unhandled: its destination is governed
// entirely by classifyRValue against its right-hand side.
func instrMintsDest(ins *Instr) (Place, bool) {
	if ins == nil {
		return Place{}, false
	}
	switch ins.Kind {
	case InstrCall:
		if !ins.Call.HasDst {
			return Place{}, false
		}
		return ins.Call.Dst, true
	case InstrAwait:
		return ins.Await.Dst, true
	case InstrSpawn:
		return ins.Spawn.Dst, true
	case InstrCrossing:
		return ins.Crossing.Dst, true
	case InstrBlocking:
		return ins.Blocking.Dst, true
	case InstrPoll:
		return ins.Poll.Dst, true
	case InstrJoinAll:
		return ins.JoinAll.Dst, true
	case InstrChanRecv:
		return ins.ChanRecv.Dst, true
	case InstrTimeout:
		return ins.Timeout.Dst, true
	case InstrSelect:
		return ins.Select.Dst, true
	case InstrAssign, InstrDrop, InstrEndBorrow, InstrChanSend, InstrNetWait,
		InstrNop, InstrEnvelopeRelease:
		return Place{}, false
	}
	return Place{}, false
}

// castIsIdentity reports whether a cast converts nothing, asked the way
// castKeepsRepresentation asks it at lowering time: aliases resolve, so
// `type Celsius = float` is the same representation as `float`, while `own` and
// `&` do not, because they say something about who holds the value.
func castIsIdentity(typesIn *types.Interner, cast *CastOp) bool {
	if typesIn == nil || cast == nil {
		return false
	}
	// An unknown type on either side answers NO, keeping the cast owned. Both
	// of this question's defaults must fall that way: a missing release is a
	// leak and a spare one is a use-after-free, and only the leak survives.
	if cast.Value.Type == types.NoTypeID || cast.TargetTy == types.NoTypeID {
		return false
	}
	return resolveAlias(typesIn, cast.Value.Type) == resolveAlias(typesIn, cast.TargetTy)
}

// operandType is the type an operand's ownership question is asked against. A
// constant carries its own, which is the one to trust when the operand's was
// never filled in.
func operandType(op *Operand) types.TypeID {
	if op.Type != types.NoTypeID {
		return op.Type
	}
	if op.Kind == OperandConst {
		return op.Const.Type
	}
	return types.NoTypeID
}
