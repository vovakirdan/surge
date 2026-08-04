package goldencheck

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type testRepository struct {
	root         string
	goldenRoot   string
	expectations string
	seed         string
}

func newTestRepository(t *testing.T) testRepository {
	t.Helper()
	root := t.TempDir()
	goldenRoot := filepath.Join(root, "testdata", "golden")
	if err := os.MkdirAll(goldenRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(goldenRoot, "seed.sg")
	writeTestFile(t, seed, "fn main() {}\n", 0o644)
	writeTestFile(t, filepath.Join(root, ".gitignore"), "*.ignored\n", 0o644)
	expectationsPath := filepath.Join(root, "testdata", "golden.expectations.json")
	snapshot, err := Scan(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	expectations := Expectations{
		Version:      expectationVersion,
		GoldenRoot:   goldenRootPath,
		EntryCount:   len(snapshot.Entries),
		CorpusSHA256: snapshot.Digest(),
	}
	if writeErr := WriteExpectations(expectationsPath, &expectations); writeErr != nil {
		t.Fatal(writeErr)
	}
	runGitCommand(t, root, "init", "--initial-branch=main", "-q")
	runGitCommand(t, root, "config", "user.name", "Golden Test")
	runGitCommand(t, root, "config", "user.email", "golden@example.invalid")
	runGitCommand(t, root, "add", ".gitignore", "testdata")
	runGitCommand(t, root, "commit", "-qm", "baseline")
	return testRepository{root: root, goldenRoot: goldenRoot, expectations: expectationsPath, seed: seed}
}

func (repo testRepository) options(command ...string) Options {
	return Options{
		RepoRoot:         repo.root,
		ExpectationsPath: repo.expectations,
		Command:          command,
		Runs:             2,
	}
}

func writeTestFile(t *testing.T, filename, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func runGitCommand(t *testing.T, dir string, arguments ...string) {
	t.Helper()
	arguments = append([]string{"-c", "core.hooksPath=/dev/null"}, arguments...)
	cmd := exec.Command("git", arguments...)
	cmd.Dir = dir
	cmd.Env = cleanGitEnvironment(os.Environ())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func checkError(t *testing.T, options Options) error {
	t.Helper()
	return Check(context.Background(), &options)
}
