package driver

import "testing"

const genericCrossingSource = `fn route<T>(dst: Placement) -> TaskResult<T> {
    return on dst {
        ret default::<T>();
    };
}
fn use_int(dst: Placement) -> TaskResult<int> {
    return route::<int>(dst);
}
fn use_bool(dst: Placement) -> TaskResult<bool> {
    return route::<bool>(dst);
}
fn use_ref(dst: Placement) -> TaskResult<&int> {
    return route::<&int>(dst);
}
`

func TestAnalyzeGenericCrossingReplyCondition(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "generic_crossing_reply", genericCrossingSource, false, nil)
	for _, name := range []string{"use_int", "use_bool"} {
		fn := patternFn(t, genericCrossingSource, "fn "+name+"(")
		originExactPending(t, analysis, f.unit.SourceKey, fn, nil)
		originNoEscape(t, analysis, f.owner.File.ID, fn)
	}
	ref := patternFn(t, genericCrossingSource, "fn use_ref(")
	call := patternIn(t, genericCrossingSource, ref, "route::<&int>(dst)")
	if !originPendingAt(analysis, f.unit.SourceKey, call, genericConditionRefuted) {
		t.Errorf("reference reply lost its concrete NoBorrowedState refusal: %+v", originPendingWithin(analysis, f.unit.SourceKey, ref.start, ref.end))
	}
}
