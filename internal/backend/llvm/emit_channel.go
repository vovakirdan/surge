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

// channelElementTypeID resolves what a channel is told about its element: the
// element's own type id, unconditionally.
//
// It is an id AND a descriptor pointer at the call, because the two answer
// different callers. In-process the pointer is what the channel keeps. A FAR
// channel is built on the other side of a boundary from a request that carried
// a number, so the id is what survives, and the far path turns it back into a
// descriptor with __surge_value_ops_for.
//
// Unlike the drop-fn id it replaces, this is never zero for a real element: the
// runtime needs the element's LAYOUT whether or not that element has to be
// destroyed, and a channel that does not know its element's stride cannot size
// a cell at all.
func (fe *funcEmitter) channelElementTypeID(channelDstType types.TypeID) types.TypeID {
	elem := channelElemType(fe.emitter.types, resolveValueType(fe.emitter.types, channelDstType))
	if elem == types.NoTypeID {
		return types.NoTypeID
	}
	return elem
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
	case "new":
		// `new` is a shared name: RwLock declares its own intrinsic `new`, and
		// Mutex, Condition, Semaphore and Barrier declare ordinary `new`
		// functions that pass through this same dispatcher. The destination
		// type is what separates them, exactly as it does in the VM
		// (internal/vm/intrinsic_channel.go).
		if !fe.callDstIsChannel(call) {
			return false, nil
		}
		if len(call.Args) != 1 {
			return true, fmt.Errorf("channel constructor expects 1 argument")
		}
		cap64, err := fe.emitUintOperandToI64(&call.Args[0], "channel capacity out of range")
		if err != nil {
			return true, err
		}
		elementTypeID := types.TypeID(0)
		if call.HasDst {
			dstType, err := fe.placeBaseType(call.Dst)
			if err != nil {
				return true, err
			}
			elementTypeID = fe.channelElementTypeID(dstType)
		}
		descriptor := "null"
		if elementTypeID != types.NoTypeID {
			descriptor = "@" + valueOpsSymbol(elementTypeID)
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_channel_new(i64 %s, ptr %s, i64 %d)\n",
			tmp, cap64, descriptor, elementTypeID)
		if call.HasDst {
			ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
			if err != nil {
				return true, err
			}
			if !isStorageRun(dstTy) {
				dstTy = handleType
			}
			fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
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
		srcPtr, err := fe.emitChannelSendSource(&call.Args[1])
		if err != nil {
			return true, err
		}
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_channel_send_blocking(ptr %s, ptr %s)\n", chVal, srcPtr)
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
		recvDstType := types.NoTypeID
		if call.HasDst {
			var dstErr error
			recvDstType, dstErr = fe.placeBaseType(call.Dst)
			if dstErr != nil {
				return true, dstErr
			}
		}
		recvPayloadType := fe.channelElementTypeOf(&call.Args[0])
		payloadPtr, payloadStorageTy, err := fe.emitChannelPayloadSlot(recvPayloadType)
		if err != nil {
			return true, err
		}
		kindVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_channel_recv_blocking(ptr %s, ptr %s)\n", kindVal, chVal, payloadPtr)
		if call.HasDst {
			dstType := recvDstType
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
			payloadVal, payloadTy, err := fe.emitChannelPayloadValue(payloadType, payloadStorageTy, payloadPtr)
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
			ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
			if err != nil {
				return true, err
			}
			if !isStorageRun(dstTy) {
				dstTy = handleType
			}
			fe.emitValueStore(dstTy, resultVal, ptr, dstAlign)
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
		srcPtr, err := fe.emitChannelSendSource(&call.Args[1])
		if err != nil {
			return true, err
		}
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_channel_try_send(ptr %s, ptr %s)\n", okVal, chVal, srcPtr)
		if call.HasDst {
			ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
			if err != nil {
				return true, err
			}
			if dstTy != "i1" {
				dstTy = "i1"
			}
			fe.emitValueStore(dstTy, okVal, ptr, dstAlign)
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
		payloadPtr, payloadStorageTy, err := fe.emitChannelPayloadSlot(fe.channelElementTypeOf(&call.Args[0]))
		if err != nil {
			return true, err
		}
		okVal := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_channel_try_recv(ptr %s, ptr %s)\n", okVal, chVal, payloadPtr)
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
			payloadVal, payloadTy, err := fe.emitChannelPayloadValue(payloadType, payloadStorageTy, payloadPtr)
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
			ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
			if err != nil {
				return true, err
			}
			if !isStorageRun(dstTy) {
				dstTy = handleType
			}
			fe.emitValueStore(dstTy, resultVal, ptr, dstAlign)
		}
		return true, nil
	default:
		return false, nil
	}
}

// emitChannelSendSource materializes what a NON-suspending send hands the
// runtime and answers with its address. The runtime moves the bits it is
// pointed at and never retains, so the reference the channel ends up owning
// has to be made here, once, on the one execution this call site gets:
//
//   - a RETAIN of a counted scalar bumps the count and points the runtime at
//     the caller's own storage — the caller keeps its reference, the channel
//     leaves with the bump;
//   - a clone (CopyValue) of a `@copy` composite is built into storage of its
//     own, and THAT storage is what the runtime moves from: the composite the
//     caller holds is left exactly as it was, and the box the clone allocated
//     leaves with the value instead of being dropped on the floor;
//   - a move, a plain copy, a constant: the operand's storage as it stands.
//
// Not for InstrChanSend, which is polled again after every park and takes its
// value from a temp the lowering filled in the prelude.
func (fe *funcEmitter) emitChannelSendSource(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil channel send operand")
	}
	switch op.Kind {
	case mir.OperandRetain:
		if _, _, err := fe.emitValueOperand(op); err != nil {
			return "", err
		}
		return fe.emitChannelValueAddress(op)
	case mir.OperandCopyValue:
		// The same question emitOperand asks before it clones: a shape the
		// backend keeps flat is read as its word, and the word's storage is
		// the operand's own.
		cloneTy := op.Type
		if cloneTy == types.NoTypeID {
			if base, baseErr := fe.placeBaseType(op.Place); baseErr == nil {
				cloneTy = base
			}
		}
		resolved := resolveValueType(fe.emitter.types, cloneTy)
		if !fe.emitter.hasInlineStorage(resolved) || !fe.emitter.isCloneableComposite(resolved) {
			return fe.emitChannelValueAddress(op)
		}
		clonePtr, _, err := fe.emitValueOperand(op)
		if err != nil {
			return "", err
		}
		return clonePtr, nil
	default:
		return fe.emitChannelValueAddress(op)
	}
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
	payloadPtr, payloadStorageTy, err := fe.emitChannelPayloadSlot(fe.channelElementTypeOf(&ins.ChanRecv.Channel))
	if err != nil {
		return err
	}
	kindVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i8 @rt_channel_recv(ptr %s, ptr %s)\n", kindVal, chVal, payloadPtr)
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
	payloadVal, payloadTy, err := fe.emitChannelPayloadValue(payloadType, payloadStorageTy, payloadPtr)
	if err != nil {
		return err
	}
	somePtr, err := fe.emitTagValueSinglePayload(dstType, someIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(ins.ChanRecv.Dst)
	if err != nil {
		return err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, somePtr, ptr, dstAlign)
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
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	fe.emitValueStore(dstTy, nonePtr, ptr, dstAlign)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", ins.ChanRecv.ReadyBB)

	fe.blockTerminated = true
	return nil
}
