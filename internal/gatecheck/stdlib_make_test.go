package gatecheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	makeAssignment  = `(?:[A-Za-z_][A-Za-z0-9_]*=(?:"(?:\\.|[^"\\])*"|'[^']*'|[^[:space:]]+)[[:space:]]+)`
	fullTestCommand = `(?:go[[:space:]]+test[[:space:]]+\./\.\.\.|\./scripts/go_test_full\.sh[[:space:]]+"?go"?)`
	realFullTest    = regexp.MustCompile(`^(` + makeAssignment + `*)` + fullTestCommand + `(?:[[:space:]]|$)`)
	anyFullTest     = regexp.MustCompile(`(?:^|[[:space:]])` + fullTestCommand + `(?:[[:space:]]|$)`)
	stdlibAssign    = regexp.MustCompile(`(?:^|[[:space:]])SURGE_STDLIB=`)
)

func TestMakeTestPinsStdlibToRepository(t *testing.T) {
	root := repoRoot(t)
	stale := filepath.Join(t.TempDir(), "stale-stdlib")
	output := makeTestDryRun(t, root, stale)
	if err := validateStdlibMakeDryRun(output, root); err != nil {
		t.Fatalf("make -n test under SURGE_STDLIB=%q: %v\n%s", stale, err, output)
	}
}

func TestStdlibMakeDryRunValidatorNegativeControls(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "repo with space")
	pinned := "SURGE_STDLIB=" + strconv.Quote(root)
	valid := "echo \"full tests are next\"\n" + pinned + " SURGE_SKIP_TIMEOUT_TESTS=1 ./scripts/go_test_full.sh \"go\"\n"
	if err := validateStdlibMakeDryRun(valid, root); err != nil {
		t.Fatalf("non-vacuous positive control: %v", err)
	}

	mutants := map[string]string{
		"missing command":      "echo \"./scripts/go_test_full.sh go\"\n# full tests\n",
		"missing assignment":   "./scripts/go_test_full.sh \"go\"\n",
		"stale assignment":     "SURGE_STDLIB=\"/stale\" ./scripts/go_test_full.sh \"go\"\n",
		"late assignment":      "./scripts/go_test_full.sh \"go\" SURGE_STDLIB=" + strconv.Quote(root) + "\n",
		"duplicate assignment": pinned + " SURGE_STDLIB=\"/stale\" ./scripts/go_test_full.sh \"go\"\n",
		"duplicate command":    valid + pinned + " ./scripts/go_test_full.sh \"go\"\n",
		"false prefix":         "false && " + pinned + " ./scripts/go_test_full.sh \"go\"\n",
	}
	for name, output := range mutants {
		t.Run(name, func(t *testing.T) {
			if err := validateStdlibMakeDryRun(output, root); err == nil {
				t.Fatalf("validator accepted mutant:\n%s", output)
			}
		})
	}
}

func makeTestDryRun(t *testing.T, root, staleStdlib string) string {
	t.Helper()
	cmd := exec.Command("make", "--no-print-directory", "-n", "test", "GO=go")
	cmd.Dir = root
	cmd.Env = append(withoutEnvironment(os.Environ(), "SURGE_STDLIB", "MAKEFLAGS", "GNUMAKEFLAGS"), "SURGE_STDLIB="+staleStdlib)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n test: %v\n%s", err, output)
	}
	return string(output)
}

func withoutEnvironment(environment []string, names ...string) []string {
	drop := make(map[string]bool, len(names))
	for _, name := range names {
		drop[name] = true
	}
	clean := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if !drop[name] {
			clean = append(clean, entry)
		}
	}
	return clean
}

func validateStdlibMakeDryRun(output, root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("repository root %q is not absolute", root)
	}

	commandCount := 0
	commandPrefix := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		match := realFullTest.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		invocations := len(anyFullTest.FindAllStringIndex(line, -1))
		commandCount += invocations
		if invocations == 1 {
			commandPrefix = match[1]
		}
	}
	if commandCount != 1 {
		return fmt.Errorf("real full-test command count = %d, want 1", commandCount)
	}

	assignmentCount := len(stdlibAssign.FindAllStringIndex(commandPrefix, -1))
	if assignmentCount != 1 {
		return fmt.Errorf("SURGE_STDLIB assignments before full-test command = %d, want 1", assignmentCount)
	}
	expected := "SURGE_STDLIB=" + strconv.Quote(filepath.Clean(root)) + " "
	if !strings.Contains(commandPrefix, expected) {
		return fmt.Errorf("full-test command is not preceded by %s", strings.TrimSpace(expected))
	}
	return nil
}
