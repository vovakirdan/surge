package hir

import (
	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// The compare-scrutinee-release half of normalizeCompareExpr: deciding
// WHETHER a `compare` may free its lowering temp's union box, and WHICH
// shape of free (shallow envelope release vs. deep drop) each arm needs.
//
// Root cause (RV2-DEBT-052): `compare v { ... }` desugars to a synthetic
// `let __cmpN = v` created in the HIR normalize pass, AFTER sema computed
// drop obligations — so `__cmpN` never acquires a scope-exit drop, and its
// union box leaks on every evaluated compare. The fix mirrors
// RV2-DEBT-040's for-loop iterator envelope: synthesize the release
// ourselves, per arm, right before that arm's `return`.

// scrutineeOwnership says what `__cmpN` holds once `let __cmpN = v` has
// run, which is the only thing that decides whether this compare may free
// it and how.
//
// The question is NOT what type the scrutinee has. MIR decides whether a
// consuming read transfers or duplicates at the USE SITE (see
// placeOperand): a non-Copy value MOVES, while a Copy value composite is
// heap-boxed underneath and DUPLICATES into an independent clone. Both
// leave this compare owning something it has to reclaim; only a read that
// did neither leaves it owning nothing.
type scrutineeOwnership int

const (
	// scrutineeBorrowed: the read only LOOKED at storage someone else
	// owns and still holds. Freeing anything here would reclaim it out
	// from under its real owner, who frees it again.
	scrutineeBorrowed scrutineeOwnership = iota
	// scrutineeMoved: the read TRANSFERRED the original's box. The
	// payload comes with it, so an arm that binds every payload position
	// takes the payload away and leaves only the envelope to free.
	scrutineeMoved
	// scrutineeDuplicated: the read produced an INDEPENDENT clone. The
	// original is untouched and stays usable — and the clone's payload
	// stays inside the clone, because the arm's binding duplicates it
	// again rather than taking it out.
	scrutineeDuplicated
)

// compareScrutineeOwnership classifies how the scrutinee reached
// normalizeCompareExpr's `__cmpN` temp. Errs toward scrutineeBorrowed: a
// caller that cannot prove this compare owns the value keeps the
// pre-existing (leaky) behavior rather than risk freeing storage someone
// else still owns.
//
// Scoped to union-typed scrutinees, because that is the only shape whose
// arms carry the tag/payload structure the release decisions depend on. A
// tuple, string, or struct scrutinee compare is left as a separate,
// unfiled gap.
func compareScrutineeOwnership(ctx *normCtx, value *Expr, ty types.TypeID) scrutineeOwnership {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || ty == types.NoTypeID {
		return scrutineeBorrowed
	}
	typesIn := ctx.mod.TypeInterner
	resolved := resolveAlias(typesIn, ty, 0)
	if tt, ok := typesIn.Lookup(resolved); ok {
		switch tt.Kind {
		case types.KindReference, types.KindPointer:
			// A reference-typed scrutinee is a POINTER value: reading it
			// copies the pointer, never the pointee, so this compare
			// holds nothing of its own.
			return scrutineeBorrowed
		}
	}
	unwrapped := stripOwnType(typesIn, resolved)
	tt, ok := typesIn.Lookup(unwrapped)
	if !ok || tt.Kind != types.KindUnion {
		return scrutineeBorrowed
	}

	if typesIn.IsCopy(resolved) {
		// Copy at the surface. If it is ALSO a value composite it is
		// heap-boxed underneath, and consuming it duplicates the box —
		// this compare owns that clone and nothing else refers to it.
		// Freeing it cannot disturb the original, which is exactly why
		// the deref below does not disqualify this case.
		if typesIn.IsValueComposite(resolved) {
			return scrutineeDuplicated
		}
		// Copy and not boxed: there is no allocation to reclaim, and
		// sema leaves the original binding usable.
		return scrutineeBorrowed
	}

	if scrutineeReadsThroughBorrow(value) {
		// `compare *r { ... }` where `r` borrows a MOVE-ONLY union: the
		// deref STRIPS the reference, so the type test above sees a plain
		// union and would wrongly conclude this compare owns the box.
		// Moving out of a borrow is not this pass's to do — the referent
		// belongs to someone else, who drops it again.
		//
		// Only the moving case is disqualified. A duplicating read
		// through the same deref is fine, and returned above.
		return scrutineeBorrowed
	}
	return scrutineeMoved
}

// scrutineeReadsThroughBorrow reports whether the scrutinee expression only
// READS a value someone else owns, so that no ownership reached the compare's
// temp however the expression's type reads.
//
// The type test alone is not enough. `*r` on an `&FmtArg` has type `FmtArg`,
// not `&FmtArg` — the deref already stripped the reference — so a type-only
// check sees a bare union and concludes the compare may free its box. It may
// not: the box belongs to whoever the borrow points at, and they free it too.
// That is a double free on the native backend (the VM survives it only because
// its heap is counted, which is why this never showed in the differential).
//
// Parenthesised and `own`-wrapped forms are unwrapped so the check cannot be
// dodged by spelling. `&x` / `&mut x` are not scrutinee reads-through: taking a
// reference produces a reference-typed value, which the type test below already
// refuses.
func scrutineeReadsThroughBorrow(value *Expr) bool {
	for value != nil {
		data, ok := value.Data.(UnaryOpData)
		if !ok {
			return false
		}
		switch data.Op {
		case ast.ExprUnaryDeref:
			return true
		case ast.ExprUnaryOwn:
			value = data.Operand
		default:
			return false
		}
	}
	return false
}

// tagPayloadReleaseShape classifies a tag arm's payload patterns for
// release purposes. shallow reports whether every payload position was
// extracted via a plain binding (or there was no payload at all) — safe
// to free the envelope box shallowly, since every heap-owning field is
// now aliased by its own binding and will be dropped there. safe reports
// whether the classification is unambiguous at all: a MIXED arm (some
// positions bound, others not) or a nested tag/tuple/literal pattern at
// a payload position can leave a field's ownership unclear (a shallow
// free would leak an un-bound field; a deep drop would double-free a
// bound one) — safe is false in that case and the caller must skip
// release entirely for this arm, matching the errs-toward-false
// direction used by RV2-DEBT-040's cursor-release safety gate.
// takenOut marks the positions something already took out of the envelope,
// whether the pattern named them or `claimIgnoredPayloads` gave them a binding
// of their own. A MIXED arm becomes all-bound that way, and the one shape left
// unsafe is a nested pattern, whose disposition is still unknown.
func tagPayloadReleaseShape(ctx *normCtx, payload []*Expr, owned scrutineeOwnership, takenOut []bool) (shallow, safe bool) {
	if owned == scrutineeDuplicated {
		// The scrutinee is a clone, so an arm's payload binding CLONES
		// AGAIN rather than taking the payload out (placeOperand answers
		// CopyValue for a Copy value composite, and a payload that is a
		// plain scalar is copied outright). Either way this clone keeps
		// its own payload, so the only correct release is the deep one —
		// a shallow envelope free would abandon the payload box.
		//
		// No arm shape escapes this: nothing was taken out, so there is
		// nothing a deep drop could double-free.
		return false, true
	}
	if len(payload) == 0 {
		return false, true
	}
	allBound := true
	allUnbound := true
	for i, pat := range payload {
		if i < len(takenOut) && takenOut[i] {
			// Named by the pattern, or claimed by a binding synthesized for
			// it: either way this field has an owner outside the envelope.
			allUnbound = false
			continue
		}
		if pat == nil {
			allBound = false
			continue
		}
		if _, _, ok := bindingPattern(ctx, pat); ok {
			allUnbound = false
			continue
		}
		if isWildcardPattern(pat) || isNothingPattern(pat) {
			allBound = false
			continue
		}
		// Nested tag/tuple pattern, or a literal-equality comparison:
		// the field is read but its ownership disposition can't be
		// determined without recursing into destructuring this
		// function doesn't model. Refuse to guess.
		return false, false
	}
	switch {
	case allBound:
		return true, true
	case allUnbound:
		return false, true
	default:
		return false, false
	}
}

// claimIgnoredPayloads gives every payload position the pattern ignored a
// binding of its own, so an arm that bound SOME of its fields ends up having
// taken out all of them.
//
// That is the whole mixed-arm residual: the envelope could not be freed either
// way while one field was still inside it and its siblings were not. The
// binding is the same `let` a named position gets, dropped by the same
// arm-return drop list, so nothing new decides ownership — the pattern's
// silence stops meaning "leave it in the box".
//
// Positions that own no heap are skipped: they need no owner, and adding one
// would put a drop on a machine word. Positions whose type is unknown are
// skipped too, which leaves the arm exactly as unsafe as it was.
func claimIgnoredPayloads(
	ctx *normCtx,
	span source.Span,
	subject *Expr,
	tag string,
	payload []*Expr,
	takenOut []bool,
	body *Block,
	owned *[]DropLocal,
) {
	if ctx == nil || subject == nil || body == nil || owned == nil {
		return
	}
	for i, pat := range payload {
		if i < len(takenOut) && takenOut[i] {
			continue
		}
		// Only a position the pattern IGNORED. A nested pattern already took
		// parts of this field out, and a literal one compares it in place —
		// binding either here would claim what somebody else holds.
		if pat != nil && !isWildcardPattern(pat) {
			continue
		}
		payloadType := tagPayloadType(ctx, subject.Type, tag, i)
		if payloadType == types.NoTypeID {
			continue
		}
		if !ctx.ownsHeap(payloadType) {
			// Bits, not storage: nothing to give an owner, and nothing the
			// envelope's shallow free could abandon. Counted as taken out so a
			// field like the `int` in `Pair(s, _)` stops blocking the release
			// of the box around it.
			takenOut[i] = true
			continue
		}
		if ctx.clonesWhenRead(payloadType) {
			// A `@copy` composite DUPLICATES when it is read, so a binding here
			// would hold a clone while the original stayed in the envelope —
			// and calling the position "taken out" would then free that
			// envelope shallowly and abandon the original. Left untouched, so
			// the arm keeps the classification it had: all-unbound still
			// deep-drops, which reclaims the payload correctly, and a mixed arm
			// stays refused. Measured after a review caught it: 16 bytes per
			// evaluation of `compare Hold(Pair{...}) { Hold(_) => 1; ... }`,
			// where `Pair` is `@copy` — a shape the string-payload censuses
			// could not see.
			continue
		}
		sym, name := ctx.newTemp("ignored_payload")
		body.Stmts = append(body.Stmts, Stmt{
			Kind: StmtLet,
			Span: span,
			Data: LetData{
				Name:     name,
				SymbolID: sym,
				Type:     payloadType,
				Value: &Expr{
					Kind: ExprTagPayload,
					Type: payloadType,
					Span: span,
					Data: TagPayloadData{Value: subject, TagName: tag, Index: i, SubjectBorrowed: false},
				},
				Ownership: ctx.inferOwnership(payloadType),
			},
		})
		*owned = append(*owned, DropLocal{SymbolID: sym, Type: payloadType, Span: span})
		takenOut[i] = true
	}
}

// clonesWhenRead reports whether reading a value of this type DUPLICATES it
// rather than taking it out of where it was.
//
// That is the Copy side of the ownership axes: a `@copy` composite is
// heap-boxed and its read allocates a second box (`OperandCopyValue`), so the
// original never leaves its container. Anything that transfers answers false.
func (ctx *normCtx) clonesWhenRead(ty types.TypeID) bool {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || ty == types.NoTypeID {
		return false
	}
	typesIn := ctx.mod.TypeInterner
	return typesIn.IsCopy(resolveAlias(typesIn, ty, 0))
}

// ownsHeap reports whether a value of this type has something to reclaim. It
// answers from the interner alone — the normalize pass runs past sema's
// checker — through the axis's interner-only leg (`sema.OwnsHeapIn`), so a
// composite of plain scalars is bits here exactly as it is for sema's drop
// obligations and MIR's drop emission. An unknown shape errs toward YES there,
// so it keeps whatever release it had rather than losing one.
func (ctx *normCtx) ownsHeap(ty types.TypeID) bool {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || ty == types.NoTypeID {
		return false
	}
	return sema.OwnsHeapIn(ctx.mod.TypeInterner, ty)
}

// envelopeReleaseStmt builds a shallow, own-declared-type box free of
// subject (see EnvelopeReleaseData) — used when the taken arm moved the
// scrutinee's payload out to a binding, so only the envelope itself
// remains to reclaim.
func envelopeReleaseStmt(subject *Expr) Stmt {
	return Stmt{Kind: StmtEnvelopeRelease, Span: subject.Span, Data: EnvelopeReleaseData{Value: subject, Cursor: false}}
}

// deepDropStmt builds a full drop of subject through its type's drop
// glue (see StmtDrop) — used whenever the taken arm did NOT move the
// scrutinee's payload out, so the payload (if any) is only reachable
// through this box and must be reclaimed along with it.
func deepDropStmt(subject *Expr) Stmt {
	return Stmt{Kind: StmtDrop, Span: subject.Span, Data: DropData{Value: subject}}
}

// armReturnStmts builds the statements for a compare arm's taken branch:
// the release (if any) immediately followed by the arm's return. release
// is nil whenever no release applies to this arm (release-unsafe
// scrutinee, or an arm that aliased the WHOLE scrutinee box into a live
// binding — see the bindingPattern case in lowerCompareArm, which never
// calls this helper with a non-nil release for exactly that reason).
func armReturnStmts(span source.Span, release *Stmt, result *Expr, owned []DropLocal) []Stmt {
	ret := mkReturnWithDrops(span, result, owned)
	if release == nil {
		return []Stmt{ret}
	}
	return []Stmt{*release, ret}
}
