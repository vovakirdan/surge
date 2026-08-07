package llvm

import (
	"fmt"
	"os"
)

// The physical-operation counters the carrier benchmark scores.
//
// They count PHYSICAL work, not source syntax: one recorded copy of exactly the
// bytes moved, and one recorded callback, per generated duplication. That is
// what bounds the permission to inline a copy — a representation that quietly
// duplicated a value twice, or called generated glue in a loop, would still
// finish fast enough to pass a throughput threshold, and would show up here
// immediately as twice the bytes or twice the callbacks.
//
// The instrumentation is a build-time decision, not a runtime one. A timing
// artifact must contain no carrier-benchmark symbol at all — the harness checks
// its symbol table and fails if it finds one — so the calls cannot be emitted
// and then made no-ops. The same environment variable that compiles the runtime
// counters in decides whether the compiler emits their calls, which is what
// keeps the two artifacts differing in instrumentation and nowhere else.

// carrierCounterEnvVar names the build-time switch. It is the variable the
// runtime build already reads, so an artifact cannot be built with the counters
// linked but not called, or called but not linked.
const carrierCounterEnvVar = "SURGE_INTERNAL_CARRIER_BENCH_COUNTERS"

// carrierCountersEnabled reports whether this build records physical carrier
// operations.
func carrierCountersEnabled() bool {
	return os.Getenv(carrierCounterEnvVar) == "1"
}

// emitCloneCopyCounter records one generated duplication of `size` bytes.
//
// It sits at the top of the clone glue, after the byte move and before the
// member fixups, so that a nested composite's own clone records its own bytes
// and its own callback. Counting at the physical operation is what makes a
// clone storm visible: the callback count is the number of generated bodies
// that ran, whatever the source looked like.
func (e *Emitter) emitCloneCopyCounter(size uint64) {
	if !carrierCountersEnabled() {
		return
	}
	if size != 0 {
		// A zero-byte operation is deliberately not an event.
		fmt.Fprintf(&e.buf, "  call void @rt_carrier_bench_record_copy(i64 %d)\n", size)
	}
	fmt.Fprintf(&e.buf, "  call void @rt_carrier_bench_record_callback()\n")
}

// carrierCounterDecls are the declarations an instrumented build needs. A
// timing build emits none of them, so its symbol table stays clean.
func carrierCounterDecls() []builtinDecl {
	if !carrierCountersEnabled() {
		return nil
	}
	return []builtinDecl{
		{name: "rt_carrier_bench_record_copy", ret: "void", params: []string{"i64"}},
		{name: "rt_carrier_bench_record_callback", ret: "void", params: nil},
	}
}
