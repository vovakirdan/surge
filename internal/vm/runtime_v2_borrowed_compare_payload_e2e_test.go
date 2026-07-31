//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A compare that only READS its union owns nothing it takes out of it.
//
// `compare *arg { ... }` is the ordinary way to look at a borrowed union — it
// is how `core/format.sg` reads a `&FmtArg`, and how `stdlib/json` walks a
// borrowed value. The deref strips the reference, so the subject's TYPE is a
// bare union and a payload binding read out of it looked like a value the arm
// owned. It is not: the union belongs to whoever the borrow points at, so
// releasing the payload at the arm's end frees the caller's storage. Every
// formatted print with a string argument did exactly that.
//
// The opposite rows are what make this a contract rather than a patch, because
// each one wants the obligation KEPT:
//
//   - a `@copy` union read through the same deref is CLONED into the compare,
//     so the payload really is the arm's;
//   - a reference-counted scalar payload takes a reference of its own even from
//     a borrowed union, so the arm gives it back;
//   - an owned subject transfers its payload into the arm as it always did.
//
// The caller reads its value AFTER every borrow, which is the only way a
// release too many shows up as anything but a leak, and the leak column is
// asserted at strict zero, because that is the direction this fix could trade
// into.
const runtimeV2BorrowedComparePayloadSource = `
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
    return len(x) to int;
}

// The defect itself: an arm that only LOOKS at a payload of a borrowed union.
fn reads_payload(slot: &Slot) -> int {
    return compare *slot {
        Payload(s) => peek(&s);
        Empty() => 0;
    };
}

// The same shape as core/format.sg's append_fmt_arg: the payload feeds a concat
// that builds a new string, and the union keeps its own.
fn appends_payload(out: string, slot: &Slot) -> string {
    return compare *slot {
        Payload(s) => out + s;
        Empty() => out;
    };
}

// A @copy union through the same deref: the read CLONES, so this arm owns its
// payload and must still release it. Kept in the same file as the row above
// because the two answers are opposite and one predicate decides both.
fn reads_copy_payload(h: &Holder) -> int {
    return compare *h {
        Held(c) => c.a;
        _ => 0 - 1;
    };
}

// A reference-counted scalar payload of a BORROWED union: the extraction
// retains, so the arm holds a reference of its own and gives it back.
fn reads_float_payload(m: &Measure) -> float {
    return compare *m {
        Reading(v) => v;
        _ => 0.0;
    };
}

// A compare over something that is NOT a union at all: the subject moves into
// the pattern's binding, which is then its only owner. The borrowed-union rule
// must not reach this — a review caught exactly that, one leaked string per
// evaluation.
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
    let mut i = 0;
    let mut acc = 0;
    while i < 16 {
        let slot: Slot = Payload(build("v-"));
        // 6, then 10 for "out:" + the payload, then the OWNER read after both
        // borrows — still six characters long, which is the whole assertion.
        acc = acc + reads_payload(&slot);
        let rendered = appends_payload("out:", &slot);
        acc = acc + peek(&rendered);
        acc = acc + compare slot {
            Payload(s) => peek(&s);
            Empty() => 0;
        };
        i = i + 1;
    }
    if acc != 352 {
        print("a borrowed payload did not survive its readers");
        print(acc to string);
        return 1;
    }

    let held: Holder = Held(Cell { a = 1, b = 2 });
    let mut copies = 0;
    i = 0;
    while i < 16 {
        copies = copies + reads_copy_payload(&held);
        i = i + 1;
    }
    if copies != 16 {
        print("a cloned payload went wrong");
        return 2;
    }

    let measure: Measure = Reading(1.5 + 0.25);
    let mut total: float = 0.0;
    i = 0;
    while i < 16 {
        total = total + reads_float_payload(&measure);
        i = i + 1;
    }
    if total != 28.0 {
        print("a borrowed float payload went wrong");
        return 3;
    }

    if owns_its_subject(0) != 6 {
        print("an owned subject went wrong");
        return 4;
    }

    let mut plain = 0;
    i = 0;
    while i < 16 {
        plain = plain + binds_a_plain_value();
        i = i + 1;
    }
    if plain != 96 {
        print("a plain bound subject went wrong");
        return 5;
    }

    print("borrowed-compare-payload-ok");
    return 0;
}
`

func TestRuntimeV2BorrowedComparePayloadStaysTheOwners(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2BorrowedComparePayloadSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("a borrowed compare payload hit a memcheck error (invalid read / invalid free)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("borrowed-compare-payload probe failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "borrowed-compare-payload-ok") {
		t.Fatalf("borrowed-compare-payload probe missing completion marker; stdout=%q", stdout)
	}
	// The rows that KEEP their obligation are what this column watches: drop it
	// for them and the payload they really own is abandoned.
	lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("borrowed compare payload: %v\nstderr:\n%s", err, stderr)
	}
	if lostBytes != 0 || lostBlocks != 0 {
		t.Fatalf("borrowed compare payload leaked %d bytes in %d blocks; want strict zero\nstderr:\n%s",
			lostBytes, lostBlocks, stderr)
	}
}

func TestBorrowedComparePayloadSurvivesTheHeapSanitizer(t *testing.T) {
	res := runProgramFromSource(t, runtimeV2BorrowedComparePayloadSource, runOptions{})
	if res.exitCode != 0 {
		t.Fatalf("borrowed-compare-payload probe failed (exit=%d)\nstderr:\n%s", res.exitCode, res.stderr)
	}
	if strings.TrimSpace(res.stderr) != "" {
		t.Fatalf("borrowed-compare-payload probe reported a runtime error:\n%s", res.stderr)
	}
}
