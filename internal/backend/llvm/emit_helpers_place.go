package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitPlacePtr(place mir.Place) (ptr, ty string, err error) {
	if fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	var curPtr string
	var curType types.TypeID
	curIsValue := false
	curStorageLocal := mir.NoLocalID
	switch place.Kind {
	case mir.PlaceLocal:
		name, ok := fe.localAlloca[place.Local]
		if !ok {
			return "", "", fmt.Errorf("unknown local %d", place.Local)
		}
		curPtr = fmt.Sprintf("%%%s", name)
		curType = fe.f.Locals[place.Local].Type
		curStorageLocal = place.Local
	case mir.PlaceGlobal:
		name := fe.emitter.globalNames[place.Global]
		if name == "" {
			return "", "", fmt.Errorf("unknown global %d", place.Global)
		}
		curPtr = fmt.Sprintf("@%s", name)
		curType = fe.emitter.mod.Globals[place.Global].Type
	default:
		return "", "", fmt.Errorf("unsupported place kind %v", place.Kind)
	}
	curLLVMType, err := llvmValueType(fe.emitter.types, curType)
	if err != nil {
		return "", "", err
	}

	for i, proj := range place.Proj {
		switch proj.Kind {
		case mir.PlaceProjDeref:
			if curLLVMType != "ptr" {
				return "", "", fmt.Errorf("deref requires pointer type, got %s (%s)", curLLVMType, types.Label(fe.emitter.types, curType))
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", tmp, curPtr)
			nextType, nextPlace, ok := fe.derefStorageType(curStorageLocal, curType)
			if !ok {
				return "", "", fmt.Errorf("unsupported place deref type %s (id=%d)", types.Label(fe.emitter.types, curType), curType)
			}
			curPtr = tmp
			curType = nextType
			curStorageLocal = storageLocal(nextPlace)
			curLLVMType, err = llvmValueType(fe.emitter.types, curType)
			if err != nil {
				return "", "", err
			}
			curIsValue = false
			if i+1 < len(place.Proj) && !isRefType(fe.emitter.types, curType) && isHandleValueType(fe.emitter.types, resolveValueType(fe.emitter.types, curType)) {
				next := place.Proj[i+1].Kind
				if next == mir.PlaceProjField {
					tmpVal := fe.nextTemp()
					fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", tmpVal, curPtr)
					curPtr = tmpVal
					curIsValue = true
					curStorageLocal = mir.NoLocalID
				}
			}
		case mir.PlaceProjField:
			for isRefType(fe.emitter.types, curType) {
				nextType, nextPlace, ok := fe.derefStorageType(curStorageLocal, curType)
				if !ok {
					return "", "", fmt.Errorf("unsupported field reference type %s (id=%d)", types.Label(fe.emitter.types, curType), curType)
				}
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", tmp, curPtr)
				curPtr = tmp
				curType = nextType
				curStorageLocal = storageLocal(nextPlace)
				curLLVMType, err = llvmValueType(fe.emitter.types, curType)
				if err != nil {
					return "", "", err
				}
				curIsValue = false
			}
			fieldBaseType := resolveValueType(fe.emitter.types, curType)
			fieldIdx, fieldType, err := fe.structFieldInfo(fieldBaseType, proj)
			if err != nil {
				return "", "", err
			}
			layoutInfo, err := fe.emitter.layoutOf(fieldBaseType)
			if err != nil {
				return "", "", err
			}
			if fieldIdx < 0 || fieldIdx >= len(layoutInfo.FieldOffsets) {
				return "", "", fmt.Errorf("field index %d out of range", fieldIdx)
			}
			off := layoutInfo.FieldOffsets[fieldIdx]
			base := curPtr
			if !curIsValue {
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, curLLVMType, curPtr)
				base = tmp
			}
			bytePtr := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, base, off)
			fieldLLVMType, err := llvmValueType(fe.emitter.types, fieldType)
			if err != nil {
				return "", "", err
			}
			curPtr = bytePtr
			curType = fieldType
			curLLVMType = fieldLLVMType
			curIsValue = false
			curStorageLocal = mir.NoLocalID
		case mir.PlaceProjIndex:
			if proj.IndexLocal == mir.NoLocalID {
				return "", "", fmt.Errorf("missing index local")
			}
			elemType, dynamic, ok := arrayElemType(fe.emitter.types, curType)
			if !ok {
				return "", "", fmt.Errorf("index projection on non-array type")
			}
			idxLocal := proj.IndexLocal
			if int(idxLocal) < 0 || int(idxLocal) >= len(fe.f.Locals) {
				return "", "", fmt.Errorf("invalid index local %d", idxLocal)
			}
			idxLLVM, err := llvmValueType(fe.emitter.types, fe.f.Locals[idxLocal].Type)
			if err != nil {
				return "", "", err
			}
			idxPtr := fmt.Sprintf("%%%s", fe.localAlloca[idxLocal])
			idxVal := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", idxVal, idxLLVM, idxPtr)
			handlePtr := curPtr
			if curIsValue {
				handlePtr = fe.emitHandleAddr(curPtr)
			}
			if dynamic {
				elemPtr, elemLLVM, err := fe.emitArrayElemPtr(handlePtr, idxVal, idxLLVM, fe.f.Locals[idxLocal].Type, elemType)
				if err != nil {
					return "", "", err
				}
				curPtr = elemPtr
				curType = elemType
				curLLVMType = elemLLVM
			} else {
				fixedElem, fixedLen, ok := arrayFixedInfo(fe.emitter.types, curType)
				if !ok {
					return "", "", fmt.Errorf("index projection on non-array type")
				}
				elemPtr, elemLLVM, err := fe.emitArrayFixedElemPtr(handlePtr, idxVal, idxLLVM, fe.f.Locals[idxLocal].Type, fixedElem, fixedLen)
				if err != nil {
					return "", "", err
				}
				curPtr = elemPtr
				curType = fixedElem
				curLLVMType = elemLLVM
			}
			curIsValue = false
			curStorageLocal = mir.NoLocalID
		default:
			return "", "", fmt.Errorf("unsupported place projection kind %v", proj.Kind)
		}
	}

	return curPtr, curLLVMType, nil
}

func (fe *funcEmitter) derefStorageType(local mir.LocalID, curType types.TypeID) (types.TypeID, *mir.Place, bool) {
	return fe.derefStorageTypeWithTargets(local, curType, fe.addrOfTargets)
}

func (fe *funcEmitter) derefStorageTypeWithTargets(local mir.LocalID, curType types.TypeID, targets map[mir.LocalID]addrOfTarget) (types.TypeID, *mir.Place, bool) {
	if local != mir.NoLocalID && targets != nil {
		if target, ok := targets[local]; ok && target.ty != types.NoTypeID {
			return target.ty, &target.place, true
		}
	}
	next, ok := derefType(fe.emitter.types, curType)
	return next, nil, ok
}

func storageLocal(place *mir.Place) mir.LocalID {
	if place == nil || place.Kind != mir.PlaceLocal || len(place.Proj) != 0 {
		return mir.NoLocalID
	}
	return place.Local
}

type placeProjectionType struct {
	ty           types.TypeID
	storageLocal mir.LocalID
}

func (fe *funcEmitter) collectAddrOfTargets() map[mir.LocalID]addrOfTarget {
	if fe == nil || fe.f == nil {
		return nil
	}
	targets := make(map[mir.LocalID]addrOfTarget)
	conflicts := make(map[mir.LocalID]struct{})
	for bi := range fe.f.Blocks {
		for ii := range fe.f.Blocks[bi].Instrs {
			ins := &fe.f.Blocks[bi].Instrs[ii]
			if ins.Kind != mir.InstrAssign || ins.Assign.Dst.Kind != mir.PlaceLocal || len(ins.Assign.Dst.Proj) != 0 {
				continue
			}
			local := ins.Assign.Dst.Local
			if ins.Assign.Src.Kind != mir.RValueUse {
				conflicts[local] = struct{}{}
				continue
			}
			use := ins.Assign.Src.Use
			if use.Kind != mir.OperandAddrOf && use.Kind != mir.OperandAddrOfMut {
				conflicts[local] = struct{}{}
				continue
			}
			ty, err := fe.projectedPlaceTypeWithTargets(use.Place, targets)
			if err != nil || ty == types.NoTypeID {
				conflicts[local] = struct{}{}
				continue
			}
			next := addrOfTarget{place: use.Place, ty: ty}
			if existing, ok := targets[local]; ok && !sameAddrOfTarget(existing, next) {
				conflicts[local] = struct{}{}
				continue
			}
			targets[local] = next
		}
	}
	for local := range conflicts {
		delete(targets, local)
	}
	if len(targets) == 0 {
		return nil
	}
	return targets
}

func sameAddrOfTarget(a, b addrOfTarget) bool {
	if a.ty != b.ty || a.place.Kind != b.place.Kind || a.place.Local != b.place.Local || a.place.Global != b.place.Global {
		return false
	}
	if len(a.place.Proj) != len(b.place.Proj) {
		return false
	}
	for i := range a.place.Proj {
		if a.place.Proj[i] != b.place.Proj[i] {
			return false
		}
	}
	return true
}

func (fe *funcEmitter) projectedPlaceTypeWithTargets(place mir.Place, targets map[mir.LocalID]addrOfTarget) (types.TypeID, error) {
	base := place
	base.Proj = nil
	cur, err := fe.placeBaseType(base)
	if err != nil {
		return types.NoTypeID, err
	}
	state := placeProjectionType{ty: cur, storageLocal: storageLocal(&base)}
	for _, proj := range place.Proj {
		next, err := fe.projectType(state, proj, targets)
		if err != nil {
			return types.NoTypeID, err
		}
		state = next
	}
	return state.ty, nil
}

func (fe *funcEmitter) projectType(cur placeProjectionType, proj mir.PlaceProj, targets map[mir.LocalID]addrOfTarget) (placeProjectionType, error) {
	switch proj.Kind {
	case mir.PlaceProjDeref:
		next, nextPlace, ok := fe.derefStorageTypeWithTargets(cur.storageLocal, cur.ty, targets)
		if !ok {
			return placeProjectionType{}, fmt.Errorf("unsupported place deref type %s (id=%d)", types.Label(fe.emitter.types, cur.ty), cur.ty)
		}
		return placeProjectionType{ty: next, storageLocal: storageLocal(nextPlace)}, nil
	case mir.PlaceProjField:
		for isRefType(fe.emitter.types, cur.ty) {
			next, nextPlace, ok := fe.derefStorageTypeWithTargets(cur.storageLocal, cur.ty, targets)
			if !ok {
				return placeProjectionType{}, fmt.Errorf("unsupported field reference type %s (id=%d)", types.Label(fe.emitter.types, cur.ty), cur.ty)
			}
			cur = placeProjectionType{ty: next, storageLocal: storageLocal(nextPlace)}
		}
		_, fieldType, err := fe.structFieldInfo(resolveValueType(fe.emitter.types, cur.ty), proj)
		return placeProjectionType{ty: fieldType, storageLocal: mir.NoLocalID}, err
	case mir.PlaceProjIndex:
		elemType, _, ok := arrayElemType(fe.emitter.types, cur.ty)
		if !ok {
			return placeProjectionType{}, fmt.Errorf("index projection on non-array type")
		}
		return placeProjectionType{ty: elemType, storageLocal: mir.NoLocalID}, nil
	default:
		return placeProjectionType{}, fmt.Errorf("unsupported place projection kind %v", proj.Kind)
	}
}

func (fe *funcEmitter) placeBaseType(place mir.Place) (types.TypeID, error) {
	if fe == nil || fe.f == nil || fe.emitter == nil || fe.emitter.mod == nil {
		return types.NoTypeID, fmt.Errorf("missing context")
	}
	if len(place.Proj) != 0 {
		return types.NoTypeID, fmt.Errorf("unsupported projected destination")
	}
	switch place.Kind {
	case mir.PlaceLocal:
		if int(place.Local) < 0 || int(place.Local) >= len(fe.f.Locals) {
			return types.NoTypeID, fmt.Errorf("invalid local %d", place.Local)
		}
		return fe.f.Locals[place.Local].Type, nil
	case mir.PlaceGlobal:
		if int(place.Global) < 0 || int(place.Global) >= len(fe.emitter.mod.Globals) {
			return types.NoTypeID, fmt.Errorf("invalid global %d", place.Global)
		}
		return fe.emitter.mod.Globals[place.Global].Type, nil
	default:
		return types.NoTypeID, fmt.Errorf("unsupported place kind %v", place.Kind)
	}
}

func (fe *funcEmitter) structFieldInfo(typeID types.TypeID, proj mir.PlaceProj) (int, types.TypeID, error) {
	if fe.emitter.types == nil {
		return -1, types.NoTypeID, fmt.Errorf("missing type interner")
	}
	typeID = resolveAliasAndOwn(fe.emitter.types, typeID)
	info, ok := fe.emitter.types.StructInfo(typeID)
	if ok && info != nil {
		fieldIdx := proj.FieldIdx
		if proj.FieldName != "" && fe.emitter.types.Strings != nil {
			found := false
			for i, field := range info.Fields {
				if fe.emitter.types.Strings.MustLookup(field.Name) == proj.FieldName {
					fieldIdx = i
					found = true
					break
				}
			}
			if !found {
				return -1, types.NoTypeID, fmt.Errorf("unknown field %q", proj.FieldName)
			}
		}
		if fieldIdx < 0 || fieldIdx >= len(info.Fields) {
			return -1, types.NoTypeID, fmt.Errorf("unknown field %q", proj.FieldName)
		}
		return fieldIdx, info.Fields[fieldIdx].Type, nil
	}
	tupleInfo, ok := fe.emitter.types.TupleInfo(typeID)
	if !ok || tupleInfo == nil {
		kind := "unknown"
		if tt, okLookup := fe.emitter.types.Lookup(typeID); okLookup {
			kind = tt.Kind.String()
		}
		return -1, types.NoTypeID, fmt.Errorf("missing struct info for type#%d (kind=%s)", typeID, kind)
	}
	fieldIdx := proj.FieldIdx
	if fieldIdx < 0 || fieldIdx >= len(tupleInfo.Elems) {
		return -1, types.NoTypeID, fmt.Errorf("unknown field %q", proj.FieldName)
	}
	return fieldIdx, tupleInfo.Elems[fieldIdx], nil
}

func (fe *funcEmitter) bytesViewOffsets(typeID types.TypeID) (ptrOffset, lenOffset int, lenLLVM string, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return 0, 0, "", fmt.Errorf("missing type interner")
	}
	typeID = resolveValueType(fe.emitter.types, typeID)
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return 0, 0, "", err
	}
	ptrIdx, ptrType, err := fe.structFieldInfo(typeID, mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: "ptr", FieldIdx: -1})
	if err != nil {
		return 0, 0, "", err
	}
	lenIdx, lenType, err := fe.structFieldInfo(typeID, mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: "len", FieldIdx: -1})
	if err != nil {
		return 0, 0, "", err
	}
	if ptrIdx < 0 || ptrIdx >= len(layoutInfo.FieldOffsets) || lenIdx < 0 || lenIdx >= len(layoutInfo.FieldOffsets) {
		return 0, 0, "", fmt.Errorf("bytes view layout mismatch")
	}
	lenLLVM, err = llvmValueType(fe.emitter.types, lenType)
	if err != nil {
		return 0, 0, "", err
	}
	if _, err = llvmValueType(fe.emitter.types, ptrType); err != nil {
		return 0, 0, "", err
	}
	return layoutInfo.FieldOffsets[ptrIdx], layoutInfo.FieldOffsets[lenIdx], lenLLVM, nil
}
