package vm_test

import (
	"os"
	"strings"
	"testing"
)

// A body-less member of an extern<T> block is a legal declaration, but nothing
// here provides it and it is not @intrinsic. Its call used to run the builtin
// of the same name on the struct (VM1003 for __len, the array indexer for
// __index); it must now stop with the declaration's name on the VM and fail the
// native build by name.
const bodilessExternCalleeSource = `type Box = { value: int };
extern<Box> {
    fn __len(self: &Box) -> uint;
}
@entrypoint
fn main() -> int {
    let b = Box { value = 1 };
    print(b.__len() to string);
    return 0;
}
`

func TestBodilessExternCalleeIsRefusedVM(t *testing.T) {
	requireVMBackend(t)
	res := runProgramFromSource(t, bodilessExternCalleeSource, runOptions{captureStdout: true})
	if !strings.Contains(res.stderr, "VM1007") || !strings.Contains(res.stderr, "__len") {
		t.Fatalf("body-less extern callee was not refused by name: exit=%d stdout=%q\nstderr:\n%s", res.exitCode, res.stdout, res.stderr)
	}
}

func TestBodilessExternCalleeIsRefusedLLVM(t *testing.T) {
	ensureLLVMToolchain(t)
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	srcPath := artifactSourcePath(artifacts)
	if err := os.WriteFile(srcPath, []byte(bodilessExternCalleeSource), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	surge := buildSurgeBinary(t, root)
	stdout, stderr, code := runSurgeWithInput(t, root, surge, "", "build", srcPath)
	out := stdout + stderr
	if code == 0 || !strings.Contains(out, "call of __len, which is declared without a body and is not @intrinsic") {
		t.Fatalf("native build did not refuse the body-less extern callee by name (exit=%d)\n%s", code, out)
	}
}
