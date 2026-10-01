package vm_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndexCertificateModuleRunsVM(t *testing.T) {
	requireVMBackend(t)
	root := repoRoot(t)
	source := filepath.Join(root, "testdata", "index_certificate_module", "main.sg")
	out, err, code := runSurgeWithEnv(t, root, buildSurgeBinary(t, root), envWithStdlib(root), "run", "--backend", "vm", source)
	if code != 0 || out != "1\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%s", code, out, err)
	}
}

func TestIndexCertificateModuleRunsLLVM(t *testing.T) {
	ensureLLVMToolchain(t)
	root := repoRoot(t)
	source := filepath.Join(root, "testdata", "index_certificate_module", "main.sg")
	surge := buildSurgeBinary(t, root)
	out, err, code := runSurgeWithEnv(t, root, surge, envWithStdlib(root), "build", source)
	if code != 0 {
		t.Fatalf("native build exit=%d stdout=%s stderr=%s", code, out, err)
	}
	binary := llvmOutputPath(root, source)
	out, err, code = runIndexCertificateValgrind(t, binary, envWithStdlib(root), 120*time.Second)
	if code != 0 || out != "1\n" || hasValgrindMemcheckError(err) || !strings.Contains(err, "ERROR SUMMARY: 0 errors") || !strings.Contains(err, "All heap blocks were freed") {
		t.Fatalf("valgrind exit=%d stdout=%q stderr=%s", code, out, err)
	}
}
