package goldencheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type externalFileState struct {
	content string
	mode    os.FileMode
}

func captureExternalFile(t *testing.T, filename string) externalFileState {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filename)
	if err != nil {
		t.Fatal(err)
	}
	return externalFileState{content: string(data), mode: info.Mode()}
}

func assertExternalFile(t *testing.T, filename string, want externalFileState) {
	t.Helper()
	got := captureExternalFile(t, filename)
	if got != want {
		t.Fatalf("external file changed: got %#v, want %#v", got, want)
	}
}

func assertSymlink(t *testing.T, filename, target string) {
	t.Helper()
	got, err := os.Readlink(filename)
	if err != nil {
		t.Fatalf("read symlink %s: %v", filename, err)
	}
	if got != target {
		t.Fatalf("symlink target = %q, want %q", got, target)
	}
}

func TestGoldenScriptRejectsExternalSymlinkEscapes(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, scriptFixture) (string, string, string)
	}{
		{
			name: "source",
			setup: func(t *testing.T, fixture scriptFixture) (string, string, string) {
				external := filepath.Join(fixture.root, "external-source.sg")
				writeTestFile(t, external, "fn main() {}\n", 0o600)
				if err := os.Remove(fixture.sourcePath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, fixture.sourcePath); err != nil {
					t.Fatal(err)
				}
				return external, fixture.sourcePath, external
			},
		},
		{
			name: "sidecar",
			setup: func(t *testing.T, fixture scriptFixture) (string, string, string) {
				external := filepath.Join(fixture.root, "external-sidecar")
				writeTestFile(t, external, "outside\n", 0o600)
				link := filepath.Join(filepath.Dir(fixture.sourcePath), "valid.tokens")
				if err := os.Symlink(external, link); err != nil {
					t.Fatal(err)
				}
				return external, link, external
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, fixture scriptFixture) (string, string, string) {
				externalDir := filepath.Join(fixture.root, "external-directory")
				external := filepath.Join(externalDir, "marker")
				writeTestFile(t, external, "outside\n", 0o600)
				link := filepath.Join(fixture.root, "testdata", "golden", "nested")
				if err := os.Symlink(externalDir, link); err != nil {
					t.Fatal(err)
				}
				return external, link, externalDir
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newScriptFixture(t, "valid.sg")
			external, link, target := test.setup(t, fixture)
			before := captureExternalFile(t, external)
			output, err := fixture.run(t, "", nil, nil)
			if err == nil {
				t.Fatalf("script accepted %s symlink\n%s", test.name, output)
			}
			assertExternalFile(t, external, before)
			assertSymlink(t, link, target)
			assertNoGoldenStaging(t, fixture.root)
		})
	}
}

func TestGoldenScriptRejectsSymlinkedRoot(t *testing.T) {
	fixture := newScriptFixture(t, "valid.sg")
	goldenRoot := filepath.Join(fixture.root, "testdata", "golden")
	realRoot := filepath.Join(fixture.root, "golden-target")
	if err := os.Rename(goldenRoot, realRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, goldenRoot); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(realRoot, "valid.sg")
	before := captureExternalFile(t, external)
	output, err := fixture.run(t, "", nil, nil)
	if err == nil {
		t.Fatalf("script accepted symlinked root\n%s", output)
	}
	assertExternalFile(t, external, before)
	assertSymlink(t, goldenRoot, realRoot)
	assertNoGoldenStaging(t, fixture.root)
}

func TestGoldenScriptRejectsSymlinkCopiedFromCore(t *testing.T) {
	fixture := newScriptFixture(t, "valid.sg")
	goldenRoot := filepath.Join(fixture.root, "testdata", "golden")
	before, err := Scan(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(fixture.root, "external-core.sg")
	writeTestFile(t, external, "fn core() {}\n", 0o600)
	if symlinkErr := os.Symlink(external, filepath.Join(fixture.root, "core", "escape.sg")); symlinkErr != nil {
		t.Fatal(symlinkErr)
	}
	externalBefore := captureExternalFile(t, external)
	marker := filepath.Join(fixture.root, "compiler-called")
	cmd := fixture.command(t, "", nil, nil, nil)
	cmd.Env = append(cmd.Env, "CALL_MARKER="+marker)
	output, runErr := cmd.CombinedOutput()
	if runErr == nil {
		t.Fatalf("script accepted core symlink\n%s", output)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("compiler ran before staged-tree validation: %v", statErr)
	}
	after, err := Scan(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	if changes := Diff(before, after); len(changes) != 0 {
		t.Fatalf("core symlink changed live corpus: %#v", changes)
	}
	assertExternalFile(t, external, externalBefore)
	assertNoGoldenStaging(t, fixture.root)
}

func TestGoldenScriptSetsDeterministicUmask(t *testing.T) {
	fixture := newScriptFixture(t, "valid.sg")
	base := fixture.command(t, "", nil, nil, nil)
	cmd := exec.Command("bash", "-c", `umask 077; exec "$1"`, "umask", fixture.script)
	cmd.Dir = base.Dir
	cmd.Env = base.Env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script under umask 077: %v\n%s", err, output)
	}
	for _, filename := range []string{"valid.diag", "valid.tokens", "valid.ast", "valid.fmt"} {
		info, err := os.Stat(filepath.Join(fixture.root, "testdata", "golden", filename))
		if err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("%s mode = %v, %v", filename, info, err)
		}
	}
	info, err := os.Stat(filepath.Join(fixture.root, "testdata", "golden", "core_stdlib"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("core_stdlib mode = %v, %v", info, err)
	}
	assertNoGoldenStaging(t, fixture.root)
}
