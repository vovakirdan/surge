package llvm

import (
	"fmt"

	"surge/internal/ast"
)

// Fixed-width integer arithmetic that TRAPS rather than wraps.
//
// A wrap is not a smaller answer, it is a different one, and Surge already
// decided which of the two it means: the VM raises VM1101 the moment the
// mathematical result leaves the type's range, so `127:int8 + 1` stops the
// program instead of yielding -128. A plain `add i8` yields -128 silently, so
// the native backend was answering a question the language does not ask.
//
// The question is asked here with LLVM's checked-arithmetic intrinsics, which
// return the wrapped result and an overflow bit together. Asking the hardware's
// own flag is why the check costs a branch rather than a widening: the wrap and
// the fact that it wrapped come out of one instruction.
//
// Shifts and unary negation ask their own questions and are the reason this
// file grew past add/sub/mul: a shift can lose a bit the way a multiply can,
// and a count past the width has no defined answer in LLVM at all, while `-x`
// has exactly one operand it cannot answer for. See emitCheckedIntShift and
// emitCheckedIntNegate.
//
// Division needs different questions and gets them. Dividing by zero is a trap
// in the VM and UNDEFINED in LLVM, so the zero is caught before the divide
// reaches the machine. Signed MIN / -1 is the one quotient a two's-complement
// register cannot hold; the VM calls it an overflow and LLVM calls it
// undefined, so it too is caught first. Signed MIN % -1 is the same
// undefined-in-LLVM case with a DIFFERENT right answer — the VM computes 0 —
// so it is not trapped but steered, by handing the remainder a divisor of 1,
// which yields exactly that 0.
//
// Arbitrary-precision `int`/`uint` never reach any of this. They are not fixed
// width, they carry their own overflow rules in the bignum runtime, and
// intInfo declines them, which is what keeps this file to the widths that have
// a range to leave.

// emitCheckedIntArith emits `op` on two same-width integers, trapping instead of
// wrapping, and returns the value holding the result.
//
// handled is false for every operation and every width this does not own — a
// bitwise and/or/xor, a bool — leaving the caller's plain opcode in place.
func (fe *funcEmitter) emitCheckedIntArith(
	op ast.ExprBinaryOp, info intMeta, ty, leftVal, rightVal string,
) (val string, handled bool, err error) {
	if ty != fmt.Sprintf("i%d", info.bits) {
		return "", false, nil
	}
	switch info.bits {
	case 8, 16, 32, 64:
	default:
		return "", false, nil
	}
	switch op {
	case ast.ExprBinaryAdd, ast.ExprBinarySub, ast.ExprBinaryMul:
		result, arithErr := fe.emitOverflowCheckedArith(op, info, ty, leftVal, rightVal)
		return result, true, arithErr
	case ast.ExprBinaryDiv, ast.ExprBinaryMod:
		result, divErr := fe.emitCheckedIntDivide(op, info, ty, leftVal, rightVal)
		return result, true, divErr
	case ast.ExprBinaryShiftLeft, ast.ExprBinaryShiftRight:
		result, shiftErr := fe.emitCheckedIntShift(op, info, ty, leftVal, rightVal)
		return result, true, shiftErr
	default:
		return "", false, nil
	}
}

// emitCheckedIntShift emits a shift that stops where the VM stops.
//
// Two questions, and only one of them is about losing a value.
//
// A COUNT outside [0, width) is undefined in LLVM: `shl i8 1, 9` is poison, not
// zero. That is worse than disagreeing with the VM, because poison is not an
// answer that can be compared with anything — it is free to be folded one way
// here and another way after inlining. So the count is refused BEFORE the shift
// is emitted, which is the only order in which a guard can still see a defined
// value.
//
// A LEFT shift that pushes a significant bit off the top is defined and wrong:
// the VM raises VM1101 the moment a bit is lost. The check is the round trip —
// shift back and ask whether the value survived — rather than a comparison
// against a per-width maximum, because the round trip is one shape for every
// width and signedness, and the way back carries the signedness with it.
//
// A RIGHT shift needs only the count guard. Discarding low bits is what the
// operation means and the VM agrees: `65:int8 >> 2` is 16 on both backends.
func (fe *funcEmitter) emitCheckedIntShift(
	op ast.ExprBinaryOp, info intMeta, ty, leftVal, rightVal string,
) (string, error) {
	if err := fe.emitShiftCountGuard(info, ty, rightVal); err != nil {
		return "", err
	}
	// The way back from a left shift is arithmetic for a signed type, because
	// the sign bit is one of the bits that has to survive.
	back := "lshr"
	if info.signed {
		back = "ashr"
	}
	if op == ast.ExprBinaryShiftRight {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, back, ty, leftVal, rightVal)
		return tmp, nil
	}
	shifted := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = shl %s %s, %s\n", shifted, ty, leftVal, rightVal)
	restored := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", restored, back, ty, shifted, rightVal)
	lost := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne %s %s, %s\n", lost, ty, restored, leftVal)
	fail := fe.nextInlineBlock()
	cont := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", lost, fail, cont)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
	if err := fe.emitPanicCoded("VM1101", "integer overflow"); err != nil {
		return "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	return shifted, nil
}

// emitShiftCountGuard refuses a shift count outside [0, width).
//
// ONE unsigned comparison answers both halves, for a signed count as well as an
// unsigned one, and the reason is worth stating because the code reads as if it
// forgot about negatives. A negative count has its high bit set, so read as
// unsigned it is at least 2^(width-1) — 128 for an i8 — which is past every
// width this backend emits. `icmp uge` therefore catches `-1` and `9` alike on
// an i8, while `icmp sge` would let `-1` through as "less than 8".
func (fe *funcEmitter) emitShiftCountGuard(info intMeta, ty, rightVal string) error {
	outOfRange := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp uge %s %s, %d\n", outOfRange, ty, rightVal, info.bits)
	fail := fe.nextInlineBlock()
	cont := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", outOfRange, fail, cont)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
	if err := fe.emitPanicCoded("VM1101", "integer overflow"); err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	return nil
}

// emitCheckedIntNegate emits `-x` as the subtraction it already was, and asks
// the overflow bit the plain `sub` threw away.
//
// There is exactly one signed value whose negation does not fit in its own
// width: MIN, whose magnitude is one past the largest positive. `sub i8 0, -128`
// yields -128 — the wrong answer is the operand unchanged, which is the shape
// hardest to notice in a program's output. The VM raises VM1101 for it
// (evalUnaryMinus, internal/vm/eval_ops.go).
//
// handled is false for everything this does not own: an unsigned type, which
// has no unary minus at all, and the arbitrary-precision widths, which the
// caller has already routed to the bignum runtime.
func (fe *funcEmitter) emitCheckedIntNegate(info intMeta, ty, val string) (negated string, handled bool, err error) {
	if !info.signed || ty != fmt.Sprintf("i%d", info.bits) {
		return "", false, nil
	}
	switch info.bits {
	case 8, 16, 32, 64:
	default:
		return "", false, nil
	}
	// Negation IS a subtraction from zero, so it asks the same intrinsic the
	// binary path asks rather than growing a second way to read an overflow bit.
	result, err := fe.emitOverflowCheckedArith(ast.ExprBinarySub, info, ty, "0", val)
	if err != nil {
		return "", false, err
	}
	return result, true, nil
}

// emitOverflowCheckedArith runs add/sub/mul through the checked intrinsic and
// panics on the overflow bit.
func (fe *funcEmitter) emitOverflowCheckedArith(
	op ast.ExprBinaryOp, info intMeta, ty, leftVal, rightVal string,
) (string, error) {
	verb, ok := checkedArithVerb(op)
	if !ok {
		return "", fmt.Errorf("unsupported checked arithmetic op %v", op)
	}
	sign := "u"
	if info.signed {
		sign = "s"
	}
	pair := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call {%s, i1} @llvm.%s%s.with.overflow.%s(%s %s, %s %s)\n",
		pair, ty, sign, verb, ty, ty, leftVal, ty, rightVal)
	flag := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = extractvalue {%s, i1} %s, 1\n", flag, ty, pair)
	fail := fe.nextInlineBlock()
	cont := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", flag, fail, cont)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
	if err := fe.emitPanicCoded("VM1101", "integer overflow"); err != nil {
		return "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	result := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = extractvalue {%s, i1} %s, 0\n", result, ty, pair)
	return result, nil
}

// emitCheckedIntDivide emits div/mod with the two guards a machine divide needs
// before it is allowed to run.
func (fe *funcEmitter) emitCheckedIntDivide(
	op ast.ExprBinaryOp, info intMeta, ty, leftVal, rightVal string,
) (string, error) {
	if err := fe.emitDivisorZeroGuard(ty, rightVal); err != nil {
		return "", err
	}
	if !info.signed {
		opcode := "udiv"
		if op == ast.ExprBinaryMod {
			opcode = "urem"
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s %s %s, %s\n", tmp, opcode, ty, leftVal, rightVal)
		return tmp, nil
	}

	// The single pair a signed divide cannot represent: |MIN| is one past the
	// largest positive value of the same width.
	minVal := int64(-1) << (info.bits - 1)
	atMin := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq %s %s, %d\n", atMin, ty, leftVal, minVal)
	byNegOne := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq %s %s, -1\n", byNegOne, ty, rightVal)
	unrepresentable := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = and i1 %s, %s\n", unrepresentable, atMin, byNegOne)

	if op == ast.ExprBinaryMod {
		// The remainder of that pair IS representable and the VM answers 0.
		// Dividing by 1 instead reaches the same 0 without ever handing the
		// machine the pair it has no answer for.
		safeRight := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, %s 1, %s %s\n",
			safeRight, unrepresentable, ty, ty, rightVal)
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = srem %s %s, %s\n", tmp, ty, leftVal, safeRight)
		return tmp, nil
	}

	fail := fe.nextInlineBlock()
	cont := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", unrepresentable, fail, cont)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
	if err := fe.emitPanicCoded("VM1101", "integer overflow"); err != nil {
		return "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = sdiv %s %s, %s\n", tmp, ty, leftVal, rightVal)
	return tmp, nil
}

// emitDivisorZeroGuard stops a divide by zero before the machine sees it.
func (fe *funcEmitter) emitDivisorZeroGuard(ty, rightVal string) error {
	isZero := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq %s %s, 0\n", isZero, ty, rightVal)
	fail := fe.nextInlineBlock()
	cont := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", isZero, fail, cont)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", fail)
	if err := fe.emitPanicCoded("VM3203", "division by zero"); err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	return nil
}

// checkedArithVerb names the LLVM intrinsic family for an operation.
func checkedArithVerb(op ast.ExprBinaryOp) (string, bool) {
	switch op {
	case ast.ExprBinaryAdd:
		return "add", true
	case ast.ExprBinarySub:
		return "sub", true
	case ast.ExprBinaryMul:
		return "mul", true
	default:
		return "", false
	}
}

// checkedArithDecls declares the checked-arithmetic intrinsics for every
// signedness, operation and width this backend emits.
//
// They are declared unconditionally, like every other runtime entry point in
// builtins.go: a declaration nothing calls costs a line of IR, and deciding
// per module which of the twenty-four to emit would put a second answer about
// what this backend emits next to the one the emitter already gives.
func checkedArithDecls() []builtinDecl {
	decls := make([]builtinDecl, 0, 24)
	for _, sign := range []string{"s", "u"} {
		for _, verb := range []string{"add", "sub", "mul"} {
			for _, bits := range []int{8, 16, 32, 64} {
				ty := fmt.Sprintf("i%d", bits)
				decls = append(decls, builtinDecl{
					name:   fmt.Sprintf("llvm.%s%s.with.overflow.%s", sign, verb, ty),
					ret:    fmt.Sprintf("{%s, i1}", ty),
					params: []string{ty, ty},
				})
			}
		}
	}
	return decls
}
