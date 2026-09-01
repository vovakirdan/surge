package gatecheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostedRuntimeV2AggregateHasValgrindBeforeGate(t *testing.T) {
	job, count := workflowJobBody(readHostedCI(t), "runtime-v2-check")
	if count != 1 {
		t.Fatalf("runtime-v2-check: job definitions = %d, want 1", count)
	}
	if err := validateRuntimeV2AggregateJob(job); err != nil {
		t.Fatal(err)
	}
}

func TestHostedRuntimeV2AggregateValgrindContractRejectsMissingOrLateInstall(t *testing.T) {
	valid := `
      - run: sudo apt-get install -y clang valgrind
      - run: make runtime-v2-check
`
	if err := validateRuntimeV2AggregateJob(valid); err != nil {
		t.Fatalf("valid Runtime V2 aggregate job rejected: %v", err)
	}

	tests := map[string]string{
		"comment_only": `
      # sudo apt-get install -y valgrind
      - run: sudo apt-get install -y clang
      - run: make runtime-v2-check
`,
		"echo_only": `
      - run: echo "apt-get install valgrind"
      - run: make runtime-v2-check
`,
		"false_prefix": `
      - run: false && sudo apt-get install -y valgrind
      - run: make runtime-v2-check
`,
		"missing_package": `
      - run: sudo apt-get install -y clang llvm
      - run: make runtime-v2-check
`,
		"late_install": `
      - run: make runtime-v2-check
      - run: sudo apt-get install -y valgrind
`,
		"missing_gate": `
      - run: sudo apt-get install -y valgrind
`,
	}
	for name, job := range tests {
		t.Run(name, func(t *testing.T) {
			if err := validateRuntimeV2AggregateJob(job); err == nil {
				t.Fatal("invalid Runtime V2 aggregate job satisfied the hosted Valgrind contract")
			}
		})
	}
}

func TestHostedCSanitizersRunsRuntimeV2OwnedStorageGate(t *testing.T) {
	job, count := workflowJobBody(readHostedCI(t), "c-sanitizers")
	if count != 1 {
		t.Fatalf("c-sanitizers: job definitions = %d, want 1", count)
	}
	if err := validateCSanitizersJob(job); err != nil {
		t.Fatal(err)
	}
}

func TestHostedCSanitizersContractRejectsGenericGoTest(t *testing.T) {
	valid := `
      - run: make runtime-v2-owned-storage-check
`
	if err := validateCSanitizersJob(valid); err != nil {
		t.Fatalf("valid hosted sanitizer job rejected: %v", err)
	}

	tests := map[string]struct {
		job       string
		wantError string
	}{
		"comment_only": {
			job: `
      # make runtime-v2-owned-storage-check
      - run: echo no sanitizer gate
`,
			wantError: `command "make runtime-v2-owned-storage-check" count = 0, want 1`,
		},
		"missing_target": {
			job:       "      - run: go test ./...\n",
			wantError: `command "make runtime-v2-owned-storage-check" count = 0, want 1`,
		},
		"generic_tests_remain": {
			job: `
      - run: |
          make runtime-v2-owned-storage-check
          go test ./internal/backend/llvm/...
          go test ./... -run TestRuntime
`,
			wantError: "direct go test commands = 2, want 0",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateCSanitizersJob(test.job)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("invalid hosted sanitizer job error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func readHostedCI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".github", "workflows", "ci.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hosted CI workflow: %v", err)
	}
	return string(content)
}

func workflowJobBody(workflow, jobID string) (string, int) {
	lines := strings.Split(workflow, "\n")
	header := "  " + jobID + ":"
	start := -1
	count := 0
	for i, line := range lines {
		if line == header {
			start = i
			count++
		}
	}
	if count != 1 {
		return "", count
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if leadingSpaces(lines[i]) == 2 && strings.HasSuffix(trimmed, ":") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n"), count
}

func workflowExecutableLines(job string) []string {
	raw := strings.Split(job, "\n")
	commands := make([]string, 0)
	for i := 0; i < len(raw); i++ {
		trimmed := strings.TrimSpace(raw[i])
		trimmed = strings.TrimPrefix(trimmed, "- ")
		if !strings.HasPrefix(trimmed, "run:") {
			continue
		}
		command := workflowCommand(strings.TrimPrefix(trimmed, "run:"))
		if command != "|" && command != ">" {
			if command != "" {
				commands = append(commands, command)
			}
			continue
		}

		runIndent := leadingSpaces(raw[i])
		for i+1 < len(raw) {
			next := raw[i+1]
			if strings.TrimSpace(next) != "" && leadingSpaces(next) <= runIndent {
				break
			}
			i++
			if line := workflowCommand(next); line != "" {
				commands = append(commands, line)
			}
		}
	}
	return commands
}

func workflowCommand(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	if before, _, ok := strings.Cut(line, "#"); ok {
		line = before
	}
	return strings.TrimSpace(line)
}

func commandCount(lines []string, exact string) int {
	count := 0
	for _, line := range lines {
		if line == exact {
			count++
		}
	}
	return count
}

func shellLineHasWord(line, word string) bool {
	for _, field := range strings.FieldsFunc(line, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '-' && r != '_'
	}) {
		if field == word {
			return true
		}
	}
	return false
}

func shellLineInstallsPackage(line, packageName string) bool {
	fields := strings.Fields(line)
	if len(fields) > 0 && fields[0] == "sudo" {
		fields = fields[1:]
	}
	if len(fields) < 3 || fields[0] != "apt-get" || fields[1] != "install" {
		return false
	}
	return shellLineHasWord(line, packageName)
}

func validateRuntimeV2AggregateJob(job string) error {
	lines := workflowExecutableLines(job)
	gateCount := commandCount(lines, "make runtime-v2-check")
	if gateCount != 1 {
		return fmt.Errorf("runtime-v2-check: command count = %d, want 1", gateCount)
	}

	gateIndex := -1
	for i, line := range lines {
		if line == "make runtime-v2-check" {
			gateIndex = i
			break
		}
	}
	qualifyingInstalls := 0
	for i, line := range lines {
		if i >= gateIndex {
			break
		}
		if shellLineInstallsPackage(line, "valgrind") {
			qualifyingInstalls++
		}
	}
	if qualifyingInstalls != 1 {
		return fmt.Errorf("runtime-v2-check: qualifying valgrind install lines before gate = %d, want 1", qualifyingInstalls)
	}
	return nil
}

func validateCSanitizersJob(job string) error {
	lines := workflowExecutableLines(job)
	const target = "make runtime-v2-owned-storage-check"
	if count := commandCount(lines, target); count != 1 {
		return fmt.Errorf("c-sanitizers: command %q count = %d, want 1", target, count)
	}

	directGoTests := 0
	for _, line := range lines {
		fields := strings.Fields(line)
		for len(fields) > 0 && (fields[0] == "env" || strings.Contains(fields[0], "=")) {
			fields = fields[1:]
		}
		if len(fields) >= 2 && fields[0] == "go" && fields[1] == "test" {
			directGoTests++
		}
	}
	if directGoTests != 0 {
		return fmt.Errorf("c-sanitizers: direct go test commands = %d, want 0", directGoTests)
	}
	return nil
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}
