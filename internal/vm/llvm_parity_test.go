package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLLVMParity(t *testing.T) {
	root := repoRoot(t)

	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed; skipping LLVM parity test")
	}
	if _, err := exec.LookPath("ar"); err != nil {
		t.Skip("ar not installed; skipping LLVM parity test")
	}

	surge := buildSurgeBinary(t, root)

	cases := []struct {
		name  string
		file  string
		setup func(t *testing.T) []string
	}{
		{name: "exit_code", file: "exit_code.sg"},
		{name: "panic", file: "panic.sg"},
		{name: "string_concat", file: "string_concat.sg"},
		{name: "tagged_switch", file: "tagged_switch.sg"},
		{name: "unicode_len", file: "unicode_len.sg"},
		{name: "path_smoke", file: "path_smoke.sg"},
		{
			name: "fs_dir_smoke",
			file: "fs_dir_smoke.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o600); err != nil {
					t.Fatalf("write a.txt: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bravo"), 0o600); err != nil {
					t.Fatalf("write b.txt: %v", err)
				}
				return []string{dir}
			},
		},
		{
			name: "fs_metadata_smoke",
			file: "fs_metadata_smoke.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
					t.Fatalf("write a.txt: %v", err)
				}
				return []string{dir}
			},
		},
		{
			name: "file_rw_smoke",
			file: "file_rw_smoke.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				return []string{dir}
			},
		},
		{
			name: "file_seek_head_tail_smoke_lowlevel",
			file: "file_seek_head_tail_smoke_lowlevel.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				return []string{dir}
			},
		},
		{name: "net_listen_close", file: "net_listen_close.sg"},
		{
			name: "head_tail_text",
			file: "head_tail_text.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "text.txt"), []byte("hello world"), 0o600); err != nil {
					t.Fatalf("write text.txt: %v", err)
				}
				return []string{dir}
			},
		},
		{
			name: "walkdir_for_in",
			file: "walkdir_for_in.sg",
			setup: func(t *testing.T) []string {
				dir := t.TempDir()
				if err := os.MkdirAll(filepath.Join(dir, "a"), 0o700); err != nil {
					t.Fatalf("mkdir a: %v", err)
				}
				if err := os.MkdirAll(filepath.Join(dir, "b", "sub"), 0o700); err != nil {
					t.Fatalf("mkdir b/sub: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("root"), 0o600); err != nil {
					t.Fatalf("write root.txt: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "a", "a.txt"), []byte("a"), 0o600); err != nil {
					t.Fatalf("write a.txt: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "b", "sub", "b.txt"), []byte("b"), 0o600); err != nil {
					t.Fatalf("write b.txt: %v", err)
				}
				return []string{dir}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sgRel := filepath.ToSlash(filepath.Join("testdata", "llvm_parity", tc.file))

			var progArgs []string
			if tc.setup != nil {
				progArgs = tc.setup(t)
			}
			args := []string{"run", "--backend=vm", sgRel}
			if len(progArgs) > 0 {
				args = append(args, "--")
				args = append(args, progArgs...)
			}
			vmOut, vmErr, vmCode := runSurge(t, root, surge, args...)

			buildOut, buildErr, buildCode := runSurge(t, root, surge, "build", sgRel)
			if buildCode != 0 {
				t.Fatalf("build failed (code=%d)\nstdout:\n%s\nstderr:\n%s", buildCode, buildOut, buildErr)
			}

			binPath := filepath.Join(root, "target", "debug", tc.name)
			llOut, llErr, llCode := runBinary(t, binPath, progArgs...)

			if llCode != vmCode {
				t.Fatalf("exit code mismatch: vm=%d llvm=%d", vmCode, llCode)
			}
			if llOut != vmOut {
				t.Fatalf("stdout mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmOut, llOut)
			}
			if llErr != vmErr {
				t.Fatalf("stderr mismatch:\n--- vm ---\n%s\n--- llvm ---\n%s", vmErr, llErr)
			}
		})
	}
}

// runBinary is defined in llvm_smoke_test.go
