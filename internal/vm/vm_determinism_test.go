package vm_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVMDebugTraceDeterminism(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "determinism.sg")
	source := `fn write_line(s: string) -> nothing {
    let nl = "\n";
    rt_write_stdout(rt_string_ptr(&s), rt_string_len_bytes(&s));
    rt_write_stdout(rt_string_ptr(&nl), 1:uint);
}

@entrypoint
fn main() -> int {
    let mut s: string = "";
    let mut i: int = 0;
    while i < 3 {
        s = s + "a";
        i = i + 1;
    }
    let a: int[] = [1, 2, 3];
    write_line(s);
    write_line(a[1] to string);
    return 0;
}
`
	if err := os.WriteFile(srcPath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stdout1, stderr1, code1 := runSurge(t, root, surge,
		"run",
		"--backend=vm",
		"--vm-trace",
		srcPath,
	)
	stdout2, stderr2, code2 := runSurge(t, root, surge,
		"run",
		"--backend=vm",
		"--vm-trace",
		srcPath,
	)

	if code1 != 0 || code2 != 0 {
		t.Fatalf("unexpected exit codes: first=%d second=%d", code1, code2)
	}
	if stdout1 != stdout2 {
		t.Fatalf("stdout mismatch:\nfirst:\n%s\nsecond:\n%s", stdout1, stdout2)
	}
	if stderr1 != stderr2 {
		t.Fatalf("trace mismatch:\nfirst:\n%s\nsecond:\n%s", stderr1, stderr2)
	}
}
