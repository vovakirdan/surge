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
		case "Option":
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
