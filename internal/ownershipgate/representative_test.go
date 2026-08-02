package ownershipgate_test

import (
	"context"
	"path/filepath"
	"testing"

	"surge/internal/buildpipeline"
	"surge/internal/mir"
	"surge/internal/project"
)

// This deliberately small sample keeps the development ownership gate inside
// make check's ordinary package budget. Together the rows cover fresh values,
// aliases, transfers, retains/clones/constants/addresses, aggregate and
// projected stores, returns, calls, channels, local/far select, async task
// consumption, timeout, and crossing state. The exhaustive full corpus remains
// the separately timed runtime-v2-ownership-check target.
const ownershipRepresentativeCount = 22

var ownershipRepresentativeFixtures = []string{
	"testdata/golden/crossing/block02/valid/on_positive_async_crosses_fn.sg",
	"testdata/golden/mir/array_field_mut_ref_reborrow.sg",
	"testdata/golden/mir/array_fixed_struct.sg",
	"testdata/golden/mir/compare_guard_await_release.sg",
	"testdata/golden/mir/erring_option_nested_tag.sg",
	"testdata/golden/mir/imported_magic_methods.sg",
	"testdata/golden/mir/is_heir_ops.sg",
	"testdata/golden/mir/magic_ops_call.sg",
	"testdata/golden/mir/option_match.sg",
	"testdata/golden/mir/short_circuit.sg",
	"testdata/golden/sema/ownership_and_references/compare_return_move_ok.sg",
	"testdata/golden/sema/ownership_and_references/explicit_own_move.sg",
	"testdata/golden/sema/valid/concurrency/channel_basic_ops.sg",
	"testdata/golden/sema/valid/concurrency/task_pass_to_fn.sg",
	"testdata/golden/sema/valid/recursive_handles.sg",
	"testdata/golden/sema/valid/select_send_own_binding.sg",
	"testdata/golden/sema/valid/tuple_access.sg",
	"testdata/golden/vm_arrays/arrays_drop_nested.sg",
	"testdata/golden/vm_arrays/arrays_push_pop.sg",
	"testdata/golden/vm_async_suite/t20_select_default.sg",
	"testdata/golden/vm_async_suite/t25_select_task_vs_timer.sg",
	"testdata/golden/vm_maps/map_literal_order.sg",
}

func TestOwnershipRepresentativeCorpusPassesDevGate(t *testing.T) {
	root := ownershipRepoRoot(t)
	t.Setenv("SURGE_STDLIB", root)
	if got := len(ownershipRepresentativeFixtures); got != ownershipRepresentativeCount {
		t.Fatalf("representative ownership fixture count = %d, want %d after an explicit coverage review",
			got, ownershipRepresentativeCount)
	}

	seen := make(map[string]struct{}, len(ownershipRepresentativeFixtures))
	for _, relative := range ownershipRepresentativeFixtures {
		if _, duplicate := seen[relative]; duplicate {
			t.Fatalf("duplicate representative ownership fixture %q", relative)
		}
		seen[relative] = struct{}{}

		t.Run(relative, func(t *testing.T) {
			result, err := buildpipeline.Compile(context.Background(), &buildpipeline.CompileRequest{
				TargetPath:     filepath.Join(root, filepath.FromSlash(relative)),
				BaseDir:        root,
				RootKind:       project.ModuleKindUnknown,
				MaxDiagnostics: 500,
				Analysis:       true,
				Dev:            true,
				Backend:        buildpipeline.BackendLLVM,
			})
			if err != nil {
				t.Fatalf("development ownership compile failed: %v", err)
			}
			if result.MIR == nil || result.Diagnose == nil || result.Diagnose.Sema == nil {
				t.Fatalf("development ownership compile returned incomplete result: MIR=%v Diagnose=%v Sema=%v",
					result.MIR != nil, result.Diagnose != nil,
					result.Diagnose != nil && result.Diagnose.Sema != nil)
			}
			if findings := mir.VerifyOwnership(
				result.MIR,
				result.Diagnose.Sema.TypeInterner,
				result.Diagnose.Sema,
			); len(findings) != 0 {
				t.Fatalf("representative fixture still has report-only findings after passing Dev: %v", findings)
			}
		})
	}
}
