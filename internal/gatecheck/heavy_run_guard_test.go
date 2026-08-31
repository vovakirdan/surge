package gatecheck

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scripts/heavy_run_guard.sh refuses a heavy repo-owned entry point (the
// runtime-v2-* sub-gates, the aggregate, behaviour-check*) BEFORE anything
// compiles unless the tree is a clean, detached, SHA-pinned worktree on the
// dedicated measurement host with SURGE_STDLIB pointed at itself, or the
// GITHUB_ACTIONS CI lane. See docs/runtime-v2-epics/RULES.md, Global Rule 19.
//
// R1-R8 drive the script directly against a hermetic temp git repository, so
// none of them can be satisfied by anything on the machine actually running
// the test. R9-R11 read the Makefile itself: every target whose recipe walks
// internal/vm, internal/ownershipgate, internal/crossinggate or the carrier
// sanitizer sweep must call the guard before its first heavy line, unless it
// is named in the exemption roster below (and nowhere else); the cheap lane
// must carry no guard at all.

func heavyRunGuardScript(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts", "heavy_run_guard.sh")
}

func runHeavyRunGuard(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{heavyRunGuardScript(t)}, args...)...) // #nosec G204 -- fixed interpreter; arguments are test-owned paths and a fixed label.
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("run heavy_run_guard.sh %v: %v\n%s", args, err, output)
	}
	return string(output), exit.ExitCode()
}

// hermeticRepo builds a one-commit git repository in its own temp directory,
// isolated from the real repository and from any other test's marker or
// SURGE_STDLIB, and returns its path.
func hermeticRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...) // #nosec G204 -- fixed executable, test-owned fixed argv.
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=heavy-run-guard-test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=heavy-run-guard-test", "GIT_COMMITTER_EMAIL=test@example.invalid",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatalf("seed hermetic repo: %v", err)
	}
	run("init", "-q")
	run("add", "f.txt")
	run("commit", "-q", "-m", "seed")
	return dir
}

// baseEnv strips GITHUB_ACTIONS and SURGE_STDLIB from the ambient environment
// so a test's assertion about either cannot pass by inheriting the value the
// test process itself happens to run under.
func baseEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GITHUB_ACTIONS=") || strings.HasPrefix(kv, "SURGE_STDLIB=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func TestHeavyRunGuardRefusesTheSharedCheckout(t *testing.T) {
	root := hermeticRepo(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatalf("dirty the tree: %v", err)
	}
	noMarker := filepath.Join(t.TempDir(), "no-such-marker")

	output, status := runHeavyRunGuard(t, baseEnv(), "--root", root, "--marker", noMarker, "--label", "runtime-v2-waiter-check")

	if status != 3 {
		t.Fatalf("a named-branch, dirty, unmarked tree exited %d, want 3\n%s", status, output)
	}
	if !strings.Contains(output, "212.108.83.42") {
		t.Errorf("refusal did not name the dedicated host\n%s", output)
	}
	if !strings.Contains(output, "worktree add --detach") {
		t.Errorf("refusal did not give the worktree instruction\n%s", output)
	}
}

func TestHeavyRunGuardAcceptsADetachedPinnedWorktreeOnTheDedicatedHost(t *testing.T) {
	root := hermeticRepo(t)
	sha := detachHead(t, root)
	marker := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("seed marker: %v", err)
	}

	output, status := runHeavyRunGuard(t, baseEnv("SURGE_STDLIB="+root), "--root", root, "--marker", marker, "--label", "runtime-v2-waiter-check")

	if status != 0 {
		t.Fatalf("a clean, detached, marked, self-pointed tree exited %d, want 0\n%s", status, output)
	}
	if !strings.Contains(output, sha) {
		t.Errorf("acceptance did not print the pinned SHA %s\n%s", sha, output)
	}
}

func TestHeavyRunGuardRefusesADetachedWorktreeOffTheDedicatedHost(t *testing.T) {
	root := hermeticRepo(t)
	detachHead(t, root)
	noMarker := filepath.Join(t.TempDir(), "no-such-marker")

	output, status := runHeavyRunGuard(t, baseEnv("SURGE_STDLIB="+root), "--root", root, "--marker", noMarker, "--label", "runtime-v2-waiter-check")

	if status != 3 {
		t.Fatalf("a detached, clean tree with no marker exited %d, want 3\n%s", status, output)
	}
	if !strings.Contains(output, "не выделенный хост") {
		t.Errorf("refusal did not name the missing dedicated host\n%s", output)
	}
}

func TestHeavyRunGuardRefusesADirtyWorktreeOnTheDedicatedHost(t *testing.T) {
	root := hermeticRepo(t)
	detachHead(t, root)
	marker := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatalf("dirty a tracked file: %v", err)
	}

	output, status := runHeavyRunGuard(t, baseEnv("SURGE_STDLIB="+root), "--root", root, "--marker", marker, "--label", "runtime-v2-waiter-check")

	if status != 3 {
		t.Fatalf("a detached, marked, dirty tree exited %d, want 3\n%s", status, output)
	}
	if !strings.Contains(output, "дерево грязное") {
		t.Errorf("refusal did not name the dirty tree\n%s", output)
	}
}

func TestHeavyRunGuardRefusesAnUntrackedSourceFile(t *testing.T) {
	root := hermeticRepo(t)
	detachHead(t, root)
	marker := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra.go"), []byte("package extra\n"), 0o600); err != nil {
		t.Fatalf("add an untracked file: %v", err)
	}

	output, status := runHeavyRunGuard(t, baseEnv("SURGE_STDLIB="+root), "--root", root, "--marker", marker, "--label", "runtime-v2-waiter-check")

	if status != 3 {
		t.Fatalf("a detached, marked tree with an untracked file exited %d, want 3\n%s", status, output)
	}
}

func TestHeavyRunGuardRefusesAStdlibFromAnotherTree(t *testing.T) {
	root := hermeticRepo(t)
	detachHead(t, root)
	marker := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	elsewhere := t.TempDir()

	output, status := runHeavyRunGuard(t, baseEnv("SURGE_STDLIB="+elsewhere), "--root", root, "--marker", marker, "--label", "runtime-v2-waiter-check")

	if status != 3 {
		t.Fatalf("SURGE_STDLIB pointed outside the worktree exited %d, want 3\n%s", status, output)
	}
	if !strings.Contains(output, "SURGE_STDLIB") {
		t.Errorf("refusal did not name SURGE_STDLIB\n%s", output)
	}
}

func TestHeavyRunGuardRefusesAnUnsetStdlib(t *testing.T) {
	root := hermeticRepo(t)
	detachHead(t, root)
	marker := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("seed marker: %v", err)
	}

	output, status := runHeavyRunGuard(t, baseEnv(), "--root", root, "--marker", marker, "--label", "runtime-v2-waiter-check")

	if status != 3 {
		t.Fatalf("an unset SURGE_STDLIB exited %d, want 3\n%s", status, output)
	}
	if !strings.Contains(output, "SURGE_STDLIB") {
		t.Errorf("refusal did not name SURGE_STDLIB\n%s", output)
	}
}

func TestHeavyRunGuardTheCILaneIsAllowed(t *testing.T) {
	root := hermeticRepo(t) // stays on its named default branch, uncleaned on purpose
	noMarker := filepath.Join(t.TempDir(), "no-such-marker")

	output, status := runHeavyRunGuard(t, baseEnv("GITHUB_ACTIONS=true"), "--root", root, "--marker", noMarker, "--label", "runtime-v2-check")

	if status != 0 {
		t.Fatalf("GITHUB_ACTIONS=true on a named-branch tree exited %d, want 0\n%s", status, output)
	}
	if !strings.Contains(output, "CI") {
		t.Errorf("acceptance did not name the CI lane\n%s", output)
	}
}

func detachHead(t *testing.T, root string) string {
	t.Helper()
	sha := runGit(t, root, "rev-parse", "HEAD")
	runGitNoOutput(t, root, "checkout", "-q", "--detach", sha)
	return sha
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- fixed executable, test-owned fixed argv.
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func runGitNoOutput(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- fixed executable, test-owned fixed argv.
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// ===== R9-R11: the Makefile's own wiring =====

// heavyGuardExemptions is the roster of targets that legitimately walk
// internal/vm, internal/ownershipgate, internal/crossinggate or the carrier
// sanitizer sweep without calling the guard, each for a reason recorded next
// to the exemption in the Makefile. TestHeavyRunGuardTheExemptionRosterIsExactlyTheDocumentedOne
// pins this set so it can only grow with a matching test change.
var heavyGuardExemptions = map[string]bool{
	"runtime-v2-carrier-bench":            true,
	"runtime-v2-carrier-baseline-capture": true,
	"runtime-v2-carrier-bench-final":      true,
	"test":                                true,
	"check":                               true,
}

var heavyRosterMarkers = []string{
	"internal/vm",
	"internal/ownershipgate",
	"internal/crossinggate",
	"runtime_v2_carrier_sanitizer_check.sh",
}

func readMakefile(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("gatecheck: read Makefile: %v", err)
	}
	return string(content)
}

// TestHeavyRunGuardEveryHeavyTargetCallsTheGuard is the stand's non-vacuity
// half for the Makefile side: strip $(GUARD) from any one guarded target (for
// example, revert runtime-v2-waiter-check to its pre-package recipe) and this
// test names that target and fails, because before this package no target
// called scripts/heavy_run_guard.sh at all.
func TestHeavyRunGuardEveryHeavyTargetCallsTheGuard(t *testing.T) {
	makefile := readMakefile(t)
	for name := range definedTargets(makefile) {
		if heavyGuardExemptions[name] {
			continue
		}
		recipe, ok := recipeOf(makefile, name)
		if !ok {
			continue
		}
		heavy := false
		for _, line := range recipe {
			for _, marker := range heavyRosterMarkers {
				if strings.Contains(line, marker) {
					heavy = true
				}
			}
			if heavy {
				break
			}
		}
		if !heavy {
			continue
		}
		guardedBeforeHeavy := false
		for _, line := range recipe {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "$(GUARD)") {
				guardedBeforeHeavy = true
				break
			}
			isHeavy := false
			for _, marker := range heavyRosterMarkers {
				if strings.Contains(line, marker) {
					isHeavy = true
				}
			}
			if isHeavy {
				break
			}
		}
		if !guardedBeforeHeavy {
			t.Errorf("gatecheck: target %q walks a heavy roster package but does not call $(GUARD) before its first heavy line", name)
		}
	}
}

// TestHeavyRunGuardTheExemptionRosterIsExactlyTheDocumentedOne is the other
// non-vacuity half: add a target to heavyGuardExemptions to silence the test
// above without actually wiring the guard, and this test names the roster
// mismatch instead of passing quietly.
func TestHeavyRunGuardTheExemptionRosterIsExactlyTheDocumentedOne(t *testing.T) {
	want := map[string]bool{
		"runtime-v2-carrier-bench":            true,
		"runtime-v2-carrier-baseline-capture": true,
		"runtime-v2-carrier-bench-final":      true,
		"test":                                true,
		"check":                               true,
	}
	if len(heavyGuardExemptions) != len(want) {
		t.Fatalf("gatecheck: exemption roster has %d entries, want %d: %v", len(heavyGuardExemptions), len(want), heavyGuardExemptions)
	}
	for name := range want {
		if !heavyGuardExemptions[name] {
			t.Errorf("gatecheck: exemption roster is missing %q", name)
		}
	}
	for name := range heavyGuardExemptions {
		if !want[name] {
			t.Errorf("gatecheck: exemption roster has an undocumented entry %q", name)
		}
	}
}

// TestHeavyRunGuardTheCheapLaneIsNotGuarded is the negative control: the
// guard must never spread onto the targets that are supposed to run from any
// checkout, including the shared one this test itself may be running from.
func TestHeavyRunGuardTheCheapLaneIsNotGuarded(t *testing.T) {
	makefile := readMakefile(t)
	cheap := []string{
		"runtime-v2-file-size-check",
		"runtime-v2-syncpoint-check",
		"cfmt-check",
		"lint",
		"c-check",
	}
	for _, name := range cheap {
		recipe, ok := recipeOf(makefile, name)
		if !ok {
			t.Errorf("gatecheck: cheap-lane target %q not found in Makefile", name)
			continue
		}
		for _, line := range recipe {
			if strings.Contains(line, "$(GUARD)") {
				t.Errorf("gatecheck: cheap-lane target %q calls $(GUARD); it must be runnable from any checkout", name)
			}
		}
	}
}
