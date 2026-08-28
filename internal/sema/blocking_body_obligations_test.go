package sema

import (
	"context"
	"sort"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
)

// A blocking body owns what moves into it and what it builds, and sema says so
// in the same obligation maps every other function body uses.
//
// It did not, for a long time: `dropObligationsSuppressed` muted every recording
// path while `blockingDepth > 0`, on the theory that the pool's release would
// reclaim the state. The pool's release could only ever free the STATE BLOCK
// -- it never covered a local the body built, and once the job
// destroys its captures through their own descriptor (a-2) the body's unpack is
// a transfer, so a capture the body only reads has nobody else to drop it. The
// suppression is gone; these rows pin what a blocking body records now.
//
// Uses the placement-crossing prelude from on_crossing_test.go so `Task<T>`
// resolves. Every row asserts something POSITIVE -- a named binding in a named
// obligation -- because an assertion of absence is green whenever the program
// never reached the walker.

// blockingObligations runs parse + sema over the prelude plus src and returns
// the early-exit and scope-end obligations by binding name, one entry per
// recording site. It refuses a program sema rejected: an obligation table for a
// program that did not type is a table for nothing.
func blockingObligations(t *testing.T, src string) (earlyExit, scopeEnd [][]string) {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, onCrossingPrelude+src)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
	names := func(ids []symbols.SymbolID) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			sym := symRes.Table.Symbols.Get(id)
			if sym == nil {
				t.Fatalf("obligation names an unknown symbol %v", id)
			}
			out = append(out, builder.StringsInterner.MustLookup(sym.Name))
		}
		sort.Strings(out)
		return out
	}
	collect := func(m map[ast.StmtID][]symbols.SymbolID) [][]string {
		out := make([][]string, 0, len(m))
		for _, ids := range m {
			out = append(out, names(ids))
		}
		sort.Slice(out, func(i, j int) bool { return strings.Join(out[i], ",") < strings.Join(out[j], ",") })
		return out
	}
	return collect(res.EarlyExitDrops), collect(res.ScopeEndDrops)
}

func hasObligation(rows [][]string, want ...string) bool {
	sort.Strings(want)
	for _, row := range rows {
		if strings.Join(row, ",") == strings.Join(want, ",") {
			return true
		}
	}
	return false
}

// The capture the body READS and the local the body BUILDS are both released
// at the body's `ret`. This is the row that was empty under the suppression:
// the body's exit recorded nothing, so the string built inside leaked and the
// moved-in capture -- spent out of the state by the unpack -- had no owner
// left.
func TestBlockingBodyRecordsItsCaptureAndLocalAtReturn(t *testing.T) {
	earlyExit, _ := blockingObligations(t, `
fn len_of(s: &string) -> int { return 1; }
fn f() -> Task<int> {
	let s: string = "moved in";
	return blocking {
		let local: string = "built inside";
		ret len_of(&s);
	};
}`)
	if !hasObligation(earlyExit, "local", "s") {
		t.Fatalf("the blocking body's return releases neither its capture nor its local; early-exit obligations: %v", earlyExit)
	}
}

// A capture the body CONSUMES is not released a second time. The unpack moved
// it out of the state; the call moved it out of the body's binding; the return
// finds nothing live. Only the local is still the body's to drop.
func TestBlockingBodyConsumedCaptureIsNotDroppedAgain(t *testing.T) {
	earlyExit, _ := blockingObligations(t, `
fn eat(s: string) -> int { return 1; }
fn len_of(s: &string) -> int { return 1; }
fn f() -> Task<int> {
	let s: string = "moved in";
	return blocking {
		let local: string = "built inside";
		ret eat(s) + len_of(&local);
	};
}`)
	if hasObligation(earlyExit, "local", "s") {
		t.Fatalf("a capture the body consumed is dropped again at its return: %v", earlyExit)
	}
	if !hasObligation(earlyExit, "local") {
		t.Fatalf("the blocking body's return does not release the local it built; early-exit obligations: %v", earlyExit)
	}
}

// The body is a function root of its own: its `ret` stops collecting at the
// blocking boundary and never reaches the ENCLOSING function's droppables. The
// enclosing `keep` is released by the enclosing function's exit, not by the
// body's -- a body that dropped it would free a value its caller still holds.
func TestBlockingBodyReturnStopsAtTheBlockingBoundary(t *testing.T) {
	earlyExit, scopeEnd := blockingObligations(t, `
fn len_of(s: &string) -> int { return 1; }
fn f() -> int {
	let keep: string = "the caller's";
	let s: string = "moved in";
	let t: Task<int> = blocking {
		ret len_of(&s);
	};
	return len_of(&keep);
}`)
	if !hasObligation(earlyExit, "s") {
		t.Fatalf("the blocking body's return does not release its capture; early-exit obligations: %v", earlyExit)
	}
	for _, row := range earlyExit {
		for _, name := range row {
			if name == "keep" && len(row) == 1 {
				continue
			}
			if name == "keep" {
				t.Fatalf("the blocking body's return collected the caller's binding: %v", earlyExit)
			}
		}
	}
	// The caller's exit still owns `keep` and only `keep`: `s` moved into the
	// body, and `t` is an opaque struct in this prelude. What leaves the walker
	// for the enclosing function is unchanged by the body having a root.
	if !hasObligation(earlyExit, "keep") {
		t.Fatalf("the enclosing return lost its own obligations: early-exit %v scope-end %v", earlyExit, scopeEnd)
	}
}

// A `@copy` value composite is the one capture shape the caller keeps: the body
// receives a copy, the caller's binding stays live, and -- because `@copy` is
// admitted only over Copy fields, none of which own heap -- neither side has
// anything to drop for it. The positive half is the local beside it: the body
// still records what it builds, so the row cannot pass by never reaching the
// walker.
func TestBlockingBodyCopyCompositeCaptureIsNotAnObligation(t *testing.T) {
	earlyExit, _ := blockingObligations(t, `
@copy
type Pair = { a: int, b: int };
fn len_of(s: &string) -> int { return 1; }
fn f() -> int {
	let p: Pair = Pair{ a: 4, b: 2 };
	let t: Task<int> = blocking {
		let local: string = "built inside";
		ret p.a + len_of(&local);
	};
	return p.a + p.b;
}`)
	if !hasObligation(earlyExit, "local") {
		t.Fatalf("the blocking body's return does not release the local it built; early-exit obligations: %v", earlyExit)
	}
	for _, row := range earlyExit {
		for _, name := range row {
			if name == "p" {
				t.Fatalf("a @copy composite capture was recorded as a drop obligation: %v", earlyExit)
			}
		}
	}
}
