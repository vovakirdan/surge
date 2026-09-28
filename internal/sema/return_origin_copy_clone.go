package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// copyCloneRefusal names a direct clone of a Copy value whose type can still
// hold a reference or a storage loan: its copy repeats the referent's contents,
// which this transfer does not read.
const copyCloneRefusal = "copy clone of a reference-bearing value needs its referent's contents"

// copyClone answers a direct `clone(x)` whose result is Copy. The checker types
// such a call without a __clone selection and HIR lowers it to a plain copy of
// the argument's value (a load through the reference), never a call. The copy
// holds exactly what the copied bits hold; when the result type holds no
// reference and is no loan carrier, that is nothing, so the result is fresh.
//
// The call is recognized by the declarations its `clone` identifier sees --
// only core clones, among them the body-less generic core
// `clone<T>(value: &T) -> T` -- never by the spelling alone, and by the typed
// copy itself: no clone selection, no call symbol, no argument conversion, and
// a result that is the argument's value type. A published clone selection
// means typing did not take the copy path, so such a call keeps the ordinary
// reading.
func (b *returnOriginBody) copyClone(id ast.ExprID, call *ast.ExprCallData, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, bool, error) {
	u := b.function.unit
	in := u.Sema.TypeInterner
	result := u.Sema.ExprTypes[id]
	if len(call.Args) != 1 || call.HasNamedArgs() || len(call.TypeArgs) != 0 || !in.IsCopy(result) {
		return returnOriginExprResult{}, false, nil
	}
	ident, ok := u.Builder.Exprs.Ident(call.Target)
	if !ok || ident == nil {
		return returnOriginExprResult{}, false, nil
	}
	if spelled, found := u.Builder.StringsInterner.Lookup(ident.Name); !found || spelled != "clone" {
		return returnOriginExprResult{}, false, nil
	}
	if _, selected := u.Sema.CloneSymbols[id]; selected {
		return returnOriginExprResult{}, false, nil
	}
	argument := call.Args[0].Value
	if _, converted := u.Sema.ImplicitConversions[argument]; converted {
		return returnOriginExprResult{}, false, nil
	}
	if u.Symbols.ExprSymbols[id].IsValid() || !b.coreCopyCloneTarget(call.Target, ident.Name) {
		return returnOriginExprResult{}, false, nil
	}
	if value, typed := returnOriginCopiedValueType(in, u.Sema.ExprTypes[argument]); !typed || value != returnOriginResolveAlias(in, result) {
		return returnOriginExprResult{}, false, nil
	}
	out, err := b.expr(argument, env, targets)
	if err != nil || !out.flow.normal.reachable {
		return out, true, err
	}
	out.storage = returnOriginValue{}
	if returnOriginTypeShape(in, result, nil) != returnOriginRefFree || b.analyzer.loanCarrier(result) {
		b.pending(u.Builder.Exprs.Get(id).Span, copyCloneRefusal)
		out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		return out, true, nil
	}
	out.value = returnOriginValueOf()
	return out, true, nil
}

// returnOriginCopiedValueType is the value a Copy clone copies: the referent of
// a single reference argument, or the argument's own type, with aliases
// resolved. HIR removes at most one reference, so any deeper indirection -- a
// reference to a reference, an own or a raw pointer -- is not answered here.
func returnOriginCopiedValueType(in *types.Interner, argument types.TypeID) (types.TypeID, bool) {
	id := returnOriginResolveAlias(in, argument)
	typ, ok := in.Lookup(id)
	if ok && typ.Kind == types.KindReference {
		id = returnOriginResolveAlias(in, typ.Elem)
		typ, ok = in.Lookup(id)
	}
	if !ok || typ.Kind == types.KindReference || typ.Kind == types.KindOwn || typ.Kind == types.KindPointer {
		return types.NoTypeID, false
	}
	return id, true
}

// coreCopyCloneTarget reports whether the callee identifier names only core
// clone declarations. Typing and HIR treat any one-argument `clone` call as the
// builtin, whatever else is spelled clone; so the name must resolve to a core
// clone and the nearest overload set the function sees must hold nothing but
// core clones, one of them the free generic `clone<T>(value: &T) -> T`. A user
// overload, a local binding or an import named clone keeps the ordinary reading.
func (b *returnOriginBody) coreCopyCloneTarget(target ast.ExprID, name source.StringID) bool {
	u := b.function.unit
	resolved := u.Symbols.ExprSymbols[target]
	for scope := b.function.scope; scope.IsValid(); {
		data := u.Symbols.Table.Scopes.Get(scope)
		if data == nil {
			return false
		}
		entries := data.NameIndex[name]
		if len(entries) == 0 {
			scope = data.Parent
			continue
		}
		classes := make(map[symbols.SymbolID]returnOriginCoreClone, len(entries))
		for _, id := range entries {
			classes[id] = b.coreCloneDeclaration(id)
		}
		free := false
		for _, id := range entries {
			class := classes[id]
			if !class.core {
				class = b.coreCloneCopy(id, entries, classes)
			}
			if !class.core {
				return false
			}
			free = free || class.generic
		}
		// Every entry is a core clone now, so the resolved one is too.
		return free && slices.Contains(entries, resolved)
	}
	return false
}

// returnOriginCoreClone classifies one symbol named clone: whether it is a
// published builtin core clone, and whether that is the free generic one.
type returnOriginCoreClone struct {
	core, generic bool
	source        source.Span
}

// coreCloneDeclaration maps a symbol through its published candidate to its
// physical declaration: a builtin, body-less, synchronous core intrinsic named
// clone. The free generic one has one template T, no receiver, one shared `&T`
// formal and result T.
func (b *returnOriginBody) coreCloneDeclaration(id symbols.SymbolID) returnOriginCoreClone {
	fn, reason := b.analyzer.selectedCallableFunction(b.function.unit, id)
	if reason != "" || fn == nil || fn.info == nil || fn.item == nil || fn.candidate == nil {
		return returnOriginCoreClone{}
	}
	c, in := fn.candidate, fn.unit.Sema.TypeInterner
	if c.Name != "clone" || c.Name != fn.name || !c.Builtin || !c.Intrinsic || c.HasBody || c.Async || fn.item.Body.IsValid() ||
		c.SourceKey != "builtin" || !isCoreRuntimeModulePath(c.ModulePath) {
		return returnOriginCoreClone{}
	}
	if identity, err := fn.unit.owningCallableIdentity(fn.item, fn.symbol, fn.info); err != nil || identity.BodyKey != fn.key || identity.SourceKey != fn.canonicalSourceKey {
		return returnOriginCoreClone{}
	}
	out := returnOriginCoreClone{core: true, source: c.Source}
	if c.ModulePath != "core/base" || fn.unit.SourceKey != "core/base.sg" ||
		c.HasSelf || c.ReceiverType != types.NoTypeID || c.ReceiverTemplateArity != 0 || len(c.TemplateParams) != 1 ||
		len(fn.info.Params) != 1 || len(c.Defaults) != 1 || len(c.Variadic) != 1 || c.Defaults[0] || c.Variadic[0] {
		return out
	}
	formal, typed := in.Lookup(fn.info.Params[0])
	out.generic = typed && formal.Kind == types.KindReference && !formal.Mutable && formal.Elem == c.TemplateParams[0] && fn.info.Result == c.TemplateParams[0]
	return out
}

// coreCloneCopy classifies a prelude or export copy of a core clone: a builtin
// function symbol with no published candidate of its own, standing in the same
// overload set as a published core clone of the same physical declaration
// (the same source span) and the same signature. Several local symbols may
// publish one declaration; they must all classify alike.
func (b *returnOriginBody) coreCloneCopy(id symbols.SymbolID, entries []symbols.SymbolID, classes map[symbols.SymbolID]returnOriginCoreClone) returnOriginCoreClone {
	table := b.function.unit.Symbols.Table.Symbols
	sym := table.Get(id)
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Flags&symbols.SymbolFlagBuiltin == 0 || sym.Signature == nil {
		return returnOriginCoreClone{}
	}
	var match returnOriginCoreClone
	for _, other := range entries {
		class, original := classes[other], table.Get(other)
		if !class.core || class.source != sym.Span || original == nil || !moduleFunctionSignaturesEqual(sym.Signature, original.Signature) {
			continue
		}
		if match.core && match != class {
			return returnOriginCoreClone{}
		}
		match = class
	}
	return match
}
