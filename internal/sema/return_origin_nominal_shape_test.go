package sema

import (
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

// These descriptor controls supplement the real optional-payload source cases
// in driver; they do not claim that direct-reference aggregates are admitted.
func TestReturnOriginNominalPayloadShape(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	n := in.Strings.Intern
	decl := source.Span{File: 1, Start: 10, End: 20}
	array, param := in.EnsureArrayNominal(n("Array"), n("T"), decl, 1)
	fixed, fixedParams := in.EnsureArrayFixedNominal(n("ArrayFixed"), n("T"), n("N"), decl, 2, in.Builtins().Uint32)
	mapped, _ := in.EnsureMapNominal(n("Map"), n("K"), n("V"), decl, 3)
	ref := in.Intern(types.MakeReference(in.Builtins().String, false))
	count := in.Intern(types.MakeConstUint(2))
	instance := func(base types.TypeID, args ...types.TypeID) types.TypeID {
		info, ok := in.StructInfo(base)
		if !ok || info == nil {
			t.Fatal("PRECONDITION: registered nominal lost its descriptor")
		}
		return in.RegisterStructInstance(info.Name, info.Decl, args)
	}
	phantom := in.RegisterStruct(n("Phantom"), decl)
	in.SetStructTypeParams(phantom, []types.TypeID{param})
	foreignArray := in.RegisterStruct(n("Array"), source.Span{File: 2, Start: 10, End: 20})
	in.SetStructTypeParams(foreignArray, []types.TypeID{param})
	handle := in.RegisterStruct(n("Task"), decl)
	in.SetStructTypeParams(handle, []types.TypeID{param})
	in.MarkRuntimeHandleType(handle)
	borrowedArray := instance(array, ref)
	borrowedFixed := instance(fixed, ref, count)
	unknownLength := instance(fixed, ref, fixedParams[1])
	borrowedMap := instance(mapped, in.Builtins().String, ref)
	borrowedHandle := instance(handle, ref)
	if elem, ok := in.ArrayInfo(borrowedArray); !ok || elem != ref {
		t.Fatal("PRECONDITION: dynamic array element query disagrees with registration")
	}
	if elem, length, ok := in.ArrayFixedInfo(borrowedFixed); !ok || elem != ref || length != 2 {
		t.Fatal("PRECONDITION: fixed array query disagrees with registration")
	}
	if _, _, ok := in.ArrayFixedInfo(unknownLength); ok {
		t.Fatal("PRECONDITION: generic N unexpectedly became a concrete length")
	}
	if key, value, ok := in.MapInfo(borrowedMap); !ok || key != in.Builtins().String || value != ref {
		t.Fatal("PRECONDITION: map payload query disagrees with registration")
	}
	if payloads, ok := in.RuntimeHandlePayloads(borrowedHandle); !ok || len(payloads) != 1 || payloads[0] != ref {
		t.Fatal("PRECONDITION: certified runtime handle lost its logical payload")
	}
	for _, tc := range []struct {
		name string
		id   types.TypeID
		want returnOriginShape
	}{
		{"array_borrowed", borrowedArray, returnOriginCarriesRef},
		{"fixed_borrowed", borrowedFixed, returnOriginCarriesRef},
		{"fixed_generic_length", unknownLength, returnOriginCarriesRef},
		{"map_borrowed_value", borrowedMap, returnOriginCarriesRef},
		{"runtime_handle_borrowed", borrowedHandle, returnOriginCarriesRef},
		{"array_owned", instance(array, in.Builtins().String), returnOriginRefFree},
		{"fixed_owned", instance(fixed, in.Builtins().String, count), returnOriginRefFree},
		{"map_owned", instance(mapped, in.Builtins().String, in.Builtins().String), returnOriginRefFree},
		{"array_generic_payload", instance(array, param), returnOriginShapeUnknown},
		{"array_missing_payload", array, returnOriginShapeUnknown},
		{"fixed_missing_payload", fixed, returnOriginShapeUnknown},
		{"map_missing_payload", mapped, returnOriginShapeUnknown},
		{"runtime_handle_template", handle, returnOriginShapeUnknown},
		{"phantom_borrowed_argument", instance(phantom, ref), returnOriginRefFree},
		{"same_name_foreign_phantom", instance(foreignArray, ref), returnOriginRefFree},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := in.StructInfo(tc.id)
			if !ok || info == nil || len(info.Fields) != 0 {
				t.Fatal("PRECONDITION: component control lost its nominal descriptor")
			}
			t.Logf("NOMINAL_SHAPE_DESCRIPTOR type=%d descriptor=%+v expected=%d", tc.id, info, tc.want)
			if got := returnOriginTypeShape(in, tc.id, nil); got != tc.want {
				t.Fatalf("logical payload shape=%d, want %d", got, tc.want)
			}
		})
	}
}
