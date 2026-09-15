package sema

import (
	"slices"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// An external cell is caller-owned storage holding one shared string reference,
// reached through a formal `& &string` (read-only) or `&mut &string`. The
// formal's value V(slot) is that cell's address only when every root of a value
// proves it; the cell's contents on entry are R(slot). This is a bounded
// precision boundary: other layers, nominal or generic payloads, and cells a
// caller keeps in its own locals stay on their existing Pending.

// returnOriginResolveAlias follows aliases with a visited set, so a cyclic alias
// answers NoTypeID instead of looping.
func returnOriginResolveAlias(in *types.Interner, id types.TypeID) types.TypeID {
	seen := make(map[types.TypeID]bool)
	for in != nil && id != types.NoTypeID && !seen[id] {
		seen[id] = true
		target, alias := in.AliasTarget(id)
		if !alias {
			return id
		}
		id = target
	}
	return types.NoTypeID
}

// returnOriginCellDescriptor admits exactly `& &string` and `&mut &string`
// after alias resolution, and says whether the outer reference is mutable.
func returnOriginCellDescriptor(in *types.Interner, id types.TypeID) (mutable, ok bool) {
	if in == nil {
		return false, false
	}
	outer, found := in.Lookup(returnOriginResolveAlias(in, id))
	if !found || outer.Kind != types.KindReference {
		return false, false
	}
	inner, found := in.Lookup(returnOriginResolveAlias(in, outer.Elem))
	if !found || inner.Kind != types.KindReference || inner.Mutable || returnOriginResolveAlias(in, inner.Elem) != in.Builtins().String {
		return false, false
	}
	return outer.Mutable, true
}

// externalCellRoster reads a monomorphic function's cell formals once, from its
// original FnInfo and the parameter symbols that agree with it in type and scope.
func (u *returnOriginUnitIndex) externalCellRoster(f *returnOriginFunction) (cells, mutable []uint32) {
	in := u.Sema.TypeInterner
	if f.candidate == nil || len(f.candidate.TemplateParams) != 0 || types.ContainsGenericParam(in, f.info.Result) {
		return nil, nil
	}
	for _, param := range f.info.Params {
		if types.ContainsGenericParam(in, param) {
			return nil, nil
		}
	}
	for i, param := range f.info.Params {
		isMutable, cell := returnOriginCellDescriptor(in, param)
		sym := u.Symbols.Table.Symbols.Get(f.params[i])
		if !cell || sym == nil || sym.Kind != symbols.SymbolParam || sym.Scope != f.scope || sym.Type != param {
			continue
		}
		slot, err := safecast.Conv[uint32](i)
		if err != nil {
			return nil, nil
		}
		cells = append(cells, slot)
		if isMutable {
			mutable = append(mutable, slot)
		}
	}
	return cells, mutable
}

// externalCellTargets proves which of this function's cells a value addresses.
// Every root must be an unexpired V(slot) of a roster formal whose descriptor
// agrees with the actual type; one Unknown, Local, Capture or R root refuses the
// whole proof, so a singleton that survived a join with Unknown is no singleton.
func (fn *returnOriginFunction) externalCellTargets(id types.TypeID, value returnOriginValue) (targets []uint32, reason string) {
	in := fn.unit.Sema.TypeInterner
	mutable, cell := returnOriginCellDescriptor(in, id)
	if !cell {
		return nil, "value is not an admitted external cell reference"
	}
	if !value.normal || len(value.roots) == 0 || len(value.callables) != 0 {
		return nil, "external cell reference has no complete root set"
	}
	targets = make([]uint32, 0, len(value.roots))
	for _, root := range value.roots {
		if root.kind != returnOriginParam || root.selector != returnOriginInputValue || root.expired || !slices.Contains(fn.cellSlots, root.param) {
			return nil, "external cell reference is not an unexpired admitted formal cell"
		}
		if formal, _ := returnOriginCellDescriptor(in, fn.info.Params[root.param]); formal != mutable {
			return nil, "external cell reference disagrees with its formal descriptor"
		}
		targets = append(targets, root.param)
	}
	slices.Sort(targets)
	return slices.Compact(targets), ""
}

// initExternalCells seeds C_i = R_i; the formal's own binding stays V_i.
func (fn *returnOriginFunction) initExternalCells(env returnOriginEnv) returnOriginEnv {
	for _, slot := range fn.cellSlots {
		env = env.withCell(slot, returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: slot, selector: returnOriginInputContents}))
	}
	return env
}

// loadExternalCells answers a dereference whose operand proves its targets:
// the join of what those cells hold now, as a detached snapshot.
func (b *returnOriginBody) loadExternalCells(operand types.TypeID, target returnOriginValue, env returnOriginEnv, span source.Span) (returnOriginValue, bool) {
	targets, reason := b.function.externalCellTargets(operand, target)
	if reason != "" || !env.reachable {
		return returnOriginValue{}, false
	}
	return b.cellContents(env, targets, span), true
}

func (b *returnOriginBody) cellContents(env returnOriginEnv, targets []uint32, span source.Span) returnOriginValue {
	out := returnOriginValue{}
	for _, slot := range targets {
		if _, present := env.cells[slot]; !present {
			b.pending(span, "external cell contents are missing on a reachable path")
		}
		out = out.join(env.cell(slot))
	}
	return out
}

// storeExternalCells writes an evaluated RHS through a typed dereference whose
// frozen storage proves its targets. One target replaces its contents; several
// targets, and every other admitted cell that might alias one, only gain it.
func (b *returnOriginBody) storeExternalCells(left ast.ExprID, storage, rhs returnOriginValue, env returnOriginEnv) (returnOriginEnv, bool) {
	fn := b.function
	unary, ok := fn.unit.Builder.Exprs.Unary(left)
	if !ok || unary == nil || unary.Op != ast.ExprUnaryDeref || !env.reachable || !rhs.normal || len(rhs.callables) != 0 {
		return env, false
	}
	targets, reason := fn.externalCellTargets(fn.unit.Sema.ExprTypes[unary.Operand], storage)
	if reason != "" {
		return env, false
	}
	for _, slot := range targets {
		if !slices.Contains(fn.mutableCellSlots, slot) {
			return env, false
		}
	}
	writes := make(map[uint32]returnOriginValue, len(fn.cellSlots))
	for _, slot := range fn.cellSlots {
		writes[slot] = rhs
	}
	return applyExternalCellWrites(env, writes, map[uint32]bool{targets[0]: len(targets) == 1}), true
}

// taintExternalCellEffects keeps an unproved effect's Pending and lets every
// live cell possibly hold something unknown afterwards; it never assumes the
// effect missed a cell.
func (b *returnOriginBody) taintExternalCellEffects(env returnOriginEnv, span source.Span, reason string) returnOriginEnv {
	b.pending(span, reason)
	if !env.reachable || len(env.cells) == 0 {
		return env
	}
	out := env.clone()
	for slot, value := range out.cells {
		out.cells[slot] = value.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
	}
	return out
}

// collectCellExit joins one function exit's mutable cells into the post-state.
// A missing entry is Unknown, never the cell left as it was.
func (b *returnOriginBody) collectCellExit(env returnOriginEnv, span source.Span) {
	if !env.reachable {
		return
	}
	if b.postCells == nil {
		b.postCells = make(map[uint32]returnOriginValue, len(b.function.mutableCellSlots))
	}
	for _, slot := range b.function.mutableCellSlots {
		if _, present := env.cells[slot]; !present {
			b.pending(span, "function exit lost an external cell's contents")
		}
		b.postCells[slot] = b.postCells[slot].join(env.cell(slot))
	}
}

func projectReturnOriginCellPosts(posts map[uint32]returnOriginValue) map[uint32]returnOriginValue {
	out := make(map[uint32]returnOriginValue, len(posts))
	for slot, value := range posts {
		out[slot] = projectReturnOriginSummary(value)
	}
	return out
}

// joinReturnOriginCellPosts keeps bottom distinct from a normal fact: bottom
// has no posts, so its peer's vector survives, while two normal vectors that
// disagree on an entry join it with Unknown.
func joinReturnOriginCellPosts(f, other *returnOriginSummaryFact) map[uint32]returnOriginValue {
	out := make(map[uint32]returnOriginValue, max(len(f.postCells), len(other.postCells)))
	switch {
	case !f.value.normal:
		for slot, value := range other.postCells {
			out[slot] = value.clone()
		}
	case !other.value.normal:
		for slot, value := range f.postCells {
			out[slot] = value.clone()
		}
	default:
		for slot, value := range f.postCells {
			out[slot] = value.join(returnOriginCellPost(other.postCells, slot))
		}
		for slot, value := range other.postCells {
			if _, joined := out[slot]; !joined {
				out[slot] = returnOriginCellPost(f.postCells, slot).join(value)
			}
		}
	}
	return out
}

func returnOriginCellPost(posts map[uint32]returnOriginValue, slot uint32) returnOriginValue {
	if value, present := posts[slot]; present {
		return value
	}
	return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
}

func equalReturnOriginCellPosts(a, b map[uint32]returnOriginValue) bool {
	if len(a) != len(b) {
		return false
	}
	for slot, value := range a {
		if peer, present := b[slot]; !present || !value.equal(peer) {
			return false
		}
	}
	return true
}
