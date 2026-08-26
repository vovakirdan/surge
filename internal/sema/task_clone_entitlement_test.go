package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
)

// A `.clone()` on a Task<T> is a second source-level entitlement to one
// result, and structured concurrency counts entitlements, not tasks: each
// handle must be awaited, returned or passed on. The two one-sided programs
// below are mirror images and must be refused symmetrically -- before this
// rule the second one compiled, because the tracker knew the spawn and its one
// binding and nothing else.
//
// The valid rows assert NO error at all rather than merely "no SEM3107", so a
// prelude that fails to type-check cannot pass them vacuously; the invalid
// rows next to them prove the same prelude reaches the task analysis.
const taskCloneEntitlementPrelude = `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
@intrinsic
type Task<T> = { __opaque: int };
extern<Task<T>> {
    @intrinsic pub fn clone(self: &Task<T>) -> Task<T>;
    @intrinsic pub fn await(self: own Task<T>) -> TaskResult<T>;
}
async fn work() -> int { return 1; }
fn consume(t: Task<int>) -> nothing { let held = t; let _ = held; return nothing; }
`

func TestTaskCloneIsAnEntitlementOfItsOwn(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // expected error code; "" means the program must be clean
	}{
		{"source_unobserved", `async fn f() -> int { let t = spawn work(); let s = t.clone(); let _ = s.await(); return 0; }`, "SEM3107"},
		{"clone_unobserved", `async fn f() -> int { let t = spawn work(); let s = t.clone(); let _ = t.await(); return 0; }`, "SEM3107"},
		{"both_awaited", `async fn f() -> int { let t = spawn work(); let s = t.clone(); let _ = t.await(); let _ = s.await(); return 0; }`, ""},
		{"await_one_return_other", `async fn f() -> Task<int> { let t = spawn work(); let s = t.clone(); let _ = t.await(); return s; }`, ""},
		{"clone_returned_in_place", `fn f(t: &Task<int>) -> Task<int> { return t.clone(); }`, ""},
		// A generic receiver: the clone's own type may still be deferred when the
		// return is examined, so the tracker must recognise the expression it
		// registered rather than ask its type.
		{"clone_returned_in_place_generic", `fn f<T>(t: &Task<T>) -> Task<T> { return t.clone(); }`, ""},
		{"clone_awaited_in_place", `async fn f() -> int { let t = spawn work(); let _ = t.clone().await(); let _ = t.await(); return 0; }`, ""},
		{"clone_passed_on", `async fn f() -> int { let t = spawn work(); consume(t.clone()); let _ = t.await(); return 0; }`, ""},
		{"clone_bound_then_unobserved_in_place", `async fn f() -> int { let t = spawn work(); let _ = t.clone(); let _ = t.await(); return 0; }`, "SEM3107"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codes := taskCloneCodes(t, taskCloneEntitlementPrelude+tc.src)
			if tc.want == "" {
				if len(codes) != 0 {
					t.Fatalf("expected a clean program, got %v", codes)
				}
				return
			}
			if !codes[tc.want] {
				t.Fatalf("expected %s, got %v", tc.want, codes)
			}
		})
	}
}

// taskCloneCodes is onCrossingCodes without its prelude: this file declares
// its own Task, because the entitlement question needs the real `clone` and
// `await` method signatures from core/intrinsics.sg rather than a bare struct.
func taskCloneCodes(t *testing.T, src string) map[string]bool {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, src)
	codes := map[string]bool{}
	for _, d := range parseBag.Items() {
		codes[d.Code.ID()] = true
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	for _, d := range semaBag.Items() {
		if d.Severity == diag.SevError {
			codes[d.Code.ID()] = true
			t.Logf("%s: %s", d.Code.ID(), d.Message)
		}
	}
	return codes
}
