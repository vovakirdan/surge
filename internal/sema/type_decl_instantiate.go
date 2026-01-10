package sema

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/trace"
	"surge/internal/types"
)

func (tc *typeChecker) instantiationKey(symID symbols.SymbolID, args []types.TypeID) string {
	if !symID.IsValid() {
		return ""
	}
	var b strings.Builder
	b.WriteString(strconv.FormatUint(uint64(symID), 10))
	for _, arg := range args {
		b.WriteByte('#')
		b.WriteString(strconv.FormatUint(uint64(arg), 10))
	}
	return b.String()
}

func (tc *typeChecker) cachedInstantiation(key string) types.TypeID {
	if key == "" || tc.typeInstantiations == nil {
		return types.NoTypeID
	}
	if cached, ok := tc.typeInstantiations[key]; ok {
		return cached
	}
	return types.NoTypeID
}

func (tc *typeChecker) rememberInstantiation(key string, typeID types.TypeID) {
	if key == "" || typeID == types.NoTypeID || tc.typeInstantiations == nil {
		return
	}
	tc.typeInstantiations[key] = typeID
}

func (tc *typeChecker) instantiateType(symID symbols.SymbolID, args []types.TypeID, site source.Span, note string) types.TypeID {
	// Трассировка инстанциации generic типа
	var span *trace.Span
	if tc.tracer != nil && tc.tracer.Level() >= trace.LevelDebug {
		span = trace.Begin(tc.tracer, trace.ScopeNode, "instantiate_type", 0)
		span.WithExtra("args", fmt.Sprintf("%d", len(args)))
	}
	defer func() {
		if span != nil {
			span.End("")
		}
	}()

	if tc.insts != nil && symID.IsValid() && len(args) > 0 {
		tc.insts.RecordTypeInstantiation(symID, args, site, tc.currentFnSym(), note)
	}

	key := tc.instantiationKey(symID, args)
	if cached := tc.cachedInstantiation(key); cached != types.NoTypeID {
		if span != nil {
			span.WithExtra("cached", "true")
		}
		return cached
	}

	// Detect instantiation cycles (e.g., struct User { id: TypedId<User> })
	if key != "" && tc.typeInstantiationInProgress != nil {
		if _, inProgress := tc.typeInstantiationInProgress[key]; inProgress {
			// Cycle detected - return NoTypeID to break recursion
			if span != nil {
				span.WithExtra("cycle_detected", "true")
			}
			return types.NoTypeID
		}
	}

	// Mark as in progress to detect cycles
	if key != "" && tc.typeInstantiationInProgress != nil {
		tc.typeInstantiationInProgress[key] = struct{}{}
		defer func() {
			delete(tc.typeInstantiationInProgress, key)
		}()
	}

	sym := tc.symbolFromID(symID)
	if sym == nil {
		return types.NoTypeID
	}
	item := tc.builder.Items.Get(sym.Decl.Item)
	if (item == nil || item.Kind != ast.ItemType) && (sym.Flags&symbols.SymbolFlagImported != 0 || sym.Flags&symbols.SymbolFlagBuiltin != 0) {
		if instantiated := tc.instantiateImportedType(sym, args); instantiated != types.NoTypeID {
			tc.rememberInstantiation(key, instantiated)
			return instantiated
		}
	}
	if item == nil || item.Kind != ast.ItemType {
		return types.NoTypeID
	}
	typeItem, ok := tc.builder.Items.Type(sym.Decl.Item)
	if !ok || typeItem == nil {
		return types.NoTypeID
	}

	var instantiated types.TypeID
	switch typeItem.Kind {
	case ast.TypeDeclStruct:
		instantiated = tc.instantiateStruct(typeItem, symID, args)
	case ast.TypeDeclAlias:
		instantiated = tc.instantiateAlias(typeItem, symID, args)
	case ast.TypeDeclUnion:
		instantiated = tc.instantiateUnion(typeItem, symID, args)
	default:
		instantiated = types.NoTypeID
	}
	tc.rememberInstantiation(key, instantiated)
	return instantiated
}

func (tc *typeChecker) instantiateImportedType(sym *symbols.Symbol, args []types.TypeID) types.TypeID {
	if tc.types == nil || sym == nil || sym.Type == types.NoTypeID {
		return types.NoTypeID
	}
	reuseByName := sym.Flags&symbols.SymbolFlagImported != 0 &&
		(sym.Flags&symbols.SymbolFlagBuiltin != 0 || isCoreModulePath(sym.ModulePath))
	if tt, ok := tc.types.Lookup(sym.Type); ok && tt.Kind == types.KindAlias {
		if info, ok := tc.types.AliasInfo(sym.Type); ok && info != nil {
			if len(args) == 0 {
				return sym.Type
			}
			if existing, ok := tc.types.FindAliasInstanceWithDecl(info.Name, info.Decl, args); ok {
				return existing
			}
			if reuseByName {
				if existing, ok := tc.types.FindAliasInstance(info.Name, args); ok {
					return existing
				}
			}
			target := tc.substituteImportedType(info.Target, args)
			if target == types.NoTypeID {
				return types.NoTypeID
			}
			instantiated := tc.types.RegisterAliasInstance(info.Name, info.Decl, append([]types.TypeID(nil), args...))
			tc.types.SetAliasTarget(instantiated, target)
			if attrs, ok := tc.typeAttrs[sym.Type]; ok {
				tc.recordTypeAttrs(instantiated, attrs)
			}
			if tc.types.IsCopy(sym.Type) {
				tc.types.MarkCopyType(instantiated)
			}
			if name := tc.lookupName(sym.Name); name != "" {
				tc.recordTypeName(instantiated, name)
			}
			return instantiated
		}
		return types.NoTypeID
	}

	base := tc.resolveAlias(sym.Type)
	if info, ok := tc.types.UnionInfo(base); ok && info != nil {
		if existing, ok := tc.types.FindUnionInstanceWithDecl(info.Name, info.Decl, args); ok {
			return existing
		}
		if reuseByName {
			if existing, ok := tc.types.FindUnionInstance(info.Name, args); ok {
				return existing
			}
		}
		members := make([]types.UnionMember, len(info.Members))
		for i, member := range info.Members {
			members[i] = member
			members[i].Type = tc.substituteImportedType(member.Type, args)
			if len(member.TagArgs) > 0 {
				tagArgs := make([]types.TypeID, len(member.TagArgs))
				for j, arg := range member.TagArgs {
					tagArgs[j] = tc.substituteImportedType(arg, args)
				}
				members[i].TagArgs = tagArgs
			}
		}
		instantiated := tc.types.RegisterUnionInstance(info.Name, info.Decl, append([]types.TypeID(nil), args...))
		tc.types.SetUnionMembers(instantiated, members)
		if attrs, ok := tc.typeAttrs[base]; ok {
			tc.recordTypeAttrs(instantiated, attrs)
		}
		if tc.types.IsCopy(base) {
			tc.types.MarkCopyType(instantiated)
		}
		if name := tc.lookupName(sym.Name); name != "" {
			tc.recordTypeName(instantiated, name)
		}
		return instantiated
	}
	if info, ok := tc.types.StructInfo(base); ok && info != nil {
		if len(info.TypeParams) == 0 {
			return base
		}
		if existing, ok := tc.types.FindStructInstanceWithDecl(info.Name, info.Decl, args); ok {
			return existing
		}
		if reuseByName {
			if existing, ok := tc.types.FindStructInstance(info.Name, args); ok {
				return existing
			}
		}
		if len(args) != len(info.TypeParams) {
			return types.NoTypeID
		}
		mapping := make(map[types.TypeID]types.TypeID, len(info.TypeParams))
		for i, param := range info.TypeParams {
			mapping[tc.resolveAlias(param)] = args[i]
		}
		fields := make([]types.StructField, len(info.Fields))
		for i, field := range info.Fields {
			fields[i] = types.StructField{
				Name:   field.Name,
				Type:   tc.substituteTypeParams(field.Type, mapping),
				Attrs:  slices.Clone(field.Attrs),
				Layout: field.Layout,
			}
		}
		instantiated := tc.types.RegisterStructInstance(info.Name, info.Decl, args)
		tc.types.SetStructFields(instantiated, fields)
		if attrs, ok := tc.typeAttrs[base]; ok {
			tc.recordTypeAttrs(instantiated, attrs)
		}
		if tc.types.IsCopy(base) {
			tc.types.MarkCopyType(instantiated)
		}
		if attrs, ok := tc.types.TypeLayoutAttrs(base); ok {
			tc.types.SetTypeLayoutAttrs(instantiated, attrs)
		}
		if len(info.ValueArgs) > 0 {
			tc.types.SetStructValueArgs(instantiated, info.ValueArgs)
		}
		if name := tc.lookupName(sym.Name); name != "" {
			tc.recordTypeName(instantiated, name)
		}
		return instantiated
	}
	if info, ok := tc.types.AliasInfo(base); ok && info != nil {
		if len(info.TypeArgs) == 0 {
			return base
		}
		if existing, ok := tc.types.FindAliasInstanceWithDecl(info.Name, info.Decl, args); ok {
			return existing
		}
		if reuseByName {
			if existing, ok := tc.types.FindAliasInstance(info.Name, args); ok {
				return existing
			}
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) substituteImportedType(id types.TypeID, args []types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil || len(args) == 0 {
		return id
	}
	cache := make(map[types.TypeID]types.TypeID)
	visiting := make(map[types.TypeID]struct{})
	var walk func(types.TypeID) types.TypeID
	walk = func(t types.TypeID) types.TypeID {
		if t == types.NoTypeID {
			return t
		}
		if cached, ok := cache[t]; ok {
			return cached
		}
		if _, ok := visiting[t]; ok {
			return t
		}
		visiting[t] = struct{}{}
		defer delete(visiting, t)

		if info, ok := tc.types.TypeParamInfo(t); ok && info != nil {
			if idx := int(info.Index); idx >= 0 && idx < len(args) && args[idx] != types.NoTypeID {
				cache[t] = args[idx]
				return args[idx]
			}
			cache[t] = t
			return t
		}

		tt, ok := tc.types.Lookup(t)
		if !ok {
			cache[t] = t
			return t
		}
		switch tt.Kind {
		case types.KindArray, types.KindPointer, types.KindReference, types.KindOwn:
			elem := walk(tt.Elem)
			if elem == tt.Elem {
				cache[t] = t
				return t
			}
			clone := tt
			clone.Elem = elem
			out := tc.types.Intern(clone)
			cache[t] = out
			return out
		case types.KindTuple:
			info, ok := tc.types.TupleInfo(t)
			if !ok || info == nil || len(info.Elems) == 0 {
				cache[t] = t
				return t
			}
			elems := make([]types.TypeID, len(info.Elems))
			changed := false
			for i := range info.Elems {
				elems[i] = walk(info.Elems[i])
				changed = changed || elems[i] != info.Elems[i]
			}
			if !changed {
				cache[t] = t
				return t
			}
			out := tc.types.RegisterTuple(elems)
			cache[t] = out
			return out
		case types.KindFn:
			info, ok := tc.types.FnInfo(t)
			if !ok || info == nil {
				cache[t] = t
				return t
			}
			params := make([]types.TypeID, len(info.Params))
			changed := false
			for i := range info.Params {
				params[i] = walk(info.Params[i])
				changed = changed || params[i] != info.Params[i]
			}
			result := walk(info.Result)
			changed = changed || result != info.Result
			if !changed {
				cache[t] = t
				return t
			}
			out := tc.types.RegisterFn(params, result)
			cache[t] = out
			return out
		case types.KindStruct:
			info, ok := tc.types.StructInfo(t)
			if !ok || info == nil || len(info.TypeArgs) == 0 {
				cache[t] = t
				return t
			}
			newArgs := make([]types.TypeID, len(info.TypeArgs))
			changed := false
			for i := range info.TypeArgs {
				newArgs[i] = walk(info.TypeArgs[i])
				changed = changed || newArgs[i] != info.TypeArgs[i]
			}
			if !changed {
				cache[t] = t
				return t
			}
			if existing, ok := tc.types.FindStructInstance(info.Name, newArgs); ok {
				cache[t] = existing
				return existing
			}
			var inst types.TypeID
			if vals := tc.types.StructValueArgs(t); len(vals) > 0 {
				inst = tc.types.RegisterStructInstanceWithValues(info.Name, info.Decl, newArgs, vals)
			} else {
				inst = tc.types.RegisterStructInstance(info.Name, info.Decl, newArgs)
			}
			fields := tc.types.StructFields(t)
			for i := range fields {
				fields[i].Type = walk(fields[i].Type)
			}
			tc.types.SetStructFields(inst, fields)
			if attrs, ok := tc.typeAttrs[t]; ok {
				tc.recordTypeAttrs(inst, attrs)
			}
			if tc.types.IsCopy(t) {
				tc.types.MarkCopyType(inst)
			}
			if attrs, ok := tc.types.TypeLayoutAttrs(t); ok {
				tc.types.SetTypeLayoutAttrs(inst, attrs)
			}
			cache[t] = inst
			return inst
		case types.KindUnion:
			info, ok := tc.types.UnionInfo(t)
			if !ok || info == nil || len(info.TypeArgs) == 0 {
				cache[t] = t
				return t
			}
			newArgs := make([]types.TypeID, len(info.TypeArgs))
			changed := false
			for i := range info.TypeArgs {
				newArgs[i] = walk(info.TypeArgs[i])
				changed = changed || newArgs[i] != info.TypeArgs[i]
			}
			if !changed {
				cache[t] = t
				return t
			}
			if existing, ok := tc.types.FindUnionInstance(info.Name, newArgs); ok {
				cache[t] = existing
				return existing
			}
			inst := tc.types.RegisterUnionInstance(info.Name, info.Decl, newArgs)
			members := make([]types.UnionMember, len(info.Members))
			for i, member := range info.Members {
				members[i] = member
				members[i].Type = walk(member.Type)
				if len(member.TagArgs) > 0 {
					tagArgs := make([]types.TypeID, len(member.TagArgs))
					for j := range member.TagArgs {
						tagArgs[j] = walk(member.TagArgs[j])
					}
					members[i].TagArgs = tagArgs
				}
			}
			tc.types.SetUnionMembers(inst, members)
			if attrs, ok := tc.typeAttrs[t]; ok {
				tc.recordTypeAttrs(inst, attrs)
			}
			if tc.types.IsCopy(t) {
				tc.types.MarkCopyType(inst)
			}
			cache[t] = inst
			return inst
		case types.KindAlias:
			info, ok := tc.types.AliasInfo(t)
			if !ok || info == nil || len(info.TypeArgs) == 0 {
				cache[t] = t
				return t
			}
			newArgs := make([]types.TypeID, len(info.TypeArgs))
			changed := false
			for i := range info.TypeArgs {
				newArgs[i] = walk(info.TypeArgs[i])
				changed = changed || newArgs[i] != info.TypeArgs[i]
			}
			if !changed {
				cache[t] = t
				return t
			}
			if existing, ok := tc.types.FindAliasInstance(info.Name, newArgs); ok {
				cache[t] = existing
				return existing
			}
			target := walk(info.Target)
			if target == types.NoTypeID {
				cache[t] = types.NoTypeID
				return types.NoTypeID
			}
			inst := tc.types.RegisterAliasInstance(info.Name, info.Decl, newArgs)
			tc.types.SetAliasTarget(inst, target)
			if attrs, ok := tc.typeAttrs[t]; ok {
				tc.recordTypeAttrs(inst, attrs)
			}
			if tc.types.IsCopy(t) {
				tc.types.MarkCopyType(inst)
			}
			cache[t] = inst
			return inst
		default:
			cache[t] = t
			return t
		}
	}
	return walk(id)
}

func isCoreModulePath(path string) bool {
	if path == "" {
		return false
	}
	if path == "core" {
		return true
	}
	return strings.HasPrefix(path, "core/")
}

// instantiateGenericType instantiates a generic type (given by TypeID) with concrete type arguments.
// This is used for static method calls like Type::<Args>::method().
func (tc *typeChecker) instantiateGenericType(baseType types.TypeID, typeArgs []types.TypeID, site source.Span) types.TypeID {
	if baseType == types.NoTypeID || len(typeArgs) == 0 || tc.types == nil {
		return types.NoTypeID
	}
	tc.ensureBuiltinMapType()
	if tc.mapType != types.NoTypeID && tc.resolveAlias(baseType) == tc.mapType {
		if len(typeArgs) != 2 {
			return types.NoTypeID
		}
		return tc.instantiateMapType(typeArgs[0], typeArgs[1], site)
	}

	// Get the type name to find its symbol
	resolved := tc.resolveAlias(baseType)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return types.NoTypeID
	}

	var typeName string
	switch tt.Kind {
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
			typeName = tc.lookupName(info.Name)
		}
	case types.KindUnion:
		if info, ok := tc.types.UnionInfo(resolved); ok && info != nil {
			typeName = tc.lookupName(info.Name)
		}
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
			typeName = tc.lookupName(info.Name)
		}
	default:
		return types.NoTypeID
	}

	if typeName == "" {
		return types.NoTypeID
	}

	// Find the symbol for this type
	nameID := tc.builder.StringsInterner.Intern(typeName)
	scope := tc.fileScope()
	if !scope.IsValid() {
		scope = tc.scopeOrFile(tc.currentScope())
	}

	symID := tc.lookupTypeSymbol(nameID, scope)
	if !symID.IsValid() {
		return types.NoTypeID
	}

	// Instantiate the type with the given type args
	return tc.instantiateType(symID, typeArgs, site, "type")
}
