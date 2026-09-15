package llvm

import (
	"fmt"

	"surge/internal/ast"
)

// Inline fixnum fast paths for WidthAny int and uint.
//
// A counted integer word is NULL (zero), an odd-tagged inline fixnum, or an
// even pointer to a heap bignum (runtime/native/rt_bignum_tag.h). The runtime
// answers the first two without touching the heap, but only behind a call.
// These paths answer them in the emitted code and hand every other case --
// a heap operand, a negative value an unsigned destination refuses, a result
// that leaves the inline range -- to the unchanged runtime emission. A fast arm
// creates nothing and releases nothing, so the ownership sequence MIR attached
// to the operation holds on both arms: an inline word owns no block, exactly
// like the runtime's own fixnum answer.

// emitFixnumWord exposes a counted integer word as an i64 so its tag can be
// tested and an inline payload decoded. It is never converted back to an
// address; a heap word only ever continues on the runtime arm.
func (fe *funcEmitter) emitFixnumWord(val string) string {
	word := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = ptrtoint ptr %s to i64\n", word, val)
	return word
}

// emitFixnumInline reports whether a word is NULL or an inline fixnum.
func (fe *funcEmitter) emitFixnumInline(word string) string {
	low, tagged, zero, inline := fe.nextTemp(), fe.nextTemp(), fe.nextTemp(), fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = and i64 %s, 1\n", low, word)
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp ne i64 %s, 0\n", tagged, low)
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i64 %s, 0\n", zero, word)
	fmt.Fprintf(&fe.emitter.buf, "  %s = or i1 %s, %s\n", inline, tagged, zero)
	return inline
}

// emitFixnumCastToI64 converts an int or uint word to a 64-bit integer. A NULL
// or inline word whose value the destination's sign accepts is decoded in
// place; anything else runs slow, the existing checked runtime conversion,
// byte for byte. Narrower destinations are still range-checked by the caller
// after the join.
func (fe *funcEmitter) emitFixnumCastToI64(val string, signedSource, unsignedDest bool, slow func() (string, error)) (string, error) {
	slot := fe.nextTemp()
	if _, err := fe.emitAlloca(slot, "i64"); err != nil {
		return "", err
	}
	word := fe.emitFixnumWord(val)
	take := fe.emitFixnumInline(word)
	decoded, shift := fe.nextTemp(), "lshr"
	if signedSource {
		shift = "ashr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  %s = %s i64 %s, 1\n", decoded, shift, word)
	if signedSource && unsignedDest {
		nonneg, both := fe.nextTemp(), fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = icmp sge i64 %s, 0\n", nonneg, decoded)
		fmt.Fprintf(&fe.emitter.buf, "  %s = and i1 %s, %s\n", both, take, nonneg)
		take = both
	}
	fast, slowBB, join := fe.nextInlineBlock(), fe.nextInlineBlock(), fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", take, fast, slowBB)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n  store i64 %s, ptr %s, align 8\n  br label %%%s\n", fast, decoded, slot, join)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", slowBB)
	slowVal, err := slow()
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store i64 %s, ptr %s, align 8\n  br label %%%s\n", slowVal, slot, join)
	out := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "%s:\n  %s = load i64, ptr %s, align 8\n", join, out, slot)
	return out, nil
}

// emitFixnumIntArith adds or subtracts two WidthAny int words. When both are
// NULL or inline the exact i64 result cannot overflow (each input is within
// +-2^62); it is boxed inline when it fits the fixnum range, as NULL when it is
// zero, and otherwise the original operands go to the runtime.
func (fe *funcEmitter) emitFixnumIntArith(opcode, runtimeFn, leftVal, rightVal string) (string, error) {
	slot := fe.nextTemp()
	if _, err := fe.emitAlloca(slot, "ptr"); err != nil {
		return "", err
	}
	lv, rv, both := fe.emitFixnumIntPair(leftVal, rightVal)
	sum, shifted, back, fits, take := fe.nextTemp(), fe.nextTemp(), fe.nextTemp(), fe.nextTemp(), fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = %s i64 %s, %s\n", sum, opcode, lv, rv)
	fmt.Fprintf(&fe.emitter.buf, "  %s = shl i64 %s, 1\n", shifted, sum)
	fmt.Fprintf(&fe.emitter.buf, "  %s = ashr i64 %s, 1\n", back, shifted)
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i64 %s, %s\n", fits, back, sum)
	fmt.Fprintf(&fe.emitter.buf, "  %s = and i1 %s, %s\n", take, both, fits)
	fast, slowBB, join := fe.nextInlineBlock(), fe.nextInlineBlock(), fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n%s:\n", take, fast, slowBB, fast)
	tagged, zero, boxed, word := fe.nextTemp(), fe.nextTemp(), fe.nextTemp(), fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = or i64 %s, 1\n", tagged, shifted)
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i64 %s, 0\n", zero, sum)
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, i64 0, i64 %s\n", boxed, zero, tagged)
	fmt.Fprintf(&fe.emitter.buf, "  %s = inttoptr i64 %s to ptr\n", word, boxed)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s, align 8\n  br label %%%s\n%s:\n", word, slot, join, slowBB)
	called := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @%s(ptr %s, ptr %s)\n", called, runtimeFn, leftVal, rightVal)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s, align 8\n  br label %%%s\n", called, slot, join)
	out := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "%s:\n  %s = load ptr, ptr %s, align 8\n", join, out, slot)
	return out, nil
}

// emitFixnumIntCompare compares two WidthAny int words with icmp when both are
// NULL or inline, and through rt_bigint_cmp otherwise.
func (fe *funcEmitter) emitFixnumIntCompare(op ast.ExprBinaryOp, leftVal, rightVal string) (string, error) {
	pred, err := bigComparePredicate(op)
	if err != nil {
		return "", err
	}
	slot := fe.nextTemp()
	if _, allocErr := fe.emitAlloca(slot, "i1"); allocErr != nil {
		return "", allocErr
	}
	lv, rv, both := fe.emitFixnumIntPair(leftVal, rightVal)
	fast, slowBB, join := fe.nextInlineBlock(), fe.nextInlineBlock(), fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n%s:\n", both, fast, slowBB, fast)
	inline := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp %s i64 %s, %s\n", inline, pred, lv, rv)
	fmt.Fprintf(&fe.emitter.buf, "  store i1 %s, ptr %s, align 1\n  br label %%%s\n%s:\n", inline, slot, join, slowBB)
	called, _, err := fe.emitBigCompare("rt_bigint_cmp", op, leftVal, rightVal)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store i1 %s, ptr %s, align 1\n  br label %%%s\n", called, slot, join)
	out := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "%s:\n  %s = load i1, ptr %s, align 1\n", join, out, slot)
	return out, nil
}

// emitFixnumIntPair decodes two signed words and reports whether both are
// NULL or inline. The decoded values are meaningful only when they are.
func (fe *funcEmitter) emitFixnumIntPair(leftVal, rightVal string) (lv, rv, both string) {
	lw, rw := fe.emitFixnumWord(leftVal), fe.emitFixnumWord(rightVal)
	linline, rinline := fe.emitFixnumInline(lw), fe.emitFixnumInline(rw)
	lv, rv, both = fe.nextTemp(), fe.nextTemp(), fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = ashr i64 %s, 1\n", lv, lw)
	fmt.Fprintf(&fe.emitter.buf, "  %s = ashr i64 %s, 1\n", rv, rw)
	fmt.Fprintf(&fe.emitter.buf, "  %s = and i1 %s, %s\n", both, linline, rinline)
	return lv, rv, both
}
