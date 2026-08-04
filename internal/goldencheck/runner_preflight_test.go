package goldencheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckPreflightRejectsDirtyCorpusBeforeGenerator(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, testRepository)
		code   string
	}{
		{
			name: "tracked modification",
			mutate: func(t *testing.T, repo testRepository) {
				writeTestFile(t, repo.seed, "modified\n", 0o644)
			},
			code: " M",
		},
		{
			name: "staged modification",
			mutate: func(t *testing.T, repo testRepository) {
				writeTestFile(t, repo.seed, "staged\n", 0o644)
				runGitCommand(t, repo.root, "add", "testdata/golden/seed.sg")
			},
			code: "M ",
		},
		{
			name: "missing tracked file",
			mutate: func(t *testing.T, repo testRepository) {
				if err := os.Remove(repo.seed); err != nil {
					t.Fatal(err)
				}
			},
			code: " D",
		},
		{
			name: "untracked file",
			mutate: func(t *testing.T, repo testRepository) {
				writeTestFile(t, filepath.Join(repo.goldenRoot, "new.tokens"), "new\n", 0o644)
			},
			code: "??",
		},
		{
			name: "ignored file",
			mutate: func(t *testing.T, repo testRepository) {
				writeTestFile(t, filepath.Join(repo.goldenRoot, "hidden.ignored"), "ignored\n", 0o644)
			},
			code: "!!",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newTestRepository(t)
			test.mutate(t, repo)
			sentinel := filepath.Join(repo.root, "generator-ran")
			command := []string{"sh", "-c", `printf ran > "$1"`, "generator", sentinel}
			err := checkError(t, repo.options(command...))
			if err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("check error = %v, want status %q", err, test.code)
			}
			if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
				t.Fatalf("generator ran during preflight: %v", statErr)
			}
		})
	}
}

func TestCheckPreflightPreservesLexicalPath(t *testing.T) {
	repo := newTestRepository(t)
	path := filepath.Join(repo.goldenRoot, "line\nbreak\t雪.tokens")
	writeTestFile(t, path, "untracked\n", 0o644)
	err := checkError(t, repo.options("true"))
	if err == nil || !strings.Contains(err.Error(), "line\\nbreak\\t雪.tokens") {
		t.Fatalf("check error did not quote the complete path: %v", err)
	}
}
