package driver

import (
	"strings"
	"testing"

	"surge/internal/sema"
)

// An async fn is typed as returning Task<P>, but its body gives P: falling off its end gives
// `nothing`, admitted by sema only when P is `nothing`. Return-origin compared that end with Task<P>
// and read every such fn as a result with no proven value (and each caller of it as calling one),
// so the program stayed unfinished on D2 (return_origin_summary.go; PAF). Each program is analyzed with
// every owning unit, as the binding-transfer rows do (returnOriginStdlibFixture, the finalized closure,
// collectReturnOriginUnits): on D2 core itself still carries pending rows, so only the rows whose
// SourceKey is this program are judged. There must be none of the three fall-through reasons, and the
// named function of this program must have a proven summary. The controls were clean before the fix too.
func TestAnalyzeAsyncFallthroughIsItsPayload(t *testing.T) {
	for _, row := range []struct{ name, fn, src string }{
		{"async_fn_payload_nothing_falls_through", "f", `async fn f() -> nothing {
    let x: int = 1;
}
`},
		{"async_fn_unannotated_falls_through", "f", `async fn f() {
    let x: int = 1;
}
`},
		{"async_method_falls_through", "tick", `type C = { n: int }

extern<C> {
    pub async fn tick(self: &C) {
        let x: int = self.n;
    }
}
`},
		{"caller_of_a_falling_through_async_fn", "g", `async fn f() {
    let x: int = 1;
}

fn g() -> Task<nothing> {
    return f();
}
`},
		{"control_async_fn_explicit_return", "f", `async fn f() -> nothing {
    let x: int = 1;
    return nothing;
}
`},
		{"control_sync_fn_falls_through", "f", `fn f() {
    let x: int = 1;
}
`},
		{"control_async_block_without_ret", "f", `fn f() -> Task<nothing> {
    return async {
        let x: int = 1;
    };
}
`},
	} {
		t.Run(row.name, func(t *testing.T) {
			res := returnOriginStdlibFixture(t, row.src, false)
			if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
				t.Fatalf("PRECONDITION: closure failed: %v", err)
			}
			inputs, err := collectReturnOriginUnits(res)
			if err != nil {
				t.Fatalf("PRECONDITION: owning units: %v", err)
			}
			rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			if err != nil || analysis == nil {
				t.Fatalf("analysis did not run: %v", err)
			}
			for _, pending := range analysis.Pending {
				if pending.SourceKey != rootKey {
					continue // core carries its own pending rows on D2; this row judges the program only
				}
				if strings.Contains(pending.Reason, "reachable function fallthrough has no proven value for its declared result") || strings.Contains(pending.Reason, "function result contains an unproved source") ||
					strings.Contains(pending.Reason, "callee returned an unproved source") {
					t.Fatalf("an async fall-through is still read as an unproved result: %+v", pending)
				}
			}
			found := false
			for _, summary := range analysis.Summaries {
				if summary.Name != row.fn || summary.Source.File != res.File.ID {
					continue
				}
				found = true
				if summary.Unknown {
					t.Fatalf("%s: summary is still unknown: %+v", row.fn, summary)
				}
			}
			if !found {
				t.Fatalf("PRECONDITION: no summary for %s in this program", row.fn)
			}
		})
	}
}

// An async fn whose payload is not `nothing` cannot fall off its end: sema refuses it before
// return-origin runs (SEM3051), exactly as for a plain function.
func TestAsyncFallthroughWithAPayloadIsAMissingReturn(t *testing.T) {
	probe := taskCheckProbe{"async_fn_int_falls_through", "c78eef8bd796fd77f9c0330cffb97207686173ec843e243f946d1583e0dea82d", "SEM3051", `async fn f() -> int {
    let x: int = 1;
}

@entrypoint
fn main() -> int {
    return 0;
}
`}
	if got, _ := taskCheckErrorCodes(t, probe); got != probe.want {
		t.Fatalf("error codes %q, want %q", got, probe.want)
	}
}

// The barrier PAF removes (RV2-DEBT-365: every packet that removes one names it). Before PAF a borrowing
// async worker that FALLS THROUGH left its whole program unfinished in return-origin, whatever else it did.
// After PAF such a program reaches the task check with nothing in front of it -- exactly as its
// `return nothing;` twin always did -- so the two must be refused with the same, non-empty code set: the
// task check, not the old fall-through row, stops a task that borrows a frame out of it.
func TestTaskCheckRefusesAFallingThroughBorrowingWorkerLikeItsTwin(t *testing.T) {
	falls := taskCheckProbe{"borrowing_worker_falls_through", "43156184447c1ba08d34c20a046c3c4db47a542d512d49ec27f1d556e9ff4b9b", "", `async fn worker(x: &string) {
    let y: int = 1;
}

fn leak() -> Task<nothing> {
    let l: string = "abc";
    let t = worker(&l);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`}
	twin := taskCheckProbe{"borrowing_worker_returns_nothing", "ce64edce65268ef0e087cc44ddf51fad754c93fd68075b50a668a29e7b0d10fb", "", `async fn worker(x: &string) {
    let y: int = 1;
    return nothing;
}

fn leak() -> Task<nothing> {
    let l: string = "abc";
    let t = worker(&l);
    return t;
}

@entrypoint
fn main() -> int {
    return 0;
}
`}
	gotFalls, _ := taskCheckErrorCodes(t, falls)
	gotTwin, _ := taskCheckErrorCodes(t, twin)
	if gotTwin == "" || gotFalls != gotTwin {
		t.Fatalf("falling through: %q, explicit return: %q; want the same non-empty refusal", gotFalls, gotTwin)
	}
}
