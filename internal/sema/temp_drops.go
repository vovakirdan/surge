package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// Statement-end temporaries: owned rvalues no binding ever owns (borrowed
// concat operands, discarded results, condition temporaries). Sema
// publishes the FINAL set — an expression has exactly one syntactic use
// position, so "produces an owned value and nothing consumes it" is
// decidable during the walk; HIR wraps the flagged expressions and MIR
// frees them at their evaluation region's end. Consumption never needs
// runtime deregistration because a consumed expression is simply never
// in the set.

type tempFrame struct {
	flags map[ast.ExprID]struct{}
	// tainted marks statements containing suspension-bearing constructs
	// (await/select/spawn/crossing/blocking): temp lifetimes across
	// suspension belong to the crossing vertical, so the whole frame is
	// discarded (leak — the pre-epic status quo — never a free).
	tainted bool
}

func (tc *typeChecker) pushTempFrame() {
	tc.tempFrames = append(tc.tempFrames, tempFrame{})
}

// popTempFrame publishes the frame's surviving flags unless tainted, each with
// the plan that reclaims what is LEFT of it.
//
// The plan is computed here rather than at the move because this is the point
// where the answer is complete: a temporary cannot be named again, so every
// path that will ever be taken out of it was taken inside the region now
// closing. A temporary nothing was taken from carries no plan, which is the
// ordinary whole release and every case there was before fields could move.
func (tc *typeChecker) popTempFrame() {
	if len(tc.tempFrames) == 0 {
		return
	}
	frame := tc.tempFrames[len(tc.tempFrames)-1]
	tc.tempFrames = tc.tempFrames[:len(tc.tempFrames)-1]
	if frame.tainted || len(frame.flags) == 0 {
		return
	}
	if tc.result.TempDrops == nil {
		tc.result.TempDrops = make(map[ast.ExprID][]DropStep)
	}
	for id := range frame.flags {
		ty := tc.result.ExprTypes[id]
		// An argument converted on the way into a call is released as what
		// the conversion PRODUCED, not as what was written: `print(1)` against
		// `@allow_to s: &string` owes a string, not an int.
		if conv, ok := tc.result.ImplicitConversions[id]; ok && conv.Kind == ImplicitConversionTo {
			ty = conv.Target
		}
		tc.result.TempDrops[id] = tc.temporaryResidualPlan(ty, tc.tempTaken[id])
	}
}

// noteConvertedTemporary flags an argument whose implicit `__to` conversion
// produced a value nobody consumes: the parameter only borrows it, so the
// temporary is the statement's to release. The expression itself may be a
// literal or a place -- what is produced is the conversion's result.
func (tc *typeChecker) noteConvertedTemporary(exprID ast.ExprID, produced types.TypeID) {
	if len(tc.tempFrames) == 0 || !tc.isDroppableType(produced) {
		return
	}
	top := &tc.tempFrames[len(tc.tempFrames)-1]
	if top.flags == nil {
		top.flags = make(map[ast.ExprID]struct{})
	}
	top.flags[exprID] = struct{}{}
}

// recordTemporaryTaken notes that a path was moved out of an evaluation nothing
// holds, so its statement-end release can be narrowed to the remainder.
func (tc *typeChecker) recordTemporaryTaken(base ast.ExprID, path []PlaceSegment) {
	if !base.IsValid() || len(path) == 0 {
		return
	}
	if tc.tempTaken == nil {
		tc.tempTaken = make(map[ast.ExprID][][]PlaceSegment)
	}
	tc.tempTaken[base] = append(tc.tempTaken[base], path)
}

// pendingTempCandidate reports whether this evaluation is still an owned
// temporary nobody has consumed — the only kind whose release this pass may
// narrow, since anything else already has an owner that frees it whole.
func (tc *typeChecker) pendingTempCandidate(exprID ast.ExprID) bool {
	if !exprID.IsValid() {
		return false
	}
	exprID = tc.unwrapTempCandidate(exprID)
	for i := len(tc.tempFrames) - 1; i >= 0; i-- {
		if tc.tempFrames[i].flags == nil {
			continue
		}
		if _, ok := tc.tempFrames[i].flags[exprID]; ok {
			return true
		}
	}
	return false
}

// noteChoiceOwnsItsValue decides who releases a control-flow expression's
// value, from what each branch handed it.
//
// Three answers. Every branch FORWARDED a place: nothing here is this
// expression's to free, and releasing would be a use-after-free through the
// owner. Every branch MINTED one: the release is this expression's and
// unconditional. They disagree: the release is still this expression's, but
// only on the paths that built something, so the minting branches are recorded
// and the drop is emitted under a guard they set.
//
// Asked before the branch candidacies are consumed, because consuming is what
// erases the evidence: `pendingTempCandidate` answers "still an owned temporary
// nobody took" and the transfer into this expression is exactly the taking.
func (tc *typeChecker) noteChoiceOwnsItsValue(exprID ast.ExprID, branches []ast.ExprID) {
	if !exprID.IsValid() || len(branches) == 0 {
		return
	}
	// Three answers per branch, not two. A branch that is itself a GUARDED
	// choice mints on SOME of its paths: enough that this expression must own
	// and release the value, and not enough for this branch to raise the guard,
	// which its own minting branches do instead once the guard reaches them.
	minting := make([]ast.ExprID, 0, len(branches))
	sometimes := 0
	for _, branch := range branches {
		switch {
		case tc.branchMintsItsValue(branch):
			minting = append(minting, branch)
		case tc.branchSometimesMintsItsValue(branch):
			sometimes++
		}
	}
	if len(minting)+sometimes == 0 {
		// Every branch forwards: the value belongs to whoever owns the place,
		// and this expression releases nothing.
		return
	}
	tc.markChoiceOwnsItsValue(exprID)
	if sometimes == 0 && len(minting) == len(branches) {
		// Every branch built one, so the release is unconditional.
		return
	}
	// The branches DISAGREE, so the release has to ask which one ran. Recorded
	// against the same TempDrops entry `markChoiceOwnsItsValue` just earned, so
	// a context that turns out to CONSUME the value removes both at once and no
	// guarded drop is left behind.
	if tc.result.ChoiceReleaseGuards == nil {
		tc.result.ChoiceReleaseGuards = make(map[ast.ExprID][]ast.ExprID)
	}
	tc.result.ChoiceReleaseGuards[exprID] = minting
}

// branchSometimesMintsItsValue reports whether a branch is itself a choice that
// builds on some of its paths and forwards on others.
//
// Such a branch is why the enclosing choice needs a guard AND cannot raise it
// here: the value is this expression's to release when the inner built one, and
// nobody else's business when it forwarded, and only the inner branches know
// which. They raise the guard themselves once it reaches them. Left out of the
// count entirely — as an earlier version had it — a choice with such a branch on
// EVERY side owns nothing at all, and every path that built something leaks.
func (tc *typeChecker) branchSometimesMintsItsValue(branch ast.ExprID) bool {
	if !tc.pendingTempCandidate(branch) {
		return false
	}
	_, guarded := tc.result.ChoiceReleaseGuards[tc.unwrapTempCandidate(branch)]
	return guarded
}

// branchMintsItsValue reports whether a branch handed this expression a value
// it BUILT on every path, rather than one it forwarded.
//
// Being a pending temp candidate is necessary and not sufficient. The flag says
// "nothing has consumed this", not "this is the only reference": a unary or an
// index expression is flagged, yet each has a spelling that reads through to
// storage its container still owns, so `c ? (a[i]) : (b[i])` would look like two
// fresh values while both alias a live binding. Releasing that is a
// use-after-free through the binding, which is the exact hazard the blanket
// never-produce rule was avoiding. (A cast that converts NOTHING no longer
// reaches here as a cast at all — it is transparent, and the question lands on
// the operand it forwards, which is the expression that really answers it.)
//
// So the kind has to say the value was MINTED. Call, aggregate literal and
// string-literal evaluations allocate their result; a non-assigning binary is a
// magic-operator call and does too. Cast, unary and index are excluded because
// each has a spelling that forwards its operand, and being wrong there costs a
// double free while being wrong the other way costs a leak.
func (tc *typeChecker) branchMintsItsValue(branch ast.ExprID) bool {
	if !tc.pendingTempCandidate(branch) {
		return false
	}
	node := tc.builder.Exprs.Get(tc.unwrapTempCandidate(branch))
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprCall, ast.ExprStruct, ast.ExprArray, ast.ExprMap, ast.ExprTuple, ast.ExprLit:
		return true
	case ast.ExprTernary, ast.ExprCompare:
		// A nested choice that already PROVED every one of its own branches
		// minted has the same standing as a call: whichever branch ran built
		// the value. Inner choices are settled first, so the answer is here by
		// the time the outer one asks. Without this, `a ? (b ? make() : make())
		// : (c ? make() : make())` leaves the outer unflagged and leaks.
		//
		// A nested choice whose OWN release is guarded does not qualify: it
		// mints on some of its paths and forwards on others, so claiming it
		// would raise this expression's guard where nothing was built and free
		// a place its owner still holds. It keeps its own guarded release
		// instead, and this branch counts as forwarding.
		inner := tc.unwrapTempCandidate(branch)
		if _, guarded := tc.result.ChoiceReleaseGuards[inner]; guarded {
			return false
		}
		_, proven := tc.choiceOwnsItsValue[inner]
		return proven
	case ast.ExprBinary:
		data, ok := tc.builder.Exprs.Binary(tc.unwrapTempCandidate(branch))
		if !ok || data == nil {
			return false
		}
		_, isAssign := tc.assignmentBaseOp(data.Op)
		return !isAssign && data.Op != ast.ExprBinaryAssign
	default:
		return false
	}
}

// markChoiceOwnsItsValue is noteChoiceOwnsItsValue for a caller that already
// knows the answer — a compare, which must ask arm by arm as it types them and
// cannot hand over a branch list after the fact.
func (tc *typeChecker) markChoiceOwnsItsValue(exprID ast.ExprID) {
	if !exprID.IsValid() {
		return
	}
	if tc.choiceOwnsItsValue == nil {
		tc.choiceOwnsItsValue = make(map[ast.ExprID]struct{})
	}
	tc.choiceOwnsItsValue[exprID] = struct{}{}
}

func (tc *typeChecker) taintTempFrame() {
	if len(tc.tempFrames) == 0 {
		return
	}
	tc.tempFrames[len(tc.tempFrames)-1].tainted = true
}

// consumeTempCandidate removes an evaluation from the pending set: its
// value transferred somewhere that now owns it (binding, return, callee,
// aggregate position, outer control-flow expression) or escaped through
// an explicit borrow.
func (tc *typeChecker) consumeTempCandidate(exprID ast.ExprID) {
	if !exprID.IsValid() {
		return
	}
	exprID = tc.unwrapTempCandidate(exprID)
	for i := len(tc.tempFrames) - 1; i >= 0; i-- {
		if tc.tempFrames[i].flags != nil {
			delete(tc.tempFrames[i].flags, exprID)
		}
	}
}

// unwrapTempCandidate sees through the wrappers that pass a value along
// unchanged, so consumption reaches the flagged node.
//
// Grouping parentheses are one. A cast from a type to ITSELF is the other: it
// converts nothing and builds nothing, so the value it hands on is still the
// one its operand produced — and it is that operand which carries the
// candidacy. Without this, `let x = 1.5 to float` would leave the literal's
// candidacy unconsumed and release the very block the binding just took.
func (tc *typeChecker) unwrapTempCandidate(exprID ast.ExprID) ast.ExprID {
	for {
		node := tc.builder.Exprs.Get(exprID)
		if node == nil {
			return exprID
		}
		switch node.Kind {
		case ast.ExprGroup:
			group, ok := tc.builder.Exprs.Group(exprID)
			if !ok || group == nil || !group.Inner.IsValid() {
				return exprID
			}
			exprID = group.Inner
		case ast.ExprCast:
			cast, ok := tc.builder.Exprs.Cast(exprID)
			if !ok || cast == nil || !cast.Value.IsValid() {
				return exprID
			}
			if tc.castProducesItsValue(exprID, tc.result.ExprTypes[exprID]) {
				return exprID
			}
			exprID = cast.Value
		default:
			return exprID
		}
	}
}

// noteTempCandidate runs at typeExpr's single exit: flag evaluations that
// PRODUCE an owned value. Place reads (idents, member/element access,
// derefs) are owned by their containers and never flagged; assign-shaped
// binaries yield the assigned PLACE and never flag; suspension constructs
// taint the whole statement instead.
func (tc *typeChecker) noteTempCandidate(exprID ast.ExprID, kind ast.ExprKind, ty types.TypeID) {
	if len(tc.tempFrames) == 0 {
		return
	}

	switch kind {
	case ast.ExprAwait, ast.ExprSelect, ast.ExprRace, ast.ExprAsync,
		ast.ExprBlocking, ast.ExprOn, ast.ExprTask, ast.ExprSpawn:
		tc.taintTempFrame()
		return
	}

	if !tc.isDroppableType(ty) {
		return
	}

	producer := false
	switch kind {
	case ast.ExprCall, ast.ExprStruct, ast.ExprArray,
		ast.ExprMap, ast.ExprTuple:
		producer = true
	case ast.ExprCast:
		producer = tc.castProducesItsValue(exprID, ty)
	// A control-flow expression produces only when EVERY branch handed it a
	// freshly produced owned value — see noteChoiceOwnsItsValue. It cannot be
	// flagged unconditionally, because its value can forward a PLACE from any
	// branch (an assignment arm yields its target's live value, and a branch
	// that names a binding yields the binding's), and releasing that is a
	// use-after-free through the owner — caught by the VM sanitizer on the
	// compare-arm-mutation row.
	//
	// The all-branches test is what separates the two: a branch that forwards a
	// place was never a temp candidate, so one such branch is enough to leave
	// this unflagged and keep the old, safe behavior. MIXED branches therefore
	// still leak whatever the producing side built, which is the remaining
	// bound.
	case ast.ExprTernary, ast.ExprCompare:
		_, producer = tc.choiceOwnsItsValue[exprID]
	case ast.ExprLit:
		producer = true // string literals heap-allocate per use
	case ast.ExprBinary:
		if data, ok := tc.builder.Exprs.Binary(exprID); ok && data != nil {
			if _, isAssign := tc.assignmentBaseOp(data.Op); !isAssign && data.Op != ast.ExprBinaryAssign {
				producer = true
			}
		}
	case ast.ExprUnary:
		if tc.result.MagicUnarySymbols != nil {
			_, producer = tc.result.MagicUnarySymbols[exprID]
		}
	case ast.ExprIndex:
		// Only slice expressions MINT a value - an owned view header for an
		// array, a whole new string for a string - while element reads stay
		// owned by the container. See mintsOwnedValue: the bound form asks the
		// same question one predicate away, and the two answering differently
		// is what leaked an array view header and then a sliced string.
		producer = tc.mintsOwnedValue(exprID)
	}
	if !producer {
		return
	}

	top := &tc.tempFrames[len(tc.tempFrames)-1]
	if top.flags == nil {
		top.flags = make(map[ast.ExprID]struct{})
	}
	top.flags[exprID] = struct{}{}
}

// castProducesItsValue answers the producer question for `to`, whose two
// lowerings own their result differently:
//
//   - a `to` that resolved to a `__to` method is a CALL, and its result is a
//     freshly produced value like any other call's;
//   - an INTRINSIC cast produces one only when it changes representation.
//     Casting a type to itself hands the source straight back, so the result is
//     a second name for storage its owner still holds — flagging it would
//     release that storage out from under the owner.
//
// The identity question is settled here for good: `to` on a type PARAMETER is
// rejected (SEM3015), so no cast can become an identity only after
// monomorphization.
func (tc *typeChecker) castProducesItsValue(exprID ast.ExprID, ty types.TypeID) bool {
	if tc.result == nil || tc.result.ExprTypes == nil || ty == types.NoTypeID {
		return true
	}
	if _, isCall := tc.result.ToSymbols[exprID]; isCall {
		return true
	}
	cast, ok := tc.builder.Exprs.Cast(exprID)
	if !ok || cast == nil || !cast.Value.IsValid() {
		return true
	}
	sourceTy, ok := tc.result.ExprTypes[cast.Value]
	if !ok || sourceTy == types.NoTypeID {
		return true
	}
	return tc.resolveAlias(sourceTy) != tc.resolveAlias(ty)
}
