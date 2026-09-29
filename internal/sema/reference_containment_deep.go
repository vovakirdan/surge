package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// The containment rule holds at every depth of the stored type: a struct field of type
// `Option<&T>` holds a reference exactly as a field of type `&T` does, only one level down,
// and the borrow checker sees neither. So every aggregate position asks what a value of
// the type holds by value anywhere inside it -- through aliases, tuples, arrays, maps,
// struct fields, union members and tag payloads -- and refuses the first reference found.
//
// A reference at the top of a local, a parameter or a function result is not stored in
// an aggregate: `Option<&V>` stays legal there, which is how `Map.get_ref` hands a value
// out. A reference or raw pointer is not walked through: what it points to was checked
// where that type was written.
//
// A generic struct is also a site of its own: `Box<&int>` over `type Box<T> = { v: T }` is a
// struct whose field holds a reference, however it is spelled (a type, a struct literal, a
// static call's receiver). A core Map or Array written by name (`Array<&T>`, `Map<K, &V>`)
// holds its elements as `(&T)[]` and a map literal do. The array the compiler builds for a
// variadic `...args: &T` is not written by name, so no site sees it. A union or a tag is not
// a site: like `Option<&T>`, a `Maybe<&T>` of the user's own is a value a local may
// hold, refused only where it is stored. A nominal refused where it is written keeps its type,
// so its uses do not cascade, and the walk does not look into it again. The field types
// re-resolved while a nominal is instantiated are not sites: the nominal is reported where it
// is written.
//
// Not sites yet: a call whose result becomes such a struct, array or map only through its
// type arguments (`fn wrap<T>(x: T) -> Box<T>` or `-> Array<T>` called with a reference); a tag imported from another module
// and written with a reference argument (`Wrap<&int>`); a generic tag spelled with one inside
// a union declaration (`type V = Wrap<Option<&int>> | nothing`), refused only where V is
// stored; and a union of the user's own, which a local may hold as it holds an Option.

// refInAggregateState is SEM3138's bookkeeping: refusals counts the refusals, so a type whose
// argument was refused is not built on top of that refusal; argsRefused marks a named type
// being built from arguments one of which was refused; spans holds where it was reported, and
// refused the nominals refused where they were written.
type refInAggregateState struct {
	refusals    int
	argsRefused bool
	spans       map[source.Span]bool
	refused     map[types.TypeID]bool
}

// refuse remembers a nominal refused where it was written.
func (s *refInAggregateState) refuse(id types.TypeID) {
	if s.refused == nil {
		s.refused = make(map[types.TypeID]bool)
	}
	s.refused[id] = true
}

// record counts a refusal at span and answers whether span is reported for the first time.
func (s *refInAggregateState) record(span source.Span) bool {
	s.refusals++
	if s.spans == nil {
		s.spans = make(map[source.Span]bool)
	}
	if s.spans[span] {
		return false
	}
	s.spans[span] = true
	return true
}

// heldReference returns the first reference a value of t holds by value, at any depth,
// or NoTypeID when it holds none. A nominal already refused where it was written is not
// looked into again.
func (tc *typeChecker) heldReference(t types.TypeID, seen map[types.TypeID]bool) types.TypeID {
	id := tc.taskWalkType(t)
	if id == types.NoTypeID || seen[id] || tc.refInAggregate.refused[id] {
		return types.NoTypeID
	}
	seen[id] = true
	if tt, ok := tc.types.Lookup(id); ok && tt.Kind == types.KindReference {
		return id
	}
	for _, part := range tc.refWalkParts(id) {
		if found := tc.heldReference(part, seen); found != types.NoTypeID {
			return found
		}
	}
	return types.NoTypeID
}

// refWalkParts lists what the containment walk visits inside id: array and map elements,
// tuple elements, the type arguments of a generic struct or union, and a union's member types
// and generic tags' arguments. A named type's own fields and a non-generic tag's payload are
// not visited: their declarations answered them, and a type argument is the only way
// something new gets inside.
func (tc *typeChecker) refWalkParts(id types.TypeID) []types.TypeID {
	if elem, ok := tc.arrayElemType(id); ok {
		return []types.TypeID{elem}
	}
	if key, value, ok := tc.types.MapInfo(id); ok {
		return []types.TypeID{key, value}
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return nil
	}
	switch tt.Kind {
	case types.KindTuple:
		if info, found := tc.types.TupleInfo(id); found && info != nil {
			return info.Elems
		}
	case types.KindStruct:
		return tc.types.StructArgs(id)
	case types.KindUnion:
		info, found := tc.types.UnionInfo(id)
		if !found || info == nil {
			return nil
		}
		parts := append([]types.TypeID(nil), info.TypeArgs...)
		for _, member := range info.Members {
			switch member.Kind {
			case types.UnionMemberType:
				parts = append(parts, member.Type)
			case types.UnionMemberTag:
				if tc.tagIsGeneric(member.TagName) {
					parts = append(parts, member.TagArgs...)
				}
			}
		}
		return parts
	}
	return nil
}

// tagIsGeneric reports a tag whose payload comes from its type arguments; a tag without
// type parameters has its payload checked where the tag is declared.
func (tc *typeChecker) tagIsGeneric(name source.StringID) bool {
	sym := tc.symbolFromID(tc.lookupTagSymbol(name, tc.fileScope()))
	return sym == nil || len(sym.TypeParams) > 0
}

// refInAggregateLabel names t for the message: the reference itself when t is one, or
// "t holds &U" when the reference sits deeper.
func (tc *typeChecker) refInAggregateLabel(t, ref types.TypeID) string {
	label := tc.typeLabel(t)
	if tc.taskWalkType(t) == ref {
		return label
	}
	return fmt.Sprintf("%s holds %s", label, tc.typeLabel(ref))
}

// rejectNominalHoldingRef answers the rule where a generic nominal was instantiated from
// its type arguments at span: a core Array or Map whose element holds a reference, or a
// struct declared outside core whose type arguments hold one. It reports and returns true.
func (tc *typeChecker) rejectNominalHoldingRef(symID symbols.SymbolID, instantiated types.TypeID, span source.Span) bool {
	if instantiated == types.NoTypeID || tc.types == nil || span == (source.Span{}) || tc.taskInMapAnsweredOutside() ||
		tc.refInAggregate.argsRefused {
		return false
	}
	resolved := tc.resolveAlias(instantiated)
	refused := false
	if elem, ok := tc.arrayElemType(instantiated); ok {
		refused = tc.rejectRefInAggregate(elem, span, "array element")
	} else if key, value, isMap := tc.types.MapInfo(resolved); isMap {
		refused = tc.rejectRefInAggregate(key, span, "map key") || tc.rejectRefInAggregate(value, span, "map value")
	} else if tc.structDeclaredOutsideCore(symID, resolved) {
		seen := map[types.TypeID]bool{resolved: true}
		for _, part := range tc.refWalkParts(resolved) {
			if ref := tc.heldReference(part, seen); ref != types.NoTypeID {
				tc.reportRefInAggregate(tc.refInAggregateLabel(instantiated, ref), ref, span, "struct field")
				refused = true
				break
			}
		}
	}
	if refused {
		tc.refInAggregate.refuse(instantiated)
		tc.refInAggregate.refuse(resolved)
	}
	return refused
}

// structDeclaredOutsideCore reports that resolved is a struct and that neither symID, the
// name it was written with, nor the struct's own declaration belongs to core.
func (tc *typeChecker) structDeclaredOutsideCore(symID symbols.SymbolID, resolved types.TypeID) bool {
	info, ok := tc.types.StructInfo(resolved)
	if tt, found := tc.types.Lookup(resolved); !found || tt.Kind != types.KindStruct || !ok || info == nil {
		return false
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || tc.declaredByCore(sym) {
		return false
	}
	own := tc.symbolFromID(tc.lookupTypeSymbol(info.Name, tc.fileScope()))
	return own == nil || !tc.declaredByCore(own)
}

// declaredByCore reports a type core declares: seen through the core prelude, imported
// from a core module, or declared while checking core itself.
func (tc *typeChecker) declaredByCore(sym *symbols.Symbol) bool {
	return sym.Flags&symbols.SymbolFlagBuiltin != 0 || isCoreRuntimeModulePath(sym.ModulePath) ||
		isCoreRuntimeModulePath(tc.modulePath)
}

// resolveNamedTypeOverRefusedArgs resolves seg's type arguments and the named type they
// build. An argument refused where it was written answers the type it would build, and a
// type built on a refused argument is not refused again.
func (tc *typeChecker) resolveNamedTypeOverRefusedArgs(
	seg *ast.TypePathSegment, params []symbols.TypeParamSymbol, span source.Span, scope symbols.ScopeID,
) types.TypeID {
	refusals := tc.refInAggregate.refusals
	args, argSpans := tc.resolveTypeArgsWithParams(seg.Generics, params, scope)
	argsRefused := tc.refInAggregate.refusals != refusals
	if argsRefused && slices.Contains(args, types.NoTypeID) {
		return types.NoTypeID
	}
	outer := tc.refInAggregate.argsRefused
	tc.refInAggregate.argsRefused = argsRefused
	resolved := tc.resolveNamedType(seg.Name, args, argSpans, span, scope)
	tc.refInAggregate.argsRefused = outer
	return resolved
}
