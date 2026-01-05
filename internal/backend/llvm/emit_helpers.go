package llvm

import (
	"fmt"
	"strings"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitPlacePtr(place mir.Place) (ptr, ty string, err error) {
	if fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	var curPtr string
	var curType types.TypeID
	switch place.Kind {
	case mir.PlaceLocal:
		name, ok := fe.localAlloca[place.Local]
		if !ok {
			return "", "", fmt.Errorf("unknown local %d", place.Local)
		}
		curPtr = fmt.Sprintf("%%%s", name)
		curType = fe.f.Locals[place.Local].Type
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

	for _, proj := range place.Proj {
		switch proj.Kind {
		case mir.PlaceProjDeref:
			if curLLVMType != "ptr" {
				return "", "", fmt.Errorf("deref requires pointer type, got %s", curLLVMType)
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", tmp, curPtr)
			nextType, ok := derefType(fe.emitter.types, curType)
			if !ok {
				return "", "", fmt.Errorf("unsupported deref type")
			}
			curPtr = tmp
			curType = nextType
			curLLVMType, err = llvmValueType(fe.emitter.types, curType)
			if err != nil {
				return "", "", err
			}
		case mir.PlaceProjField:
			fieldIdx, fieldType, err := fe.structFieldInfo(curType, proj)
			if err != nil {
				return "", "", err
			}
			layoutInfo, err := fe.emitter.layoutOf(curType)
			if err != nil {
				return "", "", err
			}
			if fieldIdx < 0 || fieldIdx >= len(layoutInfo.FieldOffsets) {
				return "", "", fmt.Errorf("field index %d out of range", fieldIdx)
			}
			off := layoutInfo.FieldOffsets[fieldIdx]
			base := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", base, curLLVMType, curPtr)
			bytePtr := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, base, off)
			fieldLLVMType, err := llvmValueType(fe.emitter.types, fieldType)
			if err != nil {
				return "", "", err
			}
			curPtr = bytePtr
			curType = fieldType
			curLLVMType = fieldLLVMType
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
			if dynamic {
				elemPtr, elemLLVM, err := fe.emitArrayElemPtr(curPtr, idxVal, idxLLVM, fe.f.Locals[idxLocal].Type, elemType)
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
				elemPtr, elemLLVM, err := fe.emitArrayFixedElemPtr(curPtr, idxVal, idxLLVM, fe.f.Locals[idxLocal].Type, fixedElem, fixedLen)
				if err != nil {
					return "", "", err
				}
				curPtr = elemPtr
				curType = fixedElem
				curLLVMType = elemLLVM
			}
		default:
			return "", "", fmt.Errorf("unsupported place projection kind %v", proj.Kind)
		}
	}

	return curPtr, curLLVMType, nil
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

func (fe *funcEmitter) nextTemp() string {
	fe.tmpID++
	return fmt.Sprintf("%%t%d", fe.tmpID)
}

func (fe *funcEmitter) nextInlineBlock() string {
	fe.inlineBlock++
	return fmt.Sprintf("bb.inline%d", fe.inlineBlock)
}

func boolValue(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

type intMeta struct {
	bits   int
	signed bool
}

func intInfo(typesIn *types.Interner, id types.TypeID) (intMeta, bool) {
	if typesIn == nil {
		return intMeta{}, false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return intMeta{}, false
	}
	switch tt.Kind {
	case types.KindBool:
		return intMeta{bits: 1, signed: false}, true
	case types.KindInt:
		if tt.Width == types.WidthAny {
			return intMeta{}, false
		}
		return intMeta{bits: widthBits(tt.Width), signed: true}, true
	case types.KindUint:
		if tt.Width == types.WidthAny {
			return intMeta{}, false
		}
		return intMeta{bits: widthBits(tt.Width), signed: false}, true
	default:
		return intMeta{}, false
	}
}

func widthBits(width types.Width) int {
	if width == types.WidthAny {
		return 64
	}
	return int(width)
}

func isBigIntType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindInt && tt.Width == types.WidthAny
}

func isBigUintType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindUint && tt.Width == types.WidthAny
}

func isBigFloatType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindFloat && tt.Width == types.WidthAny
}

func formatLLVMBytes(data []byte, arrayLen int) string {
	var sb strings.Builder
	sb.WriteString("c\"")
	for i := range arrayLen {
		b := byte(0)
		if i < len(data) {
			b = data[i]
		}
		fmt.Fprintf(&sb, "\\%02X", b)
	}
	sb.WriteString("\"")
	return sb.String()
}

func decodeStringLiteral(raw string) []byte {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch != '\\' {
			out = append(out, ch)
			continue
		}
		if i+1 >= len(raw) {
			break
		}
		i++
		switch raw[i] {
		case '\\':
			out = append(out, '\\')
		case '"':
			out = append(out, '"')
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		default:
			out = append(out, raw[i])
		}
	}
	return out
}

func (e *Emitter) symFor(symID symbols.SymbolID) *symbols.Symbol {
	if e == nil || e.syms == nil || e.syms.Symbols == nil {
		return nil
	}
	if !symID.IsValid() {
		return nil
	}
	return e.syms.Symbols.Get(symID)
}

func (e *Emitter) layoutOf(id types.TypeID) (layout.TypeLayout, error) {
	if e == nil || e.mod == nil || e.mod.Meta == nil || e.mod.Meta.Layout == nil {
		return layout.TypeLayout{}, fmt.Errorf("missing layout engine")
	}
	return e.mod.Meta.Layout.LayoutOf(id)
}

func (e *Emitter) hasTagLayout(id types.TypeID) bool {
	if e == nil || e.mod == nil || e.mod.Meta == nil || len(e.mod.Meta.TagLayouts) == 0 {
		return false
	}
	id = resolveAliasAndOwn(e.types, id)
	_, ok := e.mod.Meta.TagLayouts[id]
	return ok
}

func (e *Emitter) tagCases(id types.TypeID) ([]mir.TagCaseMeta, error) {
	if e == nil || e.mod == nil || e.mod.Meta == nil || len(e.mod.Meta.TagLayouts) == 0 {
		return nil, fmt.Errorf("missing tag layout metadata")
	}
	id = resolveAliasAndOwn(e.types, id)
	cases, ok := e.mod.Meta.TagLayouts[id]
	if !ok {
		return nil, fmt.Errorf("missing tag layout for type#%d", id)
	}
	return cases, nil
}

func (e *Emitter) canonicalTagSym(sym symbols.SymbolID) symbols.SymbolID {
	if !sym.IsValid() || e == nil || e.mod == nil || e.mod.Meta == nil || len(e.mod.Meta.TagAliases) == 0 {
		return sym
	}
	if orig, ok := e.mod.Meta.TagAliases[sym]; ok && orig.IsValid() {
		return orig
	}
	return sym
}

func (e *Emitter) tagCaseMeta(id types.TypeID, tagName string, tagSym symbols.SymbolID) (int, mir.TagCaseMeta, error) {
	cases, err := e.tagCases(id)
	if err != nil {
		return -1, mir.TagCaseMeta{}, err
	}
	tagSym = e.canonicalTagSym(tagSym)
	if tagSym.IsValid() {
		for i, c := range cases {
			if c.TagSym == tagSym {
				return i, c, nil
			}
		}
	}
	if tagName != "" {
		for i, c := range cases {
			if c.TagName == tagName {
				return i, c, nil
			}
		}
	}
	return -1, mir.TagCaseMeta{}, fmt.Errorf("unknown tag %q", tagName)
}

func (e *Emitter) tagCaseIndex(id types.TypeID, tagName string, tagSym symbols.SymbolID) (int, error) {
	idx, _, err := e.tagCaseMeta(id, tagName, tagSym)
	return idx, err
}

func (e *Emitter) payloadOffsets(payload []types.TypeID) ([]int, error) {
	offsets := make([]int, len(payload))
	size := 0
	for i, t := range payload {
		layoutInfo, err := e.layoutOf(t)
		if err != nil {
			return nil, err
		}
		align := layoutInfo.Align
		if align <= 0 {
			align = 1
		}
		size = roundUpInt(size, align)
		offsets[i] = size
		size += layoutInfo.Size
	}
	return offsets, nil
}

func (fe *funcEmitter) emitTagDiscriminant(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil operand")
	}
	typeID := op.Type
	if typeID == types.NoTypeID && op.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(op.Place); err == nil {
			typeID = baseType
		}
	}
	typeID = resolveValueType(fe.emitter.types, typeID)
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return "", err
	}
	if layoutInfo.TagSize != 4 {
		return "", fmt.Errorf("unsupported tag size %d for type#%d", layoutInfo.TagSize, typeID)
	}
	val, valTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	if isRefType(fe.emitter.types, op.Type) {
		if valTy != "ptr" {
			return "", fmt.Errorf("tag value must be ptr, got %s", valTy)
		}
		deref := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", deref, val)
		val = deref
		valTy = "ptr"
	}
	if valTy != "ptr" {
		return "", fmt.Errorf("tag value must be ptr, got %s", valTy)
	}
	tagVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i32, ptr %s\n", tagVal, val)
	return tagVal, nil
}

func (fe *funcEmitter) emitTagValue(typeID types.TypeID, tagName string, tagSym symbols.SymbolID, args []mir.Operand) (string, error) {
	if typeID == types.NoTypeID {
		return "", fmt.Errorf("missing tag type")
	}
	typeID = resolveValueType(fe.emitter.types, typeID)
	caseIdx, meta, err := fe.emitter.tagCaseMeta(typeID, tagName, tagSym)
	if err != nil {
		return "", err
	}
	if len(args) != len(meta.PayloadTypes) {
		return "", fmt.Errorf("tag %q expects %d payload value(s), got %d", meta.TagName, len(meta.PayloadTypes), len(args))
	}
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return "", err
	}
	if layoutInfo.TagSize != 4 {
		return "", fmt.Errorf("unsupported tag size %d for type#%d", layoutInfo.TagSize, typeID)
	}
	size := layoutInfo.Size
	align := layoutInfo.Align
	if size <= 0 {
		size = 1
	}
	if align <= 0 {
		align = 1
	}
	mem := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", mem, size, align)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s\n", caseIdx, mem)

	if len(meta.PayloadTypes) > 0 {
		offsets, err := fe.emitter.payloadOffsets(meta.PayloadTypes)
		if err != nil {
			return "", err
		}
		for i := range args {
			arg := &args[i]
			val, valTy, err := fe.emitValueOperand(arg)
			if err != nil {
				return "", err
			}
			payloadTy := meta.PayloadTypes[i]
			payloadLLVM, err := llvmValueType(fe.emitter.types, payloadTy)
			if err != nil {
				return "", err
			}
			if valTy != payloadLLVM {
				valType := operandValueType(fe.emitter.types, arg)
				if valType == types.NoTypeID && arg.Kind != mir.OperandConst {
					if baseType, err := fe.placeBaseType(arg.Place); err == nil {
						valType = baseType
					}
				}
				casted, castTy, err := fe.coerceNumericValue(val, valTy, valType, payloadTy)
				if err != nil {
					return "", err
				}
				val = casted
				valTy = castTy
			}
			if valTy != payloadLLVM {
				valTy = payloadLLVM
			}
			off := layoutInfo.PayloadOffset + offsets[i]
			bytePtr := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
			fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, bytePtr)
		}
	}
	return mem, nil
}

func (fe *funcEmitter) structFieldInfo(typeID types.TypeID, proj mir.PlaceProj) (int, types.TypeID, error) {
	if fe.emitter.types == nil {
		return -1, types.NoTypeID, fmt.Errorf("missing type interner")
	}
	typeID = resolveAliasAndOwn(fe.emitter.types, typeID)
	info, ok := fe.emitter.types.StructInfo(typeID)
	if ok && info != nil {
		fieldIdx := proj.FieldIdx
		if fieldIdx < 0 && proj.FieldName != "" && fe.emitter.types.Strings != nil {
			for i, field := range info.Fields {
				if fe.emitter.types.Strings.MustLookup(field.Name) == proj.FieldName {
					fieldIdx = i
					break
				}
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

func resolveValueType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	for i := 0; i < 32 && id != types.NoTypeID; i++ {
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID {
				return id
			}
			id = target
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func isStringLike(typesIn *types.Interner, id types.TypeID) bool {
	id = resolveValueType(typesIn, id)
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindString
}

func isArrayLike(typesIn *types.Interner, id types.TypeID) bool {
	_, dynamic, ok := arrayElemType(typesIn, id)
	return ok && dynamic
}

func arrayFixedInfo(typesIn *types.Interner, id types.TypeID) (elem types.TypeID, length uint32, ok bool) {
	if typesIn == nil || id == types.NoTypeID {
		return types.NoTypeID, 0, false
	}
	id = resolveValueType(typesIn, id)
	if elem, length, ok := typesIn.ArrayFixedInfo(id); ok {
		return elem, length, true
	}
	if tt, ok := typesIn.Lookup(id); ok && tt.Kind == types.KindArray && tt.Count != types.ArrayDynamicLength {
		return tt.Elem, tt.Count, true
	}
	return types.NoTypeID, 0, false
}

func arrayElemType(typesIn *types.Interner, id types.TypeID) (types.TypeID, bool, bool) {
	if typesIn == nil || id == types.NoTypeID {
		return types.NoTypeID, false, false
	}
	id = resolveValueType(typesIn, id)
	if elem, ok := typesIn.ArrayInfo(id); ok {
		return elem, true, true
	}
	if elem, _, ok := typesIn.ArrayFixedInfo(id); ok {
		return elem, false, true
	}
	if tt, ok := typesIn.Lookup(id); ok && tt.Kind == types.KindArray {
		dynamic := tt.Count == types.ArrayDynamicLength
		return tt.Elem, dynamic, true
	}
	return types.NoTypeID, false, false
}

func isBytesViewType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID || typesIn.Strings == nil {
		return false
	}
	id = resolveValueType(typesIn, id)
	info, ok := typesIn.StructInfo(id)
	if !ok || info == nil {
		return false
	}
	return typesIn.Strings.MustLookup(info.Name) == "BytesView"
}

func isRangeType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID || typesIn.Strings == nil {
		return false
	}
	id = resolveValueType(typesIn, id)
	info, ok := typesIn.StructInfo(id)
	if !ok || info == nil {
		return false
	}
	return typesIn.Strings.MustLookup(info.Name) == "Range"
}

func isRefType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindReference
}

func isNothingType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindNothing
}

func (fe *funcEmitter) emitHandleOperandPtr(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil operand")
	}
	if isRefType(fe.emitter.types, op.Type) {
		val, ty, err := fe.emitOperand(op)
		if err != nil {
			return "", err
		}
		if ty != "ptr" {
			return "", fmt.Errorf("expected ptr handle, got %s", ty)
		}
		return val, nil
	}
	return fe.emitOperandAddr(op)
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
	_, err = llvmValueType(fe.emitter.types, ptrType)
	if err != nil {
		return 0, 0, "", err
	}
	return layoutInfo.FieldOffsets[ptrIdx], layoutInfo.FieldOffsets[lenIdx], lenLLVM, nil
}
