package sema

import (
	"sort"

	"surge/internal/symbols"
	"surge/internal/types"
)

// reachableValueTypes lists, in a stable order, every type the reachable
// program names.
//
// The seeds come from the finalized closure and from nothing else, because the
// closure is the program: a callable it did not keep alive is a body no backend
// will emit, and requiring an operation on a type only that body mentions would
// pull a dead implementation into the output.
//
// Reachability is a property of callables, so the seeds are their signatures
// and the concrete arguments the closure settled for them, plus the types the
// driver contributed from reachable bodies, all expanded through the structure
// of each type.
func (r *Result) reachableValueTypes(c *CapabilityClassifier) []types.TypeID {
	walk := &valueTypeWalk{classifier: c, seen: make(map[types.TypeID]struct{}, 64)}
	closure := r.InstantiationClosure
	if closure == nil {
		return nil
	}
	signatures := r.callableSignatureIndex()
	for _, callable := range closure.LiveCallables {
		walk.pushAll(signatures[callable])
	}
	for i := range closure.Instances {
		instance := &closure.Instances[i]
		walk.pushAll(instance.TemplateArgs)
		walk.pushAll(signatures[instance.Template])
	}
	for i := range closure.UseSites {
		use := &closure.UseSites[i]
		walk.pushAll(use.TemplateArgs)
		walk.pushAll(use.CallerTemplateArgs)
	}
	for i := range closure.ResolvedDeferredCalls {
		call := &closure.ResolvedDeferredCalls[i]
		walk.pushAll(call.CalleeTemplateArgs)
		walk.pushAll(call.CalleeParamTypes)
		walk.pushAll(call.Args)
		walk.push(call.CalleeResultType)
		walk.push(call.Receiver)
		walk.push(call.ExpectedResult)
	}
	for id := range r.ReachableBodyTypes {
		walk.push(id)
	}
	walk.drain()
	return walk.sorted()
}

// callableSignatureIndex collects the types each callable names in its own
// declaration: the receiver it dispatches on, its parameters and its result.
func (r *Result) callableSignatureIndex() map[symbols.SymbolID][]types.TypeID {
	index := make(map[symbols.SymbolID][]types.TypeID, len(r.CallableCandidates))
	for i := range r.CallableCandidates {
		candidate := &r.CallableCandidates[i]
		if !candidate.Symbol.IsValid() {
			continue
		}
		signature := index[candidate.Symbol]
		signature = append(signature, candidate.ReceiverType, candidate.ResultType)
		signature = append(signature, candidate.ParamTypes...)
		index[candidate.Symbol] = signature
	}
	return index
}

// valueTypeWalk is a breadth-first expansion over the type graph, keyed by the
// alias-resolved node identity so one type is expanded once however it is
// spelled at the sites that named it.
type valueTypeWalk struct {
	classifier *CapabilityClassifier
	seen       map[types.TypeID]struct{}
	pending    []types.TypeID
}

func (w *valueTypeWalk) push(id types.TypeID) {
	resolved := w.classifier.resolve(id)
	if resolved == types.NoTypeID {
		return
	}
	if _, known := w.seen[resolved]; known {
		return
	}
	w.seen[resolved] = struct{}{}
	w.pending = append(w.pending, resolved)
}

func (w *valueTypeWalk) pushAll(ids []types.TypeID) {
	for _, id := range ids {
		w.push(id)
	}
}

func (w *valueTypeWalk) drain() {
	for len(w.pending) > 0 {
		current := w.pending[len(w.pending)-1]
		w.pending = w.pending[:len(w.pending)-1]
		w.pushAll(w.classifier.namedComponents(current))
	}
}

func (w *valueTypeWalk) sorted() []types.TypeID {
	out := make([]types.TypeID, 0, len(w.seen))
	for id := range w.seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// namedComponents is the census walk: which types does naming THIS type bring
// into the program.
//
// It is a superset of the capability component relation and deliberately so.
// The component relation answers what a value CARRIES, and a reference carries
// nothing — the pointee is somebody else's value with its own answers. But a
// function taking `&Wrapper` has put Wrapper into the program just as surely as
// one taking it by value, and Wrapper is the type whose clone must exist. So
// this walk follows the indirections the component relation stops at, and takes
// everything else from it unchanged, so no member kind is described twice.
func (c *CapabilityClassifier) namedComponents(id types.TypeID) []types.TypeID {
	carried := c.components(id)
	tt, ok := c.types.Lookup(id)
	if !ok {
		return carried
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer, types.KindFar:
		return append(carried, c.resolveAll([]types.TypeID{tt.Elem})...)
	case types.KindFn:
		info, found := c.types.FnInfo(id)
		if !found || info == nil {
			return carried
		}
		named := make([]types.TypeID, 0, len(info.Params)+1)
		named = append(named, info.Params...)
		named = append(named, info.Result)
		return append(carried, c.resolveAll(named)...)
	}
	return carried
}
