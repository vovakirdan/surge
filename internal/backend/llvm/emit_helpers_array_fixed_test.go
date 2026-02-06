package llvm

import (
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

func TestIsArrayOrMapType_IncludesArrayFixed(t *testing.T) {
	// This is a unit test for the LLVM backend's "handle pointer" logic.
	//
	// Before the fix, fixed arrays (ArrayFixed<T, N>) were not treated as array-like
	// in emitHandleOperandPtr(), which caused iter_init / indexing to accidentally
	// dereference the array data pointer as if it were a handle pointer.
	strings := source.NewInterner()
	typesIn := types.NewInterner()
	typesIn.Strings = strings

	arrayFixedName := strings.Intern("ArrayFixed")
	elemParam := strings.Intern("T")
	lenParam := strings.Intern("N")
	decl := source.Span{}
	owner := uint32(1)
	constType := typesIn.Builtins().Uint32
	typesIn.EnsureArrayFixedNominal(arrayFixedName, elemParam, lenParam, decl, owner, constType)

	dirName := strings.Intern("Dir")
	dirType := typesIn.RegisterStruct(dirName, source.Span{})

	arr2 := typesIn.RegisterStructInstanceWithValues(arrayFixedName, decl, []types.TypeID{dirType}, []uint64{2})

	fe := &funcEmitter{emitter: &Emitter{types: typesIn}}
	if !fe.isArrayOrMapType(arr2) {
		t.Fatalf("expected ArrayFixed<T, N> to be treated as array/map type (for handle pointer emission)")
	}
}
