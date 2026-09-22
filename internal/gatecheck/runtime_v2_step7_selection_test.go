package gatecheck

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

type step7GateRow struct {
	name, target, pkg, tags, backend string
	tests                            []string
}

// Independent of both editable Makefile selectors; pairing a narrower -run
// with a narrower --expect must not remove a Step 7 proof from its home.
func step7GateRows() []step7GateRow {
	nativeOffer := []string{
		"TestRuntimeV2ChannelSendOfferTakeOrDrop",
		"TestRuntimeV2ChannelSendOfferOldAPINegativeControl",
		"TestRuntimeV2ChannelSendOfferRepeatedParkedCancellation",
		"TestRuntimeV2ChannelSendOfferClaimKeepsSenderAlive",
	}
	numericHeap := []string{
		"TestRuntimeV2NumericCastTemporariesValgrindZero",
		"TestRuntimeV2UintLiteralValueValgrindZero",
		"TestRuntimeV2BigfloatFromF64ExactValgrindZero",
		"TestRuntimeV2EntrypointArgvStorageValgrindZero",
		"TestRuntimeV2RangeNumericCallsValgrindZero",
		"TestRuntimeV2ChannelCancelFrameValgrindBaseline",
		"TestRuntimeV2FixnumFastPathHeapWitnessValgrindZero",
	}
	nativeStorage := []string{
		"TestRuntimeV2BigfloatFromF64Exact",
		"TestRuntimeV2BigfloatFromF64ExactUnderAddressAndUndefinedSanitizers",
		"TestRuntimeV2RangeInlineBoundsAvoidNumericCalls",
		"TestRuntimeV2RangeNumericCallsUnderAddressAndUndefinedSanitizers",
		"TestRuntimeV2ChannelCloseThenCancelRetainsStagedSend",
		"TestRuntimeV2ChannelCloseThenCancelUnderAddressAndUndefinedSanitizers",
		"TestRuntimeV2ChannelCancelKeepsLastFrameHandle",
		"TestRuntimeV2ChannelCancelFrameUnderAddressAndUndefinedSanitizers",
	}
	tripwireCompile := []string{
		"TestH2TripwireAwaitRunnerBuilds",
		"TestH2TripwireRefusedG5ModuleTimeout",
		"TestH2TripwireImportAddsCoreRowsG5b",
		"TestH2TripwireImportDifferentialRejectsTheTwin",
		"TestH2TripwireTaskCheckRefusesLeaks",
	}
	tripwireRuntime := []string{
		"TestH2TripwireNotRunnableG1OwnBinding",
		"TestH2TripwireNotRunnableG1bOwnExpr",
		"TestH2TripwireNotRunnableG3ScopeJoin",
		"TestH2TripwireNotRunnableG4xAsyncEntry",
		"TestH2TripwireNotRunnableRo6xAsyncEntry",
		"TestH2TripwireNotRunnableRo7xAsyncEntry",
		"TestH2TripwireWitnessControl",
		"TestH2TripwireHarnessObservesExit",
		"TestH2TripwireMatcherRejectsRecordedFaults",
		"TestH2TripwireTaskCheckRefusesLeakedRuns",
		"TestH2TripwireAwaitRunnerRuns",
	}
	return []step7GateRow{
		{"cast_offer_ir", "runtime-v2-carrier-check", "./internal/backend/llvm", "", "llvm", []string{"TestEmitNumericCastTemporaryCleanup", "TestEmitNumericCastBorrowedAndFixedControls", "TestChannelSendOfferUsesDisposablePollStorage"}},
		{"uint_offer_mir", "runtime-v2-carrier-check", "./internal/mir", "", "llvm", []string{"TestLowerUintLiteralPreservesKindAndText", "TestChannelSendPollPreservesTheCountedCopyOwner"}},
		{"argv_cfg", "runtime-v2-carrier-check", "./internal/vm", "runtime_v2_pending", "llvm", []string{"TestRuntimeV2EntrypointArgvBorrowsAndReleasesStorage"}},
		{"allocation_xml", "runtime-v2-carrier-check", "./internal/vm", "", "llvm", []string{"TestRuntimeV2AsyncAllocationXMLRejectsMalformedReports", "TestRuntimeV2AsyncAllocationXMLExactMultiset", "TestRuntimeV2AsyncAllocationBaselineOrigins"}},
		{"offer_vm", "runtime-v2-heap-check", "./internal/vm", "", "vm", []string{"TestRuntimeV2ChannelSendOfferPreservesOriginal"}},
		{"offer_llvm", "runtime-v2-heap-check", "./internal/vm", "", "llvm", []string{"TestRuntimeV2ChannelSendOfferPreservesOriginal", "TestRuntimeV2ChannelSendOfferValgrindBaseline"}},
		{"offer_native", "runtime-v2-heap-check", "./internal/vm", "runtime_v2_pending", "llvm", nativeOffer},
		{"numeric_heap", "runtime-v2-heap-check", "./internal/vm", "runtime_v2_pending", "llvm", numericHeap},
		{"failfast_return", "runtime-v2-heap-check", "./internal/vm", "runtime_v2_pending", "llvm", []string{"TestRuntimeV2FailfastReturnReleasesPreparedValue", "TestRuntimeV2FailfastReturnReleasesPreparedComposite"}},
		{"task_result_census", "runtime-v2-heap-check", "./internal/vm", "", "llvm", []string{"TestRuntimeV2TaskResultCensusBalanced", "TestRuntimeV2AsyncPlainCompositeReturn"}},
		{"allocation_negative", "runtime-v2-heap-check", "./internal/vm", "", "llvm", []string{"TestRuntimeV2AsyncAllocationBaselineRejectsRetainedChannel"}},
		{"native_storage", "runtime-v2-owned-storage-check", "./internal/vm", "runtime_v2_pending", "llvm", nativeStorage},
		{"panic_vm", "runtime-v2-panic-surface-check", "./internal/vm", "runtime_v2_pending", "vm", []string{"TestVMNumericCastFailureContract", "TestRuntimeV2EntrypointArgvFailurePaths"}},
		{"panic_llvm", "runtime-v2-panic-surface-check", "./internal/vm", "runtime_v2_pending", "llvm", []string{"TestVMNumericCastFailureContract", "TestRuntimeV2EntrypointArgvFailurePaths"}},
		{"fixed_abi", "runtime-v2-abi-manifest-check", "./internal/backend/llvm", "", "llvm", []string{"TestRuntimeOpaqueABIFieldsAreFixed64", "TestNetAsyncHandleSignaturesAreFixed64", "TestTermEventResizeABIIsFixed64"}},
		{"diagnostics_sema", "runtime-v2-crossing-check", "./internal/sema", "", "llvm", []string{"TestCountedBlockRefusalPaths", "TestCountedBlockRefusalPlainAndReachable", "TestCountedBlockRefusalIncomplete"}},
		{"diagnostics_compile", "runtime-v2-crossing-check", "./internal/buildpipeline", "", "llvm", []string{"TestCountedBlockDiagnosticRepairsAtAllSites", "TestCountedBlockDiagnosticRecursiveRepairs", "TestCountedBlockDiagnosticValidDeclarationsRepair", "TestCountedBlockDiagnosticDoesNotOfferFalseRepair"}},
		{"sweep_offer_native", "runtime-v2-carrier-sanitizer-check", "./internal/vm", "runtime_v2_pending", "llvm", nativeOffer},
		{"sweep_allocation", "runtime-v2-carrier-sanitizer-check", "./internal/vm", "", "llvm", []string{"TestRuntimeV2ChannelSendOfferValgrindBaseline", "TestRuntimeV2AsyncAllocationBaselineRejectsRetainedChannel"}},
		{"sweep_numeric_heap", "runtime-v2-carrier-sanitizer-check", "./internal/vm", "runtime_v2_pending", "llvm", numericHeap},
		{"sweep_native_storage", "runtime-v2-carrier-sanitizer-check", "./internal/vm", "runtime_v2_pending", "llvm", nativeStorage},
		{"h2_tripwire_compile", "runtime-v2-h2-tripwire-check", "./internal/driver", "", "llvm", tripwireCompile},
		{"h2_tripwire_vm", "runtime-v2-h2-tripwire-check", "./internal/vm", "", "vm", tripwireRuntime},
		{"h2_tripwire_llvm", "runtime-v2-h2-tripwire-check", "./internal/vm", "", "llvm", tripwireRuntime},
	}
}

func step7GateSelection(makefile string, row step7GateRow) (Gate, error) {
	if row.target != carrierSanitizerTarget && !ReachableTargets(makefile, "runtime-v2-check")[row.target] {
		return Gate{}, fmt.Errorf("%s is absent from the Runtime V2 aggregate", row.target)
	}
	lines := strings.Split(makefile, "\n")
	var matches []Gate
	counts := map[string]int{}
	for _, gate := range ParseGates(makefile) {
		line := lines[gate.Line-1]
		if gate.Target != row.target || !hasField(line, "SURGE_BACKEND="+row.backend) {
			continue
		}
		declared := expectedTests(line)
		for _, name := range declared {
			counts[name]++
		}
		if slices.Contains(declared, row.tests[0]) {
			matches = append(matches, gate)
		}
	}
	if len(matches) != 1 {
		return Gate{}, fmt.Errorf("expected one %s/%s row declaring %s, got %d", row.target, row.backend, row.tests[0], len(matches))
	}
	gate := matches[0]
	line := lines[gate.Line-1]
	if !slices.Equal(gate.Packages, []string{row.pkg}) || gate.Tags != row.tags {
		return Gate{}, fmt.Errorf("package/tags changed: %+v", gate)
	}
	if !strings.Contains(line, "bash scripts/runtime_v2_carrier_sanitizer_check.sh run --expect ") {
		return Gate{}, fmt.Errorf("row bypasses the skip/empty/expected-test wrapper")
	}
	for _, field := range []string{
		"SURGE_GATE_NAME=" + row.target, "SURGE_SKIP_TIMEOUT_TESTS=0",
		"-short=false", "-count=1", "-parallel=1", "-p=1", "-v",
	} {
		if !hasField(line, field) {
			return Gate{}, fmt.Errorf("row lost required field %s", field)
		}
	}
	want, declared := slices.Clone(row.tests), expectedTests(line)
	slices.Sort(want)
	slices.Sort(declared)
	if !slices.Equal(declared, want) {
		return Gate{}, fmt.Errorf("declared census changed: got %v, want %v", declared, want)
	}
	for _, name := range row.tests {
		if counts[name] != 1 {
			return Gate{}, fmt.Errorf("%s/%s declares %s %d times, want once", row.target, row.backend, name, counts[name])
		}
	}
	if gate.Run == "" {
		return Gate{}, fmt.Errorf("row has no explicit live selection")
	}
	return gate, nil
}

func TestStep7GateCoverage(t *testing.T) {
	makefile := makefileText(t)
	for _, row := range step7GateRows() {
		t.Run(row.name, func(t *testing.T) {
			gate, err := step7GateSelection(makefile, row)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := ListTests(repoRoot(t), gate.Packages, gate.Tags, gate.Run)
			if err != nil {
				t.Fatal(err)
			}
			want := slices.Clone(row.tests)
			slices.Sort(want)
			slices.Sort(selected)
			if !slices.Equal(selected, want) {
				t.Fatalf("live selection changed: got %v, want %v", selected, want)
			}
		})
	}
}

// These controls exercise the declaration contract without invoking a compiler.
// The positive test above delegates actual Go selection semantics to ListTests.
func TestStep7GateCoverageRejectsDrift(t *testing.T) {
	row := step7GateRows()[0]
	line := "\tSURGE_GATE_NAME=" + row.target + " SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 " +
		"bash scripts/runtime_v2_carrier_sanitizer_check.sh run --expect " + strings.Join(row.tests, ",") +
		" -- $(GO) test " + row.pkg + " -run '^(" + strings.Join(row.tests, "|") +
		")$$' -short=false -count=1 -parallel=1 -p=1 -v --timeout 180s\n"
	fixture := "runtime-v2-check: " + row.target + "\n" + row.target + ":\n" + line
	if _, err := step7GateSelection(fixture, row); err != nil {
		t.Fatalf("valid control fixture: %v", err)
	}
	sweep := row
	sweep.target = carrierSanitizerTarget
	sweepFixture := strings.ReplaceAll(fixture, row.target, sweep.target)
	if _, err := step7GateSelection(sweepFixture, sweep); err != nil {
		t.Fatalf("valid sweep control fixture: %v", err)
	}
	last := row.tests[len(row.tests)-1]
	for _, tc := range []struct {
		name, makefile, want string
		row                  step7GateRow
	}{
		{"paired_census_narrowing", strings.ReplaceAll(strings.ReplaceAll(fixture, ","+last, ""), "|"+last, ""), "declared census", row},
		{"wrong_tag", strings.Replace(fixture, "test "+row.pkg, "test -tags runtime_v2_pending "+row.pkg, 1), "package/tags", row},
		{"wrong_backend", strings.Replace(fixture, "SURGE_BACKEND=llvm", "SURGE_BACKEND=vm", 1), "expected one", row},
		{"missing_required_flag", strings.Replace(fixture, " -p=1", "", 1), "required field -p=1", row},
		{"missing_wrapper", strings.Replace(fixture, "bash scripts/runtime_v2_carrier_sanitizer_check.sh run ", "", 1), "wrapper", row},
		{"duplicate_row", fixture + line, "expected one", row},
		{"lost_aggregate_home", strings.Replace(fixture, "runtime-v2-check: "+row.target, "runtime-v2-check:", 1), "aggregate", row},
		{"missing_sweep_row", fixture, "expected one", sweep},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := step7GateSelection(tc.makefile, tc.row); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q refusal, got %v", tc.want, err)
			}
		})
	}
}
