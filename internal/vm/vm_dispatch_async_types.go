package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

type pollExecResult struct {
	hasStore bool
	storeLoc Location
	storeVal Value
	writes   []LocalWrite
	doJump   bool
	jumpBB   mir.BlockID
}

func (vm *VM) awaitResultType(frame *Frame, dst mir.Place) (types.TypeID, *VMError) {
	if len(dst.Proj) != 0 {
		return types.NoTypeID, vm.eb.makeError(PanicUnimplemented, "await destination projection unsupported")
	}
	switch dst.Kind {
	case mir.PlaceGlobal:
		if int(dst.Global) < 0 || int(dst.Global) >= len(vm.Globals) {
			return types.NoTypeID, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", dst.Global))
		}
		return vm.Globals[dst.Global].TypeID, nil
	default:
		if int(dst.Local) < 0 || int(dst.Local) >= len(frame.Locals) {
			return types.NoTypeID, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", dst.Local))
		}
		return frame.Locals[dst.Local].TypeID, nil
	}
}

func (vm *VM) joinResultType(frame *Frame, dst mir.Place) (types.TypeID, *VMError) {
	if len(dst.Proj) != 0 {
		return types.NoTypeID, vm.eb.makeError(PanicUnimplemented, "join_all destination projection unsupported")
	}
	switch dst.Kind {
	case mir.PlaceGlobal:
		if int(dst.Global) < 0 || int(dst.Global) >= len(vm.Globals) {
			return types.NoTypeID, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", dst.Global))
		}
		return vm.Globals[dst.Global].TypeID, nil
	default:
		if int(dst.Local) < 0 || int(dst.Local) >= len(frame.Locals) {
			return types.NoTypeID, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", dst.Local))
		}
		return frame.Locals[dst.Local].TypeID, nil
	}
}
