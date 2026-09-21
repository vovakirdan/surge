package sema

import (
	"slices"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A backing is the element storage of a canonical Array, ArrayFixed or Map. Its
// identity is the formal slot that references it, or the local binding that
// owns it; nothing tracks a per-call or per-site allocation. For a
// reference-bearing element a backing value is the element contents; for a
// payload-free element it is the storage loans a view or cursor over it keeps.

// returnOriginContainer answers a canonical Array or ArrayFixed after alias
// resolution, reached directly or through one outer reference.
func returnOriginContainer(in *types.Interner, id types.TypeID) (returnOriginIndexType, bool) {
	c, ok := returnOriginIndexContainer(in, id)
	return c, ok && c.family != in.Builtins().String
}

// returnOriginBackingDescriptor admits `&C` and `&mut C` for a canonical container or Map.
func returnOriginBackingDescriptor(in *types.Interner, id types.TypeID) (mutable, ok bool) {
	c, canonical := returnOriginBackingContainer(in, id)
	if !canonical || !c.reference {
		return false, false
	}
	_, outer, _ := returnOriginIndexResolve(in, id)
	return outer.Mutable, true
}

// backingRoster reads a body's container formals from its original FnInfo and
// the parameter symbols that agree with it. Generic templates are admitted.
func (u *returnOriginUnitIndex) backingRoster(f *returnOriginFunction) (backings, mutable []uint32) {
	if f.candidate == nil {
		return nil, nil
	}
	in := u.Sema.TypeInterner
	for i, param := range f.info.Params {
		isMutable, backing := returnOriginBackingDescriptor(in, param)
		sym := u.Symbols.Table.Symbols.Get(f.params[i])
		if !backing || sym == nil || sym.Kind != symbols.SymbolParam || sym.Scope != f.scope || sym.Type != param {
			continue
		}
		slot, err := safecast.Conv[uint32](i)
		if err != nil {
			return nil, nil
		}
		backings = append(backings, slot)
		if isMutable {
			mutable = append(mutable, slot)
		}
	}
	return backings, mutable
}

// backing answers one container formal's backing. A reachable environment that
// lacks the slot knows nothing about it, which is Unknown, not an identity.
func (e returnOriginEnv) backing(slot uint32) returnOriginValue {
	if !e.reachable {
		return returnOriginValue{}
	}
	if value, ok := e.backings[slot]; ok {
		return value.clone()
	}
	return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
}

// withBacking stores what it is given; every caller writes a weak union.
func (e returnOriginEnv) withBacking(slot uint32, value returnOriginValue) returnOriginEnv {
	if !e.reachable || !value.normal {
		return returnOriginEnv{}
	}
	out := e.clone()
	out.backings[slot] = value.clone()
	return out
}

// initBackings seeds each container formal's backing with E(slot).
func (fn *returnOriginFunction) initBackings(env returnOriginEnv) returnOriginEnv {
	for _, slot := range fn.backingSlots {
		env = env.withBacking(slot, returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: slot, selector: returnOriginInputElements}))
	}
	return env
}

// collectBackingExit joins one exit's mutable container backings into the
// post-state. A missing entry is Unknown, never the backing left as it was.
func (b *returnOriginBody) collectBackingExit(env returnOriginEnv, span source.Span) {
	if !env.reachable {
		return
	}
	if b.postBackings == nil {
		b.postBackings = make(map[uint32]returnOriginValue, len(b.function.mutableBackingSlots))
	}
	for _, slot := range b.function.mutableBackingSlots {
		if _, present := env.backings[slot]; !present {
			b.pending(span, "function exit lost a container formal's backing")
		}
		b.postBackings[slot] = b.postBackings[slot].join(env.backing(slot))
	}
}

// returnOriginBackingTargets names the storage a container operand reaches:
// owning local bindings, and container formals of this function.
type returnOriginBackingTargets struct {
	locals []returnOrigin
	slots  []uint32
}

// backingTargets is G1. Every root must be an unexpired live local binding of
// the same canonical family and element, or an unexpired V(slot) of a roster
// formal (writable when mutable); anything else refuses the whole proof.
func (b *returnOriginBody) backingTargets(container returnOriginIndexType, owner returnOriginValue, env returnOriginEnv, mutable bool) (returnOriginBackingTargets, bool) {
	fn := b.function
	in := fn.unit.Sema.TypeInterner
	var out returnOriginBackingTargets
	if !env.reachable || !owner.normal || len(owner.roots) == 0 || len(owner.callables) != 0 {
		return out, false
	}
	same := func(id types.TypeID, reference bool) bool {
		c, canonical := returnOriginBackingContainer(in, id)
		return canonical && c.reference == reference && c.family == container.family && c.element == container.element && (c.family != in.MapNominalType() || c.container == container.container)
	}
	for _, root := range owner.roots {
		switch {
		case root.expired:
			return out, false
		case root.kind == returnOriginLocal:
			sym := fn.unit.Symbols.Table.Symbols.Get(root.binding)
			binding, live := env.bindings[root.binding]
			if sym == nil || !live || binding.scope != root.scope || sym.Scope != root.scope || !same(sym.Type, false) {
				return out, false
			}
			out.locals = append(out.locals, root)
		case root.kind == returnOriginParam && root.selector == returnOriginInputValue && slices.Contains(fn.backingSlots, root.param) &&
			(!mutable || slices.Contains(fn.mutableBackingSlots, root.param)) && same(fn.info.Params[root.param], true):
			out.slots = append(out.slots, root.param)
		default:
			return out, false
		}
	}
	return out, true
}

// elementsFree says whether a container's element holds no borrow, read through
// this function's own view; a template parameter is never free.
func (b *returnOriginBody) elementsFree(container returnOriginIndexType) bool {
	return returnOriginView(b.function).shape(container.element) == returnOriginRefFree
}

// loadBackingContents joins what the targets hold now. A payload-free element
// has no contents; a backing missing on a reachable path is Unknown.
func (b *returnOriginBody) loadBackingContents(env returnOriginEnv, container returnOriginIndexType, t returnOriginBackingTargets, span source.Span) returnOriginValue {
	if b.elementsFree(container) {
		return returnOriginValueOf()
	}
	out := returnOriginValue{}
	for _, root := range t.locals {
		out = out.join(env.value(root.binding))
	}
	for _, slot := range t.slots {
		if _, present := env.backings[slot]; !present {
			b.pending(span, "container formal backing is missing on a reachable path")
		}
		out = out.join(env.backing(slot))
	}
	return out
}

// storeBackingContents is a weak write. A payload-free element keeps no stored
// loan: the value's local or parameter roots raise the loan-discard refusal
// (G6-iii). A write through a formal may alias every other container formal.
func (b *returnOriginBody) storeBackingContents(env returnOriginEnv, container returnOriginIndexType, t returnOriginBackingTargets, rhs returnOriginValue,
	exprs []ast.ExprID, span source.Span,
) returnOriginEnv {
	in := b.function.unit.Sema.TypeInterner
	if !env.reachable || !rhs.normal {
		return env
	}
	if b.elementsFree(container) {
		guarded := len(exprs) == 0
		for _, expr := range exprs {
			guarded = guarded || b.shape(expr) == returnOriginRefFree
		}
		if guarded {
			b.discardLoans(rhs, span)
		}
		return env
	}
	out := env.clone()
	for _, root := range t.locals {
		binding := out.bindings[root.binding]
		binding.value = binding.value.join(rhs)
		out.bindings[root.binding] = binding
	}
	for _, slot := range t.slots {
		out.backings[slot] = out.backing(slot).join(rhs)
	}
	if len(t.slots) != 0 {
		for _, slot := range b.function.backingSlots {
			if other, _ := returnOriginBackingContainer(in, b.function.info.Params[slot]); !slices.Contains(t.slots, slot) && (other.family == in.MapNominalType()) == (container.family == in.MapNominalType()) {
				out.backings[slot] = out.backing(slot).join(rhs)
			}
		}
	}
	return out
}

// loanCarrier says whether a value of this type keeps storage loans even when
// its shape is ref-free: a canonical container, or a Range cursor.
func (a *returnOriginAnalyzer) loanCarrier(id types.TypeID) bool {
	in := a.units[0].authority.TypeInterner
	c, canonical := returnOriginContainer(in, id)
	return (canonical && !c.reference) || a.rangeFamily(id)
}

// containerLoans is G4: the loans a container or cursor value carries, read from
// its owner roots. An owning local's value is its loans and a payload-free
// container formal carries L(slot); any other root has no proven base.
func (b *returnOriginBody) containerLoans(owner returnOriginValue, env returnOriginEnv, span source.Span) returnOriginValue {
	fn := b.function
	in := fn.unit.Sema.TypeInterner
	out := returnOriginValueOf()
	for _, root := range owner.roots {
		if root.kind == returnOriginLocal && !root.expired {
			sym := fn.unit.Symbols.Table.Symbols.Get(root.binding)
			if _, live := env.bindings[root.binding]; sym != nil && live && b.analyzer.loanCarrier(sym.Type) {
				out = out.join(env.value(root.binding))
				continue
			}
		}
		if root.kind == returnOriginParam && root.selector == returnOriginInputValue && !root.expired && slices.Contains(fn.backingSlots, root.param) {
			if c, _ := returnOriginContainer(in, fn.info.Params[root.param]); b.elementsFree(c) {
				out = out.join(returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: root.param, selector: returnOriginInputLoans}))
				continue
			}
		}
		b.pending(span, "container loans lack a proven base")
		return out.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
	}
	return out
}

// discardLoans is G6: erasing a value that keeps a local or parameter root
// would hide a storage loan, so that stays an explicit refusal.
func (b *returnOriginBody) discardLoans(value returnOriginValue, span source.Span) returnOriginValue {
	if slices.ContainsFunc(value.roots, func(root returnOrigin) bool {
		return root.kind == returnOriginLocal || root.kind == returnOriginParam
	}) {
		b.pending(span, "storage loan would be discarded by a payload-free value")
	}
	return returnOriginValueOf()
}

// legacyBackingValue rebases a payload at a call without proven targets: V(i) is
// the actual, and E(i) is empty where argument i's element is payload-free and
// keeps no storage loan, or where i is the written slot itself (self). Any other
// root has no transfer, and the answer names the refusal.
func (b *returnOriginBody) legacyBackingValue(payload returnOriginValue, slots []returnOriginArgument, actuals []returnOriginValue, self int) (returnOriginValue, string) {
	out := returnOriginValueOf()
	for _, root := range payload.roots {
		i := int(root.param)
		c, canonical := b.callSiteContainer(slots, i)
		live := root.kind == returnOriginParam && !root.expired
		switch {
		case live && root.selector == returnOriginInputValue && i < len(actuals):
			out = out.join(actuals[i])
		case !live || root.selector != returnOriginInputElements || !canonical || !b.elementsFree(c):
			return returnOriginValue{}, "container-content result lacks its checked backing call transfer"
		case i != self && b.loanElement(c):
			return returnOriginValue{}, returnOriginCursorLoanElement
		}
	}
	if !payload.normal || len(payload.callables) != 0 {
		return returnOriginValue{}, "container-content result lacks its checked backing call transfer"
	}
	return out, ""
}

// callSiteContainer answers argument i's single canonical container expression at this call.
func (b *returnOriginBody) callSiteContainer(slots []returnOriginArgument, i int) (returnOriginIndexType, bool) {
	if i < 0 || i >= len(slots) || len(slots[i].exprs) != 1 {
		return returnOriginIndexType{}, false
	}
	return returnOriginBackingContainer(b.function.unit.Sema.TypeInterner, b.function.unit.Sema.ExprTypes[slots[i].exprs[0]])
}

// joinReturnOriginBackingPosts keeps the Cell rule: bottom has no posts, and two
// normal vectors that disagree on an entry join it with Unknown.
func joinReturnOriginBackingPosts(f, other *returnOriginSummaryFact) map[uint32]returnOriginValue {
	return joinReturnOriginCellPosts(&returnOriginSummaryFact{value: f.value, postCells: f.postBackings},
		&returnOriginSummaryFact{value: other.value, postCells: other.postBackings})
}
