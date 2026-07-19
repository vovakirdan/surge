package hir

import (
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

// compareScrutineeReleaseSafe reports whether normalizeCompareExpr may
// synthesize ANY release of its `__cmpN` scrutinee temp. Errs toward
// false: a caller that can't prove ownership genuinely transferred into
// the temp keeps the pre-existing (leaky) behavior rather than risk
// freeing storage someone else still owns.
//
// Ownership only transfers when sema's move tracking would mark the
// original scrutinee binding moved (observeMove skips move-tracking, and
// leaves the original binding valid, exactly when the value is Copy) —
// so a Copy or borrowed/pointer scrutinee must get no release at all. The
// fix is additionally scoped to union-typed scrutinees: that is the
// shape RV2-DEBT-052 observed leaking (every `TaskResult` consumed from
// `t.await()`), and it is the only shape whose arms carry the
// tag/payload structure the shallow-vs-deep decision below depends on.
// A tuple, string, or struct scrutinee compare is left as a separate,
// unfiled gap.
func compareScrutineeReleaseSafe(ctx *normCtx, ty types.TypeID) bool {
	if ctx == nil || ctx.mod == nil || ctx.mod.TypeInterner == nil || ty == types.NoTypeID {
		return false
	}
	typesIn := ctx.mod.TypeInterner
	resolved := resolveAlias(typesIn, ty, 0)
	if tt, ok := typesIn.Lookup(resolved); ok {
		switch tt.Kind {
		case types.KindReference, types.KindPointer:
			// Borrowed or raw-pointer scrutinee: this compare never took
			// ownership of the box, so freeing it here would reclaim
			// storage its actual owner still holds.
			return false
		}
	}
	if typesIn.IsCopy(resolved) {
		// Copy scrutinee: sema's observeMove never marks the original
		// binding moved for a Copy type, so it stays valid (and
		// re-usable) after the compare — releasing __cmpN's box would
		// free storage the original owner can still read.
		return false
	}
	unwrapped := stripOwnType(typesIn, resolved)
	tt, ok := typesIn.Lookup(unwrapped)
	return ok && tt.Kind == types.KindUnion
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
func tagPayloadReleaseShape(ctx *normCtx, payload []*Expr) (shallow, safe bool) {
	if len(payload) == 0 {
		return false, true
	}
	allBound := true
	allUnbound := true
	for _, pat := range payload {
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
func armReturnStmts(span source.Span, release *Stmt, result *Expr) []Stmt {
	ret := mkReturn(span, result)
	if release == nil {
		return []Stmt{ret}
	}
	return []Stmt{*release, ret}
}
