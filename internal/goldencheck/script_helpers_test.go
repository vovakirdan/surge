package goldencheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type scriptFixture struct {
	root       string
	script     string
	surge      string
	sourcePath string
	sourceRel  string
}

func newScriptFixture(t *testing.T, sourceRel string) scriptFixture {
	t.Helper()
	repositoryRoot := testRepoRoot(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "scripts", "golden_update.sh")
	copyTestFile(t, filepath.Join(repositoryRoot, "scripts", "golden_update.sh"), script, 0o755)
	if err := os.MkdirAll(filepath.Join(root, "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "testdata", "golden", filepath.FromSlash(sourceRel))
	writeTestFile(t, sourcePath, "fn main() {}\n", 0o644)
	surge := filepath.Join(root, "fake-surge")
	writeTestFile(t, surge, fakeSurgeScript, 0o755)
	return scriptFixture{root: root, script: script, surge: surge, sourcePath: sourcePath, sourceRel: sourceRel}
}

func (fixture scriptFixture) run(t *testing.T, failPhases string, diagnosticZero, formatterFailure []string) (string, error) {
	return fixture.runWithEmit(t, failPhases, diagnosticZero, formatterFailure, nil)
}

func (fixture scriptFixture) runWithEmit(t *testing.T, failPhases string, diagnosticZero, formatterFailure []string, emitFailure []PhaseExpectation) (string, error) {
	t.Helper()
	cmd := fixture.command(t, failPhases, diagnosticZero, formatterFailure, emitFailure)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (fixture scriptFixture) command(t *testing.T, failPhases string, diagnosticZero, formatterFailure []string, emitFailure []PhaseExpectation) *exec.Cmd {
	t.Helper()
	diagFile := filepath.Join(fixture.root, "diag.paths0")
	fmtFile := filepath.Join(fixture.root, "fmt.paths0")
	emitFile := filepath.Join(fixture.root, "emit.paths0")
	writeNULPaths(t, diagFile, diagnosticZero)
	writeNULPaths(t, fmtFile, formatterFailure)
	writeNULPhaseExpectations(t, emitFile, emitFailure)
	cmd := exec.Command(fixture.script)
	cmd.Dir = fixture.root
	cmd.Env = append(os.Environ(),
		"SURGE_BIN="+fixture.surge,
		"FAIL_PHASES="+failPhases,
		"SURGE_GOLDEN_DIAG_ZERO_EXPECTATIONS="+diagFile,
		"SURGE_GOLDEN_FMT_FAILURE_EXPECTATIONS="+fmtFile,
		"SURGE_GOLDEN_EMIT_FAILURE_EXPECTATIONS="+emitFile,
	)
	return cmd
}

func writeNULPhaseExpectations(t *testing.T, filename string, expectations []PhaseExpectation) {
	t.Helper()
	var content strings.Builder
	for _, expectation := range expectations {
		content.WriteString(expectation.Phase)
		content.WriteByte(0)
		content.WriteString(expectation.Path)
		content.WriteByte(0)
	}
	if err := os.WriteFile(filename, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeNULPaths(t *testing.T, filename string, paths []string) {
	t.Helper()
	var content strings.Builder
	for _, path := range paths {
		content.WriteString(path)
		content.WriteByte(0)
	}
	if err := os.WriteFile(filename, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyTestFile(t *testing.T, source, destination string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, destination, string(data), mode)
}

func testRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(dir, "..", ".."))
}

const fakeSurgeScript = `#!/usr/bin/env bash
set -euo pipefail
command_name="$1"
shift
arguments=" $* "
phase="${command_name}"
if [[ "${command_name}" == "diag" ]]; then
	if [[ "${arguments}" == *" --emit-borrow "* ]]; then
		phase="hir-borrow"
	elif [[ "${arguments}" == *" --emit-instantiations "* ]]; then
		phase="instantiations"
	elif [[ "${arguments}" == *" --emit-mono "* ]]; then
		phase="mono"
	elif [[ "${arguments}" == *" --emit-mir "* ]]; then
		phase="mir"
	elif [[ "${arguments}" == *" --emit-hir "* ]]; then
		phase="hir"
	else
		phase="diagnostics"
	fi
fi
if [[ ",${FAIL_PHASES:-}," == *",${phase},"* ]]; then
	printf 'fake %s failure\n' "${phase}"
	exit 1
fi
if [[ "${BLOCK_PHASE:-}" == "${phase}" ]]; then
	printf blocked > "${BLOCK_MARKER:?}"
	while true; do sleep 1; done
fi
if [[ "${command_name}" == "fmt" ]]; then
	source="${@: -1}"
	cat "${source}"
else
	printf 'fake %s output\n' "${phase}"
fi
`
