package vm_test

import (
	"strings"
	"testing"
	"time"
)

// `let _ = e` discards what `e` produced, and a discarded owning value is
// released at the end of its statement -- the same rule as the statement
// `e;`. Before the discard rule reached `let _`, the initializer was consumed
// as if a binding had received it and then dropped by nobody: every value
// discarded through `_` leaked, and the typed map's own valgrind row read
// 210 bytes in 10 blocks from exactly its `let _ = m.remove(&k)` and
// `let _ = m.insert(k, v)` lines.
//
// The rows discard the three shapes that reach a program: a fresh
// composite, an `Option` around one, and what a map hands back from `remove`
// and from an overwriting `insert` -- in a plain function and in a task.
const runtimeV2DiscardedLetSource = `
type Owned = { label: string, extra: string };

fn owned(label: string) -> Owned {
    return Owned { label = label, extra = "payload" };
}

fn make() -> Option<Owned> {
    return Some(owned("opt"));
}

fn discard_in_sync() -> int {
    let _ = owned("plain");
    let _ = make();
    let mut m: Map<string, Owned> = Map::<string, Owned>.new();
    let _ = m.insert("a", owned("a"));
    let _ = m.insert("a", owned("a2"));
    let gone = "a";
    let _ = m.remove(&gone);
    return m.length() to int;
}

async fn discard_in_task() -> int {
    let _ = owned("task");
    let _ = make();
    let mut m: Map<string, Owned> = Map::<string, Owned>.new();
    let _ = m.insert("b", owned("b"));
    let gone = "b";
    let _ = m.remove(&gone);
    return m.length() to int;
}

@entrypoint
fn main() -> int {
    if discard_in_sync() != 0 { return 2; }
    let t = spawn discard_in_task();
    let n: int = compare t.await() { Success(v) => v; Cancelled() => 0 - 1; };
    if n != 0 { return 3; }
    print("discarded-let-ok");
    return 0;
}
`

func TestRuntimeV2DiscardedLetReleasesItsValue(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DiscardedLetSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("discarded-let e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "discarded-let-ok") {
		t.Fatalf("discarded-let e2e missing completion marker; stdout=%q", stdout)
	}
	definiteBytes, definiteBlocks := parseValgrindLeakMatch(valgrindDefiniteLeakRE, stderr)
	indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
	if definiteBytes != 0 || definiteBlocks != 0 || indirectBytes != 0 || indirectBlocks != 0 {
		t.Fatalf(
			"a value discarded through `let _` leaked: definitely_lost=%dB/%dblk indirectly_lost=%dB/%dblk, want strict zero\nstderr:\n%s",
			definiteBytes, definiteBlocks, indirectBytes, indirectBlocks, stderr,
		)
	}
}
