package gatecheck

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The mandatory carrier sanitizer gate of epic 23b section 12. It was named
// as mandatory in three documents and had no Makefile target at all, so it had
// never run once. These tests keep both halves of it honest: the tool
// availability that must fail closed, and the rows that must be seen to run.
const carrierSanitizerTarget = "runtime-v2-carrier-sanitizer-check"

func carrierSanitizerScript(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts", "runtime_v2_carrier_sanitizer_check.sh")
}

// runCarrierSanitizerScript drives the gate script and returns its combined
// output with the exit status; a signal or a start failure fails the test
// rather than being reported as an exit code.
func runCarrierSanitizerScript(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{carrierSanitizerScript(t)}, args...)...) // #nosec G204 -- fixed interpreter; arguments are repository-owned gate metadata.
	cmd.Dir = repoRoot(t)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("run gate script %v: %v\n%s", args, err, output)
	}
	return string(output), exit.ExitCode()
}

// writeExecutable writes a runnable shell script and returns its path.
func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\n"+body), 0o700); err != nil { // #nosec G302 -- must be executable to stand in for a row.
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// A tool the gate declares mandatory must fail the gate when it is absent, by
// name, before any row runs. PATH is emptied rather than doctored: the check
// under test uses only shell builtins up to that point, so the refusal is the
// script's own and not an artifact of some other missing tool.
func TestCarrierSanitizerPreflightFailsClosedOnAMissingTool(t *testing.T) {
	empty := t.TempDir()
	output, status := runCarrierSanitizerScript(t, []string{"PATH=" + empty}, "preflight")
	if status == 0 {
		t.Fatalf("preflight passed with an empty PATH; a mandatory tool gate must fail closed\n%s", output)
	}
	want := "Valgrind is not on PATH"
	if machine := bashMachineType(t); !strings.HasPrefix(machine, "x86_64-") ||
		!strings.HasSuffix(machine, "-linux-gnu") {
		// Off the reference runner the gate refuses even earlier, and says so.
		want = "the reference runner is x86_64-linux-gnu"
	}
	if !strings.Contains(output, want) {
		t.Fatalf("preflight did not name the missing requirement %q\n%s", want, output)
	}
	if strings.Contains(output, "SKIP") {
		t.Fatalf("preflight skipped instead of failing\n%s", output)
	}
}

func bashMachineType(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("bash", "-c", `printf %s "$MACHTYPE"`).Output()
	if err != nil {
		t.Fatalf("read MACHTYPE: %v", err)
	}
	return string(out)
}

// The other half of section 12: skip-on-missing disabled. `go test` reports
// ok/PASS for a package whose tests all skipped and for a selection that
// matched nothing, so a gate that only reads the exit status counts an absent
// row as a passing one. Each row below is a real shape that reached this
// repository's gates.
func TestCarrierSanitizerRowMustActuallyRun(t *testing.T) {
	dir := t.TempDir()
	row := writeExecutable(t, dir, "row.sh", `
case "$1" in
  skip)  printf '    --- SKIP: TestCarrier/fixed (0.00s)\nPASS\nok  \tsurge/internal/vm\t0.00s\n' ;;
  empty) printf 'testing: warning: no tests to run\nPASS\nok  \tsurge/internal/vm\t0.00s [no tests to run]\n' ;;
  broken) printf -- '--- FAIL: TestCarrier\n'; exit 3 ;;
  *)     printf -- '--- PASS: TestCarrier (0.01s)\nPASS\nok  \tsurge/internal/vm\t0.01s\n' ;;
esac
`)
	for _, test := range []struct {
		name   string
		mode   string
		wantOK bool
		want   string
	}{
		{name: "a skipped row fails", mode: "skip", want: "skip-on-missing disabled"},
		{name: "an empty selection fails", mode: "empty", want: "skip-on-missing disabled"},
		{name: "a failing row fails", mode: "broken", want: "row failed with exit 3"},
		{name: "a row that ran passes", mode: "ran", wantOK: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, status := runCarrierSanitizerScript(t, os.Environ(), "run", row, test.mode)
			if test.wantOK {
				if status != 0 {
					t.Fatalf("a row that really ran was rejected (exit %d)\n%s", status, output)
				}
				return
			}
			if status == 0 {
				t.Fatalf("mode %q passed the gate\n%s", test.mode, output)
			}
			if !strings.Contains(output, test.want) {
				t.Fatalf("mode %q did not report %q\n%s", test.mode, test.want, output)
			}
		})
	}
}

// The partial-selection hole, which the exit status cannot see: a `-run`
// alternation whose members were deleted or renamed away still exits 0 on the
// survivors. The row below stands in for exactly that — three tests declared,
// only one executed — and the gate must refuse it. This is the shape that
// leaves a mandatory sanitizer gate green with zero sanitizer execution.
func TestCarrierSanitizerRowMustExecuteEveryExpectedTest(t *testing.T) {
	dir := t.TempDir()
	row := writeExecutable(t, dir, "row.sh", `
printf -- '=== RUN   TestSurvivor\n--- PASS: TestSurvivor (0.00s)\n'
printf -- '--- PASS: TestPrefixLonger (0.00s)\n'
printf -- '    --- PASS: TestParent/child (0.00s)\n'
printf 'PASS\nok  \tsurge/internal/vm\t0.00s\n'
`)
	for _, test := range []struct {
		name   string
		expect string
		wantOK bool
		want   string
	}{
		{
			name:   "a test that never ran fails the row",
			expect: "TestSurvivor,TestDeletedSanitizerRow",
			want:   `TestDeletedSanitizerRow never printed "--- PASS:"`,
		},
		{
			name:   "a longer test name may not stand in for a shorter one",
			expect: "TestPrefix",
			want:   `TestPrefix never printed "--- PASS:"`,
		},
		{
			name:   "a subtest may not stand in for its parent",
			expect: "TestParent",
			want:   `TestParent never printed "--- PASS:"`,
		},
		{
			name:   "every declared test really ran",
			expect: "TestSurvivor,TestPrefixLonger,TestParent/child",
			wantOK: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, status := runCarrierSanitizerScript(t, os.Environ(),
				"run", "--expect", test.expect, "--", row)
			if test.wantOK {
				if status != 0 {
					t.Fatalf("a row that ran every declared test was rejected (exit %d)\n%s", status, output)
				}
				return
			}
			if status == 0 {
				t.Fatalf("expect list %q passed the gate\n%s", test.expect, output)
			}
			if !strings.Contains(output, test.want) {
				t.Fatalf("expect list %q did not report %q\n%s", test.expect, test.want, output)
			}
		})
	}
}

// Both declarations a `go test` row must carry. Without -v a skipped test
// prints nothing at all, so the skip detector would have nothing to read;
// without --expect a row that lost part of its selection is indistinguishable
// from one that ran in full. The gate refuses such a row instead of trusting
// it.
func TestCarrierSanitizerRowRefusesUndeclaredGoTestRows(t *testing.T) {
	dir := t.TempDir()
	shim := writeExecutable(t, dir, "go", "printf -- '--- PASS: TestCarrier (0.00s)\\nok\\n'\n")
	// The verbose row is spelled out rather than built with `append(goRow, "-v")`.
	// Two call sites below need it, and appending to a shared base hands both of
	// them the same backing array the moment that base carries spare capacity —
	// which is exactly what preallocating goRow would introduce.
	goRow := []string{shim, "test", "./internal/vm", "-count=1"}
	goRowVerbose := []string{shim, "test", "./internal/vm", "-count=1", "-v"}

	output, status := runCarrierSanitizerScript(t, os.Environ(), append([]string{"run"}, goRow...)...)
	if status == 0 {
		t.Fatalf("a go test row without -v was accepted\n%s", output)
	}
	if !strings.Contains(output, "row must pass -v") {
		t.Fatalf("refusal did not name the missing -v\n%s", output)
	}

	output, status = runCarrierSanitizerScript(t, os.Environ(), append([]string{"run"}, goRowVerbose...)...)
	if status == 0 {
		t.Fatalf("a go test row without --expect was accepted\n%s", output)
	}
	if !strings.Contains(output, "must declare the tests it executes with --expect") {
		t.Fatalf("refusal did not name the missing --expect\n%s", output)
	}

	args := append([]string{"run", "--expect", "TestCarrier", "--"}, goRowVerbose...)
	output, status = runCarrierSanitizerScript(t, os.Environ(), args...)
	if status != 0 {
		t.Fatalf("a fully declared go test row was rejected (exit %d)\n%s", status, output)
	}
}

func makefileText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	return string(data)
}

// recipeLines returns the recipe body of one Makefile target.
func recipeLines(makefile, target string) []string {
	var body []string
	inside := false
	for _, line := range strings.Split(makefile, "\n") {
		if strings.HasPrefix(line, "\t") {
			if inside {
				body = append(body, line)
			}
			continue
		}
		inside = strings.HasPrefix(line, target+":")
	}
	return body
}

// expectedTests returns the names a recipe row declares with --expect.
func expectedTests(line string) []string {
	fields := strings.Fields(line)
	for i, field := range fields {
		if field != "--expect" || i+1 >= len(fields) {
			continue
		}
		var names []string
		for _, name := range strings.Split(fields[i+1], ",") {
			if name != "" {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}

// requiredSanitizerCoverage is what the mandatory gate must actually execute,
// recorded HERE and deliberately not in the Makefile.
//
// The --expect lists and the -run alternations both live in the Makefile, so
// cross-checking one against the other cannot see a row that shrank in both at
// once: narrow `-run` to a single member and drop the other names from
// `--expect`, and the row stays perfectly self-consistent while the mandatory
// sanitizer gate goes green on one 0.00s unit test with no sanitizer execution.
// That was demonstrated against the earlier shape of this file. A ratchet in a
// second file is what closes it: shrinking the gate now means editing this list
// too, which is a visible, reviewable act rather than a quiet one.
//
// Removing a name from this list is legitimate only when the test it names is
// itself gone, and then the deletion belongs in the same commit as the test.
var requiredSanitizerCoverage = []string{
	// ASan/UBSan and TSan over the typed carrier slot API, plus the proof that
	// the sanitizer requirement itself fails closed.
	"TestRuntimeV2SlotControlAddressAndUndefinedSanitizers",
	"TestRuntimeV2SlotControlThreadSanitizer",
	"TestRuntimeV2SlotControlRequiredSanitizersFailClosed",
	// Valgrind over far-channel handle/object reclamation and the crossing census.
	"TestRuntimeV2DropFarChannelHandleAndObjectValgrindZero",
	"TestRuntimeV2CrossingStrictCensusValgrindBounded",
	// Valgrind over a map's teardown, at one shard and at four: a map owns its
	// keys and values, and reclaiming them is what RV2-DEBT-156 was about.
	"TestRuntimeV2MapOwnedEntriesValgrindZero",
	// Valgrind, ASan/UBSan and TSan over a channel's own lifetime: the object
	// behind a handle is reclaimed by the last release and not before
	// (RV2-DEBT-155). The leak figure is the only instrument that can say the
	// CHANNEL was destroyed rather than only the payloads inside it.
	"TestRuntimeV2ChannelHandleRefcountValgrindZero",
	"TestRuntimeV2ChannelHandleRefcountUnderAddressAndUndefinedSanitizers",
	"TestRuntimeV2ChannelHandleRefcountUnderThreadSanitizer",
	// Valgrind over a blocking job's captured state, at one iteration and at
	// eight, and over the zero-sized state a capture-less body still gets.
	// RV2-DEBT-080 recorded its loss as CONSTANT in the iteration count and
	// left it unattributed, so both counts are the row: at one iteration a
	// per-execution loss and a one-off loss are the same number.
	"TestRuntimeV2BlockingCaptureValgrindZero",
	"TestRuntimeV2BlockingCapturelessStateIsFreed",
	// The race detector over the carrier bench bridge.
	"TestRuntimeV2CarrierBenchBlockingRegisterThenVerify",
	"TestRuntimeV2CarrierBenchCounterMatrix",
	"TestRuntimeV2CarrierBenchBridgeHasNoHotPathEnvironmentLookup",
}

// The target's shape is the gate. Every row must reach the wrapper, because a
// row invoked directly would be back to trusting `go test`'s exit status, and
// availability must be settled before the first row rather than discovered
// halfway through one. Every row must also declare the tests it executes, and
// the rows together must cover requiredSanitizerCoverage.
func TestCarrierSanitizerMakefileTargetShape(t *testing.T) {
	makefile := makefileText(t)
	body := recipeLines(makefile, carrierSanitizerTarget)
	if len(body) == 0 {
		t.Fatalf("%s has no recipe; section 12 names it mandatory", carrierSanitizerTarget)
	}

	preflight := -1
	var declared [][]string
	for i, line := range body {
		if strings.Contains(line, "runtime_v2_carrier_sanitizer_check.sh preflight") && preflight < 0 {
			preflight = i
		}
		if !strings.Contains(line, "$(GO) test") {
			continue
		}
		row := strings.TrimSpace(line)
		if !strings.Contains(line, "runtime_v2_carrier_sanitizer_check.sh run") {
			t.Errorf("row bypasses the skip detector: %s", row)
		}
		// A field match, not " -v ": a row ending in -v carries it too.
		if !hasField(line, "-v") {
			t.Errorf("row cannot show a skip without -v: %s", row)
		}
		names := expectedTests(line)
		if len(names) == 0 {
			t.Errorf("row declares no --expect tests, so a lost -run member would pass unseen: %s", row)
		}
		declared = append(declared, names)
		if preflight < 0 {
			t.Errorf("row runs before tool availability is proven: %s", row)
		}
	}
	if preflight < 0 {
		t.Errorf("%s never runs the availability preflight", carrierSanitizerTarget)
	}
	if len(declared) == 0 {
		t.Errorf("%s runs no carrier row at all", carrierSanitizerTarget)
	}

	// The ratchet. Both fields a row could be narrowed by live in the Makefile,
	// so this list — which does not — is the only thing that can notice the gate
	// getting smaller.
	covered := make(map[string]bool)
	for _, names := range declared {
		for _, name := range names {
			covered[name] = true
		}
	}
	for _, want := range requiredSanitizerCoverage {
		if !covered[want] {
			t.Errorf("%s no longer executes %s; the mandatory gate may not shrink by "+
				"narrowing a row's --expect and -run together. If that test is gone, "+
				"delete it from requiredSanitizerCoverage in the same commit that deletes it",
				carrierSanitizerTarget, want)
		}
	}

	// A closeout gate, not a pre-commit one: make check is the hook, and this
	// target spends minutes under valgrind. It therefore needs its owned
	// exemption from the reachability rule instead.
	if ReachableTargets(makefile, "check")[carrierSanitizerTarget] {
		t.Errorf("%s is reachable from check; it is a closeout gate, not a pre-commit hook", carrierSanitizerTarget)
	}
	ledger, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "gatecheck", "exemptions.txt"))
	if err != nil {
		t.Fatalf("read exemptions: %v", err)
	}
	if !strings.Contains(string(ledger), "gate "+carrierSanitizerTarget+" ") {
		t.Errorf("%s has no owned exemption entry", carrierSanitizerTarget)
	}

	t.Run("declared tests are live", func(t *testing.T) {
		if testing.Short() {
			t.Skip("the live cross-check invokes go test -list; skipped in -short")
		}
		root := repoRoot(t)
		var gates []Gate
		for _, gate := range ParseGates(makefile) {
			if gate.Target == carrierSanitizerTarget {
				gates = append(gates, gate)
			}
		}
		if len(gates) != len(declared) {
			t.Fatalf("parsed %d gate rows for %d recipe rows; the row parsers disagree",
				len(gates), len(declared))
		}
		// Only this direction is enforced. A declared test that the row's own
		// -run no longer selects is the rot this gate exists to catch — the
		// test was deleted or renamed and the row silently shrank. The other
		// direction (a newly added test the row selects but does not declare)
		// costs the gate no coverage, and enforcing it would red every lane
		// that adds a test in one of these families.
		for i, gate := range gates {
			live, listErr := ListTests(root, gate.Packages, gate.Tags, gate.Run)
			if listErr != nil {
				t.Errorf("row %d (Makefile line %d): %v", i+1, gate.Line, listErr)
				continue
			}
			selected := make(map[string]bool, len(live))
			for _, name := range live {
				selected[name] = true
			}
			for _, name := range declared[i] {
				if !selected[name] {
					t.Errorf("row %d (Makefile line %d) declares --expect %s, which -run %q under -tags %q no longer selects",
						i+1, gate.Line, name, gate.Run, gate.Tags)
				}
			}
		}
	})
}

func hasField(line, want string) bool {
	for _, field := range strings.Fields(line) {
		if field == want {
			return true
		}
	}
	return false
}
