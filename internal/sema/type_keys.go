package sema

import (
	"strings"

	"surge/internal/symbols"
	"surge/internal/types"
)

type typeKeyCandidate struct {
	key   symbols.TypeKey
	alias types.TypeID
	base  types.TypeID
}

func (tc *typeChecker) typeKeyCandidates(id types.TypeID) []typeKeyCandidate {
	key := tc.typeKeyForType(id)
	candidates := []typeKeyCandidate{{key: key, base: id}}
	candidates = tc.appendFamilyFallback(candidates, id, key, types.NoTypeID)

	// Add base type candidate for references/own types
	// This allows &Foo to find methods defined in extern<Foo>
	// Skip for aliases - they're handled separately below with proper alias field
	if tt, ok := tc.types.Lookup(id); ok && tt.Kind != types.KindAlias {
		if baseType := tc.valueType(id); baseType != types.NoTypeID && baseType != id {
			baseKey := tc.typeKeyForType(baseType)
			if baseKey != "" && baseKey != key {
				cand := typeKeyCandidate{
					key:  baseKey,
					base: baseType,
				}
				duplicate := false
				for _, existing := range candidates {
					if existing.key == cand.key && existing.base == cand.base {
						duplicate = true
						break
					}
				}
				if !duplicate {
					candidates = append(candidates, cand)
				}
			}
		}
	}

	// Добавляем generic fallback для типов с аргументами
	if genericKey := tc.genericKeyForType(id); genericKey != "" {
		cand := typeKeyCandidate{key: genericKey, base: id}
		duplicate := false
		for _, existing := range candidates {
			if existing.key == cand.key && existing.base == cand.base {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, cand)
		}
	}

	// Добавляем fallback для generic type definitions (без type args, но с type params)
	// Это позволяет My.new() найти методы из extern<My<T>>
	if genericDefKey := tc.genericDefKeyForType(id); genericDefKey != "" {
		cand := typeKeyCandidate{key: genericDefKey, base: id}
		duplicate := false
		for _, existing := range candidates {
			if existing.key == cand.key && existing.base == cand.base {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, cand)
		}
	}

	if aliasBase := tc.aliasBaseType(id); aliasBase != types.NoTypeID {
		baseKey := tc.typeKeyForType(aliasBase)
		if baseKey != "" {
			cand := typeKeyCandidate{
				key:   baseKey,
				alias: id,
				base:  aliasBase,
			}
			candidates = append(candidates, cand)
			candidates = tc.appendFamilyFallback(candidates, aliasBase, baseKey, id)
		}
	}
	if base := tc.structBases[tc.valueType(id)]; base != types.NoTypeID {
		baseKey := tc.typeKeyForType(base)
		if baseKey != "" {
			cand := typeKeyCandidate{key: baseKey, base: base}
			duplicate := false
			for _, existing := range candidates {
				if existing.key == cand.key && existing.base == cand.base {
					duplicate = true
					break
				}
			}
			if !duplicate {
				candidates = append(candidates, cand)
				candidates = tc.appendFamilyFallback(candidates, base, baseKey, types.NoTypeID)
			}
		}
	}
	return candidates
}

func (tc *typeChecker) appendFamilyFallback(c []typeKeyCandidate, base types.TypeID, key symbols.TypeKey, alias types.TypeID) []typeKeyCandidate {
	fallback := tc.familyKeyForType(base)
	if fallback == "" || fallback == key {
		return c
	}
	for _, cand := range c {
		if cand.key == fallback && cand.alias == alias && cand.base == base {
			return c
		}
	}
	return append(c, typeKeyCandidate{
		key:   fallback,
		alias: alias,
		base:  base,
	})
}

func (tc *typeChecker) aliasBaseType(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	current := id
	for {
		tt, ok := tc.types.Lookup(current)
		if !ok || tt.Kind != types.KindAlias {
			if current != id {
				return current
			}
			return types.NoTypeID
		}
		target, ok := tc.types.AliasTarget(current)
		if !ok || target == types.NoTypeID || target == current {
			if current != id {
				return current
			}
			return types.NoTypeID
		}
		current = target
	}
}

func compatibleAliasFallback(left, right typeKeyCandidate) bool {
	switch {
	case left.alias == types.NoTypeID && right.alias == types.NoTypeID:
		return true
	case left.alias != types.NoTypeID && right.alias != types.NoTypeID:
		return left.alias == right.alias
	case left.alias != types.NoTypeID:
		return left.base != right.base
	case right.alias != types.NoTypeID:
		return right.base != left.base
	default:
		return false
	}
}

func (tc *typeChecker) adjustAliasUnaryResult(res types.TypeID, cand typeKeyCandidate) types.TypeID {
	if res == types.NoTypeID {
		return res
	}
	if cand.alias != types.NoTypeID && cand.base == res {
		return cand.alias
	}
	return res
}

func (tc *typeChecker) familyKeyForType(id types.TypeID) symbols.TypeKey {
	if id == types.NoTypeID || tc.types == nil {
		return ""
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return ""
	}
	switch tt.Kind {
	case types.KindInt:
		return symbols.TypeKey("int")
	case types.KindUint:
		return symbols.TypeKey("uint")
	case types.KindFloat:
		return symbols.TypeKey("float")
	default:
		return ""
	}
}

func (tc *typeChecker) adjustAliasBinaryResult(res types.TypeID, left, right typeKeyCandidate) types.TypeID {
	if res == types.NoTypeID {
		return res
	}
	if left.alias != types.NoTypeID && right.alias != types.NoTypeID && left.alias == right.alias && left.base == res {
		return left.alias
	}
	return res
}

// genericKeyForType генерирует generic ключ для типа с аргументами (например, "Option<T>" для Option<int>)
func (tc *typeChecker) genericKeyForType(id types.TypeID) symbols.TypeKey {
	if id == types.NoTypeID || tc.types == nil {
		return ""
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return ""
	}

	var name string
	var typeArgs []types.TypeID

	switch tt.Kind {
	case types.KindUnion:
		if info, ok := tc.types.UnionInfo(resolved); ok && info != nil {
			nameStr := tc.lookupTypeName(resolved, info.Name)
			if nameStr == "" {
				// Fallback: используем имя из info.Name напрямую
				nameStr = tc.lookupName(info.Name)
			}
			if nameStr != "" {
				name = nameStr
				typeArgs = info.TypeArgs
			}
		}
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
			nameStr := tc.lookupTypeName(resolved, info.Name)
			if nameStr == "" {
				// Fallback: используем имя из info.Name напрямую
				nameStr = tc.lookupName(info.Name)
			}
			if nameStr != "" {
				name = nameStr
				typeArgs = info.TypeArgs
			}
		}
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
			nameStr := tc.lookupTypeName(resolved, info.Name)
			if nameStr == "" {
				// Fallback: используем имя из info.Name напрямую
				nameStr = tc.lookupName(info.Name)
			}
			if nameStr != "" {
				name = nameStr
				typeArgs = info.TypeArgs
			}
		}
	default:
		return ""
	}

	if name == "" || len(typeArgs) == 0 {
		return ""
	}

	// Получаем символ типа для получения имен generic параметров
	// Используем file scope для поиска символа типа
	nameID := tc.builder.StringsInterner.Intern(name)
	scope := tc.fileScope()
	if !scope.IsValid() {
		scope = tc.scopeOrFile(tc.currentScope())
	}
	symID := tc.lookupTypeSymbol(nameID, scope)
	if !symID.IsValid() {
		// Пробуем найти через lookupSymbolAny
		if anySymID := tc.lookupSymbolAny(nameID, scope); anySymID.IsValid() {
			if sym := tc.symbolFromID(anySymID); sym != nil && sym.Kind == symbols.SymbolType {
				symID = anySymID
			}
		}
	}

	var paramNames []string
	if symID.IsValid() {
		sym := tc.symbolFromID(symID)
		if sym != nil && len(sym.TypeParamSymbols) > 0 {
			// Формируем generic ключ с именами параметров из символа
			paramNames = make([]string, 0, len(sym.TypeParamSymbols))
			for _, tp := range sym.TypeParamSymbols {
				if paramName := tc.lookupName(tp.Name); paramName != "" {
					paramNames = append(paramNames, paramName)
				}
			}
		}
	}

	// Fallback для известных типов из core модуля
	if len(paramNames) == 0 {
		switch name {
		case "Option", "Task", "Channel":
			if len(typeArgs) == 1 {
				paramNames = []string{"T"}
			}
		case "Erring":
			if len(typeArgs) == 2 {
				paramNames = []string{"T", "E"}
			}
		default:
			return ""
		}
	}

	if len(paramNames) != len(typeArgs) {
		return ""
	}

	return symbols.TypeKey(name + "<" + strings.Join(paramNames, ",") + ">")
}

// genericDefKeyForType generates a generic key for a type definition (e.g., "My<T>" for type My<T>).
// This is used when the TypeID represents a generic type definition without instantiation.
// Unlike genericKeyForType which handles instantiated types (My<int>), this handles the definition itself.
func (tc *typeChecker) genericDefKeyForType(id types.TypeID) symbols.TypeKey {
	if id == types.NoTypeID || tc.types == nil || tc.builder == nil {
		return ""
	}

	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return ""
	}

	var name string
	var hasTypeArgs bool

	switch tt.Kind {
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
			name = tc.lookupTypeName(resolved, info.Name)
			if name == "" {
				name = tc.lookupName(info.Name)
			}
			hasTypeArgs = len(info.TypeArgs) > 0
		}
	case types.KindUnion:
		if info, ok := tc.types.UnionInfo(resolved); ok && info != nil {
			name = tc.lookupTypeName(resolved, info.Name)
			if name == "" {
				name = tc.lookupName(info.Name)
			}
			hasTypeArgs = len(info.TypeArgs) > 0
		}
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
			name = tc.lookupTypeName(resolved, info.Name)
			if name == "" {
				name = tc.lookupName(info.Name)
			}
			hasTypeArgs = len(info.TypeArgs) > 0
		}
	default:
		// Not a struct/union/alias, return empty
		return ""
	}

	// If type already has args, use genericKeyForType instead
	if name == "" || hasTypeArgs {
		return ""
	}

	// Look up the type symbol to get its type parameters
	nameID := tc.builder.StringsInterner.Intern(name)
	scope := tc.fileScope()
	if !scope.IsValid() {
		scope = tc.scopeOrFile(tc.currentScope())
	}

	symID := tc.lookupTypeSymbol(nameID, scope)
	if !symID.IsValid() {
		if anySymID := tc.lookupSymbolAny(nameID, scope); anySymID.IsValid() {
			if sym := tc.symbolFromID(anySymID); sym != nil && sym.Kind == symbols.SymbolType {
				symID = anySymID
			}
		}
	}

	if !symID.IsValid() {
		return ""
	}

	sym := tc.symbolFromID(symID)
	if sym == nil || len(sym.TypeParams) == 0 {
		return ""
	}

	// Build key with type parameter names
	paramNames := make([]string, 0, len(sym.TypeParams))
	for _, param := range sym.TypeParams {
		if paramName := tc.lookupName(param); paramName != "" {
			paramNames = append(paramNames, paramName)
		}
	}

	if len(paramNames) != len(sym.TypeParams) {
		return ""
	}

	return symbols.TypeKey(name + "<" + strings.Join(paramNames, ",") + ">")
}

// buildTypeParamSubst builds a substitution map from type parameter names to actual type keys.
// For example, for receiver Channel<int> and candidateKey "Channel<T>", returns {"T": "int"}.
func (tc *typeChecker) buildTypeParamSubst(recv types.TypeID, candidateKey symbols.TypeKey) map[string]symbols.TypeKey {
	if recv == types.NoTypeID || candidateKey == "" || tc.types == nil {
		return nil
	}

	// Extract type parameter names from candidateKey (e.g., "T" from "Channel<T>")
	keyStr := string(candidateKey)
	start := strings.Index(keyStr, "<")
	end := strings.LastIndex(keyStr, ">")
	if start < 0 || end <= start {
		return nil
	}
	paramStr := keyStr[start+1 : end]
	paramNames := strings.Split(paramStr, ",")
	for i := range paramNames {
		paramNames[i] = strings.TrimSpace(paramNames[i])
	}

	// Get actual type arguments from receiver
	resolved := tc.resolveAlias(recv)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return nil
	}

	var typeArgs []types.TypeID
	switch tt.Kind {
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
			typeArgs = info.TypeArgs
		}
	case types.KindUnion:
		if info, ok := tc.types.UnionInfo(resolved); ok && info != nil {
			typeArgs = info.TypeArgs
		}
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
			typeArgs = info.TypeArgs
		}
	}

	if len(typeArgs) != len(paramNames) {
		return nil
	}

	// Build substitution map
	subst := make(map[string]symbols.TypeKey, len(paramNames))
	for i, paramName := range paramNames {
		argKey := tc.typeKeyForType(typeArgs[i])
		if argKey != "" {
			subst[paramName] = argKey
		}
	}
	return subst
}

// substituteTypeKeyParams substitutes type parameter names in a key string.
// For example, "own T" with {"T": "int"} becomes "own int".
func substituteTypeKeyParams(key symbols.TypeKey, subst map[string]symbols.TypeKey) symbols.TypeKey {
	if len(subst) == 0 || key == "" {
		return key
	}
	s := string(key)
	for param, actual := range subst {
		// Replace standalone type param names (e.g., "T", "own T", "&T")
		// Must be careful not to replace partial matches (e.g., "Task" when param is "T")
		s = replaceTypeParam(s, param, string(actual))
	}
	return symbols.TypeKey(s)
}

// replaceTypeParam replaces a type parameter name with its substitution,
// being careful to only replace whole words.
func replaceTypeParam(s, param, replacement string) string {
	result := ""
	i := 0
	for i < len(s) {
		found := strings.Index(s[i:], param)
		if found < 0 {
			result += s[i:]
			break
		}
		pos := i + found
		// Check if this is a whole word match
		before := pos == 0 || !isIdentChar(s[pos-1])
		after := pos+len(param) >= len(s) || !isIdentChar(s[pos+len(param)])
		if before && after {
			result += s[i:pos] + replacement
			i = pos + len(param)
		} else {
			result += s[i : pos+1]
			i = pos + 1
		}
	}
	return result
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}
