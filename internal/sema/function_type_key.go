package sema

import (
	"strings"

	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) resolveFunctionTypeKey(key symbols.TypeKey, actual types.TypeID, resolve func(symbols.TypeKey, types.TypeID) types.TypeID) types.TypeID {
	params, result, sources, ok := symbols.ParseFunctionTypeKey(key)
	if !ok {
		return types.NoTypeID
	}
	var actualParams []types.TypeID
	var actualResult types.TypeID
	if info, found := tc.types.FnInfo(tc.valueType(actual)); found && info != nil {
		actualParams, actualResult = info.Params, info.Result
	}
	if len(actualParams) > 0 && len(actualParams) != len(params) {
		return types.NoTypeID
	}
	typed := make([]types.TypeID, len(params))
	for i, param := range params {
		actualParam := types.NoTypeID
		if i < len(actualParams) {
			actualParam = actualParams[i]
		}
		typed[i] = resolve(param, actualParam)
		if typed[i] == types.NoTypeID {
			return types.NoTypeID
		}
	}
	typedResult := resolve(result, actualResult)
	if typedResult == types.NoTypeID {
		return types.NoTypeID
	}
	return tc.types.RegisterFnWithReturnSources(typed, typedResult, sources)
}

func splitTopLevel(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '<', '[', '(', '{':
			depth++
		case '>', ']', ')', '}':
			if s[i] == '>' && i > 0 && s[i-1] == '-' {
				continue
			}
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	return filtered
}
