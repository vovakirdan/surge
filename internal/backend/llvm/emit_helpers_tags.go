package llvm

import (
	"fmt"
	"strings"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
	id = resolveValueType(e.types, id)
	_, ok := e.mod.Meta.TagLayouts[id]
	return ok
}

func (e *Emitter) tagCases(id types.TypeID) ([]mir.TagCaseMeta, error) {
	if e == nil || e.mod == nil || e.mod.Meta == nil || len(e.mod.Meta.TagLayouts) == 0 {
		return nil, fmt.Errorf("missing tag layout metadata")
	}
	id = resolveValueType(e.types, id)
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
	typeLabel := fmt.Sprintf("type#%d", id)
	if e != nil && e.types != nil {
		if info, ok := e.types.UnionInfo(id); ok && info != nil {
			if e.syms != nil && e.syms.Strings != nil && info.Name != 0 {
				typeLabel = fmt.Sprintf("%s %q", typeLabel, e.syms.Strings.MustLookup(info.Name))
			}
		}
	}
	caseNames := make([]string, 0, len(cases))
	for _, c := range cases {
		if c.TagName != "" {
			caseNames = append(caseNames, c.TagName)
			continue
		}
		if c.TagSym.IsValid() {
			caseNames = append(caseNames, fmt.Sprintf("sym#%d", c.TagSym))
		}
	}
	return -1, mir.TagCaseMeta{}, fmt.Errorf("unknown tag %q for %s (cases: %s)", tagName, typeLabel, strings.Join(caseNames, ", "))
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

	if len(meta.PayloadTypes) == 0 {
		return mem, nil
	}
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
	return mem, nil
}
