package driver

import (
	"fmt"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
)

// `far Task<T>.await()`, `.cancel()` and `far Channel<T>.share()` are typed as crossings: the
// checker keeps a CrossingLowering record for the call and never types the selector. Each row
// is a root program; the record rows change the record before the analysis reads it.
const farSelectorTaskSource = `fn wait_result(t: far Task<int>) -> TaskResult<int> {
    return t.await();
}
fn cancel_remote(t: far Task<int>) -> TaskResult<nothing> {
    return t.cancel();
}
`

const farSelectorShareSource = `async fn lease(ch: far Channel<int>) -> int {
    let sibling: far Channel<int> = ch.share();
    @drop sibling;
    @drop ch;
    return 1;
}
`

var (
	farSelectorAwait       = originSpan{65, 72, "t.await"}
	farSelectorCancel      = originSpan{149, 157, "t.cancel"}
	farSelectorCancelCall  = originSpan{149, 159, "t.cancel()"}
	farSelectorShare       = originSpan{82, 90, "ch.share"}
	farSelectorShareCall   = originSpan{82, 92, "ch.share()"}
	farSelectorTaskDigest  = "b9a62d300631c6d8254b903a5cca4cd9744e0436f5bb40ed03a204e8104b7df5"
	farSelectorShareDigest = "9cd8b3070f95c1cb0555216f3d813c00a653eb53ff08d8ffc13e831f3ebae3ce"
)

// farSelectorAnalyze is analyzeOriginRoot that hands back the analysis error instead of
// stopping on it: three rows expect an exact error.
func farSelectorAnalyze(t *testing.T, text string, prepare func(originalGenericFixture)) (originalGenericFixture, *sema.ReturnOriginAnalysis, error) {
	t.Helper()
	res := returnOriginStdlibFixture(t, text, false)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: source closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil || len(inputs.units) != 11 {
		t.Fatalf("PRECONDITION: full eleven-unit input missing: units=%d error=%v", len(inputs.units), err)
	}
	checkReturnOriginStdlibBags(t, res, false)
	f := originalGenericFixture{owner: res, authority: res.Sema, inputs: inputs}
	owners := 0
	for _, unit := range inputs.units {
		if unit.Builder.Files.Get(unit.FileID).Span.File == res.File.ID {
			f.unit, owners = unit, owners+1
		}
	}
	if owners != 1 {
		t.Fatalf("PRECONDITION: the test source has %d owning units", owners)
	}
	if prepare != nil {
		prepare(f)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	return f, analysis, err
}

// farSelectorRecord finds the one crossing record of a call and checks what the checker kept.
func farSelectorRecord(t *testing.T, f originalGenericFixture, call, selector originSpan, kind sema.CrossingLoweringKind, consumes bool) (ast.ExprID, int) {
	t.Helper()
	id := originExprAt(t, f.unit, f.owner.File.ID, call, ast.ExprCall)
	target := originExprAt(t, f.unit, f.owner.File.ID, selector, ast.ExprMember)
	member, _ := f.unit.Builder.Exprs.Member(target)
	found := -1
	for i := range f.unit.Sema.CrossingLowering {
		if entry := &f.unit.Sema.CrossingLowering[i]; entry.Expr == id {
			if found >= 0 {
				t.Fatalf("PRECONDITION: two crossing records for %q", call.snippet)
			}
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("PRECONDITION: no crossing record for %q", call.snippet)
	}
	entry := f.unit.Sema.CrossingLowering[found]
	_, typed := f.unit.Sema.ExprTypes[target]
	if entry.Kind != kind || entry.ConsumesHandle != consumes || member == nil || entry.ReceiverExpr != member.Target ||
		entry.ResultType != f.unit.Sema.ExprTypes[id] || typed || f.unit.Symbols.ExprSymbols[id].IsValid() {
		t.Fatalf("PRECONDITION: the record of %q changed: %+v selector_typed=%t", call.snippet, entry, typed)
	}
	return id, found
}

func farSelectorTaskRecords(t *testing.T, f originalGenericFixture) (ast.ExprID, int) {
	t.Helper()
	farSelectorRecord(t, f, originSpan{65, 74, "t.await()"}, farSelectorAwait, sema.CrossingLoweringFarTaskAwait, true)
	return farSelectorRecord(t, f, farSelectorCancelCall, farSelectorCancel, sema.CrossingLoweringFarTaskCancel, true)
}

// 8 RUN: 1 parent, 7 leaves.
func TestAnalyzeFarSelectors(t *testing.T) {
	checkOriginSource(t, farSelectorTaskSource, farSelectorTaskDigest, farSelectorAwait, farSelectorCancel, farSelectorCancelCall)
	checkOriginSource(t, farSelectorShareSource, farSelectorShareDigest, farSelectorShare, farSelectorShareCall)
	t.Run("far_task_selectors", func(t *testing.T) {
		f, analysis, err := farSelectorAnalyze(t, farSelectorTaskSource, func(f originalGenericFixture) { farSelectorTaskRecords(t, f) })
		if err != nil || analysis == nil {
			t.Fatalf("return-origin analysis did not run: %v", err)
		}
		originExactPending(t, analysis, f.unit.SourceKey, originSpan{0, len(farSelectorTaskSource), farSelectorTaskSource}, nil)
		requireOriginSummary(t, analysis, f.owner.File.ID, "wait_result", false, nil)
		requireOriginSummary(t, analysis, f.owner.File.ID, "cancel_remote", false, nil)
	})
	t.Run("record_missing_stays_refused", func(t *testing.T) {
		var selectors []ast.ExprID
		f, analysis, err := farSelectorAnalyze(t, farSelectorTaskSource, func(f originalGenericFixture) {
			farSelectorTaskRecords(t, f)
			selectors = []ast.ExprID{originExprAt(t, f.unit, f.owner.File.ID, farSelectorAwait, ast.ExprMember),
				originExprAt(t, f.unit, f.owner.File.ID, farSelectorCancel, ast.ExprMember)}
			f.unit.Sema.CrossingLowering = nil
		})
		// Whichever body is solved first stops the analysis at its own untyped selector.
		wants := []string{fmt.Sprintf("return origins: expression %d is not typed in %s", selectors[0], f.unit.SourceKey),
			fmt.Sprintf("return origins: expression %d is not typed in %s", selectors[1], f.unit.SourceKey)}
		if err == nil || analysis != nil || !slices.Contains(wants, err.Error()) {
			t.Errorf("want one of %q and nil analysis; got %v, %+v", wants, err, analysis)
		}
	})
	t.Run("record_disagrees_is_an_error", func(t *testing.T) {
		var call ast.ExprID
		f, analysis, err := farSelectorAnalyze(t, farSelectorTaskSource, func(f originalGenericFixture) {
			var at int
			call, at = farSelectorTaskRecords(t, f)
			f.unit.Sema.CrossingLowering[at].ResultType = f.authority.TypeInterner.Builtins().Bool
		})
		want := fmt.Sprintf("return origins: inconsistent far selector evidence for expression %d in %s", call, f.unit.SourceKey)
		if err == nil || analysis != nil || err.Error() != want {
			t.Errorf("want exact %q and nil analysis; got %v, %+v", want, err, analysis)
		}
	})
	t.Run("record_duplicated_is_an_error", func(t *testing.T) {
		var call ast.ExprID
		f, analysis, err := farSelectorAnalyze(t, farSelectorTaskSource, func(f originalGenericFixture) {
			var at int
			call, at = farSelectorTaskRecords(t, f)
			f.unit.Sema.CrossingLowering = append(f.unit.Sema.CrossingLowering, f.unit.Sema.CrossingLowering[at])
		})
		want := fmt.Sprintf("return origins: duplicate far selector evidence for expression %d in %s", call, f.unit.SourceKey)
		if err == nil || analysis != nil || err.Error() != want {
			t.Errorf("want exact %q and nil analysis; got %v, %+v", want, err, analysis)
		}
	})
	t.Run("far_channel_share_selector", func(t *testing.T) {
		f, analysis, err := farSelectorAnalyze(t, farSelectorShareSource, func(f originalGenericFixture) {
			farSelectorRecord(t, f, farSelectorShareCall, farSelectorShare, sema.CrossingLoweringChannelShare, false)
		})
		if err != nil || analysis == nil {
			t.Fatalf("return-origin analysis did not run: %v", err)
		}
		originExactPending(t, analysis, f.unit.SourceKey, originSpan{0, len(farSelectorShareSource), farSelectorShareSource}, nil)
		requireOriginSummary(t, analysis, f.owner.File.ID, "lease", false, nil)
	})
	t.Run("share_record_that_consumes_is_an_error", func(t *testing.T) {
		var call ast.ExprID
		f, analysis, err := farSelectorAnalyze(t, farSelectorShareSource, func(f originalGenericFixture) {
			var at int
			call, at = farSelectorRecord(t, f, farSelectorShareCall, farSelectorShare, sema.CrossingLoweringChannelShare, false)
			f.unit.Sema.CrossingLowering[at].ConsumesHandle = true
		})
		want := fmt.Sprintf("return origins: inconsistent far selector evidence for expression %d in %s", call, f.unit.SourceKey)
		if err == nil || analysis != nil || err.Error() != want {
			t.Errorf("want exact %q and nil analysis; got %v, %+v", want, err, analysis)
		}
	})
	// The checker types a stray argument and refuses nothing (spawn_on_crossing.go:172–175); the
	// analysis answers with a row of its own rather than an internal evidence error.
	t.Run("stray_argument_is_refused", func(t *testing.T) {
		const text = `fn wait_arg(t: far Task<int>) -> TaskResult<int> {
    return t.await(1);
}
`
		call := originSpan{62, 72, "t.await(1)"}
		checkOriginSource(t, text, "4960e3e1879dcbcaaac6879bf61f6bf134f13e28ed770b605965491f65219751", call)
		f, analysis, err := farSelectorAnalyze(t, text, nil)
		if err != nil || analysis == nil {
			t.Fatalf("return-origin analysis did not run: %v", err)
		}
		if !originPendingAt(analysis, f.unit.SourceKey, call, "`await()`, `cancel()` and `share()` on a far handle take no arguments; remove the argument") {
			t.Errorf("a far await with an argument was not refused by name: %+v", analysis.Pending)
		}
	})
}
