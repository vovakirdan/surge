package sema

import (
	"fmt"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A shared reference grants read access and nothing else, so no place reached
// through one may be written.
//
// This is the mirror of isWriteThroughMutRef. That predicate exists because a
// write through `&mut` is the whole point of an exclusive borrow and must not
// be reported as a conflict with the borrow it IS; this one exists because the
// same write through `&T` is not a conflict either — it is simply not allowed,
// and nothing downstream was asking.
//
// It has to be answered here, in the one place that still distinguishes the two
// reference kinds. A backend cannot: the VM carries a mutability bit on every
// location and traps on the store, while a native reference is a bare pointer
// with nowhere to keep that bit, so the identical store lands in the referent
// and the program keeps running with the wrong answer. Two backends disagreeing
// about what a program means is the failure this refusal removes.

// refuseDisallowedStore asks whether the language permits this write AT ALL,
// and reports it when it does not.
//
// Both refusals it gathers have to be asked here, before any of handleAssignment's
// bookkeeping and before the place is expanded through its borrow: a write the
// language does not allow has no drop to record and no place to revive, and
// expanding it first reports the referent's borrow conflicting with itself
// rather than naming what actually cannot carry the write.
//
// The two are the same shape asked of two things — a place reached through a
// shared reference belongs to whoever lent it, and a `for` loop binding is a
// copy the container still owns.
func (tc *typeChecker) refuseDisallowedStore(desc placeDescriptor, span source.Span) bool {
	if tc.storesThroughSharedRef(desc) {
		tc.reportStoreThroughSharedRef(desc, span)
		return true
	}
	return tc.rejectWriteToLoopBinding(desc, span)
}

// storesThroughSharedRef reports whether writing to this place would write
// through a shared reference.
//
// The question is asked of the ROOT binding and any projection off it. The
// deref is usually implicit — `r.x = 1` on `r: &Leaf` carries no deref segment,
// because the surface lets a projection reach through a reference on its own —
// so the segment kinds say nothing useful and only their PRESENCE matters: with
// no segments the assignment rebinds the reference itself, which is a write to
// the local and is fine, and with any segment at all it reaches the referent.
func (tc *typeChecker) storesThroughSharedRef(desc placeDescriptor) bool {
	if tc == nil || tc.types == nil || !desc.Base.IsValid() || len(desc.Segments) == 0 {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(tc.bindingType(desc.Base)))
	if !ok {
		return false
	}
	return tt.Kind == types.KindReference && !tt.Mutable
}

// reportStoreThroughSharedRef refuses the write and says which reference is in
// the way.
//
// The headline names the edit rather than the rule, because the rule alone
// leaves an author holding a `&T` with nothing to do about it. Where the
// reference was declared is a second span whenever the symbol carries one: the
// write and the declaration are usually far apart, and the write is not the
// line that has to change.
//
// No automatic fix is offered. Widening `&T` to `&mut T` at a declaration is
// only half the edit — every borrow feeding it has to widen too, and for a
// parameter that means every caller — so a single-site rewrite would hand back
// a program that does not compile. The alternative edit, cloning the referent
// and writing to the clone, changes what the program costs and sometimes what
// it means. Both are real choices, and neither is the compiler's to make.
func (tc *typeChecker) reportStoreThroughSharedRef(desc placeDescriptor, span source.Span) {
	if tc.reporter == nil {
		return
	}
	base := tc.bindingName(desc.Base)
	label := tc.writtenPlaceLabel(desc)
	pointee := tc.typeLabel(tc.valueType(tc.bindingType(desc.Base)))

	b := diag.ReportError(tc.reporter, diag.SemaStoreThroughSharedRef, span,
		fmt.Sprintf("cannot write to `%s`: `%s` is a shared reference, so declare it `&mut %s` to write through it",
			label, base, pointee))
	if b == nil {
		return
	}
	b.WithNote(span, fmt.Sprintf(
		"`&%s` lends read access only: several holders may borrow the same value at once, and each one is "+
			"entitled to keep seeing what it borrowed, which this write would change under them",
		pointee))
	if declSpan, ok := tc.bindingDeclSpan(desc.Base); ok {
		b.WithNote(declSpan, fmt.Sprintf("`%s` is declared shared here", base))
	}
	b.WithNote(span, fmt.Sprintf(
		"hint: widen the borrow that produced `%s` to `&mut` as well, or clone what it points at and write "+
			"to the clone if the original must not change",
		base))
	b.Emit()
}

// writtenPlaceLabel renders the place the way its author wrote it.
//
// The shared renderer spells a dereference as a trailing `.*`, which suits a
// borrow-table dump but turns `*r` into "r.*" in a sentence addressed to
// someone reading their own source. Here the label appears inside the headline,
// so it is built back into surface syntax: a deref moves to the front of what
// it applies to, and takes parentheses whenever a projection follows it.
func (tc *typeChecker) writtenPlaceLabel(desc placeDescriptor) string {
	label := tc.bindingName(desc.Base)
	if label == "" {
		label = "value"
	}
	strs := tc.placeStringInterner()
	for i, seg := range desc.Segments {
		switch seg.Kind {
		case PlaceSegmentDeref:
			if i+1 < len(desc.Segments) {
				label = "(*" + label + ")"
				continue
			}
			label = "*" + label
		case PlaceSegmentField:
			name := "?"
			if strs != nil && seg.Name != source.NoStringID {
				name = strs.MustLookup(seg.Name)
			}
			label += "." + name
		case PlaceSegmentTupleIndex:
			label += fmt.Sprintf(".%d", seg.Elem)
		case PlaceSegmentIndex:
			label += "[..]"
		default:
			label += ".?"
		}
	}
	return label
}

// bindingDeclSpan returns where a binding was introduced, when the symbol
// carries a span worth pointing at.
func (tc *typeChecker) bindingDeclSpan(symID symbols.SymbolID) (source.Span, bool) {
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Span == (source.Span{}) {
		return source.Span{}, false
	}
	return sym.Span, true
}

// placeStringInterner returns the interner that renders field names in place
// labels, preferring the builder's and falling back to the symbol table's.
func (tc *typeChecker) placeStringInterner() *source.Interner {
	if tc.builder != nil && tc.builder.StringsInterner != nil {
		return tc.builder.StringsInterner
	}
	if tc.symbols != nil && tc.symbols.Table != nil {
		return tc.symbols.Table.Strings
	}
	return nil
}
