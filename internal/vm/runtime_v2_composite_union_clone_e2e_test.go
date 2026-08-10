package vm_test

import (
	"strings"
	"testing"
	"time"
)

// Cloning a COPY union must duplicate only the ACTIVE arm's payload.
//
// It lives apart from the copy contract for the same reason the borrow-cost
// census does, and the reason is worth stating because it recurred: walking
// every arm has no semantic signature. The arms' payload slots overlap, so a
// clone that walks them all still produces the right ANSWER — it just also
// reads the live arm's bytes as some other arm's type. With a single
// payload-carrying arm it is not even wrong, which is why the contract's union
// row could not catch it; with two arms of different shape it dereferences a
// pointer read out of bytes that were never a pointer.
//
// So the instrument is valgrind, not an assertion. Measured: the wrong version
// prints the correct answer and reports `Invalid read of size 8`.
//
// The two arms must differ in payload SHAPE, not just in name — that is what
// puts a non-pointer where the other arm keeps a pointer.
const runtimeV2CompositeUnionCloneSource = `
@copy type One = { a: int };
@copy type Two = { x: int, y: int };

tag AsOne(One);
tag AsTwo(Two);
@copy type Either = AsOne(One) | AsTwo(Two);

fn value_of(e: &Either) -> int {
    return compare *e {
        AsOne(o) => o.a;
        AsTwo(t) => t.x;
    };
}

@entrypoint
fn main() -> int {
    let a: Either = AsOne(One { a = 1 });
    let a_copy = a;
    if value_of(&a) != 1 || value_of(&a_copy) != 1 {
        print("first arm copied wrong");
        return 1;
    }

    let b: Either = AsTwo(Two { x = 7, y = 8 });
    let b_copy = b;
    if value_of(&b) != 7 || value_of(&b_copy) != 7 {
        print("second arm copied wrong");
        return 2;
    }

    print("union-clone-active-arm-ok");
    return 0;
}
`

func TestRuntimeV2CompositeUnionCloneTouchesOnlyActiveArm(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CompositeUnionCloneSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("cloning a union touched a non-active arm (valgrind memcheck error)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("union clone probe failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "union-clone-active-arm-ok") {
		t.Fatalf("union clone probe missing completion marker; stdout=%q", stdout)
	}
}
