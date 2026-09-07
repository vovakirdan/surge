package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// A remote channel's element is asked, at the channel's creation, the same
// question a capture is asked: not "does it hold a counted block" but "can
// the walk make the block private before the value leaves". Every entry into
// the ring hands over a private reference now -- a far-select SEND payload is
// un-shared in the relinquishing operand, and an anchored body's
// `ch.send(own f)` gives away the capture's own reference, one the caller made
// private when the capture entered the state (sema holds the send to that
// shape: SemaAnchoredSendGiveAway) -- so a bare `float`, a union of structs
// carrying one, a fixed array of them, and a `@copy` union all mint a remote
// channel and ship.
//
// Red on the tree before the narrowing: every row here was refused with
// FUT7020 ("remote channel cannot carry ..."); the first two of them had
// been refused since the element gate was widened to unions on 2026-09-06.
func TestRefCountedScalarChannelElementsShip(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "remote channel with a float element, fed by an anchored send",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let a: float = 1.5;
    let f: float = a;
    let sent: TaskResult<nothing> = on ch { ch.send(own f); ret nothing; };
    print(a to string);
    return 0;
}
`,
		},
		{
			name: "remote channel with a float element, fed by a far-select send arm",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let a: float = 1.5;
    let won: int = select {
        ch.send(a) => 1;
    };
    print(a to string);
    return won;
}
`,
		},
		{
			name: "remote channel with a union element carrying a float",
			src: `
type P = { v: float };

tag Held(P);
tag Empty();
type U = Held(P) | Empty();

async fn go() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(0:ShardId), 4);
    let a: float = 2.5;
    let held: U = Held(P{ v: a });
    let won: int = select {
        ch.send(own held) => 1;
    };
    print(a to string);
    return won;
}
`,
		},
		{
			// Fed by a far-select arm: a fixed array is an owned user value to the
			// capture rule of an `on` body ("must be `@shard_movable`", the
			// pre-existing limit recorded on RV2-DEBT-038), so the anchored form
			// is not the one to exercise here.
			name: "remote channel with a fixed float array element",
			src: `
async fn go() -> int {
    let ch: far Channel<float[4]> = channel_on::<float[4]>(shard(0:ShardId), 4);
    let a: float = 1.5;
    let quad: float[4] = [a, a, a, a];
    let won: int = select {
        ch.send(own quad) => 1;
    };
    print(a to string);
    return won;
}
`,
		},
		{
			name: "remote channel with a copy union element carrying a float",
			src: `
@copy
type P = { v: float };

tag Held(P);
tag Empty();
@copy
type U = Held(P) | Empty();

async fn go() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(0:ShardId), 4);
    let a: float = 2.5;
    let held: U = Held(P{ v: a });
    let sent: TaskResult<nothing> = on ch { ch.send(own held); ret nothing; };
    print(a to string);
    return 0;
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compileCleanly(t, tc.src)
		})
	}
}

// What the walk cannot make private stays refused at the channel's creation,
// in words that say why: a dynamic array's buffer is storage the sender keeps.
func TestRefCountedScalarChannelElementsStayRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	src := `
async fn go() -> int {
    let ch: far Channel<float[]> = channel_on::<float[]>(shard(0:ShardId), 4);
    return 0;
}
`
	path := filepath.Join(t.TempDir(), "main.sg")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	res, _ := Compile(context.Background(), &CompileRequest{
		TargetPath: path, Backend: BackendLLVM, MaxDiagnostics: 200,
	})
	if res.Diagnose == nil || res.Diagnose.Bag == nil {
		t.Fatal("missing diagnostics bag")
	}
	found := findDiagnostic(res.Diagnose.Bag.Items(), diag.FutCrossingPayloadNotShippable)
	if found == nil {
		t.Fatalf("the dynamic array element was not refused; got %s", summarizeCodes(res.Diagnose.Bag.Items()))
	}
	for _, want := range []string{"remote channel cannot carry `", "dynamic array's buffer"} {
		if !strings.Contains(found.Message, want) {
			t.Fatalf("refusal does not say %q: %s", want, found.Message)
		}
	}
}

// The anchored send of a counted scalar is held to `ch.send(own f)`: the
// ring takes the only reference and the body cannot keep or make another
// (a parked send re-enters the body from the top). The three shapes below
// compiled before the element gate narrowed only because the channel itself
// was refused; each is refused on its own now, with the way out.
func TestAnchoredSendOfACountedScalarMustGiveTheBindingAway(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
		code diag.Code
		want string
	}{
		{
			name: "a plain read of the capture",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let f: float = 1.5;
    let sent: TaskResult<nothing> = on ch { ch.send(f); ret nothing; };
    return 0;
}
`,
			code: diag.SemaAnchoredSendGiveAway,
			want: "must give a captured binding away",
		},
		{
			name: "a literal built inside the body",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let sent: TaskResult<nothing> = on ch { ch.send(1.5); ret nothing; };
    return 0;
}
`,
			code: diag.SemaAnchoredSendGiveAway,
			want: "must give a captured binding away",
		},
		{
			name: "the capture read after it was given away",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let f: float = 1.5;
    let sent: TaskResult<nothing> = on ch { ch.send(own f); print(f to string); ret nothing; };
    return 0;
}
`,
			code: diag.SemaUseAfterMove,
			want: "use of moved value 'f'",
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
			found := findDiagnostic(res.Diagnose.Bag.Items(), tc.code)
			if found == nil {
				t.Fatalf("no %s; got %s", tc.code.ID(), summarizeCodes(res.Diagnose.Bag.Items()))
			}
			if !strings.Contains(found.Message, tc.want) {
				t.Fatalf("message does not say %q: %s", tc.want, found.Message)
			}
		})
	}
}
