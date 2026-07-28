package llvm

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/mir"
	"surge/internal/numlit"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitTerminator(term *mir.Terminator) error {
	if term == nil {
		return nil
	}
	switch term.Kind {
	case mir.TermReturn:
		if term.Return.HasValue {
			op := term.Return.Value
			if op.Kind == mir.OperandConst && op.Const.Kind == mir.ConstNothing {
				if op.Type == types.NoTypeID && fe.f != nil && fe.f.Result != types.NoTypeID {
					op.Type = fe.f.Result
				}
				if op.Const.Type == types.NoTypeID && op.Type != types.NoTypeID {
					op.Const.Type = op.Type
				}
			}
			val, ty, err := fe.emitOperand(&op)
			if err != nil {
				return err
			}
			if fe.f != nil && fe.emitter != nil && fe.emitter.types != nil && fe.f.Result != types.NoTypeID {
				if isUnionType(fe.emitter.types, fe.f.Result) {
					val, ty, err = fe.emitUnionReturn(val, ty, &op, fe.f.Result)
					if err != nil {
						return err
					}
				}
			}
			fmt.Fprintf(&fe.emitter.buf, "  ret %s %s\n", ty, val)
			return nil
		}
		fmt.Fprintf(&fe.emitter.buf, "  ret void\n")
		return nil
	case mir.TermAsyncYield:
		return fe.emitTermAsyncYield(term)
	case mir.TermAsyncReturn:
		return fe.emitTermAsyncReturn(term)
	case mir.TermAsyncReturnCancelled:
		return fe.emitTermAsyncReturnCancelled(term)
	case mir.TermGoto:
		fmt.Fprintf(&fe.emitter.buf, "  br label %%bb%d\n", term.Goto.Target)
		return nil
	case mir.TermIf:
		condVal, condTy, err := fe.emitOperand(&term.If.Cond)
		if err != nil {
			return err
		}
		if condTy != "i1" {
			return fmt.Errorf("if condition must be i1, got %s", condTy)
		}
		fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%bb%d, label %%bb%d\n", condVal, term.If.Then, term.If.Else)
		return nil
	case mir.TermSwitchTag:
		return fe.emitSwitchTag(&term.SwitchTag)
	case mir.TermUnreachable:
		fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
		return nil
	default:
		return fmt.Errorf("unsupported terminator kind %v", term.Kind)
	}
}

func (fe *funcEmitter) emitSwitchTag(term *mir.SwitchTagTerm) error {
	if term == nil {
		return nil
	}
	tagVal, err := fe.emitTagDiscriminant(&term.Value)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%bb%d [\n", tagVal, term.Default)
	for _, c := range term.Cases {
		idx, err := fe.emitter.tagCaseIndex(term.Value.Type, c.TagName, symbols.NoSymbolID)
		if err != nil {
			return err
		}
		fmt.Fprintf(&fe.emitter.buf, "    i32 %d, label %%bb%d\n", idx, c.Target)
	}
	fmt.Fprintf(&fe.emitter.buf, "  ]\n")
	return nil
}

func (fe *funcEmitter) emitOperand(op *mir.Operand) (val, ty string, err error) {
	if op == nil {
		return "", "", fmt.Errorf("nil operand")
	}
	switch op.Kind {
	case mir.OperandConst:
		c := op.Const
		if op.Type != types.NoTypeID && (c.Type == types.NoTypeID || isNothingType(fe.emitter.types, c.Type)) {
			c.Type = op.Type
		}
		return fe.emitConst(&c)
	case mir.OperandCopy, mir.OperandMove:
		ptr, ty, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, ty, ptr)
		return tmp, ty, nil
	// OperandCopyValue is where a composite read stops being a pointer load.
	// The loaded word is the SOURCE's box, so handing it on would give two
	// bindings one box — the aliasing this kind exists to end. The generated
	// glue returns a box of its own, owned by whoever receives it.
	case mir.OperandCopyValue:
		ptr, ty, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, ty, ptr)
		cloneTy := op.Type
		if cloneTy == types.NoTypeID {
			if base, baseErr := fe.placeBaseType(op.Place); baseErr == nil {
				cloneTy = base
			}
		}
		if ty != "ptr" || !fe.emitter.isCloneableComposite(resolveValueType(fe.emitter.types, cloneTy)) {
			// Not a boxed composite after all — an unresolved type, or a shape
			// the backend keeps flat. Duplicating the word IS the value then,
			// which is what the load already produced.
			return tmp, ty, nil
		}
		cloned := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(ptr %s)\n",
			cloned, fe.emitter.requireCloneGlue(resolveValueType(fe.emitter.types, cloneTy)), tmp)
		return cloned, ty, nil
	case mir.OperandRetain:
		ptr, ty, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", tmp, ty, ptr)
		fe.emitRetainValue(tmp, ty)
		return tmp, ty, nil
	case mir.OperandAddrOf, mir.OperandAddrOfMut:
		ptr, _, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		return ptr, "ptr", nil
	default:
		return "", "", fmt.Errorf("unsupported operand kind %v", op.Kind)
	}
}

func (fe *funcEmitter) emitValueOperand(op *mir.Operand) (val, ty string, err error) {
	if op == nil {
		return "", "", fmt.Errorf("nil operand")
	}
	switch op.Kind {
	case mir.OperandAddrOf, mir.OperandAddrOfMut:
		ptr, _, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", "", err
		}
		// Address-of yields a reference value, which is represented as a pointer.
		return ptr, "ptr", nil
	default:
		return fe.emitOperand(op)
	}
}

func (fe *funcEmitter) emitOperandAddr(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("nil operand")
	}
	switch op.Kind {
	case mir.OperandAddrOf, mir.OperandAddrOfMut, mir.OperandCopy, mir.OperandCopyValue, mir.OperandRetain, mir.OperandMove:
		ptr, _, err := fe.emitPlacePtr(op.Place)
		if err != nil {
			return "", err
		}
		return ptr, nil
	case mir.OperandConst:
		val, ty, err := fe.emitConst(&op.Const)
		if err != nil {
			return "", err
		}
		ptr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = alloca %s\n", ptr, ty)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", ty, val, ptr)
		return ptr, nil
	default:
		return "", fmt.Errorf("unsupported operand kind %v", op.Kind)
	}
}

// Fixnum inline range (rt_bignum_tag.h): int fits [-2^62, 2^62-1], uint
// fits [0, 2^63-1]. A literal in range is a tagged word known entirely at
// compile time, so it needs no runtime materialization at all — folding it
// here replaces the per-use rt_big*_from_literal decimal parse
// (RV2-DEBT-036: two heap allocations per digit, every evaluation).
const (
	fixiMin = -(int64(1) << 62)
	fixiMax = (int64(1) << 62) - 1
	fixuMax = (uint64(1) << 63) - 1
)

// inlineFixnumWord renders a fixnum value as an LLVM ptr operand using the
// rt_bignum_tag.h encoding: 0 is NULL (the canonical zero), otherwise
// (v<<1)|1 carried in a pointer. The shift is computed on the unsigned bit
// pattern so a negative int encodes the same way fixi_box builds it.
func inlineFixnumWord(bits uint64) string {
	if bits == 0 {
		return "null"
	}
	word := (bits << 1) | 1
	// The word is a bit pattern, not an arithmetic value; reinterpret it as
	// a signed i64 for LLVM's textual integer literal exactly as fixi_box
	// builds the tag. A high-bit-set uint fixnum prints as a negative
	// literal, which is the same 64-bit pattern.
	return fmt.Sprintf("inttoptr (i64 %d to ptr)", int64(word)) //nolint:gosec // intentional bit reinterpretation
}

// fixnumBits reinterprets a signed inline value as the unsigned bit pattern
// fixi_box shifts, matching the runtime's `(uintptr_t)(uint64_t)v` step.
func fixnumBits(v int64) uint64 {
	return uint64(v) //nolint:gosec // intentional bit reinterpretation, matches fixi_box
}

// inRangeBigIntLiteral reports the fixnum-foldable value of a ConstInt whose
// type is a big int. The literal text is the source of truth (IntValue is 0
// when the literal overflowed int64); a compiler-synthesized const with no
// text falls back to IntValue.
func inRangeBigIntLiteral(c *mir.Const) (int64, bool) {
	v := c.IntValue
	if c.Text != "" {
		parsed, ok := numlit.ParseInt64(c.Text)
		if !ok {
			return 0, false // beyond int64, so beyond the inline range
		}
		v = parsed
	}
	if v < fixiMin || v > fixiMax {
		return 0, false
	}
	return v, true
}

func inRangeBigUintLiteral(c *mir.Const) (uint64, bool) {
	v := c.UintValue
	if c.Text != "" {
		parsed, ok := numlit.ParseUint64(c.Text)
		if !ok {
			return 0, false
		}
		v = parsed
	}
	if v > fixuMax {
		return 0, false
	}
	return v, true
}

func (fe *funcEmitter) emitConst(c *mir.Const) (val, ty string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("nil const")
	}
	switch c.Kind {
	case mir.ConstInt:
		if isBigIntType(fe.emitter.types, c.Type) {
			// A literal whose value fits the inline range folds to a tagged
			// word with no runtime call. The text carries full precision
			// (IntValue is 0 on overflow), so parse it rather than trusting
			// IntValue for the in-range test.
			if v, ok := inRangeBigIntLiteral(c); ok {
				return inlineFixnumWord(fixnumBits(v)), "ptr", nil
			}
			if c.Text != "" {
				ptrTmp, dataLen, err := fe.emitBytesConst(c.Text)
				if err != nil {
					return "", "", err
				}
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_from_literal(ptr %s, i64 %d)\n", tmp, ptrTmp, dataLen)
				return tmp, "ptr", nil
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigint_from_i64(i64 %d)\n", tmp, c.IntValue)
			return tmp, "ptr", nil
		}
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		if ty == "ptr" {
			if c.IntValue == 0 {
				return "null", ty, nil
			}
			return "", "", fmt.Errorf("unsupported non-zero pointer literal %d", c.IntValue)
		}
		return fmt.Sprintf("%d", c.IntValue), ty, nil
	case mir.ConstUint:
		if isBigUintType(fe.emitter.types, c.Type) {
			if v, ok := inRangeBigUintLiteral(c); ok {
				return inlineFixnumWord(v), "ptr", nil
			}
			if c.Text != "" {
				ptrTmp, dataLen, err := fe.emitBytesConst(c.Text)
				if err != nil {
					return "", "", err
				}
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_from_literal(ptr %s, i64 %d)\n", tmp, ptrTmp, dataLen)
				return tmp, "ptr", nil
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_biguint_from_u64(i64 %d)\n", tmp, c.UintValue)
			return tmp, "ptr", nil
		}
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("%d", c.UintValue), ty, nil
	case mir.ConstBool:
		return boolValue(c.BoolValue), "i1", nil
	case mir.ConstFloat:
		if isBigFloatType(fe.emitter.types, c.Type) {
			if c.Text != "" {
				ptrTmp, dataLen, err := fe.emitBytesConst(c.Text)
				if err != nil {
					return "", "", err
				}
				tmp := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_from_literal(ptr %s, i64 %d)\n", tmp, ptrTmp, dataLen)
				return tmp, "ptr", nil
			}
			tmp := fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_bigfloat_from_f64(double %v)\n", tmp, c.FloatValue)
			return tmp, "ptr", nil
		}
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		value := c.FloatValue
		if c.Text != "" {
			clean := strings.ReplaceAll(c.Text, "_", "")
			if parsed, parseErr := strconv.ParseFloat(clean, 64); parseErr == nil {
				value = parsed
			}
		}
		formatFloat := func(bits int, v float64) string {
			prec := 17
			if bits == 32 {
				prec = 9
			}
			return strconv.FormatFloat(v, 'e', prec, bits)
		}
		switch ty {
		case "double":
			return formatFloat(64, value), ty, nil
		case "float":
			v := float32(value)
			return formatFloat(32, float64(v)), ty, nil
		default:
			return "", "", fmt.Errorf("unsupported float const type %s", ty)
		}
	case mir.ConstNothing:
		if fe.emitter.hasTagLayout(c.Type) {
			if _, _, err := fe.emitter.tagCaseMeta(c.Type, "nothing", symbols.NoSymbolID); err == nil {
				ptr, err := fe.emitTagValue(c.Type, "nothing", symbols.NoSymbolID, nil)
				if err != nil {
					return "", "", err
				}
				return ptr, "ptr", nil
			}
		}
		ty, err := llvmValueType(fe.emitter.types, c.Type)
		if err != nil {
			return "", "", err
		}
		if ty == "ptr" {
			return "null", ty, nil
		}
		return "0", ty, nil
	case mir.ConstString:
		return fe.emitStringConst(c.StringValue)
	case mir.ConstFn:
		if !c.Sym.IsValid() {
			return "", "", fmt.Errorf("missing function symbol")
		}
		if fe.emitter.mod != nil {
			if id, ok := fe.emitter.mod.FuncBySym[c.Sym]; ok {
				name := fe.emitter.funcNames[id]
				if name == "" {
					name = fmt.Sprintf("fn.%d", id)
				}
				return fmt.Sprintf("@%s", name), "ptr", nil
			}
		}
		name := fe.symbolName(c.Sym)
		if name != "" {
			if _, ok := fe.emitter.runtimeSigs[name]; ok {
				return fmt.Sprintf("@%s", name), "ptr", nil
			}
		}
		return "", "", fmt.Errorf("unknown function symbol %d", c.Sym)
	default:
		return "", "", fmt.Errorf("unsupported const kind %v", c.Kind)
	}
}

func (fe *funcEmitter) emitStringConst(raw string) (val, ty string, err error) {
	ptrTmp, dataLen, err := fe.emitBytesConst(raw)
	if err != nil {
		return "", "", err
	}
	handleTmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_from_bytes(ptr %s, i64 %d)\n", handleTmp, ptrTmp, dataLen)
	return handleTmp, "ptr", nil
}

func (fe *funcEmitter) emitBytesConst(raw string) (ptr string, length int, err error) {
	sc, ok := fe.emitter.stringConsts[raw]
	if !ok {
		return "", 0, fmt.Errorf("missing string const %q", raw)
	}
	ptrTmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds [%d x i8], ptr @%s, i64 0, i64 0\n", ptrTmp, sc.arrayLen, sc.globalName)
	return ptrTmp, sc.dataLen, nil
}
