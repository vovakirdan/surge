package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// A crossing's result is asked, at its reply, the same question a capture is
// asked: not "does it hold a counted block" but "can the walk make the block
// private before the value leaves". The producer's `ret` un-shares the result
// in its relinquishing operand (site 3, step 4) before the reply names it, and
// the asker moves it exactly once — so a bare `float` rides `far Task<float>`
// and an `on` block's `TaskResult<float>`, and a `@copy` composite carrying one
// rides with it.
//
// Red on the tree before the narrowing: every row here was refused with
// FUT7020 ("`float` cannot cross a shard boundary yet").
func TestRefCountedScalarResultsRideTheReply(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "float riding the reply of a spawn on",
			src: `
async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let t: far Task<float> = spawn on dst { ret a; };
    let r: TaskResult<float> = t.await();
    print(a to string);
    return 0;
}
`,
		},
		{
			name: "float produced beside a sibling inside the body",
			src: `
async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let t: far Task<float> = spawn on dst { let x: float = a; ret x; };
    let r: TaskResult<float> = t.await();
    print(a to string);
    return 0;
}
`,
		},
		{
			name: "float riding the reply of an immediate on",
			src: `
async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let r: TaskResult<float> = on dst { ret a; };
    print(a to string);
    return 0;
}
`,
		},
		{
			name: "copy struct carrying a float riding the reply",
			src: `
@copy
type P = { v: float };

async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let t: far Task<P> = spawn on dst { ret P { v: a }; };
    let r: TaskResult<P> = t.await();
    print(a to string);
    return 0;
}
`,
		},
		{
			// Never gated on the reply axis (a `blocking` body is not a crossing
			// lowering kind), and covered by the same `ret` un-share since step 4:
			// the control that the narrowing changes nothing here.
			name: "float riding a blocking result",
			src: `
async fn go() -> int {
    let a: float = 1.5;
    let job: Task<float> = blocking { let x: float = a; ret x; };
    let r: TaskResult<float> = job.await();
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

// What the walk cannot make private stays refused at the reply, in words that
// say why: a dynamic array's buffer is storage the producer keeps. A result
// that is not Copy at all stays refused for that reason, as before.
func TestRefCountedScalarResultsStayRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "float array riding the reply",
			src: `
async fn go(dst: Placement) -> int {
    let t: far Task<float[]> = spawn on dst { let xs: float[] = [1.5]; ret xs; };
    let r: TaskResult<float[]> = t.await();
    return 0;
}
`,
			contains: []string{"cannot cross a shard boundary yet", "dynamic array's buffer"},
		},
		{
			name: "owned struct carrying a float riding the reply",
			src: `
type P = { v: float };

async fn go(dst: Placement) -> int {
    let t: far Task<P> = spawn on dst { ret P { v: 1.5 }; };
    let r: TaskResult<P> = t.await();
    return 0;
}
`,
			contains: []string{"cannot ride the reply", "not plain-copy"},
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
			found := findDiagnostic(res.Diagnose.Bag.Items(), diag.FutCrossingPayloadNotShippable)
			if found == nil {
				t.Fatalf("the result was not refused; got %s", summarizeCodes(res.Diagnose.Bag.Items()))
			}
			for _, want := range tc.contains {
				if !strings.Contains(found.Message, want) {
					t.Fatalf("refusal does not say %q: %s", want, found.Message)
				}
			}
		})
	}
}
