package goldencheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckPostflightLeavesEveryProposedChange(t *testing.T) {
	tests := []struct {
		name    string
		command func(testRepository) []string
		want    string
		assert  func(*testing.T, testRepository)
	}{
		{
			name: "added ignored file",
			command: func(repo testRepository) []string {
				path := filepath.Join(repo.goldenRoot, "added.ignored")
				return []string{"sh", "-c", `printf added > "$1"`, "generator", path}
			},
			want: "added",
			assert: func(t *testing.T, repo testRepository) {
				assertFileContent(t, filepath.Join(repo.goldenRoot, "added.ignored"), "added")
			},
		},
		{
			name: "removed tracked file",
			command: func(repo testRepository) []string {
				return []string{"sh", "-c", `rm "$1"`, "generator", repo.seed}
			},
			want: "removed",
			assert: func(t *testing.T, repo testRepository) {
				if _, err := os.Stat(repo.seed); !os.IsNotExist(err) {
					t.Fatalf("removed proposal was restored: %v", err)
				}
			},
		},
		{
			name: "changed content",
			command: func(repo testRepository) []string {
				return []string{"sh", "-c", `printf changed > "$1"`, "generator", repo.seed}
			},
			want: "changed",
			assert: func(t *testing.T, repo testRepository) {
				assertFileContent(t, repo.seed, "changed")
			},
		},
		{
			name: "changed mode",
			command: func(repo testRepository) []string {
				return []string{"chmod", "755", repo.seed}
			},
			want: "changed",
			assert: func(t *testing.T, repo testRepository) {
				info, err := os.Stat(repo.seed)
				if err != nil || info.Mode().Perm() != 0o755 {
					t.Fatalf("mode proposal = %v, %v", info, err)
				}
			},
		},
		{
			name: "changed type",
			command: func(repo testRepository) []string {
				return []string{"sh", "-c", `rm "$1" && ln -s target "$1"`, "generator", repo.seed}
			},
			want: "changed",
			assert: func(t *testing.T, repo testRepository) {
				info, err := os.Lstat(repo.seed)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("type proposal = %v, %v", info, err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newTestRepository(t)
			err := checkError(t, repo.options(test.command(repo)...))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("check error = %v, want %q", err, test.want)
			}
			test.assert(t, repo)
		})
	}
}

func assertFileContent(t *testing.T, filename, want string) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", filename, data, want)
	}
}

func TestCheckRunsGeneratorTwiceInOrder(t *testing.T) {
	repo := newTestRepository(t)
	log := filepath.Join(repo.root, "runs")
	command := []string{"sh", "-c", `printf x >> "$1"`, "generator", log}
	if err := checkError(t, repo.options(command...)); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, log, "xx")
}
