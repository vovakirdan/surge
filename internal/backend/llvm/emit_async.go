package llvm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func isPollFunc(f *mir.Func) bool {
	if f == nil || f.Name == "" {
		return false
	}
	return strings.HasSuffix(f.Name, "$poll")
}

func (e *Emitter) emitPollDispatch() error {
	if e == nil || e.mod == nil {
		return nil
	}
	pollIDs := make([]mir.FuncID, 0)
	for id, f := range e.mod.Funcs {
		if isPollFunc(f) {
			pollIDs = append(pollIDs, id)
		}
	}
	sort.Slice(pollIDs, func(i, j int) bool { return pollIDs[i] < pollIDs[j] })

	fmt.Fprintf(&e.buf, "define void @__surge_poll_call(i64 %%id) {\n")
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  switch i64 %%id, label %%poll_default [\n")
	for _, id := range pollIDs {
		fmt.Fprintf(&e.buf, "    i64 %d, label %%poll.%d\n", id, id)
	}
	fmt.Fprintf(&e.buf, "  ]\n")

	for _, id := range pollIDs {
		f := e.mod.Funcs[id]
		if f == nil {
			continue
		}
		name := e.funcNames[id]
		sig, ok := e.funcSigs[id]
		if !ok {
			return fmt.Errorf("missing poll function signature for %s", f.Name)
		}
		if len(sig.params) != 0 {
			return fmt.Errorf("poll function %s must not have parameters", f.Name)
		}
		fmt.Fprintf(&e.buf, "poll.%d:\n", id)
		if sig.ret == "void" {
			fmt.Fprintf(&e.buf, "  call void @%s()\n", name)
		} else {
			fmt.Fprintf(&e.buf, "  call %s @%s()\n", sig.ret, name)
		}
		fmt.Fprintf(&e.buf, "  ret void\n")
	}

	fmt.Fprintf(&e.buf, "poll_default:\n")
	if sc, ok := e.stringConsts["missing poll function"]; ok && sc.globalName != "" {
		fmt.Fprintf(&e.buf, "  call void @rt_panic(ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d)\n", sc.arrayLen, sc.globalName, sc.dataLen)
	}
	fmt.Fprintf(&e.buf, "  ret void\n")
	fmt.Fprintf(&e.buf, "}\n\n")
	return nil
}

func isTaskType(typesIn *types.Interner, typeID types.TypeID) bool {
	if typesIn == nil || typeID == types.NoTypeID {
		return false
	}
	typeID = resolveAliasAndOwn(typesIn, typeID)
	tt, ok := typesIn.Lookup(typeID)
	if !ok || tt.Kind != types.KindStruct {
		if info, aliasOK := typesIn.AliasInfo(typeID); aliasOK && info != nil && typesIn.Strings != nil {
			name, nameOK := typesIn.Strings.Lookup(info.Name)
			return nameOK && name == "Task"
		}
		return false
	}
	info, ok := typesIn.StructInfo(typeID)
	if !ok || info == nil || typesIn.Strings == nil {
		return false
	}
	name, ok := typesIn.Strings.Lookup(info.Name)
	return ok && name == "Task"
}

func (fe *funcEmitter) emitInstrSpawn(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	val, valTy, err := fe.emitValueOperand(&ins.Spawn.Value)
	if err != nil {
		return err
	}
	if valTy != "ptr" {
		return fmt.Errorf("spawn expects Task pointer, got %s", valTy)
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_task_wake(ptr %s)\n", val)
	ptr, dstTy, err := fe.emitPlacePtr(ins.Spawn.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, val, ptr)
	return nil
}

func (fe *funcEmitter) emitInstrAwait(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	val, valTy, err := fe.emitValueOperand(&ins.Await.Task)
	if err != nil {
		return err
	}
	if valTy != "ptr" {
		return fmt.Errorf("await expects Task pointer, got %s", valTy)
	}
	kindPtr := fe.nextTemp()
	bitsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i8\n", kindPtr)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", bitsPtr)
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_task_await(ptr %s, ptr %s, ptr %s)\n", val, kindPtr, bitsPtr)
	kindVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i8, ptr %s\n", kindVal, kindPtr)
	bitsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)

	resultType, err := fe.placeBaseType(ins.Await.Dst)
	if err != nil {
		return err
	}
	successIdx, payloadType, err := fe.taskResultInfo(resultType)
	if err != nil {
		return err
	}
	resultPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", resultPtr)
	successBB := fe.nextInlineBlock()
	cancelBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()
	isSuccess := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", isSuccess, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isSuccess, successBB, cancelBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", successBB)
	payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
	if err != nil {
		return err
	}
	successPtr, err := fe.emitTagValueSinglePayload(resultType, successIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", successPtr, resultPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cancelBB)
	cancelPtr, err := fe.emitTagValue(resultType, "Cancelled", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", cancelPtr, resultPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	resultVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", resultVal, resultPtr)
	ptr, dstTy, err := fe.emitPlacePtr(ins.Await.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, resultVal, ptr)
	return nil
}

func (fe *funcEmitter) emitInstrPoll(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	val, valTy, err := fe.emitValueOperand(&ins.Poll.Task)
	if err != nil {
		return err
	}
	if valTy != "ptr" {
		return fmt.Errorf("poll expects Task pointer, got %s", valTy)
	}
	bitsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", bitsPtr)
	kindVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_task_poll(ptr %s, ptr %s)\n", kindVal, val, bitsPtr)
	pendingCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 0\n", pendingCond, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb.inline.poll_done%d\n", pendingCond, ins.Poll.PendBB, fe.inlineBlock)

	doneBB := fmt.Sprintf("bb.inline.poll_done%d", fe.inlineBlock)
	fe.inlineBlock++
	successBB := fe.nextInlineBlock()
	cancelBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", doneBB)
	successCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", successCond, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", successCond, successBB, cancelBB)

	resultType, err := fe.placeBaseType(ins.Poll.Dst)
	if err != nil {
		return err
	}
	successIdx, payloadType, err := fe.taskResultInfo(resultType)
	if err != nil {
		return err
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", successBB)
	bitsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)
	payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
	if err != nil {
		return err
	}
	successPtr, err := fe.emitTagValueSinglePayload(resultType, successIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	ptr, dstTy, err := fe.emitPlacePtr(ins.Poll.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, successPtr, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Poll.ReadyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cancelBB)
	cancelPtr, err := fe.emitTagValue(resultType, "Cancelled", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	ptr, dstTy, err = fe.emitPlacePtr(ins.Poll.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, cancelPtr, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Poll.ReadyBB)

	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitInstrJoinAll(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	scopeVal, scopeTy, err := fe.emitValueOperand(&ins.JoinAll.Scope)
	if err != nil {
		return err
	}
	if scopeTy != "ptr" {
		return fmt.Errorf("join_all expects scope handle, got %s", scopeTy)
	}
	pendingPtr := fe.nextTemp()
	failfastPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", pendingPtr)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i1\n", failfastPtr)
	doneVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_scope_join_all(ptr %s, ptr %s, ptr %s)\n", doneVal, scopeVal, pendingPtr, failfastPtr)
	readyBB := fmt.Sprintf("bb.inline.join_ready%d", fe.inlineBlock)
	fe.inlineBlock++
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%bb%d\n", doneVal, readyBB, ins.JoinAll.PendBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
	failfastVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i1, ptr %s\n", failfastVal, failfastPtr)
	ptr, dstTy, err := fe.emitPlacePtr(ins.JoinAll.Dst)
	if err != nil {
		return err
	}
	if dstTy != "i1" {
		dstTy = "i1"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, failfastVal, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.JoinAll.ReadyBB)

	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitTermAsyncYield(term *mir.Terminator) error {
	if term == nil {
		return nil
	}
	stateVal, stateTy, err := fe.emitValueOperand(&term.AsyncYield.State)
	if err != nil {
		return err
	}
	if stateTy != "ptr" {
		return fmt.Errorf("async_yield expects state pointer, got %s", stateTy)
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_async_yield(ptr %s)\n", stateVal)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}

func (fe *funcEmitter) emitTermAsyncReturn(term *mir.Terminator) error {
	if term == nil {
		return nil
	}
	stateVal, stateTy, err := fe.emitValueOperand(&term.AsyncReturn.State)
	if err != nil {
		return err
	}
	if stateTy != "ptr" {
		return fmt.Errorf("async_return expects state pointer, got %s", stateTy)
	}
	bitsVal := "0"
	if term.AsyncReturn.HasValue {
		val, valTy, err := fe.emitValueOperand(&term.AsyncReturn.Value)
		if err != nil {
			return err
		}
		valueType := operandValueType(fe.emitter.types, &term.AsyncReturn.Value)
		if valueType == types.NoTypeID && term.AsyncReturn.Value.Kind != mir.OperandConst {
			if baseType, baseErr := fe.placeBaseType(term.AsyncReturn.Value.Place); baseErr == nil {
				valueType = baseType
			}
		}
		bits, err := fe.emitValueToI64(val, valTy, valueType)
		if err != nil {
			return err
		}
		bitsVal = bits
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_async_return(ptr %s, i64 %s)\n", stateVal, bitsVal)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}

func (fe *funcEmitter) emitTermAsyncReturnCancelled(term *mir.Terminator) error {
	if term == nil {
		return nil
	}
	stateVal, stateTy, err := fe.emitValueOperand(&term.AsyncReturnCancelled.State)
	if err != nil {
		return err
	}
	if stateTy != "ptr" {
		return fmt.Errorf("async_cancel expects state pointer, got %s", stateTy)
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_async_return_cancelled(ptr %s)\n", stateVal)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
	return nil
}

func (fe *funcEmitter) taskResultInfo(resultType types.TypeID) (successIdx int, payloadType types.TypeID, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return -1, types.NoTypeID, fmt.Errorf("missing type info")
	}
	if resultType == types.NoTypeID {
		return -1, types.NoTypeID, fmt.Errorf("missing task result type")
	}
	resultType = resolveValueType(fe.emitter.types, resultType)
	successCaseIdx, successMeta, successErr := fe.emitter.tagCaseMeta(resultType, "Success", symbols.NoSymbolID)
	if successErr != nil {
		return -1, types.NoTypeID, successErr
	}
	if len(successMeta.PayloadTypes) != 1 {
		return -1, types.NoTypeID, fmt.Errorf("TaskResult::Success expects single payload")
	}
	_, cancelMeta, cancelErr := fe.emitter.tagCaseMeta(resultType, "Cancelled", symbols.NoSymbolID)
	if cancelErr != nil {
		return -1, types.NoTypeID, cancelErr
	}
	if len(cancelMeta.PayloadTypes) != 0 {
		return -1, types.NoTypeID, fmt.Errorf("TaskResult::Cancelled expects no payload")
	}
	return successCaseIdx, successMeta.PayloadTypes[0], nil
}

func (fe *funcEmitter) emitValueToI64(val, valTy string, typeID types.TypeID) (string, error) {
	switch valTy {
	case "i64":
		return val, nil
	case "ptr":
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = ptrtoint ptr %s to i64\n", out, val)
		return out, nil
	case "double":
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast double %s to i64\n", out, val)
		return out, nil
	case "float":
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast float %s to i32\n", tmp, val)
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = zext i32 %s to i64\n", out, tmp)
		return out, nil
	case "half":
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast half %s to i16\n", tmp, val)
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = zext i16 %s to i64\n", out, tmp)
		return out, nil
	}
	if strings.HasPrefix(valTy, "i") {
		width, err := strconv.Atoi(strings.TrimPrefix(valTy, "i"))
		if err != nil || width <= 0 {
			return "", fmt.Errorf("invalid integer type %s", valTy)
		}
		if width == 64 {
			return val, nil
		}
		op := "zext"
		if info, ok := intInfo(fe.emitter.types, typeID); ok && info.signed {
			op = "sext"
		}
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s to i64\n", out, op, valTy, val)
		return out, nil
	}
	return "", fmt.Errorf("unsupported value type %s for async payload", valTy)
}

func (fe *funcEmitter) emitI64ToValue(bits string, typeID types.TypeID) (value, valueTy string, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type info")
	}
	llvmTy, err := llvmValueType(fe.emitter.types, typeID)
	if err != nil {
		return "", "", err
	}
	switch llvmTy {
	case "ptr":
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = inttoptr i64 %s to ptr\n", out, bits)
		return out, "ptr", nil
	case "double":
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast i64 %s to double\n", out, bits)
		return out, "double", nil
	case "float":
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to i32\n", tmp, bits)
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast i32 %s to float\n", out, tmp)
		return out, "float", nil
	case "half":
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to i16\n", tmp, bits)
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = bitcast i16 %s to half\n", out, tmp)
		return out, "half", nil
	case "i64":
		return bits, "i64", nil
	}
	if strings.HasPrefix(llvmTy, "i") {
		out := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to %s\n", out, bits, llvmTy)
		return out, llvmTy, nil
	}
	return "", "", fmt.Errorf("unsupported async payload type %s", llvmTy)
}

func (fe *funcEmitter) emitTaskCancelIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "cancel" {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("cancel requires 1 argument")
	}
	argType := operandValueType(fe.emitter.types, &call.Args[0])
	if !isTaskType(fe.emitter.types, argType) {
		return true, fmt.Errorf("cancel requires Task handle")
	}
	val, valTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return true, err
	}
	if valTy != "ptr" {
		return true, fmt.Errorf("cancel expects Task pointer, got %s", valTy)
	}
	fmt.Fprintf(&fe.emitter.buf, "  call void @rt_task_cancel(ptr %s)\n", val)
	return true, nil
}
