package vm

import (
	"strings"

	"surge/internal/mir"
)

// callIntrinsic handles runtime intrinsic calls (and selected extern calls not lowered into MIR).
func (vm *VM) callIntrinsic(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	fullName := call.Callee.Name
	name := fullName
	if idx := strings.Index(fullName, "::<"); idx >= 0 {
		name = fullName[:idx]
	}

	if handled, vmErr := vm.callTagConstructor(frame, call, writes); handled {
		return vmErr
	}

	switch name {
	case "size_of", "align_of":
		return vm.handleSizeOfAlignOf(frame, call, writes, name)

	case "default":
		return vm.handleDefault(frame, call, writes)

	case "rt_argv":
		return vm.handleRtArgv(frame, call, writes)

	case "rt_stdin_read_all":
		return vm.handleRtStdinReadAll(frame, call, writes)

	case "rt_string_ptr":
		return vm.handleStringPtr(frame, call, writes)

	case "rt_string_len":
		return vm.handleStringLen(frame, call, writes)

	case "rt_string_len_bytes":
		return vm.handleStringLenBytes(frame, call, writes)

	case "rt_string_from_bytes":
		return vm.handleStringFromBytes(frame, call, writes)

	case "rt_string_from_utf16":
		return vm.handleStringFromUTF16(frame, call, writes)

	case "rt_string_force_flatten":
		return vm.handleStringForceFlatten(frame, call, writes)

	case "rt_string_bytes_view":
		return vm.handleStringBytesView(frame, call, writes)

	case "rt_range_int_new":
		return vm.handleRangeIntNew(frame, call, writes)

	case "rt_range_int_from_start":
		return vm.handleRangeIntFromStart(frame, call, writes)

	case "rt_range_int_to_end":
		return vm.handleRangeIntToEnd(frame, call, writes)

	case "rt_range_int_full":
		return vm.handleRangeIntFull(frame, call, writes)

	case "rt_alloc":
		return vm.handleRtAlloc(frame, call, writes)

	case "rt_free":
		return vm.handleRtFree(frame, call, writes)

	case "rt_realloc":
		return vm.handleRtRealloc(frame, call, writes)

	case "rt_memcpy":
		return vm.handleRtMemcpy(frame, call, writes)

	case "rt_memmove":
		return vm.handleRtMemmove(frame, call, writes)

	case "rt_array_reserve":
		return vm.handleArrayReserve(frame, call, writes)

	case "rt_array_push":
		return vm.handleArrayPush(frame, call, writes)

	case "rt_array_pop":
		return vm.handleArrayPop(frame, call, writes)

	case "rt_write_stdout":
		return vm.handleWriteStdout(frame, call, writes)

	case "rt_exit":
		return vm.handleRtExit(frame, call)

	case "rt_parse_arg":
		return vm.handleParseArg(frame, call, writes)

	case "__len":
		return vm.handleLen(frame, call, writes)

	case "__range":
		return vm.handleArrayRange(frame, call, writes)

	case "next":
		return vm.handleRangeNext(frame, call, writes)

	case "__clone":
		return vm.handleClone(frame, call, writes)

	case "__index":
		return vm.handleIndex(frame, call, writes)

	case "__to":
		return vm.handleTo(frame, call, writes)

	default:
		return vm.eb.unsupportedIntrinsic(name)
	}
}
