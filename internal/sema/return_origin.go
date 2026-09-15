package sema

import (
	"cmp"
	"slices"

	"surge/internal/symbols"
)

type returnOriginKind uint8

const (
	returnOriginUnknown returnOriginKind = iota
	returnOriginParam
	returnOriginLocal
	returnOriginCapture
)

// A Param root names one formal slot; the selector says WHICH of that slot's
// two facts it is. V(slot) is the incoming payload itself — ordinarily a
// borrowed value, and under a complete typed target certificate the address of
// an external cell. R(slot) is a frozen snapshot of the contents that cell held
// when it was read, never the address. The same public slot therefore does not
// prove two private facts equal: `read_old` returns R0 and `returned_alias`
// returns V0, and both project to ParamSlots [0].
type returnOriginInputSelector uint8

const (
	returnOriginInputValue returnOriginInputSelector = iota
	returnOriginInputContents
	// E(slot) is what the container formal slot references holds as elements;
	// L(slot) is the loan set of that container when its element is payload-free.
	returnOriginInputElements
	returnOriginInputLoans
)

// Roots use one owning typed-AST unit's symbol/scope vocabulary. Local roots
// never become a callee summary: escape checking precedes formal projection.
// Param means incoming borrowed content. The address of a by-value parameter's
// own storage is a Local root in the function scope, even for a Copy parameter.
type returnOrigin struct {
	kind     returnOriginKind
	binding  symbols.SymbolID
	scope    symbols.ScopeID
	param    uint32
	selector returnOriginInputSelector
	expired  bool
}

// The zero value means NoNormalReturn. A normal value with no roots is proven
// RefFree; an unresolved reference has an explicit Unknown root instead.
type returnOriginValue struct {
	normal    bool
	roots     []returnOrigin
	callables []returnOriginCallable
}

func returnOriginValueOf(roots ...returnOrigin) returnOriginValue {
	owned := slices.Clone(roots)
	slices.SortFunc(owned, compareReturnOrigins)
	return returnOriginValue{normal: true, roots: slices.Compact(owned)}
}

func compareReturnOrigins(a, b returnOrigin) int {
	if order := cmp.Compare(a.kind, b.kind); order != 0 {
		return order
	}
	if order := cmp.Compare(a.binding, b.binding); order != 0 {
		return order
	}
	if order := cmp.Compare(a.scope, b.scope); order != 0 {
		return order
	}
	if order := cmp.Compare(a.param, b.param); order != 0 {
		return order
	}
	if order := cmp.Compare(a.selector, b.selector); order != 0 {
		return order
	}
	if a.expired == b.expired {
		return 0
	}
	if a.expired {
		return 1
	}
	return -1
}

func (v returnOriginValue) clone() returnOriginValue {
	return returnOriginValue{normal: v.normal, roots: slices.Clone(v.roots), callables: cloneReturnOriginCallables(v.callables)}
}

func (v returnOriginValue) equal(other returnOriginValue) bool {
	return v.normal == other.normal && slices.Equal(v.roots, other.roots) &&
		slices.EqualFunc(v.callables, other.callables, func(a, b returnOriginCallable) bool { return compareReturnOriginCallables(a, b) == 0 })
}

func (v returnOriginValue) join(other returnOriginValue) returnOriginValue {
	if !v.normal {
		return other.clone()
	}
	if !other.normal {
		return v.clone()
	}
	out := returnOriginValueOf(append(slices.Clone(v.roots), other.roots...)...)
	out.callables = append(cloneReturnOriginCallables(v.callables), cloneReturnOriginCallables(other.callables)...)
	slices.SortFunc(out.callables, compareReturnOriginCallables)
	out.callables = slices.CompactFunc(out.callables, func(a, b returnOriginCallable) bool { return compareReturnOriginCallables(a, b) == 0 })
	return out
}

// Expiration persists on references carried out of a scope. A later iteration
// may create a new unexpired root for the same local, but cannot revive this one.
func (v returnOriginValue) expire(scope symbols.ScopeID, within func(symbols.ScopeID, symbols.ScopeID) bool) returnOriginValue {
	if within == nil {
		panic("return origins: missing scope ancestry")
	}
	out := v.clone()
	for i := range out.roots {
		root := &out.roots[i]
		if root.kind == returnOriginLocal && within(root.scope, scope) {
			root.expired = true
		}
	}
	if !out.normal {
		return out
	}
	out.roots = returnOriginValueOf(out.roots...).roots
	return out
}

type returnOriginBinding struct {
	scope symbols.ScopeID
	value returnOriginValue
}

// An unreachable environment is distinct from a reachable empty environment.
// Bindings must leave their lexical scope before environments join.
//
// `cells` holds the current contents of this function's admitted external
// cells, keyed by the formal slot that names each one; the function's own
// BodyKey/SourceKey namespaces those keys. A cell entry is flat — roots and
// callables like any other value — and it outlives every lexical scope,
// because the cell belongs to the caller, not to a block here.
type returnOriginEnv struct {
	reachable bool
	bindings  map[symbols.SymbolID]returnOriginBinding
	cells     map[uint32]returnOriginValue
	// backings holds each container formal's element contents or loans, keyed
	// by slot like cells; it outlives every scope and only ever gains facts.
	backings map[uint32]returnOriginValue
}

func newReturnOriginEnv() returnOriginEnv {
	return returnOriginEnv{reachable: true, bindings: make(map[symbols.SymbolID]returnOriginBinding), cells: make(map[uint32]returnOriginValue),
		backings: make(map[uint32]returnOriginValue)}
}

func (e returnOriginEnv) clone() returnOriginEnv {
	if !e.reachable {
		return returnOriginEnv{}
	}
	out := newReturnOriginEnv()
	for id, binding := range e.bindings {
		out.bindings[id] = returnOriginBinding{scope: binding.scope, value: binding.value.clone()}
	}
	for slot, value := range e.cells {
		out.cells[slot] = value.clone()
	}
	for slot, value := range e.backings {
		out.backings[slot] = value.clone()
	}
	return out
}

func (e returnOriginEnv) value(id symbols.SymbolID) returnOriginValue {
	if !e.reachable {
		return returnOriginValue{}
	}
	if binding, ok := e.bindings[id]; ok {
		return binding.value.clone()
	}
	return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
}

// cell answers the contents of one admitted external cell. A reachable
// environment that does not hold the slot knows nothing about it, which is
// Unknown rather than an identity assumption.
func (e returnOriginEnv) cell(slot uint32) returnOriginValue {
	if !e.reachable {
		return returnOriginValue{}
	}
	if value, ok := e.cells[slot]; ok {
		return value.clone()
	}
	return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
}

// withCell replaces one cell's contents. The caller decides whether the write
// is a replacement or a weak union; this only stores what it is given.
func (e returnOriginEnv) withCell(slot uint32, value returnOriginValue) returnOriginEnv {
	if !e.reachable || !value.normal {
		return returnOriginEnv{}
	}
	out := e.clone()
	out.cells[slot] = value.clone()
	return out
}

// The caller evaluates RHS first. NoNormalReturn kills the continuation; a
// normal assignment replaces its old fact, rather than unioning old loans.
func (e returnOriginEnv) assign(id symbols.SymbolID, scope symbols.ScopeID, value returnOriginValue) returnOriginEnv {
	if !e.reachable || !value.normal {
		return returnOriginEnv{}
	}
	if old, ok := e.bindings[id]; ok && old.scope != scope {
		panic("return origins: binding changed lexical scope")
	}
	out := e.clone()
	out.bindings[id] = returnOriginBinding{scope: scope, value: value.clone()}
	return out
}

func (e returnOriginEnv) equal(other returnOriginEnv) bool {
	if e.reachable != other.reachable || len(e.bindings) != len(other.bindings) || len(e.cells) != len(other.cells) || len(e.backings) != len(other.backings) {
		return false
	}
	for slot, value := range e.backings {
		if peer, ok := other.backings[slot]; !ok || !value.equal(peer) {
			return false
		}
	}
	for id, binding := range e.bindings {
		peer, ok := other.bindings[id]
		if !ok || binding.scope != peer.scope || !binding.value.equal(peer.value) {
			return false
		}
	}
	for slot, value := range e.cells {
		peer, ok := other.cells[slot]
		if !ok || !value.equal(peer) {
			return false
		}
	}
	return true
}

func (e returnOriginEnv) join(other returnOriginEnv) returnOriginEnv {
	if !e.reachable {
		return other.clone()
	}
	if !other.reachable {
		return e.clone()
	}
	out := newReturnOriginEnv()
	for id, binding := range e.bindings {
		if peer, ok := other.bindings[id]; ok && peer.scope != binding.scope {
			panic("return origins: joined binding scopes differ")
		}
		out.bindings[id] = returnOriginBinding{scope: binding.scope, value: binding.value.join(other.value(id))}
	}
	for id, binding := range other.bindings {
		if _, exists := out.bindings[id]; !exists {
			out.bindings[id] = returnOriginBinding{scope: binding.scope, value: e.value(id).join(binding.value)}
		}
	}
	// A cell one reachable arm never established is Unknown on the join, not
	// the other arm's fact: nothing here proves the two arms wrote the same
	// contents, and `cell` answers Unknown for the missing side.
	for slot, value := range e.cells {
		out.cells[slot] = value.join(other.cell(slot))
	}
	for slot, value := range other.cells {
		if _, exists := out.cells[slot]; !exists {
			out.cells[slot] = e.cell(slot).join(value)
		}
	}
	for slot, value := range e.backings {
		out.backings[slot] = value.join(other.backing(slot))
	}
	for slot, value := range other.backings {
		if _, exists := out.backings[slot]; !exists {
			out.backings[slot] = e.backing(slot).join(value)
		}
	}
	return out
}

type returnOriginScopeExit struct {
	env     returnOriginEnv
	value   returnOriginValue
	expired []returnOrigin
}

// Check both the block result and surviving outer bindings. Deleting dying
// bindings first would miss a side-effect assignment that exported their loan.
// Expired facts stay in the output even when a consumer delays its diagnostic.
//
// Every external cell survives the scope — it is the caller's storage — so a
// local loan stored into one stays visible here, expires with its owner, and is
// reported even when this scope's result is `nothing`.
func (e returnOriginEnv) leaveScope(scope symbols.ScopeID, value returnOriginValue, within func(symbols.ScopeID, symbols.ScopeID) bool) returnOriginScopeExit {
	if within == nil {
		panic("return origins: missing scope ancestry")
	}
	if !e.reachable || !value.normal {
		return returnOriginScopeExit{}
	}
	out := returnOriginScopeExit{env: newReturnOriginEnv(), value: value.expire(scope, within)}
	roots := slices.Clone(out.value.roots)
	for id, binding := range e.bindings {
		if within(binding.scope, scope) {
			continue
		}
		binding.value = binding.value.expire(scope, within)
		out.env.bindings[id] = binding
		roots = append(roots, binding.value.roots...)
	}
	for slot, cell := range e.cells {
		cell = cell.expire(scope, within)
		out.env.cells[slot] = cell
		roots = append(roots, cell.roots...)
	}
	for slot, backing := range e.backings {
		backing = backing.expire(scope, within)
		out.env.backings[slot] = backing
		roots = append(roots, backing.roots...)
	}
	for _, root := range returnOriginValueOf(roots...).roots {
		if root.expired {
			out.expired = append(out.expired, root)
		}
	}
	return out
}
