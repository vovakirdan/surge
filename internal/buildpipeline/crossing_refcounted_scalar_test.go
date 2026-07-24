package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// An arbitrary-precision scalar is Copy, so copying its bits duplicates a
// reference into a counted heap block without touching the count — and the
// count is deliberately NON-ATOMIC, which is only sound while a block stays on
// one shard. Until each boundary installs a deep copy, every path that would
// hand a second shard the same word is refused.
//
// These rows pin that closure so reopening it is a deliberate act rather than a
// silent regression. When the deep-copy barriers land, each row here should
// flip to "compiles", and the leak witness plus a cross-shard census take over
// as the gate.
//
// The two shapes that are NOT here, on purpose:
//   - an owned `@shard_movable` MOVE, which transfers the references instead of
//     sharing them, so exactly one shard ends up holding each;
//   - fixed-width `float64`, which is a machine word with no block behind it.
func TestRefCountedScalarCrossingsAreRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "bare float captured into a crossing body",
			src: `
async fn go(dst: Placement) -> int {
    let f: float = 1.5;
    let r: TaskResult<int> = on dst { let g: float = f; ret 1; };
    print(f to string);
    return 0;
}
`,
			contains: []string{"`float`", "counted heap block", "not", "safe to share"},
		},
		{
			name: "copy struct carrying a float field",
			src: `
@copy
type P = { v: float };

async fn go(dst: Placement) -> int {
    let p: P = P { v: 1.5 };
    let r: TaskResult<int> = on dst { let q: P = p; ret 1; };
    print(p.v to string);
    return 0;
}
`,
			contains: []string{"`P`", "arbitrary-precision"},
		},
		{
			name: "float riding the reply",
			src: `
async fn go(dst: Placement) -> int {
    let t: far Task<float> = spawn on dst { ret 1.5; };
    let r: TaskResult<float> = t.await();
    return 0;
}
`,
			contains: []string{"`float`", "cannot cross a shard boundary yet"},
		},
		{
			name: "remote channel with a float element",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    return 0;
}
`,
			contains: []string{"remote channel cannot carry `float`", "sender's copy alive"},
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
			var message string
			for _, item := range res.Diagnose.Bag.Items() {
				for _, want := range tc.contains {
					if !strings.Contains(item.Message, want) {
						goto next
					}
				}
				message = item.Message
			next:
			}
			if message == "" {
				t.Fatalf("no diagnostic mentioning %v; got %s",
					tc.contains, summarizeCodes(res.Diagnose.Bag.Items()))
			}
		})
	}
}

// The refusal must not spill onto fixed-width floats: `float64` is a machine word
// with no heap block and no count, and it has always crossed. If this breaks,
// the widening reached a type it was never meant to.
func TestFixedWidthFloatStillCrosses(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	src := `
async fn go(dst: Placement) -> int {
    let f: float64 = 1.5;
    let r: TaskResult<float64> = on dst { let g: float64 = f; ret g; };
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
	if found := findDiagnostic(res.Diagnose.Bag.Items(), diag.FutCrossingPayloadNotShippable); found != nil {
		t.Fatalf("float64 crossing was refused: %s", found.Message)
	}
	// Assert the program is CLEAN, not merely free of this one code: a source
	// that fails to compile for an unrelated reason would satisfy the check
	// above while proving nothing.
	for _, item := range res.Diagnose.Bag.Items() {
		if item.Severity == diag.SevError {
			t.Fatalf("float64 crossing did not compile cleanly: [%s] %s", item.Code, item.Message)
		}
	}
}
