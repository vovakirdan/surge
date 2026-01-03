package sema

import (
	"strings"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// instantiateTypeKeyWithInference parses a type key string and instantiates it as a concrete type.
// This function is the core of generic type inference during function call resolution.
//
// The function recursively processes type key strings like "T", "[]T", "(T, U)", "fn(T)->U",
// "&T", "*T", "Option<T>", "Result<T, E>", etc. As it encounters type parameters (from paramNames),
// it either uses an existing binding from the bindings map or infers the type from the actual
// value being passed and records the binding.
//
// Parameters:
//   - key: The type key string from the function signature (e.g., "[]T", "Option<T>")
//   - actual: The actual type being passed, used to infer type parameter values
//   - bindings: Map of type parameter names to their concrete types (updated during inference)
//   - paramNames: Set of type parameter names that can be bound
//
// Returns the instantiated type, or types.NoTypeID if instantiation fails.
//
// Example: For signature fn<T>(x: []T) -> T and call with []int:
//   - key="[]T", actual=[]int
//   - Parses as array, recurses with key="T", actual=int
//   - "T" is in paramNames, so bindings["T"] = int
//   - Returns []int type
func (tc *typeChecker) instantiateTypeKeyWithInference(key symbols.TypeKey, actual types.TypeID, bindings map[string]types.TypeID, paramNames map[string]struct{}) types.TypeID {
	if key == "" || tc.types == nil {
		return types.NoTypeID
	}

	s := strings.TrimSpace(string(key))
	if s == "" {
		return types.NoTypeID
	}

	// Check if this is a type parameter that needs binding
	if _, ok := paramNames[s]; ok {
		if bound := bindings[s]; bound != types.NoTypeID {
			// Already bound - return the bound type
			return bound
		}
		// Infer from actual type and record binding
		bound := tc.valueType(actual)
		if bound == types.NoTypeID {
			return types.NoTypeID
		}
		bindings[s] = bound
		return bound
	}

	// Handle array types: T[], T[N]
	if innerKey, lengthKey, length, hasLen, ok := parseArrayKey(s); ok {
		elemActual := tc.valueType(actual)
		if elem, ok := tc.arrayElemType(actual); ok {
			elemActual = elem
		}
		// Try to infer length from actual type for fixed-size arrays
		if hasLen && lengthKey != "" && length == 0 {
			if _, fixedLen, ok := tc.arrayFixedInfo(actual); ok && fixedLen > 0 {
				length = uint64(fixedLen)
			}
		}
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(innerKey), elemActual, bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		if hasLen {
			lenType := types.NoTypeID
			if _, actualLen, okLen := tc.arrayFixedInfo(actual); okLen && actualLen > 0 {
				lenType = tc.types.Intern(types.MakeConstUint(actualLen))
			}
			if lengthKey != "" {
				if bound := bindings[lengthKey]; bound != types.NoTypeID {
					lenType = bound
				} else if lenType != types.NoTypeID {
					bindings[lengthKey] = lenType
				}
			}
			if lenType == types.NoTypeID && length <= uint64(^uint32(0)) {
				lenType = tc.types.Intern(types.MakeConstUint(uint32(length)))
			}
			if lenType == types.NoTypeID {
				return types.NoTypeID
			}
			return tc.instantiateArrayFixedWithArg(inner, lenType)
		}
		return tc.instantiateArrayType(inner)
	}

	// Handle tuple types: (T, U, V)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "("), ")"))
		if inner == "" {
			return tc.types.Builtins().Unit
		}
		expectedElems := splitTopLevel(inner)
		tupleType := tc.valueType(actual)
		info, ok := tc.types.TupleInfo(tupleType)
		if !ok || info == nil {
			return types.NoTypeID
		}
		if len(expectedElems) != len(info.Elems) {
			return types.NoTypeID
		}
		elems := make([]types.TypeID, 0, len(expectedElems))
		for i, part := range expectedElems {
			elem := tc.instantiateTypeKeyWithInference(symbols.TypeKey(part), info.Elems[i], bindings, paramNames)
			if elem == types.NoTypeID {
				return types.NoTypeID
			}
			elems = append(elems, elem)
		}
		if len(elems) == 0 {
			return tc.types.Builtins().Unit
		}
		return tc.types.RegisterTuple(elems)
	}

	// Handle function types: fn(A, B) -> R
	if strings.HasPrefix(s, "fn(") {
		parts := strings.SplitN(strings.TrimPrefix(s, "fn("), ")->", 2)
		if len(parts) != 2 {
			return types.NoTypeID
		}
		paramsPart := strings.TrimSuffix(parts[0], ")")
		resultPart := strings.TrimSpace(parts[1])

		// Extract actual function parameter and result types for inference
		var actualParams []types.TypeID
		var actualResult types.TypeID
		if fnInfo, ok := tc.types.FnInfo(tc.valueType(actual)); ok && fnInfo != nil {
			actualParams = fnInfo.Params
			actualResult = fnInfo.Result
		}

		var paramTypes []types.TypeID
		if trimmed := strings.TrimSpace(paramsPart); trimmed != "" {
			paramKeys := splitTopLevel(trimmed)
			if len(actualParams) > 0 && len(actualParams) != len(paramKeys) {
				return types.NoTypeID
			}
			paramTypes = make([]types.TypeID, 0, len(paramKeys))
			for i, pk := range paramKeys {
				actualParam := types.NoTypeID
				if i < len(actualParams) {
					actualParam = actualParams[i]
				}
				paramType := tc.instantiateTypeKeyWithInference(symbols.TypeKey(pk), actualParam, bindings, paramNames)
				if paramType == types.NoTypeID {
					return types.NoTypeID
				}
				paramTypes = append(paramTypes, paramType)
			}
		}

		resolvedResult := tc.instantiateTypeKeyWithInference(symbols.TypeKey(resultPart), actualResult, bindings, paramNames)
		if resolvedResult == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.RegisterFn(paramTypes, resolvedResult)
	}

	// Handle reference, ownership, and pointer modifiers
	switch {
	case strings.HasPrefix(s, "&mut "):
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&mut "))), tc.peelReference(actual), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeReference(inner, true))

	case strings.HasPrefix(s, "&"):
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&"))), tc.peelReference(actual), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeReference(inner, false))

	case strings.HasPrefix(s, "own "):
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "own "))), tc.valueType(actual), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeOwn(inner))

	case strings.HasPrefix(s, "*"):
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "*"))), tc.valueType(actual), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakePointer(inner))

	// Handle built-in generic types
	case strings.HasPrefix(s, "Option<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Option<"), ">")
		actualPayload := actual
		if payload, ok := tc.optionPayload(actual); ok {
			actualPayload = payload
		}
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(content), actualPayload, bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.resolveOptionType(inner, source.Span{}, tc.scopeOrFile(tc.currentScope()))

	case strings.HasPrefix(s, "Result<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Result<"), ">")
		parts := splitTopLevel(content)
		if len(parts) != 2 {
			return types.NoTypeID
		}
		okActual := actual
		errActual := actual
		if okType, errType, ok := tc.resultPayload(actual); ok {
			okActual = okType
			errActual = errType
		}
		okType := tc.instantiateTypeKeyWithInference(symbols.TypeKey(parts[0]), okActual, bindings, paramNames)
		errType := tc.instantiateTypeKeyWithInference(symbols.TypeKey(parts[1]), errActual, bindings, paramNames)
		if okType == types.NoTypeID || errType == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.resolveResultType(okType, errType, source.Span{}, tc.scopeOrFile(tc.currentScope()))

	case strings.HasPrefix(s, "Task<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Task<"), ">")
		actualPayload := actual
		if payload := tc.taskPayloadType(actual); payload != types.NoTypeID {
			actualPayload = payload
		}
		inner := tc.instantiateTypeKeyWithInference(symbols.TypeKey(content), actualPayload, bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.taskType(inner, source.Span{})

	default:
		// Handle user-defined generic types (e.g., Map<K, V>) using actual type args for inference.
		if baseName, typeArgKeys, ok := parseGenericTypeKey(s); ok {
			actualBase, actualArgs, okActual := tc.genericTypeArgs(actual)
			if !okActual || actualBase != baseName || len(actualArgs) != len(typeArgKeys) {
				return types.NoTypeID
			}
			concreteArgs := make([]types.TypeID, len(typeArgKeys))
			for i, argKey := range typeArgKeys {
				argType := tc.instantiateTypeKeyWithInference(symbols.TypeKey(argKey), actualArgs[i], bindings, paramNames)
				if argType == types.NoTypeID {
					return types.NoTypeID
				}
				concreteArgs[i] = argType
			}
			return tc.instantiateNamedGenericType(baseName, concreteArgs)
		}
		// Try to resolve as a simple type name
		return tc.typeFromKey(symbols.TypeKey(s))
	}
}

// instantiateResultType instantiates a type key for a function's return type.
// Unlike instantiateTypeKeyWithInference, this function doesn't infer type parameters
// from actual values - it only uses existing bindings from the bindings map.
//
// This is used after type parameters have been inferred from arguments to
// compute the concrete return type of a generic function call.
//
// Parameters:
//   - key: The return type key from the function signature (e.g., "Option<T>")
//   - bindings: Map of type parameter names to their inferred concrete types
//   - paramNames: Set of type parameter names (used to detect unbound params)
//
// Returns types.NoTypeID if any type parameter is unbound or if parsing fails.
func (tc *typeChecker) instantiateResultType(key symbols.TypeKey, bindings map[string]types.TypeID, paramNames map[string]struct{}) types.TypeID {
	if key == "" || tc.types == nil {
		return types.NoTypeID
	}

	s := strings.TrimSpace(string(key))
	if s == "" {
		return types.NoTypeID
	}

	// Check if this is a bound type parameter
	if bound := bindings[s]; bound != types.NoTypeID {
		return bound
	}

	// If it's a type parameter but not bound, fail
	if _, ok := paramNames[s]; ok {
		return types.NoTypeID
	}

	// Handle array types
	if innerKey, lengthKey, length, hasLen, ok := parseArrayKey(s); ok {
		inner := tc.instantiateResultType(symbols.TypeKey(innerKey), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		if hasLen {
			lenType := types.NoTypeID
			if lengthKey != "" && length == 0 {
				if _, fixedLen, ok := tc.arrayFixedInfo(inner); ok && fixedLen > 0 {
					length = uint64(fixedLen)
				}
			}
			if lengthKey != "" {
				lenType = tc.instantiateResultType(symbols.TypeKey(lengthKey), bindings, paramNames)
			}
			if lenType == types.NoTypeID && length <= uint64(^uint32(0)) {
				lenType = tc.types.Intern(types.MakeConstUint(uint32(length)))
			}
			if lenType == types.NoTypeID {
				return types.NoTypeID
			}
			return tc.instantiateArrayFixedWithArg(inner, lenType)
		}
		return tc.instantiateArrayType(inner)
	}

	// Handle tuple types
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "("), ")"))
		if inner == "" {
			return tc.types.Builtins().Unit
		}
		parts := splitTopLevel(inner)
		elems := make([]types.TypeID, 0, len(parts))
		for _, part := range parts {
			elem := tc.instantiateResultType(symbols.TypeKey(part), bindings, paramNames)
			if elem == types.NoTypeID {
				return types.NoTypeID
			}
			elems = append(elems, elem)
		}
		if len(elems) == 0 {
			return tc.types.Builtins().Unit
		}
		return tc.types.RegisterTuple(elems)
	}

	// Handle function types
	if strings.HasPrefix(s, "fn(") {
		parts := strings.SplitN(strings.TrimPrefix(s, "fn("), ")->", 2)
		if len(parts) != 2 {
			return types.NoTypeID
		}
		paramsPart := strings.TrimSuffix(parts[0], ")")
		resultPart := strings.TrimSpace(parts[1])

		var paramTypes []types.TypeID
		if trimmed := strings.TrimSpace(paramsPart); trimmed != "" {
			paramKeys := splitTopLevel(trimmed)
			paramTypes = make([]types.TypeID, 0, len(paramKeys))
			for _, pk := range paramKeys {
				paramType := tc.instantiateResultType(symbols.TypeKey(pk), bindings, paramNames)
				if paramType == types.NoTypeID {
					return types.NoTypeID
				}
				paramTypes = append(paramTypes, paramType)
			}
		}

		resultType := tc.instantiateResultType(symbols.TypeKey(resultPart), bindings, paramNames)
		if resultType == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.RegisterFn(paramTypes, resultType)
	}

	// Handle modifiers and built-in generic types
	switch {
	case strings.HasPrefix(s, "&mut "):
		inner := tc.instantiateResultType(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&mut "))), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeReference(inner, true))

	case strings.HasPrefix(s, "&"):
		inner := tc.instantiateResultType(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&"))), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeReference(inner, false))

	case strings.HasPrefix(s, "own "):
		inner := tc.instantiateResultType(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "own "))), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakeOwn(inner))

	case strings.HasPrefix(s, "*"):
		inner := tc.instantiateResultType(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "*"))), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.Intern(types.MakePointer(inner))

	case strings.HasPrefix(s, "Option<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Option<"), ">")
		inner := tc.instantiateResultType(symbols.TypeKey(content), bindings, paramNames)
		if inner == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.resolveOptionType(inner, source.Span{}, tc.scopeOrFile(tc.currentScope()))

	case strings.HasPrefix(s, "Result<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Result<"), ">")
		parts := splitTopLevel(content)
		if len(parts) != 2 {
			return types.NoTypeID
		}
		okType := tc.instantiateResultType(symbols.TypeKey(parts[0]), bindings, paramNames)
		errType := tc.instantiateResultType(symbols.TypeKey(parts[1]), bindings, paramNames)
		if okType == types.NoTypeID || errType == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.resolveResultType(okType, errType, source.Span{}, tc.scopeOrFile(tc.currentScope()))

	default:
		// Try to parse as user-defined generic type like "TypeName<A, B>"
		if baseName, typeArgKeys, ok := parseGenericTypeKey(s); ok {
			concreteArgs := make([]types.TypeID, len(typeArgKeys))
			for i, argKey := range typeArgKeys {
				arg := tc.instantiateResultType(symbols.TypeKey(argKey), bindings, paramNames)
				if arg == types.NoTypeID {
					return types.NoTypeID
				}
				concreteArgs[i] = arg
			}
			return tc.instantiateNamedGenericType(baseName, concreteArgs)
		}
		return tc.typeFromKey(symbols.TypeKey(s))
	}
}

// peelReference removes reference or ownership wrappers from a type.
// This is used during type inference to get the underlying value type
// when matching reference parameters against actual arguments.
//
// Examples:
//   - &int -> int
//   - &mut string -> string
//   - own MyStruct -> MyStruct
//   - int -> int (unchanged)
func (tc *typeChecker) peelReference(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return id
	}

	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return id
	}

	switch tt.Kind {
	case types.KindReference, types.KindOwn:
		return tt.Elem
	default:
		return id
	}
}

// parseGenericTypeKey parses a type key string in the form "TypeName<A, B>"
// and returns the base type name and list of type argument keys.
//
// Returns (base, args, true) if successfully parsed, or ("", nil, false) if not.
//
// Known built-in generic types (Option, Result) are explicitly skipped
// because they have special handling elsewhere.
//
// Examples:
//   - "HashMap<string, int>" -> ("HashMap", ["string", "int"], true)
//   - "Vec<T>" -> ("Vec", ["T"], true)
//   - "Option<int>" -> ("", nil, false) - special built-in type
//   - "int" -> ("", nil, false) - not generic
func parseGenericTypeKey(s string) (base string, args []string, ok bool) {
	openIdx := strings.Index(s, "<")
	if openIdx == -1 || !strings.HasSuffix(s, ">") {
		return "", nil, false
	}

	base = strings.TrimSpace(s[:openIdx])
	if base == "" {
		return "", nil, false
	}

	// Skip known built-in generic types that have special handling
	if base == "Option" || base == "Result" {
		return "", nil, false
	}

	content := s[openIdx+1 : len(s)-1]
	args = splitTopLevel(content)
	if len(args) == 0 {
		return "", nil, false
	}

	return base, args, true
}

func (tc *typeChecker) genericTypeArgs(actual types.TypeID) (base string, args []types.TypeID, ok bool) {
	if actual == types.NoTypeID || tc.types == nil {
		return "", nil, false
	}
	resolved := tc.resolveAlias(actual)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return "", nil, false
	}
	switch tt.Kind {
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
			base = tc.lookupTypeName(resolved, info.Name)
			args = info.TypeArgs
			return base, args, base != ""
		}
	case types.KindUnion:
		if info, ok := tc.types.UnionInfo(resolved); ok && info != nil {
			base = tc.lookupTypeName(resolved, info.Name)
			args = info.TypeArgs
			return base, args, base != ""
		}
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
			base = tc.lookupTypeName(resolved, info.Name)
			args = info.TypeArgs
			return base, args, base != ""
		}
	}
	return "", nil, false
}

// instantiateNamedGenericType creates a concrete generic type from a base name
// and type arguments.
//
// For regular generic types (structs, aliases), it resolves the type definition
// and creates an instantiation with the given type arguments.
//
// For tag names (e.g., Some, Success), it creates a tag type - a single-member
// union representing that specific variant.
//
// Examples:
//   - instantiateNamedGenericType("Vec", [int]) -> Vec<int>
//   - instantiateNamedGenericType("Some", [int]) -> Some<int> (tag type)
func (tc *typeChecker) instantiateNamedGenericType(base string, args []types.TypeID) types.TypeID {
	if tc.builder == nil || base == "" {
		return types.NoTypeID
	}

	name := tc.builder.StringsInterner.Intern(base)
	scope := tc.scopeOrFile(tc.currentScope())

	// First check if this is a tag name - create tag type if so
	if tagSymID := tc.lookupTagSymbol(name, scope); tagSymID.IsValid() {
		return tc.instantiateTagType(name, args)
	}

	// Otherwise resolve as regular named generic type
	if len(args) == 0 {
		return types.NoTypeID
	}
	return tc.resolveNamedType(name, args, nil, source.Span{}, scope)
}

// instantiateTagType creates a tag type (single-member union) for a tag constructor.
// This is used when a tag like Some<int> or Success<T> is used as a type.
//
// Tag types are represented as unions with a single member - the tag variant itself.
// This allows them to be type-checked and later matched against their parent union.
//
// For example, Some(1) has type Some<int>, which is a single-member union
// that can be assigned to Option<int>.
func (tc *typeChecker) instantiateTagType(tagName source.StringID, args []types.TypeID) types.TypeID {
	if tc.types == nil {
		return types.NoTypeID
	}

	// Create a union with just this tag as a member
	typeID := tc.types.RegisterUnionInstance(tagName, source.Span{}, args)
	members := []types.UnionMember{{
		Kind:    types.UnionMemberTag,
		TagName: tagName,
		TagArgs: args,
	}}
	tc.types.SetUnionMembers(typeID, members)
	return typeID
}
