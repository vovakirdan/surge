//go:build golden
// +build golden

package vm_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func TestGoldenUpdateDeterminism(t *testing.T) {
	root := repoRoot(t)
	surge := buildSurgeBinary(t, root)
	script := filepath.Join(root, "scripts", "golden_update.sh")

	runGoldenUpdate(t, root, script, surge)
	first := snapshotGolden(t, root)

	runGoldenUpdate(t, root, script, surge)
	second := snapshotGolden(t, root)

	if len(first) != len(second) {
		t.Fatalf("golden snapshot size mismatch: first=%d second=%d", len(first), len(second))
	}

	for _, path := range sortedKeys(first) {
		if hash2, ok := second[path]; !ok {
			t.Fatalf("missing golden file after rerun: %s", path)
		} else if hash2 != first[path] {
			t.Fatalf("golden file changed after rerun: %s", path)
		}
	}
	for _, path := range sortedKeys(second) {
		if _, ok := first[path]; !ok {
			t.Fatalf("new golden file after rerun: %s", path)
		}
	}
}

func runGoldenUpdate(t *testing.T, root, script, surge string) {
	t.Helper()
	cmd := exec.Command(script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SURGE_BIN="+surge)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("golden update failed: %v\n%s", err, string(output))
	}
}

func snapshotGolden(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	goldenDir := filepath.Join(root, "testdata", "golden")
	err := filepath.WalkDir(goldenDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "spec_audit" {
				return fs.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot golden: %v", err)
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
