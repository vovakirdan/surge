package llvm

import (
	"fmt"
	"os"
	"strings"

	"surge/internal/mir"
	"surge/internal/types"
)

// Every runtime answer the generated code must test before it stores through it.
//
// rt_alloc answers NULL when the allocator refuses (runtime/native/rt_alloc.c);
// it does not report and it does not exit. A generated body that stored through
// that answer faulted at an address the program never chose, which is not "a
// failed duplication is fatal" — it is the absence of any implementation of it.
// Owner ruling 2026-08-28: the generated code tests the allocation and stops the
// process through the runtime's own reporter with a sentence naming the type
// whose storage was refused.
//
// The question is NOT whether a call is spelled `rt_alloc`. It is whether the
// call can answer NULL. `a.push(7)` reaches rt_realloc, and rt_realloc answers
// NULL on the same refusal — the first writing of this guard tested the spelling
// and let the commonest array operation in the language store an element through
// the null. So every runtime entry point that hands a pointer to generated code
// is classified in TestEveryRuntimePointerAnswerIsClassified, over the ABI
// roster in builtins.go rather than over the emitters, because an entry point
// reached through the ordinary call path is written by no emitter at all — which
// is how the three open-ended Range constructors stayed invisible.
//
// The calls whose answer this guard tests are written HERE, so that census can
// key on the file: a `call ptr @rt_realloc(` outside this file is a hole by
// construction. The one exception is the generic call path, which reaches a
// runtime symbol with no emitter of its own; see
// runtimeAnswersTestedAtTheCallSite.

// allocRefusalEnvVar names the negative-control build.
//
// A real allocator cannot be made to refuse on demand, and this repository
// already answers that with a build that differs from the shipping one in
// exactly the observed variable (RT_TEST_SYNC_POINTS for an interleaving,
// descriptorDefect for a descriptor). Here the observed variable is the size
// operand: under the control, the site named by this variable asks for
// allocRefusalSize bytes, rt_alloc's malloc/posix_memalign answer NULL, and the
// guard below is reached along exactly the path a real refusal takes. No branch
// is planted, no allocator is stubbed, and the ordinary build differs from the
// control only in that one integer.
const allocRefusalEnvVar = "SURGE_INTERNAL_TEST_ALLOC_REFUSAL"

// allocRefusalSize is 2^64-1: a request no allocator on any machine serves, so
// the refusal is deterministic rather than dependent on how much memory the
// stand happens to have.
const allocRefusalSize = "18446744073709551615"

// allocSite names one guarded allocation the negative control can aim at.
//
// The names exist so the control can refuse exactly one site and a stand can say
// which refusal it observed. A single switch that refused every allocation would
// prove only that the first one in the program is guarded. A site exists only
// where the call carries a SIZE operand the control can rewrite; a guarded call
// with no size — a Range constructor — is proven by its emitted shape instead.
type allocSite string

const (
	allocSiteRuntimeOwned     allocSite = "runtime-owned-storage"
	allocSiteArrayElements    allocSite = "array-literal-elements"
	allocSiteArrayHeader      allocSite = "array-literal-header"
	allocSiteDefaultArray     allocSite = "default-array-header"
	allocSiteErrorValue       allocSite = "error-value"
	allocSiteRangeIter        allocSite = "range-iterator"
	allocSiteArrayIter        allocSite = "array-iterator"
	allocSiteArrayGrowPush    allocSite = "array-grow-push"
	allocSiteArrayGrowReserve allocSite = "array-grow-reserve"
)

// allocGuardedSites is the roster: every allocation this file writes with a size
// the control can rewrite. A site added to the emitter and not to this list is a
// site no negative control can aim at, so the roster is asserted against the
// emitter's own call sites.
func allocGuardedSites() []allocSite {
	return []allocSite{
		allocSiteRuntimeOwned,
		allocSiteArrayElements,
		allocSiteArrayHeader,
		allocSiteDefaultArray,
		allocSiteErrorValue,
		allocSiteRangeIter,
		allocSiteArrayIter,
		allocSiteArrayGrowPush,
		allocSiteArrayGrowReserve,
	}
}

// runtimeAnswersTestedAtTheCallSite are the nullable runtime entry points the
// generic call path tests.
//
// A runtime symbol the language names arrives one of two ways: an intrinsic
// emitter claims it in a `case` and lowers it itself, or it falls through to
// emitCallSite, which spells its callee through a format operand and so writes
// no entry point's name anywhere in this package. Membership here is therefore
// not a matter of taste — an unclaimed entry point is tested here or nowhere —
// and it is checked rather than remembered, in
// TestATestedAnswerIsGuardedOnEveryPathThatReachesIt.
//
// rt_range_int_new is on this list AND written by emitCheckedRangeNew, because a
// bounded range has two spellings that share no lowering: `a..b` is a binary
// operator this package lowers itself, and `[a..b]` is lowered by
// internal/hir/lower_expr_range.go to an ordinary call to this same symbol. The
// two paths never meet — emitBinary does not go through emitCallSite — so
// neither test can double-test the other's call. Reading the list off the
// emitters instead of off the ABI is what hid the second spelling: no emitter
// writes it.
func runtimeAnswersTestedAtTheCallSite() map[string]bool {
	return map[string]bool{
		"rt_range_int_new":        true,
		"rt_range_int_from_start": true,
		"rt_range_int_to_end":     true,
		"rt_range_int_full":       true,
	}
}

// allocRefusalArmed reports whether this build makes the named site refuse.
func allocRefusalArmed(site allocSite) bool {
	return os.Getenv(allocRefusalEnvVar) == string(site)
}

// allocFailureMessage is the sentence a refused allocation reports. It names the
// type, because the type is the one fact the person reading the line can act on:
// the program did not choose the address, and it did not choose the size either.
func allocFailureMessage(typesIn *types.Interner, id types.TypeID) string {
	return allocFailureMessageFor(types.Label(typesIn, id))
}

// allocFailureMessageFor is the same sentence for storage named by spelling
// rather than by TypeID. A `a..b` carries its operands and not its result, so at
// the point the Range object is built the instruction has no TypeID for it; the
// bound type is the only thing that tells one Range type from another, which is
// what the caller spells.
func allocFailureMessageFor(label string) string {
	return "out of memory: could not allocate " + label
}

// emitRefusalTest tests one runtime answer and continues in a block reached only
// when the call was served. The tested pointer is live in the continuation block
// and nowhere else.
func (fe *funcEmitter) emitRefusalTest(label, ptr string) {
	refused := fe.nextTemp()
	refusedBB := fe.nextInlineBlock()
	servedBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq ptr %s, null\n", refused, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", refused, refusedBB, servedBB)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", refusedBB)
	fe.emitAllocRefusalPanic(label)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", servedBB)
}

// emitCheckedAlloc writes one runtime allocation and tests it.
//
// `size` is an LLVM i64 operand — a literal or a temp — because one site sizes
// its allocation from a byte the object carries at run time.
func (fe *funcEmitter) emitCheckedAlloc(site allocSite, id types.TypeID, size string, align uint64) string {
	if allocRefusalArmed(site) {
		size = allocRefusalSize
	}
	if align == 0 {
		align = 1
	}
	ptr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %s, i64 %d)\n", ptr, size, align)
	fe.emitRefusalTest(allocFailureMessage(fe.emitter.types, id), ptr)
	return ptr
}

// emitCheckedRealloc grows one runtime block and tests the answer BEFORE the
// caller records it.
//
// A refused reallocation RELEASES NOTHING (runtime/native/rt_alloc.c): the old
// block is still the caller's, and the only pointer to it is the one the caller
// is about to overwrite. So the refusal stops the process here, in front of that
// store. A guard that read NULL as "the buffer moved" — storing it and carrying
// on — would lose the old block as well as the answer, and a header claiming the
// grown capacity over a null data pointer sends every later indexed write
// through the null.
func (fe *funcEmitter) emitCheckedRealloc(site allocSite, id types.TypeID, data, oldSize, newSize string, align uint64) string {
	if allocRefusalArmed(site) {
		newSize = allocRefusalSize
	}
	if align == 0 {
		align = 1
	}
	ptr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_realloc(ptr %s, i64 %s, i64 %s, i64 %d)\n",
		ptr, data, oldSize, newSize, align)
	fe.emitRefusalTest(allocFailureMessage(fe.emitter.types, id), ptr)
	return ptr
}

// emitCheckedRangeNew builds one bounded Range object and tests the answer.
//
// The refusal is reported at the constructor rather than at the loop that walks
// the object, because the walk reads the kind byte out of the object to decide
// how big its cursor must be: a test placed there would already be a load
// through the null it was meant to catch.
//
// `typeLabel` is a TYPE SPELLING, not a sentence: it goes through the same
// wording every other refusal uses. Handing it to emitRefusalTest directly is
// what this did first, and a refused `0..3` then reported `panic: Range<int>` —
// a bare type name, no reason, nothing the reader could act on — beside a
// sibling site reporting the whole sentence for the same object.
func (fe *funcEmitter) emitCheckedRangeNew(typeLabel, start, end, inclusive string) string {
	ptr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_range_int_new(ptr %s, ptr %s, i1 %s)\n",
		ptr, start, end, inclusive)
	fe.emitRefusalTest(allocFailureMessageFor(typeLabel), ptr)
	return ptr
}

// emitRuntimeAnswerTest guards a nullable runtime entry point reached through
// the ordinary call path, where there is no emitter of its own to put a test in.
//
// The type comes from the destination the answer is about to be written into,
// which is the only language type a call to a runtime symbol carries: the
// declaration itself is machine types alone.
func (fe *funcEmitter) emitRuntimeAnswerTest(call *mir.CallInstr, callee, ptr string) {
	if call == nil || !runtimeAnswersTestedAtTheCallSite()[strings.TrimPrefix(callee, "@")] {
		return
	}
	id := types.NoTypeID
	if call.HasDst && call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
		id = fe.f.Locals[call.Dst.Local].Type
	}
	fe.emitRefusalTest(allocFailureMessage(fe.emitter.types, id), ptr)
}

// emitAllocRefusalPanic reports the refusal and does not return.
//
// The message is built per type, so it cannot come from the module's
// string-constant table: that table hands out globals by index over its sorted
// contents and has to be complete before the first function body names one of
// them. It goes through the late table emit_span.go fills, which exists for
// exactly this — a text nothing knew about until a body was written.
func (fe *funcEmitter) emitAllocRefusalPanic(label string) {
	sc := fe.emitter.messageConst(label)
	fmt.Fprintf(&fe.emitter.buf,
		"  call void @rt_panic(ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d)\n",
		sc.arrayLen, sc.globalName, sc.dataLen)
	fe.emitter.buf.WriteString("  unreachable\n")
}
