package goldencheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexCensusAcceptsTrackedLexicalPaths(t *testing.T) {
	repo := newTestRepository(t)
	for _, filename := range []string{"line\nbreak\t雪.tokens", "core_stdlib/nested/name[1].sg", `back\slash.tokens`} {
		writeTestFile(t, filepath.Join(repo.goldenRoot, filename), "content\n", 0o644)
	}
	runGitCommand(t, repo.root, "add", "testdata/golden")
	runGitCommand(t, repo.root, "commit", "-qm", "tracked lexical paths")
	snapshot, err := Scan(repo.goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyIndexCensus(context.Background(), repo.root, snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestIndexCensusRejectsFilesystemDifferences(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*testing.T, testRepository)
		want        string
		statusHides bool
	}{
		{
			name: "untracked sidecar",
			mutate: func(t *testing.T, repo testRepository) {
				writeTestFile(t, filepath.Join(repo.goldenRoot, "new.tokens"), "new\n", 0o644)
			},
			want: `filesystem entry not indexed: "new.tokens"`,
		},
		{
			name: "ignored nested sidecar",
			mutate: func(t *testing.T, repo testRepository) {
				writeTestFile(t, filepath.Join(repo.goldenRoot, "core_stdlib", "hidden.ignored"), "ignored\n", 0o644)
			},
			want: `filesystem entry not indexed: "core_stdlib/hidden.ignored"`,
		},
		{
			name: "missing tracked file",
			mutate: func(t *testing.T, repo testRepository) {
				if err := os.Remove(repo.seed); err != nil {
					t.Fatal(err)
				}
			},
			want: `missing indexed entry: "seed.sg"`,
		},
		{
			name: "missing skip-worktree file",
			mutate: func(t *testing.T, repo testRepository) {
				runGitCommand(t, repo.root, "update-index", "--skip-worktree", "testdata/golden/seed.sg")
				if err := os.Remove(repo.seed); err != nil {
					t.Fatal(err)
				}
			},
			want:        `missing indexed entry: "seed.sg"`,
			statusHides: true,
		},
		{
			name: "empty directory",
			mutate: func(t *testing.T, repo testRepository) {
				if err := os.Mkdir(filepath.Join(repo.goldenRoot, "orphan"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want:        `filesystem entry not indexed: "orphan"`,
			statusHides: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newTestRepository(t)
			test.mutate(t, repo)
			if test.statusHides {
				changes, err := GitChanges(context.Background(), repo.root, repo.goldenRoot)
				if err != nil || len(changes) != 0 {
					t.Fatalf("control requires clean git status, got %v, %v", changes, err)
				}
			}
			snapshot, err := Scan(repo.goldenRoot)
			if err != nil {
				t.Fatal(err)
			}
			err = verifyIndexCensus(context.Background(), repo.root, snapshot)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("index census error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseIndexCensusRejectsMalformedRecords(t *testing.T) {
	for _, record := range []string{
		"testdata/golden/seed.sg",
		"\x00",
		"elsewhere/seed.sg\x00",
		"testdata/golden/../outside\x00",
		"testdata/golden//absolute\x00",
		"testdata/golden/..\x00",
		"testdata/golden/seed.sg\x00testdata/golden/seed.sg\x00",
		"testdata/golden/dir\x00testdata/golden/dir/seed.sg\x00",
		"testdata/golden/dir/seed.sg\x00testdata/golden/dir\x00",
	} {
		t.Run(record, func(t *testing.T) {
			if _, err := parseIndexCensus([]byte(record)); err == nil {
				t.Fatalf("accepted malformed index records %q", record)
			}
		})
	}
}
