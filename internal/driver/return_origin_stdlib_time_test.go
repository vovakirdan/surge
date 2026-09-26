package driver

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/sema"
)

const durationIdentitySource = `import stdlib/time as time;

fn duration() -> time.Duration {
    return time.monotonic_now();
}
`

const durationCanarySource = `@intrinsic fn opaque_range() -> Range<int>;
@intrinsic fn opaque_view() -> BytesView;
@intrinsic fn opaque_task() -> Task<int>;
@intrinsic fn opaque_channel() -> Channel<int[]>;

fn range_value() -> Range<int> { return opaque_range(); }
fn view_value() -> BytesView { return opaque_view(); }
fn task_value() -> Task<int> { return opaque_task(); }
fn channel_value() -> Channel<int[]> { return opaque_channel(); }

@copy
@intrinsic
type Duration = { __opaque: int64 };
@intrinsic fn opaque_duration() -> Duration;
fn named_duration() -> Duration { return opaque_duration(); }
`

const durationHolderCanarySource = `import stdlib/time as time;

type Holder = { duration: time.Duration, value: Range<int> };

fn leak() -> Holder {
    let local: int[] = [1];
    return Holder { duration = time.monotonic_now(), value = local.__range() };
}
`

func durationCallSpan(t *testing.T, text, call string) originSpan {
	t.Helper()
	start := strings.LastIndex(text, call)
	if start < 0 {
		t.Fatalf("PRECONDITION: call %q is absent", call)
	}
	return originSpan{start: start, end: start + len(call), snippet: call}
}

func TestReturnOriginStdlibTimeDurationIdentityCertificate(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "stdlib_time_duration_identity", durationIdentitySource, false, nil)
	call := durationCallSpan(t, durationIdentitySource, "time.monotonic_now()")
	if pending := originPendingWithin(analysis, f.unit.SourceKey, call.start, call.end); len(pending) != 0 {
		t.Fatalf("stdlib/time Duration call did not finish: %+v", pending)
	}
	if !analysis.Complete() {
		t.Fatalf("stdlib/time Duration program remained unfinished: %+v", analysis.Pending)
	}
}

func TestReturnOriginStdlibTimeGoldenRowsFinish(t *testing.T) {
	paths := []string{
		"sema/invalid/directives/time_not_directive_module/main.sg",
		"sema/valid/directives/stdlib_benchmark_module/main.sg",
		"sema/valid/directives/stdlib_time_directive/main.sg",
		"sema/valid/directives/stdlib_time_import/main.sg",
	}
	for _, rel := range paths {
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			path := filepath.Join(repoRootFromDriverTest(t), "testdata", "golden", rel)
			result, err := DiagnoseWithOptions(t.Context(), path, &DiagnoseOptions{
				Stage: DiagnoseStageAll, MaxDiagnostics: 64, DirectiveMode: parser.DirectiveModeCollect,
			})
			if err != nil {
				t.Fatalf("golden row did not finish: %v", err)
			}
			if strings.Contains(rel, "/valid/") && result.Bag.HasErrors() {
				t.Fatalf("valid golden row diagnosed errors: %+v", result.Bag.Items())
			}
			if strings.Contains(rel, "/invalid/") && !slices.ContainsFunc(result.Bag.Items(), func(d *diag.Diagnostic) bool {
				return d != nil && d.Code == diag.SemaDirectiveNotDirectiveModule
			}) {
				t.Fatalf("invalid golden row did not reach SEM3120: %+v", result.Bag.Items())
			}
		})
	}
}

func TestReturnOriginStdlibTimeDurationIdentityCanaries(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "stdlib_time_duration_canaries", durationCanarySource, false, nil)
	for _, call := range []string{"opaque_range()", "opaque_task()", "opaque_channel()", "opaque_duration()"} {
		span := durationCallSpan(t, durationCanarySource, call)
		if !originPendingAt(analysis, f.unit.SourceKey, span, originOpaqueStateRefusal) {
			t.Errorf("%s lost its opaque-result row: %+v", call, originPendingWithin(analysis, f.unit.SourceKey, span.start, span.end))
		}
	}
	view := durationCallSpan(t, durationCanarySource, "opaque_view()")
	if !originPendingAt(analysis, f.unit.SourceKey, view, "opaque result type may carry borrowed state") {
		t.Errorf("opaque_view() lost its refuted borrowed-state row: %+v", originPendingWithin(analysis, f.unit.SourceKey, view.start, view.end))
	}
}

func TestReturnOriginStdlibTimeDurationHolderKeepsLoanRow(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "stdlib_time_duration_holder", durationHolderCanarySource, false, nil)
	ret := durationCallSpan(t, durationHolderCanarySource, "return Holder { duration = time.monotonic_now(), value = local.__range() };")
	if !slices.ContainsFunc(originPendingWithin(analysis, f.unit.SourceKey, ret.start, ret.end), func(p sema.ReturnOriginPending) bool { return p.Reason == backingLoanDiscard }) {
		t.Fatalf("Duration holder lost its dying-local storage-loan row: %+v", originPendingWithin(analysis, f.unit.SourceKey, ret.start, ret.end))
	}
}
