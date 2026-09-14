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

// Roots use one owning typed-AST unit's symbol/scope vocabulary. Local roots
// never become a callee summary: escape checking precedes formal projection.
// Param means incoming borrowed content. The address of a by-value parameter's
// own storage is a Local root in the function scope, even for a Copy parameter.
type returnOrigin struct {
	kind    returnOriginKind
	binding symbols.SymbolID
	scope   symbols.ScopeID
	param   uint32
	expired bool
}

// The zero value means NoNormalReturn. A normal value with no roots is proven
// RefFree; an unresolved reference has an explicit Unknown root instead.
type returnOriginValue struct {
	normal bool
	roots  []returnOrigin
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
	if a.expired == b.expired {
		return 0
	}
	if a.expired {
		return 1
	}
	return -1
}

func (v returnOriginValue) clone() returnOriginValue {
	return returnOriginValue{normal: v.normal, roots: slices.Clone(v.roots)}
}

func (v returnOriginValue) equal(other returnOriginValue) bool {
	return v.normal == other.normal && slices.Equal(v.roots, other.roots)
}

func (v returnOriginValue) join(other returnOriginValue) returnOriginValue {
	if !v.normal {
		return other.clone()
	}
	if !other.normal {
		return v.clone()
	}
	return returnOriginValueOf(append(slices.Clone(v.roots), other.roots...)...)
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
	return returnOriginValueOf(out.roots...)
}

type returnOriginBinding struct {
	scope symbols.ScopeID
	value returnOriginValue
}

// An unreachable environment is distinct from a reachable empty environment.
// Bindings must leave their lexical scope before environments join.
type returnOriginEnv struct {
	reachable bool
	bindings  map[symbols.SymbolID]returnOriginBinding
}

func newReturnOriginEnv() returnOriginEnv {
	return returnOriginEnv{reachable: true, bindings: make(map[symbols.SymbolID]returnOriginBinding)}
}

func (e returnOriginEnv) clone() returnOriginEnv {
	if !e.reachable {
		return returnOriginEnv{}
	}
	out := newReturnOriginEnv()
	for id, binding := range e.bindings {
		out.bindings[id] = returnOriginBinding{scope: binding.scope, value: binding.value.clone()}
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
	if e.reachable != other.reachable || len(e.bindings) != len(other.bindings) {
		return false
	}
	for id, binding := range e.bindings {
		peer, ok := other.bindings[id]
		if !ok || binding.scope != peer.scope || !binding.value.equal(peer.value) {
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
	for _, root := range returnOriginValueOf(roots...).roots {
		if root.expired {
			out.expired = append(out.expired, root)
		}
	}
	return out
}
