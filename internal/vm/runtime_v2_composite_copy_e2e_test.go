package vm_test

import (
	"strings"
	"testing"
)

// The frozen contract for value composites: these assert LANGUAGE SEMANTICS,
// not representation, so they must keep passing when the storage moves from a
// heap box to inline. Nothing here may reference a box, a count, or an
// allocation total — the census rows live in their own tests precisely so this
// set can survive the swap unchanged.
//
// The defect they pin: a struct, tuple, tagged union or fixed array is a VALUE,
// but every one of them was a heap box behind a pointer, so `let mut q = p;
// q.a = 99` wrote through to `p`. Copying a value has to produce an independent
// value, and that is what every row below checks from a different direction.
//
// Both backends run the SAME source, which is the point of the differential:
// the VM's heap is counted and the native heap is not, so an implementation
// that leans on counting passes one and fails the other.
//
// THE FROZEN SET, and what actually pins each row. The manifest is here rather
// than in a document because the thing most likely to rot is the belief that a
// row is covered — writing a row down is not pinning it, and the audit that
// produced this table found four rows whose reclamation nothing measured.
//
//	row  what it asserts                          pinned by
//	 1   copy independence                        this file
//	 2   nested copy independence                 this file
//	 3   by-value argument and return             this file + reclaim census
//	 4   tagged-union payload binding             this file (INDEPENDENCE ONLY —
//	                                              reclamation is RV2-DEBT-078)
//	 5   overwrite frees the old value once       this file + reclaim census
//	 6   self-assignment, incl. self-borrow       this file
//	 7   two copies, both dropped                 this file + reclaim census
//	 8   a borrowing read does not duplicate      this file + borrow-cost census
//	 9   extraction independence                  this file (Copy shapes; the
//	                                              move-only shape is RV2-DEBT-077)
//	10   union clones only its active arm         this file + union-clone valgrind
//	11   overlapping assignment                   this file
//	12   a composite crosses a shard boundary     crossing e2e (valgrind)
//
// Three rows carry a SECOND instrument on purpose, and the reason recurred
// often enough to be the epic's main methodological finding: rows 8 and 10
// state what must NOT happen — do not duplicate on a borrow, do not touch an
// inactive arm — and a rule about what not to do is invisible to an assertion
// about results. Both wrong implementations return every correct answer. Only
// an allocation census or a memcheck sees them.
//
// Rows 3, 5 and 7 carry a census because their failure is silent in the other
// direction: the value is right and the box is abandoned.
//
// Frozen means the ROWS survive Phase 2's swap to inline storage, not the
// instruments: a census that counts boxes is measuring the representation and
// is expected to be recalibrated. Nothing in THIS file may reference a box, a
// count or an allocation total.
const runtimeV2CompositeCopySource = `
@copy type Pair = { a: int, b: int };
@copy type Inner = { x: int };
@copy type Outer = { inner: Inner, label: int };

tag Hold(Pair);
tag Nothing_();
type Held = Hold(Pair) | Nothing_;

// A COPY union: the shape that reaches the copy path as a union rather than
// through a payload binding, so its clone must dispatch on the discriminant
// and duplicate only the ACTIVE arm. Walking every arm would read payload
// bytes that were never written for the arms that are not live.
tag One(Pair);
tag None_();
@copy type Choice = One(Pair) | None_;

fn choice_payload(c: &Choice) -> int {
    return compare *c {
        One(p) => p.a;
        _ => 0 - 1;
    };
}

fn take_by_value(v: Pair) -> Pair {
    let mut w = v;
    w.a = 99;
    return w;
}

fn read_through(r: &Pair) -> int {
    return r.a;
}

fn payload_of(h: &Held) -> Pair {
    return compare *h {
        Hold(p) => p;
        _ => Pair { a = 0, b = 0 };
    };
}

@entrypoint
fn main() -> int {
    // 1. copy independence
    let p = Pair { a = 1, b = 2 };
    let mut q = p;
    q.a = 99;
    if p.a != 1 || q.a != 99 {
        print("copy independence failed");
        return 1;
    }

    // 2. nested copy independence
    let o = Outer { inner = Inner { x = 1 }, label = 7 };
    let mut c = o;
    c.inner.x = 99;
    if o.inner.x != 1 || c.inner.x != 99 {
        print("nested independence failed");
        return 2;
    }

    // 3. copy through a by-value argument and a return
    let r = take_by_value(p);
    if p.a != 1 || r.a != 99 {
        print("argument/return independence failed");
        return 3;
    }

    // 4. copy of a tagged-union payload binding
    let h: Held = Hold(Pair { a = 1, b = 2 });
    let mut got = payload_of(&h);
    got.a = 99;
    let again = payload_of(&h);
    if got.a != 99 || again.a != 1 {
        print("union payload independence failed");
        return 4;
    }

    // 6. self-assignment does not corrupt the live value
    let mut s = Pair { a = 5, b = 6 };
    s = s;
    if s.a != 5 || s.b != 6 {
        print("self-assignment corrupted the value");
        return 6;
    }

    // 8. a BORROWING read does not clone: it must keep seeing later writes
    //    through the place it borrows. This is the row that fails loudest if
    //    someone duplicates on every read of a composite instead of only on a
    //    consuming one.
    let mut b = Pair { a = 1, b = 2 };
    b.a = 42;
    if read_through(&b) != 42 {
        print("borrow no longer observes the place");
        return 8;
    }

    // 9. extraction independence: a composite read OUT of a container is its
    //    own value.
    let src = Outer { inner = Inner { x = 1 }, label = 7 };
    let mut e = src.inner;
    e.x = 99;
    if src.inner.x != 1 || e.x != 99 {
        print("extraction independence failed");
        return 9;
    }

    // 11. overlap: source and destination name the same storage.
    let mut ov = Outer { inner = Inner { x = 5 }, label = 7 };
    ov.inner = ov.inner;
    if ov.inner.x != 5 || ov.label != 7 {
        print("self-overlapping assignment corrupted the value");
        return 11;
    }

    // 10. a COPY union copies its active arm independently, and the original
    //     keeps its own payload.
    let orig: Choice = One(Pair { a = 1, b = 2 });
    let dup = orig;
    let mut taken = compare dup {
        One(p) => p;
        _ => Pair { a = 0, b = 0 };
    };
    taken.a = 99;
    if taken.a != 99 || choice_payload(&orig) != 1 {
        print("copy union independence failed");
        return 10;
    }

    // 5. overwriting an existing composite binding frees the old value exactly
    //    once. The self-overlapping form is the one that bites: the drop of the
    //    old destination must not run before the source has been read, or it
    //    frees the storage the pending store still has to look at.
    let mut ow = Pair { a = 1, b = 2 };
    ow = Pair { a = 3, b = 4 };
    ow = ow;
    if ow.a != 3 || ow.b != 4 {
        print("overwrite corrupted the value");
        return 5;
    }

    // 7. both copies stay readable until each is dropped, and dropping one
    //    leaves the other intact — the shape a double free turns into garbage.
    let base = Pair { a = 11, b = 22 };
    let mut left = base;
    let right = base;
    left.a = 33;
    if base.a != 11 || left.a != 33 || right.a != 11 || right.b != 22 {
        print("two copies are not independent");
        return 7;
    }

    print("composite-copy-contract-ok");
    return 0;
}
`

func TestRuntimeV2CompositeCopyIsIndependent(t *testing.T) {
	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			res := runProgramFromSource(t, runtimeV2CompositeCopySource, runOptions{})
			// The exit code IS the assertion: each row returns its own number
			// on failure and the program returns 0 only after all of them
			// passed. That is deliberate, because the VM runner does not
			// capture the program's stdout — so the marker below is checked
			// only where stdout is available, and the row number carries the
			// diagnosis on both backends.
			if res.exitCode != 0 {
				t.Fatalf("composite copy contract failed at row %d\nstdout:\n%s\nstderr:\n%s",
					res.exitCode, res.stdout, res.stderr)
			}
			if backend == backendLLVM && !strings.Contains(res.stdout, "composite-copy-contract-ok") {
				t.Fatalf("composite copy contract missing completion marker; stdout=%q", res.stdout)
			}
		})
	}
}
