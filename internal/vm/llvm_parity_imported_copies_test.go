package vm_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A program whose imports reach their declarations through copies the
// importer spelled apart from the module (a directory module imported by one
// of its files, a module named apart from its directory) and through the
// extern methods of the intrinsic stdlib/time Duration called on borrows of
// it: both backends print the same values, and the native binary is clean
// under Valgrind. A borrow of an inline Duration names its storage, so reading
// its nanoseconds must not load an address out of it first.
func TestLLVMParityImportedCopiesAndDurationReferences(t *testing.T) {
	skipTimeoutTests(t)
	root := repoRoot(t)
	for _, tool := range []string{"clang", "ar", "valgrind"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed; skipping LLVM parity test", tool)
		}
	}
	surge := buildSurgeBinary(t, root)
	env := envForParity(root)
	sgRel := filepath.ToSlash(filepath.Join("testdata", "llvm_parity", "imported_copies", "copies.sg"))
	want := "4\n42\nbar\n1\n499\n1500250\n499749001\n"

	vmOut, vmErr, vmCode := runSurgeWithEnv(t, root, surge, env, "run", "--backend=vm", sgRel)
	if vmCode != 0 || vmOut != want {
		t.Fatalf("vm: exit %d stdout %q, want exit 0 and %q\nstderr:\n%s", vmCode, vmOut, want, vmErr)
	}
	buildOut, buildErr, buildCode := runSurgeWithEnv(t, root, surge, env, "build", sgRel)
	if buildCode != 0 {
		t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
	}
	binPath := filepath.Join(root, "target", "debug", "copies")
	llOut, llErr, llCode := runBinary(t, binPath)
	if llCode != 0 || llOut != want {
		t.Fatalf("llvm: exit %d stdout %q, want exit 0 and %q\nstderr:\n%s", llCode, llOut, want, llErr)
	}
	vgOut, vgErr, vgCode := runBinaryUnderValgrind(t, binPath, env, 2*time.Minute)
	if vgCode != 0 || vgOut != want || valgrindMemcheckErrorRE.MatchString(vgErr) ||
		!strings.Contains(vgErr, "ERROR SUMMARY: 0 errors") {
		t.Fatalf("valgrind: exit %d stdout %q\nstderr:\n%s", vgCode, vgOut, vgErr)
	}
}
