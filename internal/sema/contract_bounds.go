package sema

import (
	"strings"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// requirementsForBound builds contract requirements for a bound instance.
func (tc *typeChecker) requirementsForBound(bound symbols.BoundInstance) (contractRequirements, bool) {
	var empty contractRequirements
	if !bound.Contract.IsValid() || tc.builder == nil {
		return empty, false
	}
	contractSym := tc.symbolFromID(bound.Contract)
	if contractSym == nil || contractSym.Kind != symbols.SymbolContract {
		return empty, false
	}
	contractDecl, ok := tc.builder.Items.Contract(contractSym.Decl.Item)
	if !ok || contractDecl == nil {
		return empty, false
	}
	scope := tc.scopeForItem(contractSym.Decl.Item)
	pushed := false
	if len(contractSym.TypeParams) > 0 {
		pushed = tc.pushTypeParams(bound.Contract, contractSym.TypeParams, bound.GenericArgs)
	}
	if pushed {
		defer tc.popTypeParams()
	}
	return tc.contractRequirementSet(contractDecl, scope)
}

// boundFieldType resolves a field required by any contract bound on the given type param.
func (tc *typeChecker) boundFieldType(id types.TypeID, name source.StringID) types.TypeID {
	if id == types.NoTypeID {
		return types.NoTypeID
	}
	resolved := tc.resolveAlias(id)
	if tt, ok := tc.types.Lookup(resolved); ok {
		switch tt.Kind {
		case types.KindReference, types.KindOwn, types.KindPointer:
			return tc.boundFieldType(tt.Elem, name)
		}
	}
	for _, bound := range tc.typeParamContractBounds(id) {
		if reqs, ok := tc.requirementsForBound(bound); ok {
			if ty, exists := reqs.fields[name]; exists {
				return ty
			}
		}
	}
	return types.NoTypeID
}

// boundMethodResult resolves a method required by any contract bound on the given type param.
func (tc *typeChecker) boundMethodResult(id types.TypeID, name string, args []types.TypeID) types.TypeID {
	if id == types.NoTypeID || name == "" {
		return types.NoTypeID
	}
	resolved := tc.resolveAlias(id)
	if tt, ok := tc.types.Lookup(resolved); ok {
		switch tt.Kind {
		case types.KindReference, types.KindOwn, types.KindPointer:
			return tc.boundMethodResult(tt.Elem, name, args)
		}
	}
	for _, bound := range tc.typeParamContractBounds(id) {
		reqs, ok := tc.requirementsForBound(bound)
		if !ok {
			continue
		}
		for key, methodReqs := range reqs.methods {
			if tc.lookupName(key) != name {
				continue
			}
			for idx := range methodReqs {
				req := methodReqs[idx]
				if len(req.params) != len(args)+1 {
					continue
				}
				if !tc.contractTypesEqual(req.params[0], id) {
					continue
				}
				match := true
				for i, arg := range args {
					if !tc.contractTypesEqual(req.params[i+1], arg) {
						match = false
						break
					}
				}
				if match {
					return req.result
				}
			}
		}
	}
	return types.NoTypeID
}

// typeParamContractBounds retrieves contract bounds for a type parameter from cache or its owner symbol.
func (tc *typeChecker) typeParamContractBounds(id types.TypeID) []symbols.BoundInstance {
	if id == types.NoTypeID {
		return nil
	}
	if bounds, ok := tc.typeParamBounds[id]; ok && len(bounds) > 0 {
		return bounds
	}
	info, ok := tc.types.TypeParamInfo(id)
	if !ok || info == nil {
		return nil
	}
	owner := symbols.SymbolID(info.Owner)
	if !owner.IsValid() {
		return nil
	}
	sym := tc.symbolFromID(owner)
	if sym == nil || len(sym.TypeParamSymbols) == 0 {
		return nil
	}
	name := tc.typeParamNames[id]
	for _, tp := range sym.TypeParamSymbols {
		if name != source.NoStringID && tp.Name != name {
			continue
		}
		tc.typeParamBounds[id] = tp.Bounds
		return tp.Bounds
	}
	if int(info.Index) < len(sym.TypeParamSymbols) {
		tc.typeParamBounds[id] = sym.TypeParamSymbols[info.Index].Bounds
		return sym.TypeParamSymbols[info.Index].Bounds
	}
	return nil
}

// attrSetsEqual compares attribute name sets ignoring order and duplicates.
func (tc *typeChecker) attrSetsEqual(expected, actual []source.StringID) bool {
	if len(expected) == 0 && len(actual) == 0 {
		return true
	}
	set := make(map[source.StringID]int, len(expected))
	for _, a := range expected {
		set[a]++
	}
	for _, a := range actual {
		if set[a] == 0 {
			return false
		}
		set[a]--
	}
	for _, v := range set {
		if v != 0 {
			return false
		}
	}
	return true
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	result := "`" + names[0] + "`"
	for _, n := range names[1:] {
		result += ", `" + n + "`"
	}
	return result
}

func joinAttrNames(tc *typeChecker, attrs []source.StringID) string {
	if tc == nil || len(attrs) == 0 {
		return ""
	}
	names := make([]string, 0, len(attrs))
	for _, id := range attrs {
		if name := tc.lookupName(id); name != "" {
			names = append(names, "@"+name)
		}
	}
	return strings.Join(names, ", ")
}

func (tc *typeChecker) bindingTypeLabel(b bindingInfo) string {
	if b.sym.IsValid() {
		if t := tc.bindingType(b.sym); t != types.NoTypeID {
			if l := tc.contractTypeLabel(t); l != "" && l != "_" {
				return l
			}
		}
		if sym := tc.symbolFromID(b.sym); sym != nil {
			if sym.Kind == symbols.SymbolLet && sym.Decl.Stmt.IsValid() {
				if letStmt := tc.builder.Stmts.Let(sym.Decl.Stmt); letStmt != nil {
					scope := tc.scopeForStmt(sym.Decl.Stmt)
					if declType := tc.resolveTypeExprWithScope(letStmt.Type, scope); declType != types.NoTypeID {
						if l := tc.contractTypeLabel(declType); l != "" && l != "_" {
							return l
						}
					}
				}
			}
		}
		if sym := tc.symbolFromID(b.sym); sym != nil && sym.Name != source.NoStringID {
			if name := tc.lookupName(sym.Name); name != "" {
				return name
			}
		}
	}
	label := tc.contractTypeLabel(b.typ)
	if label != "" && label != "_" {
		return label
	}
	return tc.typeLabel(b.typ)
}

func (tc *typeChecker) contractTypeLabel(id types.TypeID) string {
	if id == types.NoTypeID || tc.types == nil {
		return tc.typeLabel(id)
	}
	resolved := tc.resolveAlias(id)
	if info, ok := tc.types.StructInfo(resolved); ok && info != nil && info.Name != source.NoStringID {
		if name := tc.lookupName(info.Name); name != "" {
			return name
		}
	}
	if info, ok := tc.types.AliasInfo(resolved); ok && info != nil && info.Name != source.NoStringID {
		if name := tc.lookupName(info.Name); name != "" {
			return name
		}
	}
	if info, ok := tc.types.UnionInfo(resolved); ok && info != nil && info.Name != source.NoStringID {
		if name := tc.lookupName(info.Name); name != "" {
			return name
		}
	}
	if info, ok := tc.types.TypeParamInfo(resolved); ok && info != nil {
		if name := tc.lookupName(info.Name); name != "" {
			return name
		}
	}
	return tc.typeLabel(id)
}
