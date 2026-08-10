package vm_test

import (
	"strings"
	"testing"
	"time"
)

// Field-read aliasing: `let body = holder.field;` reads a non-copy field
// WITHOUT a tracked partial move (sema does not track those yet), so the
// binding shares the container's heap handle. Synthesizing a scope-exit
// drop for such an alias would free memory the container still owns —
// caught here as a double free / use-after-free (the fixture returns 0
// only if the container's field is still readable after the alias'
// scope ended and the container itself is later dropped).
const runtimeV2DropFieldAliasSource = `
type Holder = { label: string }

fn make_holder() -> Holder {
    return Holder { label = "held-value" };
}

fn read_alias_then_use_container() -> int {
    let h: Holder = make_holder();
    {
        let alias: string = h.label;
        let n: int = alias.__len() to int;
        if n != 10 { return 1; }
    }
    // The alias' scope ended; if it had dropped, h.label is now freed.
    let again: int = h.label.__len() to int;
    if again != 10 { return 2; }
    return 0;
}

@entrypoint
fn main() -> int {
    let r: int = read_alias_then_use_container();
    if r != 0 { return 10 + r; }
    print("field-alias-ok");
    return 0;
}
`

// TODO(runtime-v2 cleanup): DELETE this test, do not repair it.
//
// It pins the ALIASING model — a field read yielding a second name for the
// container's storage, which must therefore never be dropped — and Epic 24
// replaces that model with real partial moves. Its program (`let alias =
// h.label;`) is rejected by the partial-move gate, so it can no longer build.
//
// Superseded, not broken. Skipped rather than rewritten, because rewriting it
// would preserve the semantics the refactor exists to remove; it goes with the
// rest of the pre-V2 suite at the end of Runtime V2.
func TestRuntimeV2DropFieldAliasDoesNotDoubleFree(t *testing.T) {
	t.Skip("superseded by Epic 24 partial moves; delete at the Runtime V2 suite cleanup")

	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DropFieldAliasSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "2"} {
		env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
		_, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 {
			t.Fatalf("field-alias e2e failed (threads=%s exit=%d)\nstdout:\n%s\nstderr:\n%s",
				threads, result.exitCode, result.stdout, result.stderr)
		}
		if !strings.Contains(result.stdout, "field-alias-ok") {
			t.Fatalf("field-alias e2e missing marker; stdout=%q", result.stdout)
		}
	}
}
