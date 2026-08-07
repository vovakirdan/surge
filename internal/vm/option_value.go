package vm

import (
	"fmt"

	"surge/internal/types"
)

// Building an Option.
//
// Every intrinsic that can answer "there was nothing there" builds one of these
// — the map lookups, a channel receive, a filesystem probe — so they live with
// the type they construct rather than with whichever intrinsic needed one
// first.
func (vm *VM) makeOptionSome(typeID types.TypeID, elem Value) (Value, *VMError) {
	if typeID == types.NoTypeID {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "invalid Option<T> type")
	}
	layout, vmErr := vm.tagLayoutFor(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	tc, ok := layout.CaseByName("Some")
	if !ok {
		return Value{}, vm.eb.unknownTagLayout(fmt.Sprintf("unknown tag %q in type#%d layout", "Some", layout.TypeID))
	}
	if len(tc.PayloadTypes) != 1 {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("tag %q expects 1 payload value, got %d", tc.TagName, len(tc.PayloadTypes)))
	}
	if elem.TypeID == types.NoTypeID && tc.PayloadTypes[0] != types.NoTypeID {
		elem.TypeID = tc.PayloadTypes[0]
	}
	return vm.buildTag(vm.currentFrame(), typeID, tc.TagSym, []Value{elem})
}

func (vm *VM) makeOptionNothing(typeID types.TypeID) (Value, *VMError) {
	if typeID == types.NoTypeID {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "invalid Option<T> type")
	}
	layout, vmErr := vm.tagLayoutFor(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	tc, ok := layout.CaseByName("nothing")
	if !ok {
		return Value{}, vm.eb.unknownTagLayout(fmt.Sprintf("unknown tag %q in type#%d layout", "nothing", layout.TypeID))
	}
	if len(tc.PayloadTypes) != 0 {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("tag %q expects 0 payload values, got %d", tc.TagName, len(tc.PayloadTypes)))
	}
	return vm.buildTag(vm.currentFrame(), typeID, tc.TagSym, nil)
}
