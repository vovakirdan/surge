package llvm

import (
	"fmt"
	"sort"
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

func isBlockingFunc(f *mir.Func) bool {
	if f == nil || f.Name == "" {
		return false
	}
	return strings.HasPrefix(f.Name, "__blocking_block$")
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
	fmt.Fprintf(&e.buf, "  unreachable\n")
	fmt.Fprintf(&e.buf, "}\n\n")
	return nil
}

func (e *Emitter) emitBlockingDispatch() error {
	if e == nil || e.mod == nil {
		return nil
	}
	blockIDs := make([]mir.FuncID, 0)
	for id, f := range e.mod.Funcs {
		if isBlockingFunc(f) {
			blockIDs = append(blockIDs, id)
		}
	}
	sort.Slice(blockIDs, func(i, j int) bool { return blockIDs[i] < blockIDs[j] })

	fmt.Fprintf(&e.buf, "define i64 @__surge_blocking_call(i64 %%id, ptr %%state) {\n")
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  switch i64 %%id, label %%blocking_default [\n")
	for _, id := range blockIDs {
		fmt.Fprintf(&e.buf, "    i64 %d, label %%blocking.%d\n", id, id)
	}
	fmt.Fprintf(&e.buf, "  ]\n")

	fe := funcEmitter{emitter: e}
	for _, id := range blockIDs {
		f := e.mod.Funcs[id]
		if f == nil {
			continue
		}
		name := e.funcNames[id]
		sig, ok := e.funcSigs[id]
		if !ok {
			return fmt.Errorf("missing blocking function signature for %s", f.Name)
		}
		if len(sig.params) != 1 {
			return fmt.Errorf("blocking function %s must have 1 parameter", f.Name)
		}
		fmt.Fprintf(&e.buf, "blocking.%d:\n", id)
		if sig.ret == "void" {
			fmt.Fprintf(&e.buf, "  call void @%s(ptr %%state)\n", name)
			fmt.Fprintf(&e.buf, "  ret i64 0\n")
			continue
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&e.buf, "  %s = call %s @%s(ptr %%state)\n", tmp, sig.ret, name)
		bits, err := fe.emitValueToI64(tmp, sig.ret, f.Result)
		if err != nil {
			return err
		}
		fmt.Fprintf(&e.buf, "  ret i64 %s\n", bits)
	}

	fmt.Fprintf(&e.buf, "blocking_default:\n")
	if sc, ok := e.stringConsts["missing blocking function"]; ok && sc.globalName != "" {
		fmt.Fprintf(&e.buf, "  call void @rt_panic(ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d)\n", sc.arrayLen, sc.globalName, sc.dataLen)
	}
	fmt.Fprintf(&e.buf, "  unreachable\n")
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

func (fe *funcEmitter) emitInstrBlocking(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	stateVal, stateTy, err := fe.emitStructLit(&ins.Blocking.State)
	if err != nil {
		return err
	}
	if stateTy != "ptr" {
		return fmt.Errorf("blocking expects state pointer, got %s", stateTy)
	}
	layout, err := fe.emitter.layoutOf(ins.Blocking.State.TypeID)
	if err != nil {
		return err
	}
	size := layout.Size
	align := layout.Align
	if align <= 0 {
		align = 1
	}
	callTmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf,
		"  %s = call ptr @rt_blocking_submit(i64 %d, ptr %s, i64 %d, i64 %d)\n",
		callTmp,
		ins.Blocking.FuncID,
		stateVal,
		size,
		align)
	ptr, dstTy, err := fe.emitPlacePtr(ins.Blocking.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, callTmp, ptr)
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

func (fe *funcEmitter) emitInstrTimeout(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	val, valTy, err := fe.emitValueOperand(&ins.Timeout.Task)
	if err != nil {
		return err
	}
	if valTy != "ptr" {
		return fmt.Errorf("timeout expects Task pointer, got %s", valTy)
	}
	ms64, err := fe.emitUintOperandToI64(&ins.Timeout.Ms, "timeout duration out of range")
	if err != nil {
		return err
	}
	bitsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", bitsPtr)
	kindVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_timeout_poll(ptr %s, i64 %s, ptr %s)\n", kindVal, val, ms64, bitsPtr)
	pendingCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 0\n", pendingCond, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb.inline.timeout_done%d\n", pendingCond, ins.Timeout.PendBB, fe.inlineBlock)

	doneBB := fmt.Sprintf("bb.inline.timeout_done%d", fe.inlineBlock)
	fe.inlineBlock++
	successBB := fe.nextInlineBlock()
	cancelBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", doneBB)
	successCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", successCond, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", successCond, successBB, cancelBB)

	resultType, err := fe.placeBaseType(ins.Timeout.Dst)
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
	ptr, dstTy, err := fe.emitPlacePtr(ins.Timeout.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, successPtr, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Timeout.ReadyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cancelBB)
	cancelPtr, err := fe.emitTagValue(resultType, "Cancelled", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	ptr, dstTy, err = fe.emitPlacePtr(ins.Timeout.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, cancelPtr, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Timeout.ReadyBB)

	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitInstrSelect(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	armCount := len(ins.Select.Arms)
	if armCount == 0 {
		return fmt.Errorf("select expects at least one arm")
	}
	defaultIndex := int64(-1)
	for i := range ins.Select.Arms {
		arm := &ins.Select.Arms[i]
		switch arm.Kind {
		case mir.SelectArmDefault:
			if defaultIndex >= 0 {
				return fmt.Errorf("select has multiple default arms")
			}
			defaultIndex = int64(i)
		case mir.SelectArmTask, mir.SelectArmChanRecv, mir.SelectArmChanSend, mir.SelectArmTimeout:
			// handled below
		default:
			return fmt.Errorf("unsupported select arm kind %v", arm.Kind)
		}
	}

	kindsPtr := fe.nextTemp()
	handlesPtr := fe.nextTemp()
	valuesPtr := fe.nextTemp()
	msPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i8]\n", kindsPtr, armCount)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x ptr]\n", handlesPtr, armCount)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i64]\n", valuesPtr, armCount)
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca [%d x i64]\n", msPtr, armCount)
	for i := range ins.Select.Arms {
		arm := &ins.Select.Arms[i]
		kindPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 %d\n", kindPtr, armCount, kindsPtr, i)
		fmt.Fprintf(&fe.emitter.buf, "  store i8 %d, ptr %s\n", arm.Kind, kindPtr)

		handlePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 %d\n", handlePtr, armCount, handlesPtr, i)
		switch arm.Kind {
		case mir.SelectArmTask, mir.SelectArmTimeout:
			val, valTy, err := fe.emitValueOperand(&arm.Task)
			if err != nil {
				return err
			}
			if valTy != "ptr" {
				return fmt.Errorf("select expects Task pointer, got %s", valTy)
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", val, handlePtr)
		case mir.SelectArmChanRecv, mir.SelectArmChanSend:
			chVal, err := fe.emitChannelHandle(&arm.Channel)
			if err != nil {
				return err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", chVal, handlePtr)
		default:
			fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", handlePtr)
		}

		valuePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 %d\n", valuePtr, armCount, valuesPtr, i)
		if arm.Kind == mir.SelectArmChanSend {
			val, valTy, err := fe.emitValueOperand(&arm.Value)
			if err != nil {
				return err
			}
			valueType := operandValueType(fe.emitter.types, &arm.Value)
			if valueType == types.NoTypeID && arm.Value.Kind != mir.OperandConst {
				if baseType, baseErr := fe.placeBaseType(arm.Value.Place); baseErr == nil {
					valueType = baseType
				}
			}
			bitsVal, err := fe.emitValueToI64(val, valTy, valueType)
			if err != nil {
				return err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", bitsVal, valuePtr)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", valuePtr)
		}

		msElemPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 %d\n", msElemPtr, armCount, msPtr, i)
		if arm.Kind == mir.SelectArmTimeout {
			ms64, err := fe.emitUintOperandToI64(&arm.Ms, "timeout duration out of range")
			if err != nil {
				return err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s\n", ms64, msElemPtr)
		} else {
			fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", msElemPtr)
		}
	}

	kindsBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0\n", kindsBase, armCount, kindsPtr)
	handlesBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x ptr], ptr %s, i64 0, i64 0\n", handlesBase, armCount, handlesPtr)
	valuesBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 0\n", valuesBase, armCount, valuesPtr)
	msBase := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i64], ptr %s, i64 0, i64 0\n", msBase, armCount, msPtr)
	idxVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_select_poll(i64 %d, ptr %s, ptr %s, ptr %s, ptr %s, i64 %d)\n",
		idxVal, armCount, kindsBase, handlesBase, valuesBase, msBase, defaultIndex)
	pendingCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp slt i64 %s, 0\n", pendingCond, idxVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb.inline.select_ready%d\n", pendingCond, ins.Select.PendBB, fe.inlineBlock)

	readyBB := fmt.Sprintf("bb.inline.select_ready%d", fe.inlineBlock)
	fe.inlineBlock++
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
	dstType, err := fe.placeBaseType(ins.Select.Dst)
	if err != nil {
		return err
	}
	val, valTy, err := fe.emitI64ToValue(idxVal, dstType)
	if err != nil {
		return err
	}
	ptr, dstTy, err := fe.emitPlacePtr(ins.Select.Dst)
	if err != nil {
		return err
	}
	if dstTy != valTy {
		dstTy = valTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, val, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.Select.ReadyBB)

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
