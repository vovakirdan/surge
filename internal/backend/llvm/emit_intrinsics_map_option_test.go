package llvm

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The three map intrinsics that answer an `Option` — a lookup, an insert and a
// removal — build it through ONE emitter, so the shape below is the shape all
// three have.
//
// This is a pin rather than a regression test: nothing was misbehaving before
// the three copies were folded into one, and the fold's own proof is that the
// emitted IR does not change. What the pin buys is that the NEXT change to the
// payload contract has to change this shape once and deliberately, instead of
// changing it in two call sites out of three and leaving the third to be found
// by whoever compiles a map next.
func TestMapIntrinsicsBuildTheirOptionThroughOneEmitter(t *testing.T) {
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	t.Setenv("SURGE_STDLIB", repoRoot)

	sourceCode := `@entrypoint
fn main() -> int {
    let mut m = Map::<string, int>.new();
    let _ = m.insert("a", 1);
    {
        let hit = m.get_mut(&"a");
        compare hit {
            Some(p) => { *p = *p + 1; }
            _ => {}
        }
    }
    return compare m.remove(&"a") {
        Some(v) => v;
        nothing => 0;
    };
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	for _, intrinsic := range []string{"rt_map_insert", "rt_map_get_mut", "rt_map_remove"} {
		assertMapOptionSkeleton(t, ir, intrinsic)
	}
}

// assertMapOptionSkeleton checks the one Option-building shape: a destination
// slot, a two-armed branch on the runtime's hit flag, a `Some` arm and a
// `nothing` arm that both store into that same slot, and a join that reads it
// back. Naming the slot is the point — an arm that stored somewhere else would
// publish a value nobody reads, which is precisely how three hand-written
// copies drift.
func assertMapOptionSkeleton(t *testing.T, ir, intrinsic string) {
	t.Helper()

	callRe := regexp.MustCompile(`(%t\d+) = call i1 @` + intrinsic + `\(`)
	call := callRe.FindStringSubmatchIndex(ir)
	if call == nil {
		t.Fatalf("no %s call in emitted IR:\n%s", intrinsic, ir)
	}
	okVal := ir[call[2]:call[3]]
	rest := ir[call[1]:]
	if end := strings.IndexByte(rest, '\n'); end >= 0 {
		rest = rest[end+1:]
	}
	// SSA names restart in every function, so the search has to stop at this
	// one's closing brace or a later function's `%t8` answers for this one's.
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		rest = rest[:end]
	}

	slotRe := regexp.MustCompile(`^\s*(%t\d+) = alloca ptr, align \d+\n`)
	lines := strings.SplitAfter(rest, "\n")
	if len(lines) == 0 {
		t.Fatalf("%s: nothing follows the call:\n%s", intrinsic, ir)
	}
	slotMatch := slotRe.FindStringSubmatch(lines[0])
	if slotMatch == nil {
		t.Fatalf("%s: expected an Option destination slot right after the call, got %q", intrinsic, lines[0])
	}
	slot := slotMatch[1]

	branchRe := regexp.MustCompile(`br i1 ` + regexp.QuoteMeta(okVal) + `, label %(\S+), label %(\S+)\n`)
	branch := branchRe.FindStringSubmatch(rest)
	if branch == nil {
		t.Fatalf("%s: the hit flag %s does not select between a Some arm and a nothing arm:\n%s",
			intrinsic, okVal, rest)
	}

	storeRe := regexp.MustCompile(`store ptr (%t\d+), ptr ` + regexp.QuoteMeta(slot) + `\n`)
	stores := storeRe.FindAllStringSubmatch(rest, -1)
	if len(stores) != 2 {
		t.Fatalf("%s: expected both arms to store into %s, found %d such stores:\n%s",
			intrinsic, slot, len(stores), rest)
	}
	if stores[0][1] == stores[1][1] {
		t.Fatalf("%s: both arms stored the same value %s into %s", intrinsic, stores[0][1], slot)
	}

	joinRe := regexp.MustCompile(`(%t\d+) = load ptr, ptr ` + regexp.QuoteMeta(slot) + `\n`)
	if joinRe.FindStringSubmatch(rest) == nil {
		t.Fatalf("%s: the join never reads the Option back out of %s:\n%s", intrinsic, slot, rest)
	}
}
