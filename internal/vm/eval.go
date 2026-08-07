package vm

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// evalRValue evaluates an rvalue to a Value.
//
// dstType is the type the result is destined for. Most rvalues do not need it —
// they know what they produce — but a composite literal does: MIR records a
// tuple or an array literal as its elements and nothing else, so the only place
// its type exists is the slot it is going into.
func (vm *VM) evalRValue(frame *Frame, rv *mir.RValue, dstType types.TypeID) (Value, *VMError) {
	switch rv.Kind {
	case mir.RValueUse:
		return vm.evalOperand(frame, &rv.Use)

	case mir.RValueBinaryOp:
		left, vmErr := vm.evalOperand(frame, &rv.Binary.Left)
		if vmErr != nil {
			return Value{}, vmErr
		}
		right, vmErr := vm.evalOperand(frame, &rv.Binary.Right)
		if vmErr != nil {
			vm.dropValue(left)
			return Value{}, vmErr
		}
		res, vmErr := vm.evalBinaryOp(rv.Binary.Op, left, right)
		vm.dropValue(right)
		vm.dropValue(left)
		return res, vmErr

	case mir.RValueUnaryOp:
		operand, vmErr := vm.evalOperand(frame, &rv.Unary.Operand)
		if vmErr != nil {
			return Value{}, vmErr
		}
		res, vmErr := vm.evalUnaryOp(rv.Unary.Op, operand)
		if vmErr != nil {
			vm.dropValue(operand)
			return Value{}, vmErr
		}
		if res != operand {
			vm.dropValue(operand)
		}
		return res, nil

	case mir.RValueCast:
		v, vmErr := vm.evalOperand(frame, &rv.Cast.Value)
		if vmErr != nil {
			return Value{}, vmErr
		}
		res, vmErr := vm.evalCast(v, rv.Cast.TargetTy)
		if vmErr != nil {
			vm.dropValue(v)
			return Value{}, vmErr
		}
		if !v.IsHeap() || !res.IsHeap() || v.Kind != res.Kind || v.H != res.H {
			vm.dropValue(v)
		}
		return res, nil

	case mir.RValueIndex:
		obj, vmErr := vm.evalOperand(frame, &rv.Index.Object)
		if vmErr != nil {
			return Value{}, vmErr
		}
		idx, vmErr := vm.evalOperand(frame, &rv.Index.Index)
		if vmErr != nil {
			vm.dropValue(obj)
			return Value{}, vmErr
		}
		res, vmErr := vm.evalIndex(obj, idx)
		vm.dropValue(idx)
		vm.dropValue(obj)
		return res, vmErr

	case mir.RValueStructLit:
		return vm.evalStructLit(frame, &rv.StructLit)

	case mir.RValueArrayLit:
		return vm.evalArrayLit(frame, &rv.ArrayLit, dstType)

	case mir.RValueTupleLit:
		return vm.evalTupleLit(frame, &rv.TupleLit, dstType)

	case mir.RValueField:
		return vm.evalFieldAccess(frame, &rv.Field)

	case mir.RValueTagTest:
		return vm.evalTagTest(frame, &rv.TagTest)

	case mir.RValueTagPayload:
		return vm.evalTagPayload(frame, &rv.TagPayload)
	case mir.RValueIterInit:
		return vm.evalIterInit(frame, &rv.IterInit)
	case mir.RValueIterNext:
		return vm.evalIterNext(frame, &rv.IterNext)
	case mir.RValueTypeTest:
		return vm.evalTypeTest(frame, &rv.TypeTest)
	case mir.RValueHeirTest:
		return vm.evalHeirTest(frame, &rv.HeirTest)

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("rvalue kind %d", rv.Kind))
	}
}

func (vm *VM) evalCast(v Value, target types.TypeID) (Value, *VMError) {
	if target == types.NoTypeID {
		return v, nil
	}

	if vm.Types != nil {
		if retagged, ok := vm.retagUnionValue(v, target); ok {
			return retagged, nil
		}
	}

	if vm.Types == nil {
		v.TypeID = target
		return v, nil
	}
	return vm.evalIntrinsicTo(v, target)
}

// evalOperand evaluates an operand to a Value.
func (vm *VM) evalOperand(frame *Frame, op *mir.Operand) (Value, *VMError) {
	switch op.Kind {
	case mir.OperandConst:
		return vm.evalConst(&op.Const), nil

	// OperandRetain names in MIR what the VM's heap has always done on a read:
	// every copy of a heap value bumps its count. The distinction exists for
	// the native backend, where a plain copy is a bare word move and the bump
	// has to be emitted; here both kinds are the same operation.
	//
	// OperandCopyValue is where they part. It names a composite read that must
	// produce an INDEPENDENT value, and a retain does not: it would leave two
	// bindings naming one object, so a write through either is visible through
	// the other. That is the aliasing defect this kind exists to fix, so this
	// case clones instead of counting.
	case mir.OperandCopy, mir.OperandRetain, mir.OperandCopyValue:
		duplicate := func(val Value) (Value, *VMError) {
			// A composite read is a COPY whatever the operand kind says, and
			// the three kinds collapse into one here. They differ only in how
			// a duplicate is made, and a composite has exactly one way to make
			// one: there is no count to raise, so the only way to hand back a
			// value the consumer OWNS is to hand it its own bytes. The aliasing
			// OperandCopyValue exists to prevent is not expressible any more —
			// two names for one extent would be one value where the language
			// says there are two.
			if val.Kind == VKComposite || op.Kind == mir.OperandCopyValue {
				return vm.cloneValueComposite(val)
			}
			if val.IsHeap() && val.H != 0 {
				vm.Heap.Retain(val.H)
			}
			return val, nil
		}
		if len(op.Place.Proj) == 0 {
			switch op.Place.Kind {
			case mir.PlaceGlobal:
				val, vmErr := vm.readGlobal(op.Place.Global)
				if vmErr != nil {
					return Value{}, vmErr
				}
				return duplicate(val)
			default:
				val, vmErr := vm.readLocal(frame, op.Place.Local)
				if vmErr != nil {
					return Value{}, vmErr
				}
				return duplicate(val)
			}
		}
		loc, vmErr := vm.EvalPlace(frame, op.Place)
		if vmErr != nil {
			return Value{}, vmErr
		}
		val, vmErr := vm.loadLocationRaw(loc)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return duplicate(val)

	case mir.OperandMove:
		if len(op.Place.Proj) == 0 {
			switch op.Place.Kind {
			case mir.PlaceGlobal:
				val, vmErr := vm.readGlobal(op.Place.Global)
				if vmErr != nil {
					return Value{}, vmErr
				}
				vm.moveGlobal(op.Place.Global)
				return val, nil
			default:
				val, vmErr := vm.readLocal(frame, op.Place.Local)
				if vmErr != nil {
					return Value{}, vmErr
				}
				vm.moveLocal(frame, op.Place.Local)
				return val, nil
			}
		}
		loc, vmErr := vm.EvalPlace(frame, op.Place)
		if vmErr != nil {
			return Value{}, vmErr
		}
		val, vmErr := vm.loadLocationRaw(loc)
		if vmErr != nil {
			return Value{}, vmErr
		}
		// Moving out of a PROJECTION has to take the member before the hole is
		// filled. A composite read handed back a reference INTO the extent that
		// is about to be overwritten, so keeping it and writing the default
		// afterwards would return a value naming bytes the default replaced.
		// Taking it first copies it out and empties the member in one step,
		// which is also what leaves the default with nothing to release.
		if val.Kind == VKComposite && loc.Kind == LKStorage {
			taken, takeErr := vm.takeMember(frame, loc.Storage)
			if takeErr != nil {
				return Value{}, takeErr
			}
			val = taken
		}
		if val.IsHeap() && val.H != 0 {
			vm.Heap.Retain(val.H)
		}
		var def Value
		if op.Type != types.NoTypeID {
			def, vmErr = vm.defaultValue(op.Type)
			if vmErr != nil {
				if val.IsHeap() && val.H != 0 {
					vm.Heap.Release(val.H)
				}
				return Value{}, vmErr
			}
		} else {
			def = MakeNothing()
		}
		if vmErr := vm.storeLocation(loc, def); vmErr != nil {
			vm.dropValue(def)
			if val.IsHeap() && val.H != 0 {
				vm.Heap.Release(val.H)
			}
			return Value{}, vmErr
		}
		return val, nil

	case mir.OperandAddrOf:
		loc, vmErr := vm.EvalPlace(frame, op.Place)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return MakeRef(loc, op.Type), nil

	case mir.OperandAddrOfMut:
		loc, vmErr := vm.EvalPlace(frame, op.Place)
		if vmErr != nil {
			return Value{}, vmErr
		}
		if !loc.IsMut {
			return Value{}, vm.eb.invalidLocation("addr_of_mut of non-mutable location")
		}
		return MakeRefMut(loc, op.Type), nil

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("operand kind %d", op.Kind))
	}
}

// evalConst converts a MIR constant to a Value.
func (vm *VM) evalConst(c *mir.Const) Value {
	switch c.Kind {
	case mir.ConstInt:
		if kind, width, ok := vm.numericKind(c.Type); ok && kind == types.KindInt && width == types.WidthAny {
			var (
				i   bignum.BigInt
				err error
			)
			if c.Text != "" {
				i, err = bignum.ParseIntLiteral(c.Text)
			} else {
				i = bignum.IntFromInt64(c.IntValue)
			}
			if err != nil {
				vm.panic(PanicInvalidNumericConversion, fmt.Sprintf("invalid int literal %q: %v", c.Text, err))
			}
			return vm.makeBigInt(c.Type, i)
		}
		return MakeInt(c.IntValue, c.Type)
	case mir.ConstUint:
		if kind, width, ok := vm.numericKind(c.Type); ok && kind == types.KindUint && width == types.WidthAny {
			var (
				u   bignum.BigUint
				err error
			)
			if c.Text != "" {
				u, err = bignum.ParseUintLiteral(c.Text)
			} else {
				u = bignum.UintFromUint64(c.UintValue)
			}
			if err != nil {
				vm.panic(PanicInvalidNumericConversion, fmt.Sprintf("invalid uint literal %q: %v", c.Text, err))
			}
			return vm.makeBigUint(c.Type, u)
		}
		return MakeInt(asInt64(c.UintValue), c.Type)
	case mir.ConstFloat:
		if kind, width, ok := vm.numericKind(c.Type); ok && kind == types.KindFloat {
			text := c.Text
			if text == "" {
				text = strconv.FormatFloat(c.FloatValue, 'g', -1, 64)
			}
			f, err := bignum.ParseFloat(text)
			if err != nil {
				vm.panic(PanicInvalidNumericConversion, fmt.Sprintf("invalid float literal %q: %v", text, err))
			}
			if width != types.WidthAny && !floatFitsWidth(f, width) {
				vm.panic(PanicInvalidNumericConversion, "float literal out of range")
			}
			return vm.makeBigFloat(c.Type, f)
		}
		vm.panic(PanicFloatUnsupported, "float constant evaluation is not supported")
		return Value{Kind: VKInvalid}
	case mir.ConstBool:
		return MakeBool(c.BoolValue, c.Type)
	case mir.ConstString:
		s := unescapeStringLiteral(c.StringValue)
		h := vm.Heap.AllocString(c.Type, s)
		return MakeHandleString(h, c.Type)
	case mir.ConstNothing:
		// A `nothing` whose type is a union is that union's nothing ARM, and an
		// arm is bytes rather than a bare marker. Building it needs the
		// activation's scratch, so a constant evaluated where none is reachable
		// stays the typeless nothing and acquires its representation at the
		// slot it is written into — coerceToSlotType builds the same arm there,
		// which is why the fallback loses nothing.
		if c.Type != types.NoTypeID && vm.tagLayouts != nil {
			if layout, ok := vm.tagLayouts.Layout(vm.valueType(c.Type)); ok && layout != nil {
				if tc, ok := layout.CaseByName("nothing"); ok {
					if val, vmErr := vm.buildTag(vm.currentFrame(), c.Type, tc.TagSym, nil); vmErr == nil {
						return val
					}
				}
			}
		}
		return MakeNothing()
	case mir.ConstFn:
		return MakeFunc(c.Sym, c.Type)
	default:
		return Value{Kind: VKInvalid}
	}
}

func unescapeStringLiteral(raw string) string {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	var sb strings.Builder
	sb.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch != '\\' {
			sb.WriteByte(ch)
			continue
		}
		if i+1 >= len(raw) {
			break
		}
		i++
		switch raw[i] {
		case '\\':
			sb.WriteByte('\\')
		case '"':
			sb.WriteByte('"')
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		default:
			sb.WriteByte(raw[i])
		}
	}
	return sb.String()
}
