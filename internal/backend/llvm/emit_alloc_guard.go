package llvm

import (
	"fmt"
	"os"

	"surge/internal/types"
)

// Every allocation the compiler writes into a generated body is tested here.
//
// rt_alloc answers NULL when the allocator refuses (runtime/native/rt_alloc.c);
// it does not report and it does not exit. A generated body that stored through
// that answer faulted at an address the program never chose, which is not "a
// failed duplication is fatal" — it is the absence of any implementation of it.
// Owner ruling 2026-08-28: the generated code tests the allocation and stops the
// process through the runtime's own reporter with a sentence naming the type
// whose storage was refused.
//
// One helper writes all of them, so a new allocation site cannot be added
// without either passing through this test or being visible as a raw
// `call ptr @rt_alloc` — which is what
// TestEveryEmittedAllocationIsTestedForRefusal looks for.

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

// allocSite names one guarded allocation.
//
// The names exist so the negative control can refuse exactly one site and a
// stand can say which refusal it observed. A single switch that refused every
// allocation would prove only that the first one in the program is guarded.
type allocSite string

const (
	allocSiteRuntimeOwned  allocSite = "runtime-owned-storage"
	allocSiteArrayElements allocSite = "array-literal-elements"
	allocSiteArrayHeader   allocSite = "array-literal-header"
	allocSiteDefaultArray  allocSite = "default-array-header"
	allocSiteErrorValue    allocSite = "error-value"
	allocSiteRangeIter     allocSite = "range-iterator"
	allocSiteArrayIter     allocSite = "array-iterator"
)

// allocGuardedSites is the roster: every allocation this file writes. A site
// added to the emitter and not to this list is a site no negative control can
// aim at, so the roster is asserted against the emitter's own call sites.
func allocGuardedSites() []allocSite {
	return []allocSite{
		allocSiteRuntimeOwned,
		allocSiteArrayElements,
		allocSiteArrayHeader,
		allocSiteDefaultArray,
		allocSiteErrorValue,
		allocSiteRangeIter,
		allocSiteArrayIter,
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
	return "out of memory: could not allocate " + types.Label(typesIn, id)
}

// emitCheckedAlloc writes one runtime allocation, tests it, and continues in a
// block reached only when the allocator answered.
//
// `size` is an LLVM i64 operand — a literal or a temp — because one site sizes
// its allocation from a byte the object carries at run time. The returned
// pointer is live in the continuation block and nowhere else.
func (fe *funcEmitter) emitCheckedAlloc(site allocSite, id types.TypeID, size string, align uint64) string {
	if allocRefusalArmed(site) {
		size = allocRefusalSize
	}
	if align == 0 {
		align = 1
	}
	ptr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %s, i64 %d)\n", ptr, size, align)
	refused := fe.nextTemp()
	refusedBB := fe.nextInlineBlock()
	servedBB := fe.nextInlineBlock()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq ptr %s, null\n", refused, ptr)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", refused, refusedBB, servedBB)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", refusedBB)
	fe.emitAllocRefusalPanic(id)
	fmt.Fprintf(&fe.emitter.buf, "%s:\n", servedBB)
	return ptr
}

// emitAllocRefusalPanic reports the refusal and does not return.
//
// The message is built per type, so it cannot come from the module's
// string-constant table: that table hands out globals by index over its sorted
// contents and has to be complete before the first function body names one of
// them. It goes through the late table emit_span.go fills, which exists for
// exactly this — a text nothing knew about until a body was written.
func (fe *funcEmitter) emitAllocRefusalPanic(id types.TypeID) {
	sc := fe.emitter.messageConst(allocFailureMessage(fe.emitter.types, id))
	fmt.Fprintf(&fe.emitter.buf,
		"  call void @rt_panic(ptr getelementptr inbounds ([%d x i8], ptr @%s, i64 0, i64 0), i64 %d)\n",
		sc.arrayLen, sc.globalName, sc.dataLen)
	fe.emitter.buf.WriteString("  unreachable\n")
}
