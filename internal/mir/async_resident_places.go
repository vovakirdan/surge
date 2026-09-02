package mir

import (
	"fmt"
	"sort"

	"surge/internal/sema"
	"surge/internal/symbols"
)

// RESIDENT PLACES: the locals an activation may not move under a live child.
//
// An ordinary local of an async body is a per-poll slot. A suspension packs the
// live ones into the frame's payload union and the next poll unpacks them into
// FRESH slots, so a child task that kept a pointer to one is left pointing at
// storage the parent no longer uses -- even when both run on the same carrier.
// `docs/RUNTIME_V2.md` section 9 and section 7 of the storage model say what has
// to be true instead: a place borrowed by a live carrier-affine child has ONE
// stable storage identity from the child's publication until its completion.
//
// A resident is that identity. It is a fixed-offset field of the activation's
// own frame, so:
//
//   - it is not packed at a suspension and not unpacked at a resume, because it
//     never left;
//   - every read and write of the local addresses the frame field instead of a
//     slot, so parent and child see one storage rather than two copies;
//   - the address is the frame's, and the frame belongs to the activation.
//
// Expressing a resident as an ORDINARY struct-field place on the state local is
// what keeps this change inside MIR: the backends already lower a field
// projection, so the frame GEP the storage model asks of LLVM and the persistent
// arena offset it asks of the VM both fall out of machinery that exists. Nothing
// downstream has to learn a new kind of place.
//
// Promotion is PLACE-oriented and selective: only the bindings sema named in
// Result.StableActivationPlaces are promoted, which is only those whose address
// entered a task capture. No type is promoted, and no local is promoted for
// merely living across an await.
//
// Promotion also only happens where it is NEEDED. A plain `fn` cannot suspend:
// its frame is stable for the whole call and the structured scope joins the
// child before the frame dies, so a borrow out of one is already sound and gets
// no resident. Sema names those places anyway -- it answers "which storage does a
// child constrain", not "which storage moves" -- and this pass simply never asks
// about a non-async activation, because it only runs over async ones.

const asyncResidentFieldPrefix = "__resident$"

// residentFieldName names a resident's field. The local id is part of the name
// because two bindings in one activation may share a source name across scopes,
// and a struct cannot hold two fields called the same thing.
func residentFieldName(id LocalID, name string) string {
	if name == "" {
		name = "local"
	}
	return fmt.Sprintf("%s%s$%d", asyncResidentFieldPrefix, name, int64(id))
}

// constrainedBindings is every BINDING sema said a live child constrains, across
// all activations.
//
// Sema files those bindings under an activation, and the obvious way to use that
// map is to ask it for the activation being lowered. It does not survive the trip
// here, and the reason is worth stating because it looks like it should: a
// function's symbol is REWRITTEN by monomorphization. `resident_parent` reaches
// MIR carrying an instance id (0x90000001) where sema had recorded 1450, so an
// activation-keyed lookup silently matches nothing for every `async fn` in the
// language, while blocks -- which carry no symbol and are keyed by span -- keep
// working. A green test suite and a promotion that never fires.
//
// The binding symbols themselves DO survive: the local holding `v` still carries
// the very id sema named. So the lookup is by binding, which is not a workaround
// but the more accurate question. A binding symbol is one declaration, so it can
// only appear as a local of the activation that declared it -- attribution comes
// out right without being asked for. And a generic async function instantiated
// twice yields two functions that both carry that binding, so BOTH get the
// resident, which is what correctness requires and what the activation key, being
// per-instance after mono, would have got wrong in the other direction.
func constrainedBindings(semaRes *sema.Result) map[symbols.SymbolID]struct{} {
	if semaRes == nil || len(semaRes.StableActivationPlaces) == 0 {
		return nil
	}
	out := make(map[symbols.SymbolID]struct{})
	for _, bindings := range semaRes.StableActivationPlaces {
		for _, sym := range bindings {
			if sym.IsValid() {
				out[sym] = struct{}{}
			}
		}
	}
	return out
}

// asyncPromotedLocals picks out the locals of one activation that hold a
// constrained binding, in a deterministic order. Returns nil when the activation
// constrains no storage, which is the ordinary case.
//
// It is only ever asked of an async function, which is also why no promotion
// happens in a plain `fn`: that frame is stable for the whole call and the
// structured scope joins the child before it dies, so a borrow out of one is
// already sound. Sema names those places too -- it answers which storage a child
// constrains, not which storage moves -- and they simply go unclaimed.
func asyncPromotedLocals(pollFn *Func, constrained map[symbols.SymbolID]struct{}) []LocalID {
	if pollFn == nil || len(constrained) == 0 {
		return nil
	}
	var out []LocalID
	for id := range pollFn.Locals {
		local := pollFn.Locals[id]
		if !local.Sym.IsValid() {
			continue
		}
		if _, ok := constrained[local.Sym]; ok {
			out = append(out, LocalID(id))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// residentSet is the promoted locals of one activation, with the field each one
// lives in.
type residentSet struct {
	fields map[LocalID]string
	order  []LocalID
}

func newResidentSet(f *Func, promoted []LocalID) residentSet {
	set := residentSet{fields: make(map[LocalID]string, len(promoted)), order: promoted}
	for _, id := range promoted {
		name := ""
		if int(id) < len(f.Locals) {
			name = f.Locals[id].Name
		}
		set.fields[id] = residentFieldName(id, name)
	}
	return set
}

func (r residentSet) empty() bool { return len(r.fields) == 0 }

func (r residentSet) has(id LocalID) bool {
	_, ok := r.fields[id]
	return ok
}

// without drops the residents from a variant's local list: a resident is not
// packed at a suspension and not unpacked at a resume, because it never left the
// frame. It returns a fresh slice rather than filtering in place, because the
// caller's slice is the liveness set and is read again afterwards.
func (r residentSet) without(locals []LocalID) []LocalID {
	if r.empty() || len(locals) == 0 {
		return locals
	}
	out := make([]LocalID, 0, len(locals))
	for _, id := range locals {
		if !r.has(id) {
			out = append(out, id)
		}
	}
	return out
}

// rewritePlaces redirects every use of a promoted local to its frame field, so
// that the parent and the child it lent the place to read and write ONE storage.
//
// A projection already on the place is KEPT and follows the field, which is what
// keeps a promoted composite's own field read working: `v.x` becomes
// `__state.__resident$v$7.x` rather than losing the `.x`.
//
// The walk's error is returned rather than dropped. A place the walker does not
// know is precisely the case this pass cannot survive: the missed use would keep
// addressing the vacated slot while every other use addresses the field, and the
// two would disagree with no crash and no diagnostic.
func (r residentSet) rewritePlaces(f *Func, stateLocal LocalID) error {
	if r.empty() || f == nil || stateLocal == NoLocalID {
		return nil
	}
	return forEachPlace(f, func(p *Place) {
		if p == nil || p.Kind != PlaceLocal || !r.has(p.Local) {
			return
		}
		proj := make([]PlaceProj, 0, len(p.Proj)+1)
		proj = append(proj, PlaceProj{Kind: PlaceProjField, FieldName: r.fields[p.Local], FieldIdx: -1})
		proj = append(proj, p.Proj...)
		p.Local = stateLocal
		p.Proj = proj
	})
}
