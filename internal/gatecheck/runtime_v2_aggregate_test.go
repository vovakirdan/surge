package gatecheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// AN AGGREGATE GATE MUST NOT SPEAK FOR SUB-GATES IT NEVER RAN.
//
// `runtime-v2-check` used to be a plain sequence of recipe lines, one per
// sub-gate. Make aborts a recipe at its first failing line, so the moment one
// sub-gate went red the rows behind it stopped executing — and the log said
// nothing about them, because a recipe that was never reached prints nothing at
// all. A silent stop and a pass looked identical. One sub-gate stayed red long
// enough that fourteen others had not run in CI for months while the aggregate
// still reported as the gate covering all of them.
//
// What has to hold is therefore behavioural, not cosmetic: an early red sub-gate
// must not prevent the later ones from running, each sub-gate's verdict must
// appear against its own name, and the aggregate must still fail at the end.
//
// The behavioural half drives the REAL `runtime-v2-check` with a substitute
// roster of probe targets — two red, two green, a red one first — injected
// through MAKEFILES, which both the outer make and its sub-makes read. So this
// tests the shipped recipe rather than a copy of it, and it costs a second
// instead of the hours the true sub-gates take.
func TestRuntimeV2CheckRunsAndReportsEverySubGate(t *testing.T) {
	root := repoRoot(t)
	makefileBytes, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("gatecheck: read Makefile: %v", err)
	}
	makefile := string(makefileBytes)

	roster := parseListVars(makefile)["RUNTIME_V2_SUBGATES"]
	if len(roster) == 0 {
		t.Fatal("gatecheck: RUNTIME_V2_SUBGATES is empty or missing; runtime-v2-check has no roster to walk, " +
			"so its sub-gates can only be recipe lines and the first red one silently drops the rest")
	}

	recipe, ok := recipeOf(makefile, "runtime-v2-check")
	if !ok {
		t.Fatal("gatecheck: no runtime-v2-check target in the Makefile")
	}
	for _, line := range recipe {
		for _, m := range makeCallRe.FindAllStringSubmatch(line, -1) {
			t.Fatalf("gatecheck: runtime-v2-check calls %q from a recipe line of its own. "+
				"Make abandons the recipe at the first failing line, so every sub-gate written below that call "+
				"goes unrun and unreported. Sub-gates belong on the RUNTIME_V2_SUBGATES roster.", m[1])
		}
	}

	defined := definedTargets(makefile)
	seen := map[string]bool{}
	for _, gate := range roster {
		if !defined[gate] {
			t.Errorf("gatecheck: roster entry %q is not a target in the Makefile", gate)
		}
		if seen[gate] {
			t.Errorf("gatecheck: roster entry %q appears twice", gate)
		}
		seen[gate] = true
	}

	for _, tool := range []string{"clang", "ar"} {
		if _, lookErr := exec.LookPath(tool); lookErr != nil {
			t.Skipf("gatecheck: runtime-v2-check's toolchain preflight needs %s", tool)
		}
	}

	probeOrder := []string{
		"gatecheck-probe-red-1",
		"gatecheck-probe-green-1",
		"gatecheck-probe-red-2",
		"gatecheck-probe-green-2",
	}
	probeFile := filepath.Join(t.TempDir(), "probes.mk")
	var probes strings.Builder
	for _, gate := range probeOrder {
		probes.WriteString(gate + ":\n\t@echo \"GATECHECK_PROBE " + gate + " body ran\"")
		if strings.Contains(gate, "red") {
			probes.WriteString("; exit 1")
		}
		probes.WriteString("\n\n")
	}
	if writeErr := os.WriteFile(probeFile, []byte(probes.String()), 0o600); writeErr != nil {
		t.Fatalf("gatecheck: write probe makefile: %v", writeErr)
	}

	cmd := exec.Command("make", "--no-print-directory", "runtime-v2-check", // #nosec G204 -- fixed executable; arguments are repository-owned names and a test-owned temp path.
		"RUNTIME_V2_SUBGATES="+strings.Join(probeOrder, " "))
	cmd.Dir = root
	// This drives the real runtime-v2-check recipe, but only to prove the
	// roster-reporting wiring against fast substitute probes -- it never runs a
	// true heavy sub-gate. scripts/heavy_run_guard.sh (Global Rule 19,
	// docs/runtime-v2-epics/RULES.md) now sits first in that recipe and would
	// otherwise refuse this test outside the dedicated host; GITHUB_ACTIONS=true
	// is the guard's documented lane for exactly this kind of ephemeral,
	// non-heavy invocation.
	cmd.Env = append(os.Environ(), "MAKEFILES="+probeFile, "GITHUB_ACTIONS=true")
	out, runErr := cmd.CombinedOutput()
	output := string(out)

	if runErr == nil {
		t.Fatalf("gatecheck: runtime-v2-check exited 0 with two red sub-gates on the roster; output:\n%s", output)
	}

	// The roster is announced before anything starts, so a reader of a log that
	// was cut short can still tell which sub-gates never got their turn.
	for _, gate := range probeOrder {
		if !strings.Contains(output, "] "+gate+"\n") {
			t.Errorf("gatecheck: sub-gate %q is missing from the roster the aggregate prints up front; output:\n%s", gate, output)
		}
	}

	// The load-bearing claim: the red row first on the roster did not stop the
	// three behind it from actually executing.
	for _, gate := range probeOrder {
		if !strings.Contains(output, "GATECHECK_PROBE "+gate+" body ran") {
			t.Errorf("gatecheck: sub-gate %q never ran; a sub-gate ahead of it failed and the aggregate stopped there. Output:\n%s", gate, output)
		}
	}

	// Each verdict is attached to its own sub-gate's name, twice: once as it
	// finishes, and once in the closing summary.
	for _, gate := range probeOrder {
		verdict, summary := "PASS", "pass  "
		if strings.Contains(gate, "red") {
			verdict, summary = "FAIL", "FAIL  "
		}
		if !strings.Contains(output, gate+" "+verdict) {
			t.Errorf("gatecheck: sub-gate %q ran without reporting %s under its own name; output:\n%s", gate, verdict, output)
		}
		if !strings.Contains(output, summary+gate) {
			t.Errorf("gatecheck: sub-gate %q is missing from the closing summary as %q; output:\n%s", gate, strings.TrimSpace(summary), output)
		}
	}

	// A failing aggregate names what failed, so the reader does not have to
	// scroll a multi-hour log to find out.
	if !strings.Contains(output, "runtime-v2-check FAILED:") {
		t.Errorf("gatecheck: the aggregate failed without naming the sub-gates that failed; output:\n%s", output)
	}
}

// recipeOf returns the recipe lines of one target.
func recipeOf(makefile, target string) ([]string, bool) {
	var lines []string
	found, inside := false, false
	for _, raw := range strings.Split(makefile, "\n") {
		if m := targetRe.FindStringSubmatch(raw); m != nil && !strings.HasPrefix(raw, "\t") {
			inside = m[1] == target
			found = found || inside
			continue
		}
		if inside && strings.HasPrefix(raw, "\t") {
			lines = append(lines, raw)
		}
	}
	return lines, found
}

// definedTargets returns every name that has a target line in the Makefile.
func definedTargets(makefile string) map[string]bool {
	defined := map[string]bool{}
	for _, raw := range strings.Split(makefile, "\n") {
		if m := targetRe.FindStringSubmatch(raw); m != nil && !strings.HasPrefix(raw, "\t") {
			defined[m[1]] = true
		}
	}
	return defined
}
