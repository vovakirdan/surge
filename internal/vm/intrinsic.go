package vm

import (
	"strings"

	"surge/internal/ast"
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

	if name == "monotonic_now" || strings.HasSuffix(name, ".monotonic_now") {
		return vm.handleMonotonicNow(frame, call, writes)
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
	case "readline":
		return vm.handleReadline(frame, call, writes)

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

	case "rt_string_index":
		return vm.handleStringIndex(frame, call, writes)

	case "rt_string_slice":
		return vm.handleStringSlice(frame, call, writes)

	case "rt_string_force_flatten":
		return vm.handleStringForceFlatten(frame, call, writes)

	case "rt_string_bytes_view":
		return vm.handleStringBytesView(frame, call, writes)

	case "rt_heap_stats":
		return vm.handleHeapStats(frame, call, writes)

	case "rt_heap_dump":
		return vm.handleHeapDump(frame, call, writes)

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

	case "rt_map_new":
		return vm.handleMapNew(frame, call, writes)
	case "rt_map_len":
		return vm.handleMapLen(frame, call, writes)
	case "rt_map_contains":
		return vm.handleMapContains(frame, call, writes)
	case "rt_map_get_ref":
		return vm.handleMapGetRef(frame, call, writes)
	case "rt_map_get_mut":
		return vm.handleMapGetMut(frame, call, writes)
	case "rt_map_insert":
		return vm.handleMapInsert(frame, call, writes)
	case "rt_map_remove":
		return vm.handleMapRemove(frame, call, writes)

	case "__task_create":
		return vm.handleTaskCreate(frame, call, writes)
	case "__task_state":
		return vm.handleTaskState(frame, call, writes)
	case "checkpoint":
		return vm.handleCheckpoint(frame, call, writes)
	case "sleep":
		return vm.handleSleep(frame, call, writes)
	case "timeout":
		return vm.handleTimeout(frame, call, writes)
	case "rt_scope_enter":
		return vm.handleScopeEnter(frame, call, writes)
	case "rt_scope_register_child":
		return vm.handleScopeRegisterChild(frame, call)
	case "rt_scope_cancel_all":
		return vm.handleScopeCancelAll(frame, call)
	case "rt_scope_join_all":
		return vm.handleScopeJoinAll(frame, call, writes)
	case "rt_scope_exit":
		return vm.handleScopeExit(frame, call)

	case "make_channel":
		return vm.handleMakeChannel(frame, call, writes)
	case "new":
		return vm.handleChannelNew(frame, call, writes)
	case "send":
		return vm.handleChannelSend(frame, call)
	case "recv":
		return vm.handleChannelRecv(frame, call, writes)
	case "try_send":
		return vm.handleChannelTrySend(frame, call, writes)
	case "try_recv":
		return vm.handleChannelTryRecv(frame, call, writes)
	case "close":
		return vm.handleChannelClose(frame, call)

	case "rt_write_stdout":
		return vm.handleWriteStdout(frame, call, writes)
	case "rt_write_stderr":
		return vm.handleWriteStderr(frame, call, writes)
	case "rt_fs_cwd":
		return vm.handleFsCwd(frame, call, writes)
	case "rt_fs_metadata":
		return vm.handleFsMetadata(frame, call, writes)
	case "rt_fs_read_dir":
		return vm.handleFsReadDir(frame, call, writes)
	case "rt_fs_mkdir":
		return vm.handleFsMkdir(frame, call, writes)
	case "rt_fs_remove_file":
		return vm.handleFsRemoveFile(frame, call, writes)
	case "rt_fs_remove_dir":
		return vm.handleFsRemoveDir(frame, call, writes)
	case "rt_fs_open":
		return vm.handleFsOpen(frame, call, writes)
	case "rt_fs_close":
		return vm.handleFsClose(frame, call, writes)
	case "rt_fs_read":
		return vm.handleFsRead(frame, call, writes)
	case "rt_fs_write":
		return vm.handleFsWrite(frame, call, writes)
	case "rt_fs_seek":
		return vm.handleFsSeek(frame, call, writes)
	case "rt_fs_flush":
		return vm.handleFsFlush(frame, call, writes)
	case "rt_fs_read_file":
		return vm.handleFsReadFile(frame, call, writes)
	case "rt_fs_write_file":
		return vm.handleFsWriteFile(frame, call, writes)
	case "rt_fs_file_name":
		return vm.handleFsFileName(frame, call, writes)
	case "rt_fs_file_type":
		return vm.handleFsFileType(frame, call, writes)
	case "rt_fs_file_metadata":
		return vm.handleFsFileMetadata(frame, call, writes)
	case "rt_net_listen":
		return vm.handleNetListen(frame, call, writes)
	case "rt_net_close_listener":
		return vm.handleNetCloseListener(frame, call, writes)
	case "rt_net_close_conn":
		return vm.handleNetCloseConn(frame, call, writes)
	case "rt_net_accept":
		return vm.handleNetAccept(frame, call, writes)
	case "rt_net_read":
		return vm.handleNetRead(frame, call, writes)
	case "rt_net_write":
		return vm.handleNetWrite(frame, call, writes)
	case "rt_net_wait_accept":
		return vm.handleNetWaitAccept(frame, call, writes)
	case "rt_net_wait_readable":
		return vm.handleNetWaitReadable(frame, call, writes)
	case "rt_net_wait_writable":
		return vm.handleNetWaitWritable(frame, call, writes)

	case "rt_exit":
		return vm.handleRtExit(frame, call)
	case "rt_panic":
		return vm.handleRtPanic(frame, call)
	case "rt_panic_bounds":
		return vm.handleRtPanicBounds(frame, call)

	case "exit":
		return vm.handleExit(frame, call)

	case "from_str":
		return vm.handleFromStr(frame, call, writes)

	case "__len":
		return vm.handleLen(frame, call, writes)

	case "__range":
		return vm.handleArrayRange(frame, call, writes)

	case "next":
		return vm.handleRangeNext(frame, call, writes)

	case "__clone":
		return vm.handleClone(frame, call, writes)

	case "clone":
		if call.HasDst && vm.isTaskType(frame.Locals[call.Dst.Local].TypeID) {
			return vm.handleTaskClone(frame, call, writes)
		}
		return vm.handleCloneValue(frame, call, writes)
	case "cancel":
		return vm.handleTaskCancel(frame, call)

	case "__index":
		return vm.handleIndex(frame, call, writes)

	case "__to":
		return vm.handleTo(frame, call, writes)

	case "__add":
		return vm.handleMagicBinary(frame, call, writes, "__add", ast.ExprBinaryAdd)
	case "__sub":
		return vm.handleMagicBinary(frame, call, writes, "__sub", ast.ExprBinarySub)
	case "__mul":
		return vm.handleMagicBinary(frame, call, writes, "__mul", ast.ExprBinaryMul)
	case "__div":
		return vm.handleMagicBinary(frame, call, writes, "__div", ast.ExprBinaryDiv)
	case "__mod":
		return vm.handleMagicBinary(frame, call, writes, "__mod", ast.ExprBinaryMod)
	case "__bit_and":
		return vm.handleMagicBinary(frame, call, writes, "__bit_and", ast.ExprBinaryBitAnd)
	case "__bit_or":
		return vm.handleMagicBinary(frame, call, writes, "__bit_or", ast.ExprBinaryBitOr)
	case "__bit_xor":
		return vm.handleMagicBinary(frame, call, writes, "__bit_xor", ast.ExprBinaryBitXor)
	case "__shl":
		return vm.handleMagicBinary(frame, call, writes, "__shl", ast.ExprBinaryShiftLeft)
	case "__shr":
		return vm.handleMagicBinary(frame, call, writes, "__shr", ast.ExprBinaryShiftRight)
	case "__eq":
		return vm.handleMagicBinary(frame, call, writes, "__eq", ast.ExprBinaryEq)
	case "__ne":
		return vm.handleMagicBinary(frame, call, writes, "__ne", ast.ExprBinaryNotEq)
	case "__lt":
		return vm.handleMagicBinary(frame, call, writes, "__lt", ast.ExprBinaryLess)
	case "__le":
		return vm.handleMagicBinary(frame, call, writes, "__le", ast.ExprBinaryLessEq)
	case "__gt":
		return vm.handleMagicBinary(frame, call, writes, "__gt", ast.ExprBinaryGreater)
	case "__ge":
		return vm.handleMagicBinary(frame, call, writes, "__ge", ast.ExprBinaryGreaterEq)
	case "__pos":
		return vm.handleMagicUnary(frame, call, writes, "__pos", ast.ExprUnaryPlus)
	case "__neg":
		return vm.handleMagicUnary(frame, call, writes, "__neg", ast.ExprUnaryMinus)
	case "__not":
		return vm.handleMagicUnary(frame, call, writes, "__not", ast.ExprUnaryNot)

	case "rt_string_concat":
		return vm.handleStringConcat(frame, call, writes)
	case "rt_string_eq":
		return vm.handleStringEq(frame, call, writes)

	default:
		return vm.eb.unsupportedIntrinsic(name)
	}
}
