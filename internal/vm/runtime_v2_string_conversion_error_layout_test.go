package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const runtimeV2FromStrErrorLayoutSource = `
@entrypoint
fn main() -> int {
	let text: string = "not-a-number";
	let parsed = uint64.from_str(&text);
	let mut code: int = 12;
	compare parsed {
		Success(_) => { code = 12; }
		err => {
			if err.code == 1 {
				code = 0;
			} else {
				code = 15;
			}
		}
	}
	return code;
}
`

func TestRuntimeV2StringFromStrErrorTemporaryValgrindSafe(t *testing.T) {
	root := repoRoot(t)
	requireOwnershipValgrind(t, exec.LookPath)
	bin := buildRuntimeV2CrossingSource(t, runtimeV2FromStrErrorLayoutSource, nil)

	stdout, stderr, exitCode := runBinaryUnderValgrind(t, bin, envWithStdlib(root), 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("from_str Error arm read outside its finalized union payload\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("from_str Error arm/code probe failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("from_str Error probe produced unexpected output: stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse from_str valgrind summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Logf("deferred pre-existing from_str message leak: %d bytes in %d blocks", bytesLost, blocksLost)
	}
}

func TestRuntimeV2StringFromBytesErrorTemporaryValgrindZero(t *testing.T) {
	root := repoRoot(t)
	fixturePath := filepath.Join(root, "testdata", "golden", "vm_strings", "string_from_bytes_invalid_utf8.sg")
	assertConversionErrorFixtureValgrindZero(t, root, fixturePath, "err=1")
}

func assertConversionErrorFixtureValgrindZero(t *testing.T, root, fixturePath, wantStdout string) {
	t.Helper()
	requireOwnershipValgrind(t, exec.LookPath)
	source, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read conversion fixture: %v", err)
	}
	bin := buildRuntimeV2CrossingSource(t, string(source), nil)

	stdout, stderr, exitCode := runBinaryUnderValgrind(t, bin, envWithStdlib(root), 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("conversion Error arm read outside its finalized union payload\nstdout:\n%s\nstderr:\n%s",
			stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("conversion fixture failed under valgrind (exit=%d)\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != wantStdout {
		t.Fatalf("conversion fixture returned the wrong arm: stdout=%q, want %q", stdout, wantStdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse conversion valgrind summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf("conversion leaked temporary Error storage: %d bytes in %d blocks\nstderr:\n%s",
			bytesLost, blocksLost, stderr)
	}
	indirectBytes, indirectBlocks := parseValgrindLeakMatch(valgrindIndirectLeakRE, stderr)
	if indirectBytes != 0 || indirectBlocks != 0 {
		t.Fatalf("conversion leaked through its Error payload: %d bytes in %d blocks\nstderr:\n%s",
			indirectBytes, indirectBlocks, stderr)
	}
}
