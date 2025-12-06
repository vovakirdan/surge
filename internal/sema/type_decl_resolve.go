package sema

import (
	"strconv"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) resolveTypeExprWithScope(id ast.TypeID, scope symbols.ScopeID) types.TypeID {
	if !id.IsValid() || tc.builder == nil {
		return types.NoTypeID
	}
	scope = tc.scopeOrFile(scope)
	key := typeCacheKey{Type: id, Scope: scope, Env: tc.currentTypeParamEnv()}
	if tc.typeCache != nil {
		if cached, ok := tc.typeCache[key]; ok {
			return cached
		}
	}
	expr := tc.builder.Types.Get(id)
	if expr == nil {
		return types.NoTypeID
	}
	var result types.TypeID
	switch expr.Kind {
	case ast.TypeExprPath:
		path, _ := tc.builder.Types.Path(id)
		result = tc.resolveTypePath(path, expr.Span, scope)
	case ast.TypeExprUnary:
		if unary, ok := tc.builder.Types.UnaryType(id); ok && unary != nil {
			inner := tc.resolveTypeExprWithScope(unary.Inner, scope)
			if inner != types.NoTypeID {
				switch unary.Op {
				case ast.TypeUnaryOwn:
					result = tc.types.Intern(types.MakeOwn(inner))
				case ast.TypeUnaryRef:
					result = tc.types.Intern(types.MakeReference(inner, false))
				case ast.TypeUnaryRefMut:
					result = tc.types.Intern(types.MakeReference(inner, true))
				case ast.TypeUnaryPointer:
					result = tc.types.Intern(types.MakePointer(inner))
				}
			}
		}
	case ast.TypeExprArray:
		if arr, ok := tc.builder.Types.Array(id); ok && arr != nil {
			elem := tc.resolveTypeExprWithScope(arr.Elem, scope)
			if elem != types.NoTypeID {
				if arr.Kind == ast.ArraySized {
					lengthArg := tc.resolveArrayLengthArg(arr, expr.Span)
					if lengthArg == types.NoTypeID {
						tc.report(diag.SemaTypeMismatch, expr.Span, "array length must be a constant")
						break
					}
					result = tc.instantiateArrayFixedWithArg(elem, lengthArg)
				} else {
					result = tc.instantiateArrayType(elem)
				}
			}
		}
	case ast.TypeExprOptional:
		if opt, ok := tc.builder.Types.Optional(id); ok && opt != nil {
			inner := tc.resolveTypeExprWithScope(opt.Inner, scope)
			result = tc.resolveOptionType(inner, expr.Span, scope)
		}
	case ast.TypeExprErrorable:
		if errable, ok := tc.builder.Types.Errorable(id); ok && errable != nil {
			inner := tc.resolveTypeExprWithScope(errable.Inner, scope)
			var errType types.TypeID
			if errable.Error.IsValid() {
				errType = tc.resolveTypeExprWithScope(errable.Error, scope)
			} else {
				errType = tc.resolveErrorType(expr.Span, scope)
			}
			result = tc.resolveResultType(inner, errType, expr.Span, scope)
		}
	case ast.TypeExprConst:
		if c, ok := tc.builder.Types.Const(id); ok && c != nil {
			if val, err := strconv.ParseUint(tc.lookupName(c.Value), 10, 64); err == nil {
				if val > uint64(^uint32(0)) {
					tc.report(diag.SemaTypeMismatch, expr.Span, "const value %d exceeds limit", val)
					break
				}
				result = tc.types.Intern(types.MakeConstUint(uint32(val)))
			}
		}
	case ast.TypeExprTuple:
		if tup, ok := tc.builder.Types.Tuple(id); ok && tup != nil {
			// Empty tuple () is unit type
			if len(tup.Elems) == 0 {
				result = tc.types.Builtins().Unit
				break
			}
			elems := make([]types.TypeID, 0, len(tup.Elems))
			allValid := true
			for _, elem := range tup.Elems {
				resolved := tc.resolveTypeExprWithScope(elem, scope)
				if resolved == types.NoTypeID {
					allValid = false
					break
				}
				elems = append(elems, resolved)
			}
			if allValid {
				result = tc.types.RegisterTuple(elems)
			}
		}
	default:
		// other type forms (fn) are not supported yet
	}
	if tc.typeCache != nil {
		tc.typeCache[key] = result
	}
	return result
}

func (tc *typeChecker) resolveTypePath(path *ast.TypePath, span source.Span, scope symbols.ScopeID) types.TypeID {
	if path == nil || len(path.Segments) == 0 {
		return types.NoTypeID
	}
	if len(path.Segments) > 1 {
		if qualified := tc.resolveQualifiedTypePath(path, span, scope); qualified != types.NoTypeID {
			return qualified
		}
		return types.NoTypeID
	}
	tc.ensureBuiltinArrayType()
	tc.ensureBuiltinArrayFixedType()
	seg := path.Segments[0]
	if len(seg.Generics) == 0 {
		if param := tc.lookupTypeParam(seg.Name); param != types.NoTypeID {
			return param
		}
	}
	var typeParams []symbols.TypeParamSymbol
	if symID := tc.lookupTypeSymbol(seg.Name, scope); symID.IsValid() {
		if sym := tc.symbolFromID(symID); sym != nil && len(sym.TypeParamSymbols) > 0 {
			typeParams = sym.TypeParamSymbols
		}
	}
	args, argSpans := tc.resolveTypeArgsWithParams(seg.Generics, typeParams, scope)
	return tc.resolveNamedType(seg.Name, args, argSpans, span, scope)
}

func (tc *typeChecker) resolveNamedType(name source.StringID, args []types.TypeID, argSpans []source.Span, span source.Span, scope symbols.ScopeID) types.TypeID {
	if name == source.NoStringID {
		return types.NoTypeID
	}
	literal := tc.lookupName(name)
	if literal != "" {
		if builtin := tc.builtinTypeByName(literal); builtin != types.NoTypeID {
			return builtin
		}
	}
	symID := tc.lookupTypeSymbol(name, scope)
	if !symID.IsValid() {
		if symAny := tc.lookupSymbolAny(name, scope); symAny.IsValid() {
			if sym := tc.symbolFromID(symAny); sym != nil && (sym.Kind == symbols.SymbolImport || sym.Kind == symbols.SymbolModule) {
				if imported := tc.resolveImportType(sym, name, span); imported != types.NoTypeID {
					return imported
				}
			}
		}
		if literal == "" {
			literal = "_"
		}
		if constSym := tc.lookupConstSymbol(name, scope); constSym.IsValid() {
			if val, ok := tc.constUintFromSymbol(constSym); ok {
				if val > uint64(^uint32(0)) {
					tc.report(diag.SemaTypeMismatch, span, "const value %d exceeds limit", val)
					return types.NoTypeID
				}
				return tc.types.Intern(types.MakeConstUint(uint32(val)))
			}
		}
		tc.report(diag.SemaUnresolvedSymbol, span, "unknown type %s", literal)
		return types.NoTypeID
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return types.NoTypeID
	}
	expected := len(sym.TypeParams)
	if expected == 0 {
		if len(args) > 0 {
			tc.report(diag.SemaTypeMismatch, span, "%s does not take type arguments", tc.lookupName(sym.Name))
			return types.NoTypeID
		}
		return tc.symbolType(symID)
	}
	if len(args) == 0 {
		tc.report(diag.SemaTypeMismatch, span, "%s requires %d type argument(s)", tc.lookupName(sym.Name), expected)
		return types.NoTypeID
	}
	if len(args) != expected {
		tc.report(diag.SemaTypeMismatch, span, "%s expects %d type argument(s), got %d", tc.lookupName(sym.Name), expected, len(args))
		return types.NoTypeID
	}
	for i, tp := range sym.TypeParamSymbols {
		if i >= len(args) {
			break
		}
		if tp.IsConst {
			if !tc.constArgAcceptable(args[i], tp.ConstType) {
				argLabel := tc.typeLabel(args[i])
				argSpan := span
				if i < len(argSpans) && argSpans[i] != (source.Span{}) {
					argSpan = argSpans[i]
				}
				tc.report(diag.SemaTypeMismatch, argSpan, "%s requires const argument %s for %s", tc.lookupName(sym.Name), tc.lookupName(tp.Name), argLabel)
				return types.NoTypeID
			}
		}
	}
	tc.enforceTypeArgBounds(sym, args, argSpans, span)
	return tc.instantiateType(symID, args)
}

func (tc *typeChecker) constArgAcceptable(arg, expect types.TypeID) bool {
	if arg == types.NoTypeID || tc.types == nil {
		return false
	}
	resolved := tc.resolveAlias(arg)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindConst:
		if expect == types.NoTypeID {
			return true
		}
		if family := tc.familyOf(expect); family == types.FamilySignedInt || family == types.FamilyUnsignedInt || family == types.FamilyAny {
			return true
		}
		return expect == arg
	case types.KindGenericParam:
		if info, ok := tc.types.TypeParamInfo(resolved); ok && info != nil {
			return info.IsConst
		}
		return false
	default:
		return false
	}
}

func (tc *typeChecker) resolveArrayLengthArg(arr *ast.TypeArray, span source.Span) types.TypeID {
	if arr == nil || tc.builder == nil || tc.types == nil {
		return types.NoTypeID
	}
	if arr.HasConstLen {
		if arr.ConstLength > uint64(^uint32(0)) {
			tc.report(diag.SemaTypeMismatch, span, "array length %d exceeds limit", arr.ConstLength)
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeConstUint(uint32(arr.ConstLength)))
	}
	if lenVal, ok := tc.constUintValue(arr.Length, nil); ok {
		if lenVal > uint64(^uint32(0)) {
			tc.report(diag.SemaTypeMismatch, span, "array length %d exceeds limit", lenVal)
			return types.NoTypeID
		}
		arr.HasConstLen = true
		arr.ConstLength = lenVal
		return tc.types.Intern(types.MakeConstUint(uint32(lenVal)))
	}
	if ident, ok := tc.builder.Exprs.Ident(arr.Length); ok && ident != nil {
		if param := tc.lookupTypeParam(ident.Name); param != types.NoTypeID {
			resolved := tc.resolveAlias(param)
			if info, okInfo := tc.types.TypeParamInfo(resolved); okInfo && info != nil {
				if info.IsConst {
					return param
				}
				return types.NoTypeID
			}
			if tt, okType := tc.types.Lookup(resolved); okType && tt.Kind == types.KindConst {
				return param
			}
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) resolveQualifiedTypePath(path *ast.TypePath, span source.Span, scope symbols.ScopeID) types.TypeID {
	if path == nil || len(path.Segments) < 2 || tc.symbols == nil || tc.exports == nil {
		return types.NoTypeID
	}
	first := path.Segments[0]
	modulePath := ""
	moduleSym := tc.lookupSymbolAny(first.Name, scope)
	if moduleSym.IsValid() {
		if sym := tc.symbolFromID(moduleSym); sym != nil && (sym.Kind == symbols.SymbolImport || sym.Kind == symbols.SymbolModule) {
			modulePath = sym.ModulePath
		}
	}
	if modulePath == "" {
		if alias := tc.lookupName(first.Name); alias != "" {
			for key := range tc.exports {
				if strings.HasSuffix(key, "/"+alias) || key == alias {
					modulePath = key
					break
				}
			}
		}
	}
	if modulePath == "" {
		tc.report(diag.SemaUnresolvedSymbol, span, "cannot resolve module %s", tc.lookupName(first.Name))
		return types.NoTypeID
	}
	exports := tc.exports[modulePath]
	if exports == nil {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no exports", modulePath)
		return types.NoTypeID
	}
	last := path.Segments[len(path.Segments)-1]
	name := tc.lookupName(last.Name)
	if name == "" {
		name = "_"
	}
	exported := exports.Lookup(name)
	if len(exported) == 0 {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no member %q", modulePath, name)
		return types.NoTypeID
	}
	var candidate *symbols.ExportedSymbol
	for i := range exported {
		if exported[i].Kind == symbols.SymbolType && exported[i].Flags&symbols.SymbolFlagPublic != 0 {
			candidate = &exported[i]
			break
		}
	}
	if candidate == nil {
		tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public or not a type", name, modulePath)
		return types.NoTypeID
	}
	// TODO: handle generic instantiation on imported types.
	return candidate.Type
}

func (tc *typeChecker) resolveQualifiedContract(path *ast.TypePath, span source.Span, scope symbols.ScopeID) symbols.SymbolID {
	if path == nil || len(path.Segments) < 2 || tc.symbols == nil || tc.exports == nil {
		return symbols.NoSymbolID
	}
	first := path.Segments[0]
	modulePath := ""
	moduleSym := tc.lookupSymbolAny(first.Name, scope)
	if moduleSym.IsValid() {
		if sym := tc.symbolFromID(moduleSym); sym != nil && (sym.Kind == symbols.SymbolImport || sym.Kind == symbols.SymbolModule) {
			modulePath = sym.ModulePath
		}
	}
	if modulePath == "" {
		if alias := tc.lookupName(first.Name); alias != "" {
			for key := range tc.exports {
				if strings.HasSuffix(key, "/"+alias) || key == alias {
					modulePath = key
					break
				}
			}
		}
	}
	if modulePath == "" {
		tc.report(diag.SemaUnresolvedSymbol, span, "cannot resolve module %s", tc.lookupName(first.Name))
		return symbols.NoSymbolID
	}
	exports := tc.exports[modulePath]
	if exports == nil {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no exports", modulePath)
		return symbols.NoSymbolID
	}
	last := path.Segments[len(path.Segments)-1]
	name := tc.lookupName(last.Name)
	if name == "" {
		name = "_"
	}
	exported := exports.Lookup(name)
	if len(exported) == 0 {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no member %q", modulePath, name)
		return symbols.NoSymbolID
	}
	var candidate *symbols.ExportedSymbol
	for i := range exported {
		if exported[i].Kind == symbols.SymbolContract && exported[i].Flags&symbols.SymbolFlagPublic != 0 {
			candidate = &exported[i]
			break
		}
	}
	if candidate == nil {
		tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public or not a contract", name, modulePath)
		return symbols.NoSymbolID
	}
	// Synthesize a symbol for the contract so bounds can reference it.
	nameID := tc.builder.StringsInterner.Intern(candidate.Name)
	symRecord := symbols.Symbol{
		Name:             nameID,
		Kind:             symbols.SymbolContract,
		Flags:            candidate.Flags | symbols.SymbolFlagImported,
		Span:             candidate.Span,
		ModulePath:       modulePath,
		TypeParams:       candidate.TypeParams,
		TypeParamSpan:    candidate.TypeParamSpan,
		TypeParamSymbols: symbols.CloneTypeParamSymbols(candidate.TypeParamSyms),
		Contract:         symbols.CloneContractSpec(candidate.Contract),
	}
	id := tc.symbols.Table.Symbols.New(&symRecord)
	if scopeData := tc.symbols.Table.Scopes.Get(tc.fileScope()); scopeData != nil {
		scopeData.Symbols = append(scopeData.Symbols, id)
		scopeData.NameIndex[nameID] = append(scopeData.NameIndex[nameID], id)
	}
	return id
}

func (tc *typeChecker) resolveImportType(sym *symbols.Symbol, name source.StringID, span source.Span) types.TypeID {
	if sym == nil || sym.ModulePath == "" || tc.exports == nil {
		return types.NoTypeID
	}
	exports := tc.exports[sym.ModulePath]
	if exports == nil {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no exports", sym.ModulePath)
		return types.NoTypeID
	}
	nameStr := tc.lookupName(name)
	if nameStr == "" {
		nameStr = "_"
	}
	exported := exports.Lookup(nameStr)
	if len(exported) == 0 {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no member %q", sym.ModulePath, nameStr)
		return types.NoTypeID
	}
	for i := range exported {
		if exported[i].Kind == symbols.SymbolType && exported[i].Flags&symbols.SymbolFlagPublic != 0 {
			return exported[i].Type
		}
	}
	tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public or not a type", nameStr, sym.ModulePath)
	return types.NoTypeID
}

func (tc *typeChecker) resolveImportContract(sym *symbols.Symbol, name source.StringID, span source.Span) symbols.SymbolID {
	if sym == nil || sym.ModulePath == "" || tc.exports == nil {
		return symbols.NoSymbolID
	}
	exports := tc.exports[sym.ModulePath]
	if exports == nil {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no exports", sym.ModulePath)
		return symbols.NoSymbolID
	}
	nameStr := tc.lookupName(name)
	if nameStr == "" {
		nameStr = "_"
	}
	exported := exports.Lookup(nameStr)
	if len(exported) == 0 {
		tc.report(diag.SemaModuleMemberNotFound, span, "module %q has no member %q", sym.ModulePath, nameStr)
		return symbols.NoSymbolID
	}
	for i := range exported {
		exp := exported[i]
		if exp.Kind != symbols.SymbolContract || exp.Flags&symbols.SymbolFlagPublic == 0 {
			continue
		}
		nameID := name
		symRecord := symbols.Symbol{
			Name:             nameID,
			Kind:             symbols.SymbolContract,
			Flags:            exp.Flags | symbols.SymbolFlagImported,
			Span:             exp.Span,
			ModulePath:       sym.ModulePath,
			TypeParams:       exp.TypeParams,
			TypeParamSpan:    exp.TypeParamSpan,
			TypeParamSymbols: symbols.CloneTypeParamSymbols(exp.TypeParamSyms),
			Contract:         symbols.CloneContractSpec(exp.Contract),
		}
		id := tc.symbols.Table.Symbols.New(&symRecord)
		if scopeData := tc.symbols.Table.Scopes.Get(tc.fileScope()); scopeData != nil {
			scopeData.Symbols = append(scopeData.Symbols, id)
			scopeData.NameIndex[nameID] = append(scopeData.NameIndex[nameID], id)
		}
		return id
	}
	tc.report(diag.SemaModuleMemberNotPublic, span, "member %q of module %q is not public or not a contract", nameStr, sym.ModulePath)
	return symbols.NoSymbolID
}

func (tc *typeChecker) resolveTypeArgs(typeIDs []ast.TypeID, scope symbols.ScopeID) ([]types.TypeID, []source.Span) {
	if len(typeIDs) == 0 {
		return nil, nil
	}
	args := make([]types.TypeID, 0, len(typeIDs))
	spans := make([]source.Span, 0, len(typeIDs))
	for _, tid := range typeIDs {
		arg := tc.resolveTypeExprWithScope(tid, scope)
		args = append(args, arg)
		if expr := tc.builder.Types.Get(tid); expr != nil {
			spans = append(spans, expr.Span)
		} else {
			spans = append(spans, source.Span{})
		}
	}
	return args, spans
}

func (tc *typeChecker) resolveTypeArgsWithParams(typeIDs []ast.TypeID, params []symbols.TypeParamSymbol, scope symbols.ScopeID) ([]types.TypeID, []source.Span) {
	if len(params) == 0 {
		return tc.resolveTypeArgs(typeIDs, scope)
	}
	args := make([]types.TypeID, 0, len(typeIDs))
	spans := make([]source.Span, 0, len(typeIDs))
	for idx, tid := range typeIDs {
		var arg types.TypeID
		if idx < len(params) && params[idx].IsConst {
			arg = tc.resolveConstTypeArg(tid, scope)
		} else {
			arg = tc.resolveTypeExprWithScope(tid, scope)
		}
		args = append(args, arg)
		if tc.builder != nil {
			if expr := tc.builder.Types.Get(tid); expr != nil {
				spans = append(spans, expr.Span)
				continue
			}
		}
		spans = append(spans, source.Span{})
	}
	return args, spans
}

func (tc *typeChecker) resolveConstTypeArg(id ast.TypeID, scope symbols.ScopeID) types.TypeID {
	if !id.IsValid() || tc.builder == nil || tc.types == nil {
		return types.NoTypeID
	}
	expr := tc.builder.Types.Get(id)
	if expr == nil {
		return types.NoTypeID
	}
	switch expr.Kind {
	case ast.TypeExprConst:
		if c, ok := tc.builder.Types.Const(id); ok && c != nil {
			if val, err := strconv.ParseUint(tc.lookupName(c.Value), 10, 64); err == nil && val <= uint64(^uint32(0)) {
				return tc.types.Intern(types.MakeConstUint(uint32(val)))
			}
		}
	case ast.TypeExprPath:
		path, _ := tc.builder.Types.Path(id)
		if path == nil || len(path.Segments) != 1 || len(path.Segments[0].Generics) != 0 {
			return types.NoTypeID
		}
		name := path.Segments[0].Name
		if param := tc.lookupTypeParam(name); param != types.NoTypeID {
			return param
		}
		if constSym := tc.lookupConstSymbol(name, scope); constSym.IsValid() {
			if val, ok := tc.constUintFromSymbol(constSym); ok && val <= uint64(^uint32(0)) {
				return tc.types.Intern(types.MakeConstUint(uint32(val)))
			}
		}
		if literal := tc.lookupName(name); literal != "" {
			if builtin := tc.builtinTypeByName(literal); builtin != types.NoTypeID {
				return builtin
			}
		}
		if symID := tc.lookupTypeSymbol(name, scope); symID.IsValid() {
			return tc.symbolType(symID)
		}
		symID := tc.lookupSymbolAny(name, scope)
		if !symID.IsValid() {
			symID = tc.lookupSymbolAny(name, tc.currentScope())
		}
		if symID.IsValid() {
			if ty := tc.bindingType(symID); ty != types.NoTypeID {
				return ty
			}
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) enforceTypeArgBounds(sym *symbols.Symbol, args []types.TypeID, argSpans []source.Span, span source.Span) {
	if sym == nil || len(sym.TypeParamSymbols) == 0 || len(args) == 0 {
		return
	}
	if len(args) != len(sym.TypeParams) {
		return
	}
	bindings := make(map[source.StringID]bindingInfo, len(args))
	for i, name := range sym.TypeParams {
		if name == source.NoStringID {
			continue
		}
		b := bindingInfo{typ: args[i], span: span}
		if i < len(argSpans) && argSpans[i] != (source.Span{}) {
			b.span = argSpans[i]
		}
		bindings[name] = b
	}
	tc.enforceContractBounds(sym.TypeParamSymbols, bindings, span)
}

func (tc *typeChecker) constUintFromSymbol(symID symbols.SymbolID) (uint64, bool) {
	if !symID.IsValid() || tc.builder == nil {
		return 0, false
	}
	_, exprID, _, _ := tc.constBinding(symID)
	return tc.constUintValue(exprID, nil)
}
