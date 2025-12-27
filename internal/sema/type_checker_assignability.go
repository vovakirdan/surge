package sema

import (
	"surge/internal/source"
	"surge/internal/types"
)

// typesAssignable checks whether a value of type 'actual' can be assigned to a location
// expecting type 'expected'. This is the core type compatibility check used throughout
// the type checker for variable bindings, function arguments, return values, and more.
//
// The check handles several forms of compatibility:
//   - Exact type match (same TypeID)
//   - Alias resolution (e.g., type MyInt = int)
//   - Union member assignment (e.g., assigning int to Option<int>)
//   - Array compatibility (element type + length matching)
//   - Tuple compatibility (element-wise assignability)
//   - Function type compatibility (parameter and return types)
//   - Numeric widening (e.g., int32 to int64)
//
// The allowAlias parameter controls whether type aliases should be resolved
// before comparison. This is typically true for user-facing checks.
func (tc *typeChecker) typesAssignable(expected, actual types.TypeID, allowAlias bool) bool {
	// Fast path: exact match
	if expected == actual {
		return true
	}

	// Resolve aliases if permitted and check again
	if allowAlias {
		if tc.resolveAlias(expected) == tc.resolveAlias(actual) {
			return true
		}
	}
	expectedResolved := expected
	actualResolved := actual
	if allowAlias {
		expectedResolved = tc.resolveAlias(expected)
		actualResolved = tc.resolveAlias(actual)
	}
	if tc.types != nil {
		expInfo, okExp := tc.types.Lookup(expectedResolved)
		actInfo, okAct := tc.types.Lookup(actualResolved)
		if okExp && okAct {
			if expInfo.Kind == types.KindOwn && actualResolved == expInfo.Elem && tc.isCopyType(expInfo.Elem) {
				return true
			}
			if actInfo.Kind == types.KindOwn && expectedResolved == actInfo.Elem && tc.isCopyType(expectedResolved) {
				return true
			}
			if actInfo.Kind == types.KindReference {
				elem := tc.resolveAlias(actInfo.Elem)
				if expInfo.Kind == types.KindOwn {
					if elem == tc.resolveAlias(expInfo.Elem) && tc.isCopyType(elem) {
						return true
					}
				} else if expInfo.Kind != types.KindReference && expInfo.Kind != types.KindPointer {
					if elem == expectedResolved && tc.isCopyType(elem) {
						return true
					}
				}
			}
		}
	}

	// Check if actual is a member of expected union type.
	// This enables constructs like: let x: Option<int> = nothing
	if tc.isUnionMember(expected, actual) {
		return true
	}

	// Array compatibility: check element types and lengths
	if expElem, expLen, expFixed, okExp := tc.arrayInfo(expected); okExp {
		if actElem, actLen, actFixed, okAct := tc.arrayInfo(actual); okAct && tc.typesAssignable(expElem, actElem, true) {
			if expFixed {
				// Fixed-size arrays must have matching lengths
				return actFixed && expLen == actLen
			}
			// Dynamic arrays are compatible if element types match
			return true
		}
	}

	// Tuple compatibility: element-wise assignability check
	expInfo, expOk := tc.types.TupleInfo(expected)
	actInfo, actOk := tc.types.TupleInfo(actual)
	if expOk && actOk {
		if len(expInfo.Elems) != len(actInfo.Elems) {
			return false
		}
		for i := range expInfo.Elems {
			if !tc.typesAssignable(expInfo.Elems[i], actInfo.Elems[i], allowAlias) {
				return false
			}
		}
		return true
	}

	// Function type compatibility: parameter and return types must match
	expFn, expOk := tc.types.FnInfo(expected)
	actFn, actOk := tc.types.FnInfo(actual)
	if expOk && actOk {
		if len(expFn.Params) != len(actFn.Params) {
			return false
		}
		for i := range expFn.Params {
			if !tc.typesAssignable(expFn.Params[i], actFn.Params[i], allowAlias) {
				return false
			}
		}
		return tc.typesAssignable(expFn.Result, actFn.Result, allowAlias)
	}

	// Numeric widening: smaller numeric types can be assigned to larger ones
	if tc.numericWidenable(actual, expected) {
		return true
	}

	return false
}

// isUnionMember checks if actual type is a member of expected union type.
// This enables assigning union members directly to union variables,
// e.g., `let x: Option<int> = nothing;` or `let y: Foo = Bar(1);`
//
// The function handles three kinds of union members:
//   - UnionMemberNothing: The unit value 'nothing' (for Option types)
//   - UnionMemberType: A type embedded in the union (e.g., int in union<int, string>)
//   - UnionMemberTag: A tagged variant (e.g., Some(T) in Option<T>)
//
// To prevent infinite recursion with mutually recursive types, the function
// tracks in-progress checks using assignabilityInProgress map.
func (tc *typeChecker) isUnionMember(expected, actual types.TypeID) bool {
	if expected == types.NoTypeID || actual == types.NoTypeID || tc.types == nil {
		return false
	}

	// Guard against infinite recursion with mutually recursive types
	// (e.g., type A = union<Tag1<B>>, type B = union<Tag2<A>>)
	key := assignabilityKey{Expected: expected, Actual: actual}
	if tc.assignabilityInProgress != nil {
		if _, inProgress := tc.assignabilityInProgress[key]; inProgress {
			return false // Break the recursion cycle
		}
		tc.assignabilityInProgress[key] = struct{}{}
		defer delete(tc.assignabilityInProgress, key)
	}

	// Resolve aliases first to get the underlying union type
	expectedResolved := tc.resolveAlias(expected)
	actualResolved := tc.resolveAlias(actual)

	info, ok := tc.types.UnionInfo(expectedResolved)
	if !ok || info == nil {
		return false
	}

	for _, member := range info.Members {
		switch member.Kind {
		case types.UnionMemberNothing:
			// 'nothing' is always a valid member of unions that contain it
			if actualResolved == tc.types.Builtins().Nothing {
				return true
			}

		case types.UnionMemberType:
			// Type member (e.g., `int` in union<int, string>)
			if member.Type == actualResolved {
				return true
			}
			// Also check if member type is assignable (for nested unions)
			if tc.resolveAlias(member.Type) == actualResolved {
				return true
			}

		case types.UnionMemberTag:
			// Tagged member (e.g., `Some(T)` in Option<T>)
			// Check if actual is a tag type matching this member
			if tc.isTagTypeMatch(actualResolved, member.TagName, member.TagArgs) {
				return true
			}
		}
	}
	return false
}

// isTagTypeMatch checks if the given type is a tag type matching the specified tag name and arguments.
// This is used to check if a value like `Some(1)` matches a union member `Some(T)`.
//
// Tag types in Surge are represented as single-member unions. The function:
//  1. Gets the union info for the type
//  2. Compares the union name against the expected tag name
//  3. Verifies that type arguments are assignable
//
// For example, when checking if `Some(1)` matches `Some(T)` where T=int:
//   - typeID would be the type of `Some(1)` (a single-member union)
//   - tagName would be the string ID for "Some"
//   - tagArgs would be [int] (the concrete type argument)
func (tc *typeChecker) isTagTypeMatch(typeID types.TypeID, tagName source.StringID, tagArgs []types.TypeID) bool {
	if typeID == types.NoTypeID || tc.types == nil {
		return false
	}

	// Get union info for the type - tag types are represented as single-member unions
	info, ok := tc.types.UnionInfo(typeID)
	if !ok || info == nil {
		return false
	}

	// Check if this is a tag type by comparing names
	typeName := tc.lookupName(info.Name)
	expectedTagName := tc.lookupName(tagName)
	if typeName != expectedTagName {
		return false
	}

	// Verify type arguments match in count and assignability
	if len(info.TypeArgs) != len(tagArgs) {
		return false
	}
	for i, arg := range info.TypeArgs {
		if !tc.typesAssignable(tagArgs[i], arg, true) {
			return false
		}
	}

	return true
}
