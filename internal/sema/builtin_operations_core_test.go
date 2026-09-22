package sema

import "testing"

// When the merge folds the records of one builtin operation, the standard library's
// record survives however a copy of core read from elsewhere sorts against `core/`.
// 4 RUN: 1 parent, 3 leaves.
func TestMergeKeepsTheStandardLibraryBuiltinRecord(t *testing.T) {
	for _, row := range []struct {
		name, first, second, survivor string
		symbol                        uint32
	}{
		{"copy_sorts_before_core", "aaa/core_copy/intrinsics", "core/intrinsics", "core/intrinsics", 12},
		{"copy_sorts_after_core", "zzz/core_copy/intrinsics", "core/intrinsics", "core/intrinsics", 12},
		{"no_standard_library_record", "aaa/copy/intrinsics", "bbb/copy/intrinsics", "aaa/copy/intrinsics", 11},
	} {
		t.Run(row.name, func(t *testing.T) {
			dst := &Result{CallableCandidates: []CallableCandidate{builtinCloneRecord(11, row.first)}}
			src := &Result{CallableCandidates: []CallableCandidate{builtinCloneRecord(12, row.second)}}
			MergeInstantiationGraphs(dst, src, nil)
			if len(dst.CallableCandidates) != 1 || dst.CallableCandidates[0].ModulePath != row.survivor || uint32(dst.CallableCandidates[0].Symbol) != row.symbol {
				t.Fatalf("merged catalog = %+v, want the one record of %s (symbol %d)", dst.CallableCandidates, row.survivor, row.symbol)
			}
		})
	}
}
