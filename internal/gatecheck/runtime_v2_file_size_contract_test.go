package gatecheck

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runFileSizeGateFrom(t *testing.T, r *fileSizeGateRepo, base, cwd string, args []string, extraEnv ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(filepath.Join(r.dir, "scripts", "runtime_v2_file_size_check.sh"), args...)
	cmd.Dir = cwd
	cmd.Env = append([]string{
		"EPIC_BASE=" + base,
		"HOME=" + r.dir,
		"LC_ALL=C",
		"PATH=" + os.Getenv("PATH"),
	}, extraEnv...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	t.Fatalf("run gate: %v", err)
	return "", -1
}

func readGateTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func requireGateTestFileUnchanged(t *testing.T, path string, before []byte, phase string) {
	t.Helper()
	if after := readGateTestFile(t, path); !bytes.Equal(after, before) {
		t.Fatalf("%s changed foreign Git file %s", phase, path)
	}
}

func TestRuntimeV2FileSizeGateIsolatesCallerGitEnvironment(t *testing.T) {
	outer := &fileSizeGateRepo{t: t, dir: t.TempDir()}
	outer.git("init", "-q")
	outer.git("config", "core.bare", "false")
	outer.git("config", "user.name", "Outer Owner")
	outer.git("config", "user.email", "outer@example.test")
	outer.write("outer.go", sourceLines("outer", 1), 0o644)
	outer.commit("outer")

	outerGitDir := filepath.Join(outer.dir, ".git")
	outerConfig := filepath.Join(outerGitDir, "config")
	outerIndex := filepath.Join(outerGitDir, "index")
	configBefore := readGateTestFile(t, outerConfig)
	indexBefore := readGateTestFile(t, outerIndex)

	for name, value := range map[string]string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": filepath.Join(outerGitDir, "objects"),
		"GIT_COMMON_DIR":                   outerGitDir,
		"GIT_CONFIG":                       outerConfig,
		"GIT_DIR":                          outerGitDir,
		"GIT_INDEX_FILE":                   outerIndex,
		"GIT_OBJECT_DIRECTORY":             filepath.Join(outerGitDir, "objects"),
		"GIT_WORK_TREE":                    outer.dir,
	} {
		t.Setenv(name, value)
	}

	r := newFileSizeGateRepo(t)
	r.write("prod.go", sourceLines("base", 2), 0o644)
	base := r.commit("base")
	r.write("prod.go", sourceLines("head", 3), 0o644)
	r.commit("head")
	requireGateTestFileUnchanged(t, outerConfig, configBefore, "fixture setup")
	requireGateTestFileUnchanged(t, outerIndex, indexBefore, "fixture setup")

	nested := filepath.Join(r.dir, "nested", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	cleanOut, cleanCode := runFileSizeGateFrom(t, r, base, nested, nil)
	poisonedOut, poisonedCode := runFileSizeGateFrom(t, r, base, nested, nil,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES="+filepath.Join(outerGitDir, "objects"),
		"GIT_CEILING_DIRECTORIES="+r.dir,
		"GIT_COMMON_DIR="+outerGitDir,
		"GIT_CONFIG="+outerConfig,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.bare",
		"GIT_CONFIG_PARAMETERS='core.bare=true'",
		"GIT_CONFIG_VALUE_0=true",
		"GIT_DIR="+outerGitDir,
		"GIT_DISCOVERY_ACROSS_FILESYSTEM=0",
		"GIT_GRAFT_FILE="+filepath.Join(outerGitDir, "info", "grafts"),
		"GIT_IMPLICIT_WORK_TREE=0",
		"GIT_INDEX_FILE="+outerIndex,
		"GIT_INTERNAL_SUPER_PREFIX=foreign/",
		"GIT_NAMESPACE=foreign",
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_OBJECT_DIRECTORY="+filepath.Join(outerGitDir, "objects"),
		"GIT_PREFIX=foreign/",
		"GIT_QUARANTINE_PATH="+filepath.Join(outerGitDir, "objects", "incoming"),
		"GIT_REPLACE_REF_BASE=refs/replace/foreign",
		"GIT_SHALLOW_FILE="+filepath.Join(outerGitDir, "shallow"),
		"GIT_WORK_TREE="+outer.dir,
	)
	if poisonedCode != cleanCode || poisonedOut != cleanOut {
		t.Fatalf("caller Git environment changed report/exit\nclean(%d):\n%s\npoisoned(%d):\n%s",
			cleanCode, cleanOut, poisonedCode, poisonedOut)
	}
	requireGateTestFileUnchanged(t, outerConfig, configBefore, "gate run")
	requireGateTestFileUnchanged(t, outerIndex, indexBefore, "gate run")
}

func commitGenuineCopy(t *testing.T, r *fileSizeGateRepo, source, destination string, lines int) string {
	t.Helper()
	original := sourceLines("copyBase", lines)
	r.write(source, original, 0o644)
	base := r.commit("base")
	r.write(source, sourceLines("copyHead", lines), 0o644)
	r.write(destination, original, 0o644)
	r.commit("copy")
	return base
}

func requireGenuineCopyRecord(t *testing.T, r *fileSizeGateRepo, base, source, destination string) {
	t.Helper()
	raw := r.git("-c", "diff.renameLimit=999999", "diff", "--raw", "-z",
		"--find-renames=50%", "--find-copies=50%", "--no-abbrev", base, "HEAD", "--")
	fields := strings.Split(raw, "\x00")
	for i := 0; i < len(fields); {
		metadata := fields[i]
		i++
		if metadata == "" {
			continue
		}
		parts := strings.Fields(metadata)
		if len(parts) != 5 || i >= len(fields) {
			t.Fatalf("malformed raw diff record %q", metadata)
		}
		status := parts[4]
		firstPath := fields[i]
		i++
		if status[0] != 'R' && status[0] != 'C' {
			continue
		}
		if i >= len(fields) {
			t.Fatalf("raw %s record has no destination", status)
		}
		secondPath := fields[i]
		i++
		if status == "C100" && firstPath == source && secondPath == destination {
			return
		}
	}
	t.Fatalf("git did not produce genuine C100 record for %q -> %q; raw=%q", source, destination, raw)
}

func TestRuntimeV2FileSizeGateCopyScopeIsNULSafe(t *testing.T) {
	t.Run("out-of-scope destination is ignored", func(t *testing.T) {
		r := newFileSizeGateRepo(t)
		source := "odd source\tline\n.go"
		destination := "-odd copy\tline\n.txt"
		base := commitGenuineCopy(t, r, source, destination, 4)
		requireGenuineCopyRecord(t, r, base, source, destination)

		out, code := r.run(base)
		if code != 0 {
			t.Fatalf("copy gate exit=%d\n%s", code, out)
		}
		if strings.Count(out, "FILE status=") != 1 || !strings.Contains(out, "FILE status=M") ||
			!strings.Contains(out, "PASS files=1 violations=0") {
			t.Fatalf("out-of-scope copy produced unexpected rows:\n%s", out)
		}
		if strings.Contains(out, "status=C") || strings.Contains(out, "physical_base=0 physical_head=0") {
			t.Fatalf("out-of-scope copy produced a fictitious source row:\n%s", out)
		}
	})

	t.Run("scoped destination remains new", func(t *testing.T) {
		r := newFileSizeGateRepo(t)
		source := "odd seed\tline\n.txt"
		destination := "-odd copy\tline\n.go"
		base := commitGenuineCopy(t, r, source, destination, 501)
		requireGenuineCopyRecord(t, r, base, source, destination)

		out, code := r.run(base)
		if code != 1 {
			t.Fatalf("copy gate exit=%d want 1\n%s", code, out)
		}
		for _, want := range []string{
			"FILE status=C100", "physical_base=0", "effective_base=0", "effective_head=501",
			"code=NEW_OVER_500", "FAIL files=1 violations=1",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("scoped copy report missing %q:\n%s", want, out)
			}
		}
		if strings.Count(out, "FILE status=") != 1 {
			t.Fatalf("scoped copy produced unexpected rows:\n%s", out)
		}
	})
}

func TestRuntimeV2FileSizeGateSourceIsChosenByArgumentThenVariable(t *testing.T) {
	r := newFileSizeGateRepo(t)
	r.write("legacy.go", sourceLines("legacy", 501), 0o644)
	base := r.commit("base")
	r.write("legacy.go", sourceLines("legacy", 520), 0o644)

	for _, tc := range []struct {
		name, env string
		args      []string
		wantCode  int
		want      string
	}{
		{name: "default", wantCode: 1, want: "measuring the worktree against " + base},
		{name: "variable picks committed", env: "committed", want: "measuring committed blobs only"},
		{name: "variable picks worktree", env: "worktree", wantCode: 1, want: "measuring the worktree against " + base},
		{name: "argument outranks variable", env: "worktree", args: []string{"--committed"},
			want: "measuring committed blobs only"},
		{name: "unusable variable", env: "index", wantCode: 2,
			want: "SIZE_CHECK_SOURCE must be worktree or committed"},
		{name: "unknown argument", args: []string{"--index"}, wantCode: 2, want: "unknown argument: --index"},
		{name: "help names both modes", args: []string{"--help"}, want: "usage: runtime_v2_file_size_check.sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var env []string
			if tc.env != "" {
				env = append(env, "SIZE_CHECK_SOURCE="+tc.env)
			}
			out, code := runFileSizeGateFrom(t, r, base, r.dir, tc.args, env...)
			if code != tc.wantCode || !strings.Contains(out, tc.want) {
				t.Fatalf("exit=%d want %d containing %q\n%s", code, tc.wantCode, tc.want, out)
			}
		})
	}
}
