package llvm

import (
	"fmt"
	"strings"

	"surge/internal/types"
)

// The offsets are pinned by rt_bignum_internal.h, including the bigint tail
// viewed internally as a biguint. Only aliases and own preserve ownership;
// resolving a reference to its pointee here would count borrowed storage.
type scalarLifecycle struct {
	prefix   string
	rcOffset uint64
	tagged   bool
}

func scalarLifecycleFor(in *types.Interner, id types.TypeID) (scalarLifecycle, bool) {
	if in == nil {
		return scalarLifecycle{}, false
	}
	tt, ok := in.Lookup(resolveAliasAndOwn(in, id))
	if !ok || tt.Width != types.WidthAny {
		return scalarLifecycle{}, false
	}
	switch tt.Kind {
	case types.KindInt:
		return scalarLifecycle{"rt_bigint", 8, true}, true
	case types.KindUint:
		return scalarLifecycle{"rt_biguint", 4, true}, true
	case types.KindFloat:
		return scalarLifecycle{"rt_bigfloat", 0, false}, true
	default:
		return scalarLifecycle{}, false
	}
}

// The branch must precede even the RC GEP: an inline word is not an address.
// Callers supply names from their own namespace and continue at done after
// finishing the heap arm. No branch-local value escapes through that join.
func emitNumericHeapGuard(out *strings.Builder, val string, next func() string, heap, done string) {
	word, lowBit, aligned, nonnull, isHeap := next(), next(), next(), next(), next()
	fmt.Fprintf(out, "  %s = ptrtoint ptr %s to i64\n", word, val)
	fmt.Fprintf(out, "  %s = and i64 %s, 1\n", lowBit, word)
	fmt.Fprintf(out, "  %s = icmp eq i64 %s, 0\n", aligned, lowBit)
	fmt.Fprintf(out, "  %s = icmp ne ptr %s, null\n", nonnull, val)
	fmt.Fprintf(out, "  %s = and i1 %s, %s\n", isHeap, nonnull, aligned)
	fmt.Fprintf(out, "  br i1 %s, label %%%s, label %%%s\n%s:\n", isHeap, heap, done, heap)
}

func (fe *funcEmitter) beginScalarHeap(val string, ops scalarLifecycle) string {
	if !ops.tagged {
		return ""
	}
	heap, done := fe.nextInlineBlock(), fe.nextInlineBlock()
	emitNumericHeapGuard(&fe.emitter.buf, val, fe.nextTemp, heap, done)
	return done
}

func (e *Emitter) beginGlueScalarHeap(g *glueTmp, val string, ops scalarLifecycle) string {
	if !ops.tagged {
		return ""
	}
	g.n++ // Reserve a unique ordinal independently of any enclosing union arm.
	heap, done := fmt.Sprintf("numeric.heap.%d", g.n), fmt.Sprintf("numeric.done.%d", g.n)
	emitNumericHeapGuard(&e.buf, val, g.next, heap, done)
	return done
}

func endScalarHeap(out *strings.Builder, done string) {
	if done != "" {
		fmt.Fprintf(out, "  br label %%%s\n%s:\n", done, done)
	}
}

// Reached only in the heap arm. Float keeps its existing branchless emission.
func emitNumericHeapRetain(out *strings.Builder, val string, offset uint64, next func() string) {
	slot, count, bumped := next(), next(), next()
	fmt.Fprintf(out, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", slot, val, offset)
	fmt.Fprintf(out, "  %s = load i32, ptr %s, align 4\n", count, slot)
	fmt.Fprintf(out, "  %s = add i32 %s, 1\n", bumped, count)
	fmt.Fprintf(out, "  store i32 %s, ptr %s, align 4\n", bumped, slot)
}

func (fe *funcEmitter) emitScalarRelease(val string, ops scalarLifecycle) {
	done := fe.beginScalarHeap(val, ops)
	fmt.Fprintf(&fe.emitter.buf, "  call void @%s_release(ptr %s)\n", ops.prefix, val)
	endScalarHeap(&fe.emitter.buf, done)
}

func (e *Emitter) emitGlueScalarRelease(g *glueTmp, val string, ops scalarLifecycle) {
	done := e.beginGlueScalarHeap(g, val, ops)
	fmt.Fprintf(&e.buf, "  call void @%s_release(ptr %s)\n", ops.prefix, val)
	endScalarHeap(&e.buf, done)
}

// Clone already copied the slot, and unshare works on its original slot.
// Keeping the call result and its store inside the heap arm leaves fixnum/NULL
// bytes intact without a phi or a runtime call on the other arm.
func (e *Emitter) emitGlueScalarUpdateAt(g *glueTmp, base string, baseAlign, off uint64, ops scalarLifecycle, operation string) {
	fp := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", fp, base, off)
	fv := g.next()
	fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %s, align %d\n", fv, fp, memberAccessAlign(baseAlign, off))
	done := e.beginGlueScalarHeap(g, fv, ops)
	updated := g.next()
	fmt.Fprintf(&e.buf, "  %s = call ptr @%s_%s(ptr %s)\n", updated, ops.prefix, operation, fv)
	fmt.Fprintf(&e.buf, "  store ptr %s, ptr %s, align %d\n", updated, fp, memberAccessAlign(baseAlign, off))
	endScalarHeap(&e.buf, done)
}
