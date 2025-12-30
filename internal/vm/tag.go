package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (vm *VM) tagLayoutFor(typeID types.TypeID) (*TagLayout, *VMError) {
	if vm == nil {
		return nil, nil
	}
	if vm.tagLayouts == nil {
		return nil, vm.eb.unknownTagLayout("no tag layout provider")
	}
	typeID = vm.valueType(typeID)
	if typeID == types.NoTypeID {
		return nil, vm.eb.unknownTagLayout("invalid tag type")
	}
	layout, ok := vm.tagLayouts.Layout(typeID)
	if !ok || layout == nil {
		return nil, vm.eb.unknownTagLayout(fmt.Sprintf("missing tag layout for type#%d", typeID))
	}
	return layout, nil
}

func (vm *VM) evalTagTest(frame *Frame, tt *mir.TagTest) (Value, *VMError) {
	if tt == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil tag_test")
	}
	val, vmErr := vm.evalOperand(frame, &tt.Value)
	if vmErr != nil {
		return Value{}, vmErr
	}
	defer vm.dropValue(val)
	if val.Kind != VKHandleTag {
		return MakeBool(false, types.NoTypeID), nil
	}
	layout, vmErr := vm.tagLayoutFor(val.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	want, ok := layout.CaseByName(tt.TagName)
	if !ok {
		return MakeBool(false, types.NoTypeID), nil
	}
	obj := vm.Heap.Get(val.H)
	if obj.Kind != OKTag {
		return Value{}, vm.eb.typeMismatch("tag", fmt.Sprintf("%v", obj.Kind))
	}
	return MakeBool(obj.Tag.TagSym == want.TagSym, types.NoTypeID), nil
}

func (vm *VM) evalTagPayload(frame *Frame, tp *mir.TagPayload) (Value, *VMError) {
	if tp == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil tag_payload")
	}
	val, vmErr := vm.evalOperand(frame, &tp.Value)
	if vmErr != nil {
		return Value{}, vmErr
	}
	defer vm.dropValue(val)
	if val.Kind != VKHandleTag {
		return Value{}, vm.eb.tagPayloadOnNonTag(val.Kind.String())
	}
	layout, vmErr := vm.tagLayoutFor(val.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	want, ok := layout.CaseByName(tp.TagName)
	if !ok {
		obj := vm.Heap.Get(val.H)
		if obj != nil && obj.Kind == OKTag {
			if gotName, ok := vm.tagNameForSym(layout, obj.Tag.TagSym); ok {
				if gotName != tp.TagName {
					return Value{}, vm.eb.tagPayloadTagMismatch(tp.TagName, gotName)
				}
				if tp.Index < 0 || tp.Index >= len(obj.Tag.Fields) {
					return Value{}, vm.eb.tagPayloadIndexOutOfRange(tp.Index, len(obj.Tag.Fields))
				}
				field, cloneErr := vm.cloneForShare(obj.Tag.Fields[tp.Index])
				if cloneErr != nil {
					return Value{}, cloneErr
				}
				return field, nil
			}
		}
		return Value{}, vm.eb.unknownTagLayout(fmt.Sprintf("unknown tag %q in type#%d layout", tp.TagName, layout.TypeID))
	}
	obj := vm.Heap.Get(val.H)
	if obj.Kind != OKTag {
		return Value{}, vm.eb.typeMismatch("tag", fmt.Sprintf("%v", obj.Kind))
	}
	if obj.Tag.TagSym != want.TagSym {
		gotName, ok := vm.tagNameForSym(layout, obj.Tag.TagSym)
		if !ok || gotName != tp.TagName {
			return Value{}, vm.eb.tagPayloadTagMismatch(tp.TagName, gotName)
		}
	}
	if tp.Index < 0 || tp.Index >= len(want.PayloadTypes) {
		return Value{}, vm.eb.tagPayloadIndexOutOfRange(tp.Index, len(want.PayloadTypes))
	}
	if tp.Index >= len(obj.Tag.Fields) {
		return Value{}, vm.eb.tagPayloadIndexOutOfRange(tp.Index, len(obj.Tag.Fields))
	}
	field := obj.Tag.Fields[tp.Index]
	wantTy := want.PayloadTypes[tp.Index]
	if wantTy != types.NoTypeID && field.TypeID != types.NoTypeID {
		if vm.valueType(wantTy) != vm.valueType(field.TypeID) {
			return Value{}, vm.eb.typeMismatch(fmt.Sprintf("type#%d", wantTy), fmt.Sprintf("type#%d", field.TypeID))
		}
	}
	out, vmErr := vm.cloneForShare(field)
	if vmErr != nil {
		return Value{}, vmErr
	}
	return out, nil
}

func (vm *VM) execSwitchTag(frame *Frame, st *mir.SwitchTagTerm) *VMError {
	if st == nil {
		return vm.eb.makeError(PanicUnimplemented, "nil switch_tag terminator")
	}
	val, vmErr := vm.evalOperand(frame, &st.Value)
	if vmErr != nil {
		return vmErr
	}
	defer vm.dropValue(val)
	if val.Kind != VKHandleTag {
		return vm.eb.switchTagOnNonTag(val.Kind.String())
	}
	layout, vmErr := vm.tagLayoutFor(val.TypeID)
	if vmErr != nil {
		return vmErr
	}
	obj := vm.Heap.Get(val.H)
	if obj.Kind != OKTag {
		return vm.eb.switchTagOnNonTag(fmt.Sprintf("%v", obj.Kind))
	}

	target := st.Default
	decision := "default"
	for _, c := range st.Cases {
		tagCase, ok := layout.CaseByName(c.TagName)
		if !ok {
			continue
		}
		if obj.Tag.TagSym == tagCase.TagSym {
			target = c.Target
			decision = c.TagName
			break
		}
		if gotName, ok := vm.tagNameForSym(layout, obj.Tag.TagSym); ok && gotName == c.TagName {
			target = c.Target
			decision = c.TagName
			break
		}
	}

	if target == mir.NoBlockID {
		return vm.eb.switchTagMissingDefault()
	}
	if target < 0 || int(target) >= len(frame.Func.Blocks) {
		return vm.eb.switchTagMissingDefault()
	}

	if vm.Trace != nil {
		vm.Trace.TraceSwitchTagDecision(decision, target)
	}

	frame.BB = target
	frame.IP = 0
	return nil
}

func (vm *VM) callTagConstructor(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) (bool, *VMError) {
	if vm == nil || vm.tagLayouts == nil || call == nil {
		return false, nil
	}
	if call.Callee.Kind != mir.CalleeSym || !call.Callee.Sym.IsValid() {
		return false, nil
	}
	if !vm.tagLayouts.KnownTagSym(call.Callee.Sym) {
		return false, nil
	}

	args := make([]Value, len(call.Args))
	for i := range call.Args {
		val, vmErr := vm.evalOperand(frame, &call.Args[i])
		if vmErr != nil {
			return true, vmErr
		}
		args[i] = val
	}

	typeID := types.NoTypeID
	if call.HasDst && call.Dst.IsValid() {
		typeID = frame.Locals[call.Dst.Local].TypeID
	}
	tagSym := call.Callee.Sym
	if vm.tagLayouts != nil {
		tagSym = vm.tagLayouts.CanonicalTagSym(tagSym)
	}
	if typeID != types.NoTypeID {
		layout, vmErr := vm.tagLayoutFor(typeID)
		if vmErr != nil {
			return true, vmErr
		}
		if tc, ok := layout.CaseBySym(tagSym); ok {
			if len(tc.PayloadTypes) != len(args) {
				return true, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("tag %q expects %d payload value(s), got %d", tc.TagName, len(tc.PayloadTypes), len(args)))
			}
		}
	}

	h := vm.Heap.AllocTag(typeID, tagSym, args)
	tagVal := MakeHandleTag(h, typeID)
	if call.HasDst {
		localID := call.Dst.Local
		if vmErr := vm.writeLocal(frame, localID, tagVal); vmErr != nil {
			return true, vmErr
		}
		if writes != nil {
			*writes = append(*writes, LocalWrite{
				LocalID: localID,
				Name:    frame.Locals[localID].Name,
				Value:   tagVal,
			})
		}
		return true, nil
	}

	// Tag value is unused; drop it to consume moved arguments deterministically.
	vm.Heap.Release(h)
	return true, nil
}

func (vm *VM) tagNameForSym(layout *TagLayout, sym symbols.SymbolID) (string, bool) {
	if layout == nil {
		return "", false
	}
	if tc, ok := layout.CaseBySym(sym); ok && tc.TagName != "" {
		return tc.TagName, true
	}
	if vm != nil && vm.tagLayouts != nil {
		return vm.tagLayouts.AnyTagName(sym)
	}
	return "", false
}
