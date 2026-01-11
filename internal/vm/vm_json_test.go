//go:build !golden
// +build !golden

package vm_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVMJsonSuite(t *testing.T) {
	requireVMBackend(t)

	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	sgRel := filepath.ToSlash(filepath.Join("testdata", "vm_json", "json_suite.sg"))

	stdout, stderr, code := runSurgeWithInput(t, root, surge, "", "run", "--backend=vm", sgRel)
	if code != 0 {
		t.Fatalf("json suite failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr:\n%s", stderr)
	}
	if strings.TrimSpace(stdout) != "ok" {
		t.Fatalf("unexpected stdout:\n%s", stdout)
	}
}
