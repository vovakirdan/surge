package goldencheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRejectsFrozenUntrackedDirectory(t *testing.T) {
	repo := newTestRepository(t)
	if err := os.Mkdir(filepath.Join(repo.goldenRoot, "orphan"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertCheckRejectsFrozenUntrackedEntry(t, repo, "orphan")
}

func TestCheckRejectsFrozenEmptyRoot(t *testing.T) {
	repo := newTestRepository(t)
	runGitCommand(t, repo.root, "rm", "--", "testdata/golden/seed.sg")
	runGitCommand(t, repo.root, "commit", "-qm", "remove the last golden entry")
	if err := os.MkdirAll(repo.goldenRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	assertCheckRejectsFrozenUntrackedEntry(t, repo, ".")
}

func assertCheckRejectsFrozenUntrackedEntry(t *testing.T, repo testRepository, entry string) {
	t.Helper()
	// A frozen filesystem digest can accidentally bless an empty directory,
	// which git status cannot see and a fresh checkout cannot reproduce.
	snapshot, err := Scan(repo.goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	expectations, err := LoadExpectations(repo.expectations)
	if err != nil {
		t.Fatal(err)
	}
	expectations.EntryCount = len(snapshot.Entries)
	expectations.CorpusSHA256 = snapshot.Digest()
	if writeErr := WriteExpectations(repo.expectations, &expectations); writeErr != nil {
		t.Fatal(writeErr)
	}
	changes, err := GitChanges(context.Background(), repo.root, repo.goldenRoot)
	if err != nil || len(changes) != 0 {
		t.Fatalf("control requires clean git status, got %v, %v", changes, err)
	}
	sentinel := filepath.Join(repo.root, "generator-ran")
	err = checkError(t, repo.options("sh", "-c", `printf ran > "$1"`, "generator", sentinel))
	if err == nil || !strings.Contains(err.Error(), `filesystem entry not indexed: "`+entry+`"`) {
		t.Fatalf("check error = %v, want unindexed directory refusal", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("generator ran before index census: %v", err)
	}
}
