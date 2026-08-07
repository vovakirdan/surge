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

func (e *Emitter) layoutOf(id types.TypeID) (layout.PhysicalFacts, error) {
	if e == nil || e.mod == nil || e.mod.Meta == nil || e.mod.Meta.Layouts == nil {
		return layout.PhysicalFacts{}, fmt.Errorf("missing finalized layout registry")
	}
	return e.mod.Meta.Layouts.Require(id)
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
	// The operand already names the union's storage, whether it came from a
	// place or from a borrow: a borrow of a value that lives inline points at
	// that value, not at a slot holding its address. The load that used to
	// stand here was the second half of the boxed representation, and reaching
	// it now would read the discriminant as an address.
	val, valTy, err := fe.emitValueOperand(op)
	if err != nil {
		return "", err
	}
	if valTy != handleType {
		return "", fmt.Errorf("tag value must be addressed, got %s", valTy)
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}
	tagVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i32, ptr %s, align %d\n", tagVal, val, align)
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
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}
	unionCase, ok := layoutInfo.UnionCase(caseIdx)
	if !ok {
		return "", fmt.Errorf("missing finalized union case %d for type#%d", caseIdx, typeID)
	}

	// A tag value is built in storage of its own, like every other literal:
	// the assignment that receives it decides where it finally lives.
	if len(meta.PayloadTypes) == 0 {
		mem, allocErr := fe.emitStorageAlloca(typeID)
		if allocErr != nil {
			return "", allocErr
		}
		fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s, align %d\n", caseIdx, mem, align)
		return mem, nil
	}
	offsets := unionCase.FieldOffsets()
	if len(offsets) != len(meta.PayloadTypes) {
		return "", fmt.Errorf("finalized union case %d for type#%d has %d payload offsets, want %d", caseIdx, typeID, len(offsets), len(meta.PayloadTypes))
	}

	mem, err := fe.emitStorageAlloca(typeID)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s, align %d\n", caseIdx, mem, align)
	for i := range args {
		arg := &args[i]
		payloadTy := meta.PayloadTypes[i]
		if isNothingType(fe.emitter.types, payloadTy) {
			continue
		}
		val, valTy, err := fe.emitValueOperand(arg)
		if err != nil {
			return "", err
		}
		payloadStorage, err := fe.emitter.llvmValueType(payloadTy)
		if err != nil {
			return "", err
		}
		payloadLLVM := operandTypeFor(payloadStorage)
		if valTy != payloadLLVM {
			valType := operandValueType(fe.emitter.types, arg)
			if valType == types.NoTypeID && arg.Kind != mir.OperandConst {
				if baseType, err := fe.placeBaseType(arg.Place); err == nil {
					valType = baseType
				}
			}
			// The coerced spelling is not carried forward: the store below
			// names the payload's own storage type, which is what the value
			// is being written as.
			casted, _, coerceErr := fe.coerceNumericValue(val, valTy, valType, payloadTy)
			if coerceErr != nil {
				return "", coerceErr
			}
			val = casted
		}
		off := unionCase.PayloadOffset + offsets[i]
		bytePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
		fe.emitValueStore(payloadStorage, val, bytePtr, memberAccessAlign(align, off))
	}
	return mem, nil
}
