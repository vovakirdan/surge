package mir_test

import (
	"testing"

	"surge/internal/mir"
)

// The other half of the acceptance gate: the SAME ownership shapes, compiled
// through today's fixed compiler, must come out clean.
//
// A hand-built reconstruction proves the pass can see a defect. Only running it
// over real lowering output proves it does not see one where there is none —
// and that is what makes the reconstructions above evidence rather than
// self-fulfilling.
//
// This source is the reproducer from the borrowed-compare-payload e2e suite,
// trimmed to what a single-file unit compile can resolve. Every function here
// is one of the shapes a row this session closed turned on.
const ownershipCorpusSource = `
tag Payload(string);
tag Empty();
type Slot = Payload(string) | Empty;

@copy type Cell = { a: int, b: int };
tag Held(Cell);
tag Absent();
@copy type Holder = Held(Cell) | Absent;

tag Reading(float);
tag NoReading();
type Measure = Reading(float) | NoReading;

fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn peek(x: &string) -> int {
    return 1;
}

// RV2-DEBT-100's program: an arm that only LOOKS at a payload of a borrowed
// union.
fn reads_payload(slot: &Slot) -> int {
    return compare *slot {
        Payload(s) => peek(&s);
        Empty() => 0;
    };
}

// The same shape as core/format.sg's append_fmt_arg: the payload feeds a
// concat that builds a new string, and the union keeps its own.
fn appends_payload(out: string, slot: &Slot) -> string {
    return compare *slot {
        Payload(s) => out + s;
        Empty() => out;
    };
}

// RV2-DEBT-052's residual: a @copy union through the same deref CLONES, so the
// arm owns its payload and must still release it.
fn reads_copy_payload(h: &Holder) -> int {
    return compare *h {
        Held(c) => c.a;
        _ => 0 - 1;
    };
}

// A reference-counted scalar payload of a BORROWED union: the extraction
// retains, so the arm holds a reference of its own.
fn reads_float_payload(m: &Measure) -> float {
    return compare *m {
        Reading(v) => v;
        _ => 0.0;
    };
}

// A compare over something that is not a union at all: the subject moves into
// the binding, which is then its only owner.
fn binds_a_plain_value() -> int {
    let text = build("v-");
    return compare text {
        s => peek(&s);
    };
}

// The control: an OWNED subject, where the payload transfers into the arm.
fn owns_its_subject(i: int) -> int {
    let slot: Slot = Payload(build("v-"));
    return compare slot {
        Payload(s) => peek(&s) + i;
        Empty() => 0;
    };
}

@entrypoint
fn main() -> int {
    let slot: Slot = Payload(build("v-"));
    let mut n = reads_payload(&slot);
    let joined = appends_payload(build("o-"), &slot);
    n = n + peek(&joined);
    let held: Holder = Held(Cell { a: 1, b: 2 });
    n = n + reads_copy_payload(&held);
    let measure: Measure = Reading(1.5);
    let f: float = reads_float_payload(&measure);
    n = n + owns_its_subject(0) + binds_a_plain_value();
    return n;
}
`

func TestOwnershipCorpusIsCleanOnFixedShapes(t *testing.T) {
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipCorpusSource)
	findings := mir.VerifyOwnership(mod, typesIn, semaRes)

	// Every function whose ownership shape a closed row turned on.
	for _, fn := range []string{
		"reads_payload",
		"appends_payload",
		"reads_copy_payload",
		"binds_a_plain_value",
		"owns_its_subject",
		"build",
		"main",
	} {
		if got := findingsIn(findings, fn); len(got) != 0 {
			t.Errorf("%s should be clean, got:\n%s", fn, joinLines(got))
		}
	}
}

// One function in the corpus above is NOT clean, and pinning it here is the
// point: it is the pass's first real candidate finding, held as a fact rather
// than filed away.
//
// `reads_float_payload` returns its reference-counted scalar result as a bare
// `copy` of a local that was correctly retained on both arms. Every DEFINITION
// reaching that return mints; the USE occupying the sink is an unretained
// alias. A string return at the same position lowers as `move`. Whether the
// right answer is a lowering change or an allowlist entry is Step 2's call, not
// this step's — Step 1 reports, and this test says exactly what it reports so
// that the answer is a deliberate decision rather than a silent drift.
func TestOwnershipCorpusReportsTheScalarReturnCandidate(t *testing.T) {
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipCorpusSource)
	findings := mir.VerifyOwnership(mod, typesIn, semaRes)

	got := findingsIn(findings, "reads_float_payload")
	if len(got) != 1 {
		t.Fatalf("expected exactly one candidate finding, got:\n%s", joinLines(got))
	}
	const want = "reads_float_payload: return of L1(tmp_block1) (def use) at bb1#term"
	if got[0] != want {
		t.Fatalf("candidate finding changed:\n  got  %s\n  want %s", got[0], want)
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += "  " + l + "\n"
	}
	return out
}
