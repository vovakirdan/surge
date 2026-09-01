// Package panicgate enumerates every place the compiler and the C runtime can
// raise a panic, and refuses one that is neither recorded by a behavioural
// fixture nor excused by an owned entry in the allowlist.
//
// The corpus compares the whole panic report - message, code, location and
// backtrace - on both backends, which is the only thing that can notice the two
// disagreeing. What it could not notice was a panic nothing reaches: three
// emitter messages drifted away from the VM's wording for the same condition
// precisely because no fixture named them. This package makes that a failure
// instead of a silence.
package panicgate

import (
	"fmt"
	"sort"
	"strings"
)

// Computed is the message of a raise whose text is decided at run time - a
// buffer the runtime formats, or the program's own `panic(...)` argument. Such
// a site can never be matched against a recorded message, so it can only be
// carried by an allowlist entry.
const Computed = "<computed>"

// Site is one place that can raise a panic.
//
// The identity is deliberately NOT the file and line. A line moves whenever
// anything above it moves, so a line-keyed allowlist goes stale on edits that
// have nothing to do with panics, and the reflex for a gate that cries wolf is
// to re-record it without reading it. The identity is also deliberately not the
// message: the message is the thing most likely to be corrected, and keying on
// it means a wording fix silently drops the entry that explained the site.
//
// The key is file + enclosing function + the raise's ordinal within that
// function. All three survive an unrelated edit anywhere else in the file, and
// the message travels alongside as a checked field, so a wording change is
// reported as a wording change at a site that is still recognised.
type Site struct {
	File     string // repository-relative
	Function string
	Ordinal  int // 1-based, among the raises in this function
	Raiser   string
	Message  string
	Line     int // for the reader; never part of the key
}

// Key is the stable identity described above.
func (s *Site) Key() string {
	return fmt.Sprintf("%s::%s#%d", s.File, s.Function, s.Ordinal)
}

func (s *Site) String() string {
	return fmt.Sprintf("%s (%s:%d, raiser %s) message %q", s.Key(), s.File, s.Line, s.Raiser, s.Message)
}

// raiser is a function that reports a panic, and the argument that decides
// which panic it reports.
//
// Arg is the index of that deciding argument. Resolve turns the literal found
// there into the message the user sees: for a reporter that takes the message
// itself this is the identity, and for the bounds reporter, which takes a kind
// and formats the text, it is the kind table.
type raiser struct {
	Name    string
	Arg     int
	Resolve func(literal string) (string, bool)
}

func identityResolve(literal string) (string, bool) { return literal, true }

// boundsResolve turns rt_panic_bounds' kind argument into the message it
// prints. The digits are the runtime's own operands and are normalised away by
// the corpus reader, so the two sides compare as templates.
func boundsResolve(literal string) (string, bool) {
	switch strings.TrimSpace(literal) {
	case "0":
		return "index {} out of bounds for length {}", true
	case "1":
		return "array index {} out of range for length {}", true
	default:
		return "", false
	}
}

// primitiveRaisers are the reporters that actually write to stderr and exit.
// Everything else that can raise is discovered from them: a function that hands
// one of these its own parameter is not a site, it is a forwarder, and its own
// callers are the sites. See derive below.
func primitiveRaisers() map[string][]raiser {
	return map[string][]raiser{
		// Declared in runtime/native/rt.h.
		// The fatal emitter covers both internal PANIC and the terminal codes
		// (RT_OOM/RT_TRAP). Keeping it in this raiser graph preserves the census
		// over every process-ending report even though only one code is a panic.
		"rt_fatal_static":  {{Name: "rt_fatal_static", Arg: 1, Resolve: identityResolve}},
		"rt_panic":         {{Name: "rt_panic", Arg: 0, Resolve: identityResolve}},
		"rt_panic_numeric": {{Name: "rt_panic_numeric", Arg: 0, Resolve: identityResolve}},
		"rt_panic_code":    {{Name: "rt_panic_code", Arg: 2, Resolve: identityResolve}},
		"rt_panic_bounds":  {{Name: "rt_panic_bounds", Arg: 0, Resolve: boundsResolve}},
		// rt_bignum_panic.c reports without going through rt.h: it formats the
		// whole "panic VM<code>: <msg>" line itself and exits. It is seeded by
		// hand for that reason, and TestPanicReportersAreAllKnown is what stops
		// a second hand-rolled reporter from being missed the same way.
		"panic_with_code": {{Name: "panic_with_code", Arg: 1, Resolve: identityResolve}},
		// The carrier benchmark signal handler emits its diagnostic with raw
		// write(2) and exits, but has no caller-supplied panic message to follow.
		"on_signal": nil,
	}
}

// emitterPrimitiveRaisers are the emitter helpers that write a call to one of
// the runtime reporters into the module. They are named rather than derived
// because the message reaches the emitted call through a string constant the
// helper interns, not through the call's own argument list, so there is no
// argument to follow. emitterEmissionSites cross-checks that no OTHER function
// writes such a call unnoticed.
func emitterPrimitiveRaisers() map[string][]raiser {
	return map[string][]raiser{
		"emitPanic":        {{Name: "emitPanic", Arg: 0, Resolve: identityResolve}},
		"emitPanicBlock":   {{Name: "emitPanicBlock", Arg: 1, Resolve: identityResolve}},
		"emitPanicNumeric": {{Name: "emitPanicNumeric", Arg: 0, Resolve: identityResolve}},
		"emitPanicCoded":   {{Name: "emitPanicCoded", Arg: 1, Resolve: identityResolve}},
		"emitPanicBounds":  {{Name: "emitPanicBounds", Arg: 0, Resolve: boundsResolve}},
	}
}

// forward records that a function passes one of its own parameters to a raiser,
// which makes the function a raiser in turn.
type forward struct {
	Function string
	Arg      int
	Resolve  func(string) (string, bool)
}

// addForwards folds newly discovered forwards into the raiser table and reports
// whether anything was learned, which is what ends the fixed-point loop. A
// function can forward more than one argument - the ready-queue deque takes an
// overflow message and an allocation message - so a name carries a variant per
// deciding argument rather than a single entry.
func addForwards(raisers map[string][]raiser, forwards []forward) bool {
	added := false
	for _, fwd := range forwards {
		known := false
		for _, existing := range raisers[fwd.Function] {
			if existing.Arg == fwd.Arg {
				known = true
				break
			}
		}
		if known {
			continue
		}
		raisers[fwd.Function] = append(raisers[fwd.Function], raiser{
			Name: fwd.Function, Arg: fwd.Arg, Resolve: fwd.Resolve,
		})
		sort.Slice(raisers[fwd.Function], func(i, j int) bool {
			return raisers[fwd.Function][i].Arg < raisers[fwd.Function][j].Arg
		})
		added = true
	}
	return added
}

// sortSites orders sites by their key so the gate's report and the allowlist
// read in the same order every run.
func sortSites(sites []Site) {
	sort.Slice(sites, func(i, j int) bool { return sites[i].Key() < sites[j].Key() })
}
