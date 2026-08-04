package carriergate

import (
	"archive/tar"
	"bytes"
	"io"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"testing/fstest"
)

const (
	frozenBaseCount  = 588
	frozenBaseDigest = "90c6a51ec4caf54e00079779cf5af7560ec540900e4a82c7ba61aafbf476f2ce"
)

func TestLegacyCarrierManifestMatchesExactBaseCensus(t *testing.T) {
	manifest, err := LoadManifest(legacyManifestPath)
	if err != nil {
		t.Fatalf("load legacy carrier manifest: %v", err)
	}
	if manifest.BaseCommit != EpicBaseCommit {
		t.Fatalf("base commit = %q, want %q", manifest.BaseCommit, EpicBaseCommit)
	}
	if manifest.BaselineCount != frozenBaseCount || manifest.BaselineDigest != frozenBaseDigest {
		t.Fatalf("frozen base count/digest = %d/%s, want %d/%s", manifest.BaselineCount, manifest.BaselineDigest, frozenBaseCount, frozenBaseDigest)
	}
	assertFrozenManifestCounts(t, manifest)

	baseFS, available := gitBaseSnapshot(t)
	if !available {
		t.Log("exact base Git object unavailable; immutable manifest digest remains enforced")
		return
	}
	actual, err := scanFS(baseFS)
	if err != nil {
		t.Fatalf("scan exact base: %v", err)
	}
	if difference := CompareExact(&manifest, actual); !difference.Empty() {
		t.Fatalf("exact-base carrier census changed:\n%s", FormatDifference(difference))
	}
	if len(actual) != frozenBaseCount || Digest(actual) != frozenBaseDigest {
		t.Fatalf("exact-base scan count/digest = %d/%s", len(actual), Digest(actual))
	}
	assertKnownBaseCounts(t, actual)
}

func assertFrozenManifestCounts(t *testing.T, manifest Manifest) {
	t.Helper()
	categoryCounts := make(map[string]int, len(manifest.Categories))
	nativeFiles := make(map[string]struct{})
	for _, category := range manifest.Categories {
		categoryCounts[category.ID] = category.BaselineCount
		if category.ID == categoryNativePayloadBits {
			for _, finding := range category.Legacy {
				nativeFiles[finding.Path] = struct{}{}
			}
		}
	}
	if categoryCounts[categoryLLVMWordBridge] != 25 || categoryCounts[categoryNativePayloadBits] != 134 || len(nativeFiles) != 21 {
		t.Fatalf("frozen manifest census = llvm:%d native tokens/files:%d/%d", categoryCounts[categoryLLVMWordBridge], categoryCounts[categoryNativePayloadBits], len(nativeFiles))
	}
}

func TestLiveCarrierRatchetAgainstRepository(t *testing.T) {
	manifest, err := LoadManifest(legacyManifestPath)
	if err != nil {
		t.Fatalf("load legacy carrier manifest: %v", err)
	}
	actual, err := Scan(repositoryRoot(t))
	if err != nil {
		t.Fatalf("scan live repository: %v", err)
	}
	if difference := Compare(&manifest, actual); !difference.Empty() {
		t.Fatalf("live carrier ratchet failed:\n%s", FormatDifference(difference))
	}
}

func gitBaseSnapshot(t *testing.T) (fs.FS, bool) {
	t.Helper()
	root := repositoryRoot(t)
	object := EpicBaseCommit + "^{commit}"
	if err := exec.Command("git", "-C", root, "cat-file", "-e", object).Run(); err != nil { // #nosec G204 -- fixed repository and commit
		return nil, false
	}
	args := []string{"-C", root, "archive", "--format=tar", EpicBaseCommit, "--"}
	for _, scope := range requiredScopes {
		args = append(args, scope.Root)
	}
	archive, err := exec.Command("git", args...).Output() // #nosec G204 -- fixed repository, commit, and code-owned scopes
	if err != nil {
		t.Fatalf("archive exact base: %v", err)
	}
	return readArchiveFS(t, archive), true
}

func readArchiveFS(t *testing.T, archive []byte) fs.FS {
	t.Helper()
	const maxSourceSize = 16 << 20
	root := fstest.MapFS{}
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read exact-base archive: %v", err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		name := path.Clean(header.Name)
		if !fs.ValidPath(name) || name != header.Name {
			t.Fatalf("invalid exact-base archive path %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeReg:
			if header.Size < 0 || header.Size > maxSourceSize {
				t.Fatalf("invalid exact-base source size %d for %s", header.Size, name)
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("read exact-base source %s: %v", name, err)
			}
			root[name] = &fstest.MapFile{Data: data, Mode: 0o444}
		case tar.TypeSymlink:
			root[name] = &fstest.MapFile{Data: []byte(header.Linkname), Mode: fs.ModeSymlink | 0o777}
		}
	}
	return root
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func assertKnownBaseCounts(t *testing.T, findings []Finding) {
	t.Helper()
	llvmWords := 0
	nativeTokens := 0
	lines := make(map[string]struct{})
	files := make(map[string]struct{})
	for _, finding := range findings {
		switch finding.Category {
		case categoryLLVMWordBridge:
			llvmWords++
		case categoryNativePayloadBits:
			nativeTokens++
			lines[finding.Path+":"+strconv.Itoa(finding.Line)] = struct{}{}
			files[finding.Path] = struct{}{}
		}
	}
	// The earlier raw-regex census was 143/133/21. A lexical scan excludes its
	// nine comment/string matches and freezes the actual source carrier sites.
	if llvmWords != 25 || nativeTokens != 134 || len(lines) != 124 || len(files) != 21 {
		t.Fatalf("known census = llvm:%d native tokens/lines/files:%d/%d/%d", llvmWords, nativeTokens, len(lines), len(files))
	}
}
