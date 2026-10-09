package driver

import (
	"path/filepath"
	"testing"
)

// A standalone stdlib module has no root-to-local remap, while its imported
// core tag copy lives in the root vocabulary. These three roots share the same
// Hash64.bucket body and therefore pin the physical-source owner fallback.
func TestStandaloneStdlibHashTagsKeepCoreOwner(t *testing.T) {
	repo := repoRootFromDriverTest(t)
	for _, rel := range []string{"stdlib/hash/hash.sg", "stdlib/hash/stable64.sg", "stdlib/hash/xxh64.sg"} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			result, err := coreRootDiagnose(t, filepath.Join(repo, rel), repo)
			if err != nil || coreRootHasErrors(result) {
				t.Fatalf("standalone %s did not retain the imported core tag owner: %v", rel, err)
			}
		})
	}
}
