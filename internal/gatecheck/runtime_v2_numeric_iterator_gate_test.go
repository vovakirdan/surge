package gatecheck

import (
	"slices"
	"strings"
	"testing"
)

// These names are independent of the Makefile's two editable declarations:
// narrowing --expect and -run together must still fail this census. The live
// selector is the existing go test -list helper, not another regexp evaluator.
func TestNumericIteratorGateCoverage(t *testing.T) {
	makefile := makefileText(t)
	lines := strings.Split(makefile, "\n")
	gates := ParseGates(makefile)
	reachable := ReachableTargets(makefile, "runtime-v2-check")
	functional := []string{
		"TestVMNumericIteratorFloatArrays",
		"TestVMNumericIteratorFastLatch",
		"TestVMNumericIteratorSuspendLifecycle",
	}
	heap := []string{
		"TestRuntimeV2NumericIteratorFloatArraysValgrindZero",
		"TestRuntimeV2NumericIteratorFloatBoundsValgrindZero",
		"TestRuntimeV2NumericIteratorSuspendValgrindZero",
		"TestRuntimeV2RangeForFloatBoundsIterateAndAnswer",
	}
	for _, row := range []struct {
		name, target, pkg, backend string
		tests                      []string
	}{
		{"hir", "runtime-v2-carrier-check", "./internal/hir", "llvm", []string{
			"TestNumericForUsesOwnedCandidatesAndSinglePost",
			"TestIteratorOwnershipCandidatesAndElementFallback",
			"TestWhilePostNormalizationAndLegacyReleaseTraversal",
			"TestWhilePostPrintingAndClassicForRemainDistinct",
		}},
		{"mono", "runtime-v2-carrier-check", "./internal/mono", "llvm", []string{
			"TestWhilePostCloneSubstitutionAndTypes",
			"TestWhilePostSymbolRewritesAndDCEEdges",
			"TestWhilePostTraversalErrorsAndNil",
			"TestGeneratedLoopCandidateSurvivesMonomorphization",
		}},
		{"mir", "runtime-v2-carrier-check", "./internal/mir", "llvm", []string{
			"TestGeneratedLoopOwnershipUsesActualType",
			"TestGeneratedResourceOnlyReplacesLegacyWholeLocalRelease",
			"TestNumericPostActualOwnershipPreservesReferences",
			"TestGeneratedLoopLexicalDropOrderAndInitializerFrontier",
			"TestGeneratedLoopReturnDetachesValueBeforeDrops",
			"TestGeneratedLoopBlockReturnsReleaseOnlyExitedFrames",
			"TestNumericLoopLatchAndExitOwnership",
			"TestNumericLoopPostDropsOnlyOwningConcreteLocals",
		}},
		{"llvm_yield", "runtime-v2-carrier-check", "./internal/backend/llvm", "llvm", []string{
			"TestArrayIteratorFloatYieldRetainsOnce",
		}},
		{"vm_source", "runtime-v2-carrier-check", "./internal/vm", "vm", functional},
		{"llvm_source", "runtime-v2-carrier-check", "./internal/vm", "llvm", functional},
		{"suspend_frame", "runtime-v2-carrier-check", "./internal/vm", "vm", []string{
			"TestNumericIteratorSuspendFrameOwnsLiveBindings",
		}},
		{"heap", "runtime-v2-heap-check", "./internal/vm", "llvm", heap},
		{"sanitizer_superset", carrierSanitizerTarget, "./internal/vm", "llvm", heap},
	} {
		t.Run(row.name, func(t *testing.T) {
			if row.target != carrierSanitizerTarget && !reachable[row.target] {
				t.Fatalf("%s is absent from the Runtime V2 aggregate", row.target)
			}
			var matches []Gate
			for _, gate := range gates {
				line := lines[gate.Line-1]
				if gate.Target == row.target && hasField(line, "SURGE_BACKEND="+row.backend) &&
					slices.Contains(expectedTests(line), row.tests[0]) {
					matches = append(matches, gate)
				}
			}
			if len(matches) != 1 {
				t.Fatalf("expected one %s/%s row declaring %s, got %d", row.target, row.backend, row.tests[0], len(matches))
			}
			gate := matches[0]
			line := lines[gate.Line-1]
			if !slices.Equal(gate.Packages, []string{row.pkg}) || gate.Tags != "" {
				t.Fatalf("iterator row changed package/tag selection: %+v", gate)
			}
			if !strings.Contains(line, "bash scripts/runtime_v2_carrier_sanitizer_check.sh run --expect ") {
				t.Fatal("iterator row bypasses the skip/empty/expected-test detector")
			}
			for _, field := range []string{
				"SURGE_GATE_NAME=" + row.target, "SURGE_SKIP_TIMEOUT_TESTS=0",
				"-count=1", "-parallel=1", "-p=1", "-v",
			} {
				if !hasField(line, field) {
					t.Errorf("iterator row lost required field %s", field)
				}
			}
			want := slices.Clone(row.tests)
			slices.Sort(want)
			declared := expectedTests(line)
			slices.Sort(declared)
			if !slices.Equal(declared, want) {
				t.Fatalf("iterator declared census changed: got %v, want %v", declared, want)
			}
			selected, err := ListTests(repoRoot(t), gate.Packages, gate.Tags, gate.Run)
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(selected)
			if !slices.Equal(selected, want) {
				t.Fatalf("iterator live selection changed: got %v, want %v", selected, want)
			}
		})
	}
}
