package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// One binding given away by two SEND arms of one `select` is refused by sema
// (SemaSelectSendPayloadGivenTwice), whatever the arms carry: every arm stages
// its payload before the select runs, so the value would sit in two arm cells
// and be freed once per cell (RV2-DEBT-338, closed here).
//
// Red on the tree before the ledger: the two `string` rows built with zero
// diagnostics and the program aborted at run with `free(): double free
// detected` (exit 134); the union row was refused, but by the channel-element
// gate (FUT7020, "remote channel cannot carry `U`"), which never looked at the
// two arms — the refusal here is the one that survives that gate's narrowing.
func TestSelectSendGivingOneBindingToTwoArmsIsRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "one owned string fed to two far-select send arms",
			src: `
async fn go(ch: far Channel<string>, ch2: far Channel<string>) -> int {
    let mut s: string = "hello-";
    s = s + "world";
    let won: int = select {
        ch.send(own s) => 1;
        ch2.send(own s) => 2;
    };
    return won;
}
`,
		},
		{
			name: "one owned string fed to two local select send arms",
			src: `
async fn go() -> int {
    let ch = Channel::<string>::new(1:uint);
    let ch2 = Channel::<string>::new(1:uint);
    let mut s: string = "hello-";
    s = s + "world";
    let won: int = select {
        ch.send(own s) => 1;
        ch2.send(own s) => 2;
    };
    return won;
}
`,
		},
		{
			name: "one owned union carrying a float fed to two far-select send arms",
			src: `
type P = { v: float };

tag Held(P);
tag Empty();
type U = Held(P) | Empty();

async fn go() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(0:ShardId), 4);
    let ch2: far Channel<U> = channel_on::<U>(shard(0:ShardId), 4);
    let a: float = 2.5;
    let held: U = Held(P{ v: a });
    let won: int = select {
        ch.send(own held) => 1;
        ch2.send(own held) => 2;
    };
    print(a to string);
    return won;
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.sg")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}
			res, _ := Compile(context.Background(), &CompileRequest{
				TargetPath: path, Backend: BackendLLVM, MaxDiagnostics: 200,
			})
			if res.Diagnose == nil || res.Diagnose.Bag == nil {
				t.Fatal("missing diagnostics bag")
			}
			found := findDiagnostic(res.Diagnose.Bag.Items(), diag.SemaSelectSendPayloadGivenTwice)
			if found == nil {
				t.Fatalf("the second arm was not refused; got %s", summarizeCodes(res.Diagnose.Bag.Items()))
			}
			if !strings.Contains(found.Message, "given away by two arms") {
				t.Fatalf("refusal does not name the shape: %s", found.Message)
			}
		})
	}
}

// The same staging, consumed from the other side: arm 1 gives `s` away, arm
// 2's AWAIT hands `s` to a by-value parameter. Every await runs before the
// select does, whichever arm wins, so `eat` frees the block the select is
// about to stage. The per-arm rollback hid this too (the reviewers' probe of
// 2026-09-07); the awaits are typed first now, what they consume for good
// accumulates, and a staged payload consumed that way is refused with the
// consuming await named. Red on the tree before: built clean, and the VM
// reported `local "s" used after move` at the select while the native lane
// died reading the delivered string.
func TestSelectSendPayloadConsumedByAnotherAwaitIsRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "local select",
			src: `
fn eat(x: string) -> int { return len(x) to int; }

async fn go() -> int {
    let ch = Channel::<string>::new(1:uint);
    let chi = Channel::<int>::new(1:uint);
    let mut s: string = "s-";
    s = s + "x";
    let won: int = select {
        ch.send(own s) => 1;
        chi.send(eat(s)) => 2;
    };
    return won;
}
`,
		},
		{
			name: "far select",
			src: `
fn eat(x: string) -> int { return len(x) to int; }

async fn go(ch: far Channel<string>, chi: far Channel<int>) -> int {
    let mut s: string = "s-";
    s = s + "x";
    let won: int = select {
        ch.send(own s) => 1;
        chi.send(eat(s)) => 2;
    };
    return won;
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.sg")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}
			res, _ := Compile(context.Background(), &CompileRequest{
				TargetPath: path, Backend: BackendLLVM, MaxDiagnostics: 200,
			})
			if res.Diagnose == nil || res.Diagnose.Bag == nil {
				t.Fatal("missing diagnostics bag")
			}
			found := findDiagnostic(res.Diagnose.Bag.Items(), diag.SemaSelectSendPayloadGivenTwice)
			if found == nil {
				t.Fatalf("the staged payload was not refused; got %s", summarizeCodes(res.Diagnose.Bag.Items()))
			}
			if !strings.Contains(found.Message, "another arm's await consumes it") {
				t.Fatalf("refusal does not name the consuming await: %s", found.Message)
			}
		})
	}
}

// The controls: two arms, two bindings; and an await that only BORROWS the
// binding another arm stages. Both are the accepted shapes and must keep
// compiling — the ledger keys on the binding, not on the type, and a borrow
// leaves the staged value where it is.
func TestSelectSendAcceptedShapesStillCompile(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	t.Run("two bindings, two arms", func(t *testing.T) {
		compileCleanly(t, `
async fn go(ch: far Channel<string>, ch2: far Channel<string>) -> int {
    let mut s: string = "hello-";
    s = s + "world";
    let mut u: string = "other-";
    u = u + "world";
    let won: int = select {
        ch.send(own s) => 1;
        ch2.send(own u) => 2;
    };
    return won;
}
`)
	})
	t.Run("another await borrows the staged payload", func(t *testing.T) {
		compileCleanly(t, `
fn measure(x: &string) -> int { return len(*x) to int; }

async fn go(ch: far Channel<string>, chi: far Channel<int>) -> int {
    let mut s: string = "s-";
    s = s + "x";
    let won: int = select {
        ch.send(own s) => 1;
        chi.send(measure(&s)) => 2;
    };
    return won;
}
`)
	})
}
