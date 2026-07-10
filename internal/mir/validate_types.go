package mir

import (
	"errors"
	"fmt"

	"surge/internal/types"
)

// validateTypes checks for TypeParam and unknown types.
func validateTypes(f *Func, typesIn *types.Interner) error {
	var errs []error

	// Check all locals have valid types
	for i, loc := range f.Locals {
		if loc.Type == types.NoTypeID {
			errs = append(errs, fmt.Errorf("local L%d (%s): unknown type", i, loc.Name))
		}
		if typesIn != nil && typeContainsParam(typesIn, loc.Type, nil) {
			errs = append(errs, fmt.Errorf("local L%d (%s): type contains generic parameter", i, loc.Name))
		}
	}

	// Check function result type
	if f.Result != types.NoTypeID && typesIn != nil {
		if typeContainsParam(typesIn, f.Result, nil) {
			errs = append(errs, fmt.Errorf("result type contains generic parameter"))
		}
	}

	return errors.Join(errs...)
}

func validateGlobalTypes(globals []Global, typesIn *types.Interner) error {
	var errs []error
	for i, g := range globals {
		if g.Type == types.NoTypeID {
			errs = append(errs, fmt.Errorf("global G%d (%s): unknown type", i, g.Name))
		}
		if typesIn != nil && typeContainsParam(typesIn, g.Type, nil) {
			errs = append(errs, fmt.Errorf("global G%d (%s): type contains generic parameter", i, g.Name))
		}
	}
	return errors.Join(errs...)
}

// typeContainsParam recursively checks if a type contains any generic parameter.
func typeContainsParam(typesIn *types.Interner, id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}

	if seen == nil {
		seen = make(map[types.TypeID]struct{})
	}
	if _, ok := seen[id]; ok {
		return false
	}
	seen[id] = struct{}{}

	tt, ok := typesIn.Lookup(id)
	if !ok {
		return false
	}

	switch tt.Kind {
	case types.KindGenericParam:
		return true
	case types.KindPointer, types.KindReference, types.KindOwn, types.KindArray:
		return typeContainsParam(typesIn, tt.Elem, seen)
	case types.KindTuple:
		if info, ok := typesIn.TupleInfo(id); ok {
			for _, elem := range info.Elems {
				if typeContainsParam(typesIn, elem, seen) {
					return true
				}
			}
		}
	case types.KindFn:
		if info, ok := typesIn.FnInfo(id); ok {
			for _, param := range info.Params {
				if typeContainsParam(typesIn, param, seen) {
					return true
				}
			}
			if typeContainsParam(typesIn, info.Result, seen) {
				return true
			}
		}
	case types.KindStruct:
		if info, ok := typesIn.StructInfo(id); ok {
			for _, field := range info.Fields {
				if typeContainsParam(typesIn, field.Type, seen) {
					return true
				}
			}
		}
	case types.KindUnion:
		if info, ok := typesIn.UnionInfo(id); ok {
			for _, member := range info.Members {
				if typeContainsParam(typesIn, member.Type, seen) {
					return true
				}
			}
		}
	case types.KindAlias:
		if target, ok := typesIn.AliasTarget(id); ok {
			return typeContainsParam(typesIn, target, seen)
		}
	}

	return false
}
