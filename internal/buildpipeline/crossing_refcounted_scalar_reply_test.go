package buildpipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// What no walk can make private stays refused at the reply, in words that
// say why: a map's table is storage the producer keeps. A result that is not
// Copy at all stays refused for that reason, as before -- and a float array
// is one: its buffer is walked where it is relinquished, but the reply takes
// only plain-copy data, so the array meets that rule first.
func TestRefCountedScalarResultsStayRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "float array riding the reply is refused as not plain-copy data, like any other non-Copy result",
			src: `
async fn go(dst: Placement) -> int {
    let t: far Task<float[]> = spawn on dst { let xs: float[] = [1.5]; ret xs; };
    let r: TaskResult<float[]> = t.await();
    return 0;
}
`,
			contains: []string{"cannot ride the reply", "not plain-copy"},
		},
		{
			name: "map valued by floats riding the reply",
			src: `
async fn go(dst: Placement) -> int {
    let t: far Task<Map<int, float>> = spawn on dst { ret Map::<int, float>.new(); };
    let r: TaskResult<Map<int, float>> = t.await();
    return 0;
}
`,
			contains: []string{"cannot cross a shard boundary yet", "map's table"},
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

// A blocking body's `ret` of a float array has no gate in front of it: the
// body is not a crossing lowering kind, and sema admits the shape. What
// answered for it was the EMITTER, refusing the `ret`'s un-share as a build
// failure ("has no buffer walk") with no diagnostic code. The buffer walk
// serves it now: the module CALLS the runtime's element walk with the float
// stride and an element body that un-shares each float. The call is what is
// pinned -- every module declares the runtime symbol, so its mere presence
// would read an empty walk body as green.
func TestFloatArrayBlockingResultEmitsThroughTheBufferWalk(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	ir := compileCleanly(t, `
async fn go() -> int {
    let job: Task<float[]> = blocking { let xs: float[] = [1.5, 2.5]; ret xs; };
    let n: int = compare job.await() { Success(xs) => (xs.__len() to int); Cancelled() => 0 - 2; };
    return n;
}
`)
	requireBufferWalk(t, ir, bufferWalk{stride: 8, elem: []string{"call ptr @rt_bigfloat_unshare("}})
}

// The same `ret`, over an array built EMPTY whose element is a union: no
// expression in the body names `Option<float>`, so its membership reaches the
// module through the array's type alone, and the element body the walk is
// handed switches on the union's tag before it un-shares the float payload.
// Sema admitted this shape while the emitter refused it as a build error
// naming sema's predicate, until the lowering looked through the handle to
// the payload it holds.
func TestOptionalFloatArrayBuiltEmptyRidesABlockingResult(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	ir := compileCleanly(t, `
async fn go() -> int {
    let job: Task<Option<float>[]> = blocking { let xs: Option<float>[] = []; ret xs; };
    let n: int = compare job.await() { Success(xs) => (xs.__len() to int); Cancelled() => 0 - 2; };
    return n;
}
`)
	requireBufferWalk(t, ir, bufferWalk{
		stride: 16,
		elem:   []string{"switch i32", "call ptr @rt_bigfloat_unshare("},
	})
}

// bufferWalk is what a row demands of the module its program emits: the
// relinquishing operand hands the array's SLOT to the runtime's element walk
// with this element stride, and the element body the walk is handed holds
// these lines.
type bufferWalk struct {
	stride int
	elem   []string
}

// requireBufferWalk pins the CALL, never the symbol. Every module declares
// rt_array_unshare_walk and rt_bigfloat_unshare in its builtin roster, so a
// row that asked only whether the module NAMES the walk is answered by the
// declaration and reads an empty walk body as green -- and an un-share whose
// body is empty is exactly what a crossing that ships a shared block emits.
// So the row asks for the call, with the stride that says which element the
// runtime steps by, and then opens the body it was handed.
func requireBufferWalk(t *testing.T, ir string, want bufferWalk) {
	t.Helper()
	call := regexp.MustCompile(fmt.Sprintf(
		`call void @rt_array_unshare_walk\(ptr %%g\d+, i64 %d, ptr @(unshare\.type\d+)\)`, want.stride))
	m := call.FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("the un-share never hands the buffer to the runtime with a stride of %d "+
			"(a declaration alone is not a walk):\n%s", want.stride, ir)
	}
	body := llvmWalkBody(t, ir, m[1])
	for _, line := range want.elem {
		if !strings.Contains(body, line) {
			t.Fatalf("the element body %s the walk was handed does not hold %q:\n%s", m[1], line, body)
		}
	}
}

// llvmWalkBody returns the text of one relinquishing walk body in an emitted
// module, so a row can look inside the body a buffer walk was handed.
func llvmWalkBody(t *testing.T, ir, name string) string {
	t.Helper()
	head := "define void @" + name + "(ptr %val) {"
	start := strings.Index(ir, head)
	if start < 0 {
		t.Fatalf("the module names %s but never defines it:\n%s", name, ir)
	}
	rest := ir[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("the body of %s is unterminated:\n%s", name, ir)
	}
	return rest[:end]
}
