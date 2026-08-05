package gatecheck

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fileSizeGateRepo struct {
	t   *testing.T
	dir string
}

func newFileSizeGateRepo(t *testing.T) *fileSizeGateRepo {
	t.Helper()
	dir := t.TempDir()
	r := &fileSizeGateRepo{t: t, dir: dir}
	root := repoRoot(t)
	for _, name := range []string{"effective_loc.awk", "runtime_v2_file_size_check.sh"} {
		data, err := os.ReadFile(filepath.Join(root, "scripts", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		r.write(filepath.Join("scripts", name), string(data), mode)
	}
	r.git("init", "-q")
	r.git("config", "user.name", "Gate Test")
	r.git("config", "user.email", "gate@example.test")
	return r
}

func (r *fileSizeGateRepo) write(name, content string, mode os.FileMode) {
	r.t.Helper()
	path := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatalf("mkdir %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		r.t.Fatalf("write %s: %v", name, err)
	}
}

func (r *fileSizeGateRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *fileSizeGateRepo) commit(message string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-qm", message)
	return r.git("rev-parse", "HEAD")
}

func (r *fileSizeGateRepo) run(base string) (string, int) {
	r.t.Helper()
	cmd := exec.Command(filepath.Join("scripts", "runtime_v2_file_size_check.sh"))
	cmd.Dir = r.dir
	cmd.Env = []string{
		"EPIC_BASE=" + base,
		"HOME=" + r.dir,
		"LC_ALL=C",
		"PATH=" + os.Getenv("PATH"),
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	r.t.Fatalf("run gate: %v", err)
	return "", -1
}

func sourceLines(prefix string, count int) string {
	var b strings.Builder
	for i := range count {
		fmt.Fprintf(&b, "var %s%d = %d\n", prefix, i, i)
	}
	return b.String()
}

func TestRuntimeV2FileSizeMakeTargetIsWired(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".PHONY: runtime-v2-file-size-check", "runtime-v2-file-size-check:",
		"./scripts/runtime_v2_file_size_check.sh"} {
		if !strings.Contains(string(makefile), want) {
			t.Errorf("Makefile missing %q", want)
		}
	}
}

func TestRuntimeV2FileSizeGateUsesCommittedProdAndTestBlobs(t *testing.T) {
	r := newFileSizeGateRepo(t)
	r.write("prod.go", sourceLines("base", 2), 0o644)
	r.write("prod_test.go", sourceLines("testBase", 1), 0o644)
	base := r.commit("base")
	r.write("prod.go", sourceLines("head", 3), 0o644)
	r.write("prod_test.go", sourceLines("testHead", 2), 0o644)
	r.commit("head")

	cleanReport, cleanCode := r.run(base)
	if cleanCode != 0 {
		t.Fatalf("clean gate exit=%d\n%s", cleanCode, cleanReport)
	}
	for _, want := range []string{
		"path=prod.go", "path=prod_test.go", "physical_base=", "physical_head=",
		"physical_churn=", "effective_base=", "effective_head=", "effective_churn=",
		"worktree state ignored",
	} {
		if !strings.Contains(cleanReport, want) {
			t.Errorf("report missing %q:\n%s", want, cleanReport)
		}
	}
	if strings.Contains(cleanReport, "%") {
		t.Fatalf("report must not use percentages:\n%s", cleanReport)
	}

	r.write("prod.go", sourceLines("dirty", 700), 0o644)
	r.write("untracked_test.go", sourceLines("untracked", 700), 0o644)
	r.write(filepath.Join(".serena", "project.yml"), "tool: state\n", 0o644)
	dirtyReport, dirtyCode := r.run(base)
	if dirtyCode != cleanCode || dirtyReport != cleanReport {
		t.Fatalf("dirty state changed committed report/exit\nclean(%d):\n%s\ndirty(%d):\n%s",
			cleanCode, cleanReport, dirtyCode, dirtyReport)
	}
}

func TestRuntimeV2FileSizeGateRejectsInvalidBase(t *testing.T) {
	r := newFileSizeGateRepo(t)
	r.write("prod.go", sourceLines("base", 1), 0o644)
	r.commit("base")
	tree := r.git("write-tree")
	unrelated := r.git("commit-tree", tree, "-m", "unrelated")

	for _, tc := range []struct {
		name string
		base string
		want string
	}{
		{name: "missing", want: "EPIC_BASE is required"},
		{name: "invalid", base: "not-a-ref", want: "does not name a commit"},
		{name: "non-ancestor", base: unrelated, want: "is not an ancestor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := r.run(tc.base)
			if code != 2 || !strings.Contains(out, tc.want) {
				t.Fatalf("exit=%d want error %q\n%s", code, tc.want, out)
			}
		})
	}
}

func TestRuntimeV2FileSizeGateReportsEveryViolationKind(t *testing.T) {
	tests := []struct {
		name, base, head string
		wants            []string
	}{
		{name: "new", head: sourceLines("new", 501), wants: []string{"code=NEW_OVER_500"}},
		{name: "legacy-growth", base: sourceLines("same", 501), head: sourceLines("same", 502), wants: []string{"code=LEGACY_GROWTH"}},
		{name: "crossing", base: sourceLines("same", 500), head: sourceLines("same", 501), wants: []string{"code=CROSSED_500"}},
		{name: "rewrite", base: sourceLines("old", 600), head: sourceLines("new", 600), wants: []string{"code=REWRITE_OVER_500"}},
		{name: "each-rule", base: sourceLines("old", 600), head: sourceLines("new", 601), wants: []string{"code=LEGACY_GROWTH", "code=REWRITE_OVER_500"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newFileSizeGateRepo(t)
			if tc.base != "" {
				r.write("target.go", tc.base, 0o644)
			}
			base := r.commit("base")
			r.write("target.go", tc.head, 0o644)
			r.commit("head")
			out, code := r.run(base)
			if code != 1 {
				t.Fatalf("exit=%d want 1\n%s", code, out)
			}
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q\n%s", want, out)
				}
			}
		})
	}
}

func TestRuntimeV2FileSizeGateRejectsBinarySource(t *testing.T) {
	r := newFileSizeGateRepo(t)
	base := r.commit("base")
	r.write("binary.go", "package p\x00\n", 0o644)
	r.commit("head")
	out, code := r.run(base)
	if code != 2 || !strings.Contains(out, "binary content is not valid source") {
		t.Fatalf("exit=%d\n%s", code, out)
	}
}

func TestRuntimeV2FileSizeGateParsesNULSafeRename(t *testing.T) {
	r := newFileSizeGateRepo(t)
	oldPath := "odd name\tline\n_test.go"
	newPath := "renamed name\tline\n_test.go"
	r.write(oldPath, sourceLines("same", 501), 0o644)
	base := r.commit("base")
	r.git("mv", "--", oldPath, newPath)
	r.commit("rename")

	out, code := r.run(base)
	if code != 0 {
		t.Fatalf("rename gate exit=%d\n%s", code, out)
	}
	for _, want := range []string{
		"status=R100", "effective_base=501", "effective_head=501", "effective_churn=0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rename report missing %q:\n%s", want, out)
		}
	}
}
