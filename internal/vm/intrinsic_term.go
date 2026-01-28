package vm

import (
	"fmt"
	"os"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

func (vm *VM) handleTermEnterAltScreen(_ *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "term_enter_alt_screen requires 0 arguments")
	}
	if tr, ok := vm.RT.(TermRuntime); ok && tr != nil {
		tr.TermEnterAltScreen()
	}
	return nil
}

func (vm *VM) handleTermExitAltScreen(_ *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "term_exit_alt_screen requires 0 arguments")
	}
	if tr, ok := vm.RT.(TermRuntime); ok && tr != nil {
		tr.TermExitAltScreen()
	}
	return nil
}

func (vm *VM) handleTermSetRawMode(frame *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "term_set_raw_mode requires 1 argument")
	}
	val, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(val)
	if val.Kind != VKBool {
		return vm.eb.typeMismatch("bool", val.Kind.String())
	}
	if tr, ok := vm.RT.(TermRuntime); ok && tr != nil {
		tr.TermSetRawMode(val.Bool)
	}
	return nil
}

func (vm *VM) handleTermHideCursor(_ *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "term_hide_cursor requires 0 arguments")
	}
	if tr, ok := vm.RT.(TermRuntime); ok && tr != nil {
		tr.TermHideCursor()
	}
	return nil
}

func (vm *VM) handleTermShowCursor(_ *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "term_show_cursor requires 0 arguments")
	}
	if tr, ok := vm.RT.(TermRuntime); ok && tr != nil {
		tr.TermShowCursor()
	}
	return nil
}

func (vm *VM) handleTermSize(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "term_size requires 0 arguments")
	}
	cols, rows := defaultTermSize()
	if tr, ok := vm.RT.(TermRuntime); ok && tr != nil {
		cols, rows = tr.TermSize()
	}
	if !call.HasDst {
		return nil
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	val, vmErr := vm.makeTermSizeTuple(dstType, cols, rows)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.dropValue(val)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	}
	return nil
}

func (vm *VM) handleTermWrite(frame *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "term_write requires 1 argument")
	}
	val, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(val)
	data, vmErr := vm.bytesFromArrayValue(val)
	if vmErr != nil {
		return vmErr
	}
	if tr, ok := vm.RT.(TermRuntime); ok && tr != nil {
		tr.TermWrite(data)
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	if _, err := os.Stdout.Write(data); err != nil {
		_ = err
	}
	return nil
}

func (vm *VM) handleTermFlush(_ *Frame, call *mir.CallInstr) *VMError {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "term_flush requires 0 arguments")
	}
	if tr, ok := vm.RT.(TermRuntime); ok && tr != nil {
		tr.TermFlush()
	}
	return nil
}

func (vm *VM) handleTermReadEvent(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "term_read_event requires 0 arguments")
	}
	ev := TermEventData{Kind: TermEventEOF}
	if tr, ok := vm.RT.(TermRuntime); ok && tr != nil {
		ev = tr.TermReadEvent()
	}
	if !call.HasDst {
		return nil
	}
	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	val, vmErr := vm.termEventValue(dstType, ev)
	if vmErr != nil {
		return vmErr
	}
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.dropValue(val)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	}
	return nil
}

func (vm *VM) bytesFromArrayValue(val Value) ([]byte, *VMError) {
	if val.Kind == VKRef || val.Kind == VKRefMut {
		loaded, vmErr := vm.loadLocationRaw(val.Loc)
		if vmErr != nil {
			return nil, vmErr
		}
		val = loaded
	}
	if val.Kind != VKHandleArray {
		return nil, vm.eb.typeMismatch("byte[]", val.Kind.String())
	}
	view, vmErr := vm.arrayViewFromHandle(val.H)
	if vmErr != nil {
		return nil, vmErr
	}
	out := make([]byte, view.length)
	base := view.start
	for i := range out {
		b, vmErr := vm.valueToUint8(view.baseObj.Arr[base+i])
		if vmErr != nil {
			return nil, vmErr
		}
		out[i] = b
	}
	return out, nil
}

func (vm *VM) makeTermSizeTuple(typeID types.TypeID, cols, rows int) (Value, *VMError) {
	elemTypes := []types.TypeID{types.NoTypeID, types.NoTypeID}
	if vm != nil && vm.Types != nil && typeID != types.NoTypeID {
		if info, ok := vm.Types.TupleInfo(vm.valueType(typeID)); ok && info != nil {
			if len(info.Elems) >= len(elemTypes) {
				copy(elemTypes, info.Elems[:len(elemTypes)])
			}
		}
	}
	colVal, vmErr := vm.makeIntForType(elemTypes[0], int64(cols))
	if vmErr != nil {
		return Value{}, vmErr
	}
	rowVal, vmErr := vm.makeIntForType(elemTypes[1], int64(rows))
	if vmErr != nil {
		vm.dropValue(colVal)
		return Value{}, vmErr
	}
	fields := []Value{colVal, rowVal}
	h := vm.Heap.AllocStruct(typeID, fields)
	return MakeHandleStruct(h, typeID), nil
}

func (vm *VM) termEventValue(typeID types.TypeID, ev TermEventData) (Value, *VMError) {
	layout, vmErr := vm.tagLayoutFor(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	switch ev.Kind {
	case TermEventKey:
		tc, ok := layout.CaseByName("Key")
		if !ok {
			return Value{}, vm.eb.unknownTagLayout(fmt.Sprintf("unknown tag %q in type#%d layout", "Key", layout.TypeID))
		}
		if len(tc.PayloadTypes) != 1 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "term Key expects 1 payload value")
		}
		keyEventVal, vmErr := vm.termKeyEventValue(tc.PayloadTypes[0], ev.Key)
		if vmErr != nil {
			return Value{}, vmErr
		}
		h := vm.Heap.AllocTag(typeID, tc.TagSym, []Value{keyEventVal})
		return MakeHandleTag(h, typeID), nil
	case TermEventResize:
		tc, ok := layout.CaseByName("Resize")
		if !ok {
			return Value{}, vm.eb.unknownTagLayout(fmt.Sprintf("unknown tag %q in type#%d layout", "Resize", layout.TypeID))
		}
		if len(tc.PayloadTypes) != 2 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "term Resize expects 2 payload values")
		}
		colsVal, vmErr := vm.makeIntForType(tc.PayloadTypes[0], int64(ev.Cols))
		if vmErr != nil {
			return Value{}, vmErr
		}
		rowsVal, vmErr := vm.makeIntForType(tc.PayloadTypes[1], int64(ev.Rows))
		if vmErr != nil {
			vm.dropValue(colsVal)
			return Value{}, vmErr
		}
		h := vm.Heap.AllocTag(typeID, tc.TagSym, []Value{colsVal, rowsVal})
		return MakeHandleTag(h, typeID), nil
	case TermEventEOF:
		tc, ok := layout.CaseByName("Eof")
		if !ok {
			return Value{}, vm.eb.unknownTagLayout(fmt.Sprintf("unknown tag %q in type#%d layout", "Eof", layout.TypeID))
		}
		if len(tc.PayloadTypes) != 0 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "term Eof expects 0 payload values")
		}
		h := vm.Heap.AllocTag(typeID, tc.TagSym, nil)
		return MakeHandleTag(h, typeID), nil
	default:
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "term event kind not supported")
	}
}

func (vm *VM) termKeyEventValue(typeID types.TypeID, ev TermKeyEventData) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	keyIdx, okKey := layout.IndexByName["key"]
	modsIdx, okMods := layout.IndexByName["mods"]
	if !okKey || !okMods {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "KeyEvent layout mismatch")
	}
	fields := make([]Value, len(layout.FieldTypes))
	keyVal, vmErr := vm.termKeyValue(layout.FieldTypes[keyIdx], ev.Key)
	if vmErr != nil {
		return Value{}, vmErr
	}
	modsVal, vmErr := vm.makeIntForType(layout.FieldTypes[modsIdx], int64(ev.Mods))
	if vmErr != nil {
		vm.dropValue(keyVal)
		return Value{}, vmErr
	}
	fields[keyIdx] = keyVal
	fields[modsIdx] = modsVal
	h := vm.Heap.AllocStruct(typeID, fields)
	return MakeHandleStruct(h, typeID), nil
}

func (vm *VM) termKeyValue(typeID types.TypeID, key TermKeyData) (Value, *VMError) {
	layout, vmErr := vm.tagLayoutFor(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	tagName, ok := termKeyTagName(key.Kind)
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "term key kind not supported")
	}
	tc, ok := layout.CaseByName(tagName)
	if !ok {
		return Value{}, vm.eb.unknownTagLayout(fmt.Sprintf("unknown tag %q in type#%d layout", tagName, layout.TypeID))
	}
	switch key.Kind {
	case TermKeyChar:
		if len(tc.PayloadTypes) != 1 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "term Char expects 1 payload value")
		}
		payload, vmErr := vm.makeIntForType(tc.PayloadTypes[0], int64(key.Char))
		if vmErr != nil {
			return Value{}, vmErr
		}
		h := vm.Heap.AllocTag(typeID, tc.TagSym, []Value{payload})
		return MakeHandleTag(h, typeID), nil
	case TermKeyF:
		if len(tc.PayloadTypes) != 1 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "term F expects 1 payload value")
		}
		payload, vmErr := vm.makeIntForType(tc.PayloadTypes[0], int64(key.F))
		if vmErr != nil {
			return Value{}, vmErr
		}
		h := vm.Heap.AllocTag(typeID, tc.TagSym, []Value{payload})
		return MakeHandleTag(h, typeID), nil
	default:
		if len(tc.PayloadTypes) != 0 {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "term key expects 0 payload values")
		}
		h := vm.Heap.AllocTag(typeID, tc.TagSym, nil)
		return MakeHandleTag(h, typeID), nil
	}
}

func (vm *VM) makeIntForType(typeID types.TypeID, n int64) (Value, *VMError) {
	if typeID == types.NoTypeID || vm.Types == nil {
		return MakeInt(n, typeID), nil
	}
	tt, ok := vm.Types.Lookup(vm.valueType(typeID))
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("unknown type#%d", typeID))
	}
	switch tt.Kind {
	case types.KindInt:
		if tt.Width == types.WidthAny {
			return vm.makeBigInt(typeID, bignum.IntFromInt64(n)), nil
		}
		return MakeInt(n, typeID), nil
	case types.KindUint:
		if n < 0 {
			return Value{}, vm.eb.invalidNumericConversion("negative value for uint")
		}
		if tt.Width == types.WidthAny {
			return vm.makeBigUint(typeID, bignum.UintFromUint64(uint64(n))), nil
		}
		return MakeInt(n, typeID), nil
	default:
		return Value{}, vm.eb.typeMismatch("int", tt.Kind.String())
	}
}

func termKeyTagName(kind TermKeyKind) (string, bool) {
	switch kind {
	case TermKeyChar:
		return "Char", true
	case TermKeyEnter:
		return "Enter", true
	case TermKeyEsc:
		return "Esc", true
	case TermKeyBackspace:
		return "Backspace", true
	case TermKeyTab:
		return "Tab", true
	case TermKeyUp:
		return "Up", true
	case TermKeyDown:
		return "Down", true
	case TermKeyLeft:
		return "Left", true
	case TermKeyRight:
		return "Right", true
	case TermKeyHome:
		return "Home", true
	case TermKeyEnd:
		return "End", true
	case TermKeyPageUp:
		return "PageUp", true
	case TermKeyPageDown:
		return "PageDown", true
	case TermKeyDelete:
		return "Delete", true
	case TermKeyF:
		return "F", true
	default:
		return "", false
	}
}
