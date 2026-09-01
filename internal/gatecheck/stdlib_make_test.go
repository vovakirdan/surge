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
	makeAssignment = `(?:[A-Za-z_][A-Za-z0-9_]*=(?:"(?:\\.|[^"\\])*"|'[^']*'|[^[:space:]]+)[[:space:]]+)`
	realGoTestAll  = regexp.MustCompile(`^(` + makeAssignment + `*)go[[:space:]]+test[[:space:]]+\./\.\.\.(?:[[:space:]]|$)`)
	anyGoTestAll   = regexp.MustCompile(`(?:^|[[:space:]])go[[:space:]]+test[[:space:]]+\./\.\.\.(?:[[:space:]]|$)`)
	stdlibAssign   = regexp.MustCompile(`(?:^|[[:space:]])SURGE_STDLIB=`)
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
	valid := "echo \"go test ./... is next\"\n" + pinned + " SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 300s\n"
	if err := validateStdlibMakeDryRun(valid, root); err != nil {
		t.Fatalf("non-vacuous positive control: %v", err)
	}

	mutants := map[string]string{
		"missing command":      "echo \"go test ./...\"\n# go test ./...\n",
		"missing assignment":   "go test ./... --timeout 300s\n",
		"stale assignment":     "SURGE_STDLIB=\"/stale\" go test ./... --timeout 300s\n",
		"late assignment":      "go test ./... SURGE_STDLIB=" + strconv.Quote(root) + "\n",
		"duplicate assignment": pinned + " SURGE_STDLIB=\"/stale\" go test ./... --timeout 300s\n",
		"duplicate command":    valid + pinned + " go test ./... --timeout 300s\n",
		"false prefix":         "false && " + pinned + " go test ./... --timeout 300s\n",
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
		match := realGoTestAll.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		invocations := len(anyGoTestAll.FindAllStringIndex(line, -1))
		commandCount += invocations
		if invocations == 1 {
			commandPrefix = match[1]
		}
	}
	if commandCount != 1 {
		return fmt.Errorf("real go test ./... command count = %d, want 1", commandCount)
	}

	assignmentCount := len(stdlibAssign.FindAllStringIndex(commandPrefix, -1))
	if assignmentCount != 1 {
		return fmt.Errorf("SURGE_STDLIB assignments before go test ./... = %d, want 1", assignmentCount)
	}
	expected := "SURGE_STDLIB=" + strconv.Quote(filepath.Clean(root)) + " "
	if !strings.Contains(commandPrefix, expected) {
		return fmt.Errorf("go test ./... is not preceded by %s", strings.TrimSpace(expected))
	}
	return nil
}
