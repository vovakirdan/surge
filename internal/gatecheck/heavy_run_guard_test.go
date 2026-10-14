package gatecheck

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scripts/heavy_run_guard.sh refuses a heavy repo-owned entry point before
// compilation unless CI owns the run or the local worktree has explicit opt-in,
// an absolute matching SURGE_STDLIB, and a clean SHA or opted-in dirty content
// fingerprint. See docs/runtime-v2-epics/RULES.md, Global Rule 19.
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
		cmd.Env = gitFixtureEnv(
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

func gitFixtureEnv(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func TestHermeticRepoIgnoresCallerGitEnvironment(t *testing.T) {
	outer := hermeticRepo(t)
	outerGitDir := filepath.Join(outer, ".git")
	outerIndex := filepath.Join(outerGitDir, "index")
	indexBefore, err := os.ReadFile(outerIndex)
	if err != nil {
		t.Fatalf("read outer index: %v", err)
	}

	for name, value := range map[string]string{
		"GIT_COMMON_DIR":       outerGitDir,
		"GIT_DIR":              outerGitDir,
		"GIT_INDEX_FILE":       outerIndex,
		"GIT_OBJECT_DIRECTORY": filepath.Join(outerGitDir, "objects"),
		"GIT_PREFIX":           "outer/",
		"GIT_WORK_TREE":        outer,
	} {
		t.Setenv(name, value)
	}

	inner := hermeticRepo(t)
	if _, statErr := os.Stat(filepath.Join(inner, ".git", "index")); statErr != nil {
		t.Fatalf("inner repository has no index: %v", statErr)
	}
	indexAfter, err := os.ReadFile(outerIndex)
	if err != nil {
		t.Fatalf("re-read outer index: %v", err)
	}
	if !bytes.Equal(indexAfter, indexBefore) {
		t.Fatal("inner repository setup changed the caller's Git index")
	}
}

// baseEnv strips every guard control from the ambient environment so no test
// can pass by inheriting the caller's opt-in or stdlib.
func baseEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GITHUB_ACTIONS=") || strings.HasPrefix(kv, "SURGE_STDLIB=") ||
			strings.HasPrefix(kv, "SURGE_LOCAL_HEAVY=") || strings.HasPrefix(kv, "SURGE_LOCAL_HEAVY_ALLOW_DIRTY=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func TestHeavyRunGuardRequiresExplicitLocalOptIn(t *testing.T) {
	root := hermeticRepo(t)
	output, status := runHeavyRunGuard(t, baseEnv("SURGE_STDLIB="+root), "--root", root, "--label", "runtime-v2-waiter-check")
	if status != 3 || !strings.Contains(output, "SURGE_LOCAL_HEAVY=1") {
		t.Fatalf("missing local opt-in: status=%d\n%s", status, output)
	}
}

func TestHeavyRunGuardAcceptsCleanNamedBranchWithMatchingStdlib(t *testing.T) {
	root := hermeticRepo(t)
	sha := runGit(t, root, "rev-parse", "HEAD")
	output, status := runHeavyRunGuard(t, baseEnv("SURGE_LOCAL_HEAVY=1", "SURGE_STDLIB="+root), "--root", root, "--label", "runtime-v2-waiter-check")
	if status != 0 || !strings.Contains(output, "state=clean") || !strings.Contains(output, "evidence=sha:"+sha) {
		t.Fatalf("clean local worktree was not accepted with its SHA: status=%d\n%s", status, output)
	}
}

func TestHeavyRunGuardRequiresAbsoluteMatchingStdlib(t *testing.T) {
	root := hermeticRepo(t)
	for _, tc := range []struct{ name, stdlib string }{
		{"relative", "."},
		{"other-tree", t.TempDir()},
		{"unset", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv("SURGE_LOCAL_HEAVY=1")
			if tc.stdlib != "" {
				env = append(env, "SURGE_STDLIB="+tc.stdlib)
			}
			output, status := runHeavyRunGuard(t, env, "--root", root, "--label", "runtime-v2-waiter-check")
			if status != 3 || !strings.Contains(output, "SURGE_STDLIB") {
				t.Fatalf("invalid stdlib was not refused: status=%d\n%s", status, output)
			}
		})
	}
}

func TestHeavyRunGuardDirtyTreeNeedsSecondOptIn(t *testing.T) {
	root := hermeticRepo(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, status := runHeavyRunGuard(t, baseEnv("SURGE_LOCAL_HEAVY=1", "SURGE_STDLIB="+root), "--root", root, "--label", "runtime-v2-waiter-check")
	if status != 3 || !strings.Contains(output, "SURGE_LOCAL_HEAVY_ALLOW_DIRTY=1") {
		t.Fatalf("dirty tree was not refused without its opt-in: status=%d\n%s", status, output)
	}
}

func TestHeavyRunGuardDirtyFingerprintCoversTrackedAndUntrackedContent(t *testing.T) {
	root := hermeticRepo(t)
	tracked := filepath.Join(root, "f.txt")
	untracked := filepath.Join(root, "extra.go")
	if err := os.WriteFile(tracked, []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(untracked, []byte("package extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := baseEnv("SURGE_LOCAL_HEAVY=1", "SURGE_LOCAL_HEAVY_ALLOW_DIRTY=1", "SURGE_STDLIB="+root)
	first, status := runHeavyRunGuard(t, env, "--root", root, "--label", "runtime-v2-waiter-check")
	if status != 0 || !strings.Contains(first, "state=dirty-opt-in") || !strings.Contains(first, "evidence=worktree:") {
		t.Fatalf("dirty opted-in tree was not fingerprinted: status=%d\n%s", status, first)
	}
	second, status := runHeavyRunGuard(t, env, "--root", root, "--label", "runtime-v2-waiter-check")
	if status != 0 || first != second {
		t.Fatalf("unchanged dirty tree fingerprint is unstable:\nfirst=%s\nsecond=%s", first, second)
	}
	if err := os.WriteFile(untracked, []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, status := runHeavyRunGuard(t, env, "--root", root, "--label", "runtime-v2-waiter-check")
	if status != 0 || third == first {
		t.Fatalf("untracked content did not change the fingerprint:\nfirst=%s\nthird=%s", first, third)
	}
}

func TestHeavyRunGuardTheCILaneIsAllowed(t *testing.T) {
	root := hermeticRepo(t)
	output, status := runHeavyRunGuard(t, baseEnv("GITHUB_ACTIONS=true"), "--root", root, "--label", "runtime-v2-check")
	if status != 0 || !strings.Contains(output, "CI") {
		t.Fatalf("CI lane was not accepted: status=%d\n%s", status, output)
	}
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
