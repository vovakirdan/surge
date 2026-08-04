package goldencheck

import (
	"context"
	"testing"
)

func TestMergeEnvironmentUsesExplicitOverride(t *testing.T) {
	merged := mergeEnvironment([]string{"A=old", "B=kept"}, []string{"A=new", "C=added"})
	want := []string{"A=new", "B=kept", "C=added"}
	if len(merged) != len(want) {
		t.Fatalf("environment = %v", merged)
	}
	for i := range want {
		if merged[i] != want[i] {
			t.Fatalf("environment[%d] = %q, want %q", i, merged[i], want[i])
		}
	}
}

func TestUpdateRefreshesFrozenSnapshot(t *testing.T) {
	repo := newTestRepository(t)
	options := repo.options("sh", "-c", `printf changed > "$1"`, "generator", repo.seed)
	if err := Update(context.Background(), &options); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Scan(repo.goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	expectations, err := LoadExpectations(repo.expectations)
	if err != nil {
		t.Fatal(err)
	}
	if err := expectations.VerifyCorpus(snapshot); err != nil {
		t.Fatalf("updated manifest does not describe generated corpus: %v", err)
	}
	assertFileContent(t, repo.seed, "changed")
}
