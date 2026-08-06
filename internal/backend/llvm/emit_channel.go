package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func isChannelType(typesIn *types.Interner, typeID types.TypeID) bool {
	if typesIn == nil || typeID == types.NoTypeID {
		return false
	}
	typeID = resolveValueType(typesIn, typeID)
	tt, ok := typesIn.Lookup(typeID)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindStruct:
		info, ok := typesIn.StructInfo(typeID)
		if !ok || info == nil || typesIn.Strings == nil {
			return false
		}
		name, ok := typesIn.Strings.Lookup(info.Name)
		return ok && name == "Channel"
	case types.KindAlias:
		info, ok := typesIn.AliasInfo(typeID)
		if !ok || info == nil || typesIn.Strings == nil {
			return false
		}
		name, ok := typesIn.Strings.Lookup(info.Name)
		return ok && name == "Channel"
	default:
		return false
	}
}

// isFarChannelType reports whether typeID is `far Channel<T>` (any
// wrapping of &/own/* around the far qualifier is stripped by
// resolveValueType first, matching isChannelType's own unwrap). Used to
// route a far-channel-typed binding's scope-exit drop to the runtime's
// far-handle release instead of the ordinary local-channel leaf path,
// which isFarChannelType-gated callers never reach: resolveValueType
// stops at a KindFar node (it does not unwrap the far qualifier itself,
// only Alias/Own/Reference/Pointer), so a local Channel<T> and a far
// Channel<T> are never confused here.
func isFarChannelType(typesIn *types.Interner, typeID types.TypeID) bool {
	if typesIn == nil || typeID == types.NoTypeID {
		return false
	}
	typeID = resolveValueType(typesIn, typeID)
	tt, ok := typesIn.Lookup(typeID)
	if !ok || tt.Kind != types.KindFar {
		return false
	}
	return isChannelType(typesIn, tt.Elem)
}

// channelElemType extracts T from Channel<T> (any wrapping the resolved
// struct/alias node itself does not already strip — callers pass a
// resolveValueType'd id), mirroring sema's channelPayloadType.
func channelElemType(typesIn *types.Interner, channelType types.TypeID) types.TypeID {
	if typesIn == nil || channelType == types.NoTypeID {
		return types.NoTypeID
	}
	if info, ok := typesIn.StructInfo(channelType); ok && info != nil && typesIn.Strings != nil {
		if name, nameOK := typesIn.Strings.Lookup(info.Name); nameOK && name == "Channel" {
			if args := typesIn.StructArgs(channelType); len(args) == 1 {
				return args[0]
			}
		}
	}
	if info, ok := typesIn.AliasInfo(channelType); ok && info != nil && typesIn.Strings != nil {
		if name, nameOK := typesIn.Strings.Lookup(info.Name); nameOK && name == "Channel" && len(info.TypeArgs) == 1 {
			return info.TypeArgs[0]
		}
	}
	return types.NoTypeID
}

// channelPayloadDropID resolves the drop-fn id a channel of element type T
// needs for its buffered/mailbox payload reclamation: 0 for Copy/inert T
// (never dispatched), matching the same gate 053a's task-result drop-fn id
// uses (a raw uint64_t bits value that may or may not be a real pointer,
// not an unconditionally-boxed envelope the way an async suspend state is).
func (fe *funcEmitter) channelPayloadDropID(channelDstType types.TypeID) types.TypeID {
	elem := channelElemType(fe.emitter.types, resolveValueType(fe.emitter.types, channelDstType))
	if elem == types.NoTypeID || !fe.emitter.typeOwnsHeap(elem) {
		return types.NoTypeID
	}
	return fe.emitter.registerCrossingDropResult(elem)
}

func (fe *funcEmitter) emitChannelHandle(op *mir.Operand) (string, error) {
	if fe == nil || op == nil {
		return "", fmt.Errorf("missing channel operand")
	}
	val, valTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	if valTy != "ptr" {
		return "", fmt.Errorf("channel expects ptr handle, got %s", valTy)
	}
	if isRefType(fe.emitter.types, op.Type) {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", tmp, val)
		return tmp, nil
	}
	return val, nil
}

func (fe *funcEmitter) emitChannelIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || fe == nil {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	if name == "" {
		return false, nil
	}
	base := stripGenericSuffix(name)
	switch base {
	case "make_channel":
		if len(call.Args) != 1 {
			return true, fmt.Errorf("make_channel expects 1 argument")
		}
		cap64, err := fe.emitUintOperandToI64(&call.Args[0], "channel capacity out of range")
		if err != nil {
			return true, err
		}
		dropID := types.TypeID(0)
		if call.HasDst {
			dstType, err := fe.placeBaseType(call.Dst)
			if err != nil {
				return true, err
			}
			dropID = fe.channelPayloadDropID(dstType)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_channel_new(i64 %s, i64 %d)\n", tmp, cap64, dropID)
		if call.HasDst {
			ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
			if err != nil {
				return true, err
			}
			if dstTy != "ptr" {
				dstTy = "ptr"
			}
			fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		}
		return true, nil
	case "close":
		if len(call.Args) != 1 {
			return true, fmt.Errorf("close expects 1 argument")
		}
		if !isChannelType(fe.emitter.types, call.Args[0].Type) {
			return false, nil
		}
		chVal, err := fe.emitChannelHandle(&call.Args[0])
		if err != nil {
			return true, err
		}
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_channel_close(ptr %s)\n", chVal)
		return true, nil
	case "send":
		if len(call.Args) != 2 {
			return true, fmt.Errorf("send expects 2 arguments")
		}
		if !isChannelType(fe.emitter.types, call.Args[0].Type) {
			return false, nil
		}
		chVal, err := fe.emitChannelHandle(&call.Args[0])
		if err != nil {
			return true, err
		}
		val, valTy, err := fe.emitValueOperand(&call.Args[1])
		if err != nil {
			return true, err
		}
		valueType := operandValueType(fe.emitter.types, &call.Args[1])
		if valueType == types.NoTypeID && call.Args[1].Kind != mir.OperandConst {
			if baseType, baseErr := fe.placeBaseType(call.Args[1].Place); baseErr == nil {
				valueType = baseType
			}
		}
		bitsVal, err := fe.emitValueToI64(val, valTy, valueType)
		if err != nil {
			return true, err
		}
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_channel_send_blocking(ptr %s, i64 %s)\n", chVal, bitsVal)
		return true, nil
	case "recv":
		if len(call.Args) != 1 {
			return true, fmt.Errorf("recv expects 1 argument")
		}
		if !isChannelType(fe.emitter.types, call.Args[0].Type) {
			return false, nil
		}
		chVal, err := fe.emitChannelHandle(&call.Args[0])
		if err != nil {
			return true, err
		}
		bitsPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", bitsPtr, alignWord)
		kindVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_channel_recv_blocking(ptr %s, ptr %s)\n", kindVal, chVal, bitsPtr)
		if call.HasDst {
			dstType, err := fe.placeBaseType(call.Dst)
			if err != nil {
				return true, err
			}
			someIdx, someMeta, err := fe.emitter.tagCaseMeta(dstType, "Some", symbols.NoSymbolID)
			if err != nil {
				return true, err
			}
			if len(someMeta.PayloadTypes) != 1 {
				return true, fmt.Errorf("Option::Some expects single payload")
			}
			payloadType := someMeta.PayloadTypes[0]
			readyBB := fe.nextInlineBlock()
			noneBB := fe.nextInlineBlock()
			contBB := fe.nextInlineBlock()
			outPtr := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr, align %d\n", outPtr, alignPtr)
			hasValue := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", hasValue, kindVal)
			fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", hasValue, readyBB, noneBB)

			fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
			bitsVal := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)
			payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
			if err != nil {
				return true, err
			}
			somePtr, err := fe.emitTagValueSinglePayload(dstType, someIdx, payloadType, payloadVal, payloadTy, payloadType)
			if err != nil {
				return true, err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", somePtr, outPtr)
			fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

			fmt.Fprintf(&fe.emitter.buf, "%s:\n", noneBB)
			nonePtr, err := fe.emitTagValue(dstType, "nothing", symbols.NoSymbolID, nil)
			if err != nil {
				return true, err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nonePtr, outPtr)
			fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

			fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
			resultVal := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", resultVal, outPtr)
			ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
			if err != nil {
				return true, err
			}
			if dstTy != "ptr" {
				dstTy = "ptr"
			}
			fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, resultVal, ptr)
		}
		return true, nil
	case "try_send":
		if len(call.Args) != 2 {
			return true, fmt.Errorf("try_send expects 2 arguments")
		}
		if !isChannelType(fe.emitter.types, call.Args[0].Type) {
			return false, nil
		}
		chVal, err := fe.emitChannelHandle(&call.Args[0])
		if err != nil {
			return true, err
		}
		val, valTy, err := fe.emitValueOperand(&call.Args[1])
		if err != nil {
			return true, err
		}
		valueType := operandValueType(fe.emitter.types, &call.Args[1])
		if valueType == types.NoTypeID && call.Args[1].Kind != mir.OperandConst {
			if baseType, baseErr := fe.placeBaseType(call.Args[1].Place); baseErr == nil {
				valueType = baseType
			}
		}
		bitsVal, err := fe.emitValueToI64(val, valTy, valueType)
		if err != nil {
			return true, err
		}
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_channel_try_send(ptr %s, i64 %s)\n", okVal, chVal, bitsVal)
		if call.HasDst {
			ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
			if err != nil {
				return true, err
			}
			if dstTy != "i1" {
				dstTy = "i1"
			}
			fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, okVal, ptr)
		}
		return true, nil
	case "try_recv":
		if len(call.Args) != 1 {
			return true, fmt.Errorf("try_recv expects 1 argument")
		}
		if !isChannelType(fe.emitter.types, call.Args[0].Type) {
			return false, nil
		}
		chVal, err := fe.emitChannelHandle(&call.Args[0])
		if err != nil {
			return true, err
		}
		bitsPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", bitsPtr, alignWord)
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_channel_try_recv(ptr %s, ptr %s)\n", okVal, chVal, bitsPtr)
		if call.HasDst {
			dstType, err := fe.placeBaseType(call.Dst)
			if err != nil {
				return true, err
			}
			someIdx, someMeta, err := fe.emitter.tagCaseMeta(dstType, "Some", symbols.NoSymbolID)
			if err != nil {
				return true, err
			}
			if len(someMeta.PayloadTypes) != 1 {
				return true, fmt.Errorf("Option::Some expects single payload")
			}
			payloadType := someMeta.PayloadTypes[0]
			readyBB := fe.nextInlineBlock()
			noneBB := fe.nextInlineBlock()
			contBB := fe.nextInlineBlock()
			outPtr := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr, align %d\n", outPtr, alignPtr)
			fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, readyBB, noneBB)

			fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
			bitsVal := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)
			payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
			if err != nil {
				return true, err
			}
			somePtr, err := fe.emitTagValueSinglePayload(dstType, someIdx, payloadType, payloadVal, payloadTy, payloadType)
			if err != nil {
				return true, err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", somePtr, outPtr)
			fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

			fmt.Fprintf(&fe.emitter.buf, "%s:\n", noneBB)
			nonePtr, err := fe.emitTagValue(dstType, "nothing", symbols.NoSymbolID, nil)
			if err != nil {
				return true, err
			}
			fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nonePtr, outPtr)
			fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

			fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
			resultVal := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", resultVal, outPtr)
			ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
			if err != nil {
				return true, err
			}
			if dstTy != "ptr" {
				dstTy = "ptr"
			}
			fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, resultVal, ptr)
		}
		return true, nil
	default:
		return false, nil
	}
}

func (fe *funcEmitter) emitInstrChanSend(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	chVal, err := fe.emitChannelHandle(&ins.ChanSend.Channel)
	if err != nil {
		return err
	}
	val, valTy, err := fe.emitValueOperand(&ins.ChanSend.Value)
	if err != nil {
		return err
	}
	valueType := operandValueType(fe.emitter.types, &ins.ChanSend.Value)
	if valueType == types.NoTypeID && ins.ChanSend.Value.Kind != mir.OperandConst {
		if baseType, baseErr := fe.placeBaseType(ins.ChanSend.Value.Place); baseErr == nil {
			valueType = baseType
		}
	}
	bitsVal, err := fe.emitValueToI64(val, valTy, valueType)
	if err != nil {
		return err
	}
	callee := "rt_channel_send"
	if ins.ChanSend.YieldAfterHandoff {
		callee = "rt_channel_send_yield"
	}
	okVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @%s(ptr %s, i64 %s)\n", okVal, callee, chVal, bitsVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb%d\n", okVal, ins.ChanSend.ReadyBB, ins.ChanSend.PendBB)
	fe.blockTerminated = true
	return nil
}

func (fe *funcEmitter) emitInstrChanRecv(ins *mir.Instr) error {
	if ins == nil {
		return nil
	}
	if ins.ChanRecv.Anchored {
		return fe.emitAnchoredChanRecv(ins)
	}
	chVal, err := fe.emitChannelHandle(&ins.ChanRecv.Channel)
	if err != nil {
		return err
	}
	bitsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64, align %d\n", bitsPtr, alignWord)
	kindVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_channel_recv(ptr %s, ptr %s)\n", kindVal, chVal, bitsPtr)
	pendingCond := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 0\n", pendingCond, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb.inline.chan_recv_done%d\n", pendingCond, ins.ChanRecv.PendBB, fe.inlineBlock)

	doneBB := fmt.Sprintf("bb.inline.chan_recv_done%d", fe.inlineBlock)
	fe.inlineBlock++
	valueBB := fe.nextInlineBlock()
	closedBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", doneBB)
	hasValue := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i8 %s, 1\n", hasValue, kindVal)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", hasValue, valueBB, closedBB)

	dstType, err := fe.placeBaseType(ins.ChanRecv.Dst)
	if err != nil {
		return err
	}
	someIdx, someMeta, err := fe.emitter.tagCaseMeta(dstType, "Some", symbols.NoSymbolID)
	if err != nil {
		return err
	}
	if len(someMeta.PayloadTypes) != 1 {
		return fmt.Errorf("Option::Some expects single payload")
	}
	payloadType := someMeta.PayloadTypes[0]

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", valueBB)
	bitsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)
	payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
	if err != nil {
		return err
	}
	somePtr, err := fe.emitTagValueSinglePayload(dstType, someIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	ptr, dstTy, err := fe.emitPlacePtr(ins.ChanRecv.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, somePtr, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.ChanRecv.ReadyBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", closedBB)
	nonePtr, err := fe.emitTagValue(dstType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	ptr, dstTy, err = fe.emitPlacePtr(ins.ChanRecv.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, nonePtr, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.ChanRecv.ReadyBB)

	fe.blockTerminated = true
	return nil
}
