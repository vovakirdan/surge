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
	"strings"
	"testing"
	"testing/fstest"
)

const (
	frozenBaseCount  = 683
	frozenBaseDigest = "db5a0f475c32c2155aa82f3606800da0668392bd2e7a7aee917b742e76e58ee9"
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
		t.Fatalf("exact-base carrier census changed:\n%s", FormatDifference(&difference))
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
	if categoryCounts[categoryLLVMWordBridge] != 25 || categoryCounts[categoryNativePayloadBits] != 134 ||
		categoryCounts[categoryNativeWord] != 85 || len(nativeFiles) != 21 {
		t.Fatalf("frozen manifest census = llvm:%d native payload/word/files:%d/%d/%d",
			categoryCounts[categoryLLVMWordBridge], categoryCounts[categoryNativePayloadBits],
			categoryCounts[categoryNativeWord], len(nativeFiles))
	}
	// A category whose frozen baseline is empty is a category that ratchets
	// nothing: it would be green at zero from the day it was added, and the
	// carriers it was written to see would never have been counted at all.
	if categoryCounts[categoryFrameOwner] != 7 || categoryCounts[categoryUntypedCaptureState] != 15 {
		t.Fatalf("frozen manifest census = frame owner/untyped capture state:%d/%d, want 7/15",
			categoryCounts[categoryFrameOwner], categoryCounts[categoryUntypedCaptureState])
	}
	if categoryCounts[categoryAsyncAny] != 35 || categoryCounts[categoryVMUniversalOwner] != 58 {
		t.Fatalf("frozen manifest async/VM owner census = %d/%d, want 35/58",
			categoryCounts[categoryAsyncAny], categoryCounts[categoryVMUniversalOwner])
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
	difference := Compare(&manifest, actual)
	if !difference.Empty() {
		t.Fatalf("live carrier ratchet failed:\n%s", FormatDifference(&difference))
	}
	// The tracked migration carriers still present are a number the ratchet
	// only reported until now; a wave that grows or shrinks it should have to
	// see it here and re-pin it (Rule 15). Every one of them is an
	// RV2-DEBT-318 path to the VM's universal `Value` owner through a
	// post-base storage or control graph; the number reads zero only when
	// that owner is gone, at which point the rows leave the manifest too.
	if difference.MigrationTracked != migrationCarriersStillPresent {
		t.Fatalf("migration carriers still present = %d, want %d (re-pin only with the census that moved it)",
			difference.MigrationTracked, migrationCarriersStillPresent)
	}
	// Owner ruling 2026-09-03 (variant (а)): Wave F exits at zero live legacy
	// findings in every category a wave owned; what stays live is the VM's
	// `Value` interchange type and the async runtime's `any`, counted here
	// and owned by a VM-representation epic of its own. A category outside
	// this map that reads non-zero is a wave's category coming back.
	legacy := make(map[findingKey]string)
	for i := range manifest.Categories {
		category := &manifest.Categories[i]
		for j := range category.Legacy {
			legacy[keyFor(&category.Legacy[j])] = category.ID
		}
	}
	live := make(map[string]int)
	for i := range actual {
		if id, ok := legacy[keyFor(&actual[i])]; ok {
			live[id]++
		}
	}
	for i := range manifest.Categories {
		id := manifest.Categories[i].ID
		if got, want := live[id], liveLegacyByCategory[id]; got != want {
			t.Fatalf("live legacy findings in %s = %d, want %d (re-pin only with the census that moved it)",
				id, got, want)
		}
	}
}

// liveLegacyByCategory is the live legacy census the closeout exits on,
// pinned 2026-09-03 after F6: the two VM-representation categories and the
// permanent fixnum allow's own row, every other category zero.
var liveLegacyByCategory = map[string]int{
	"vm-universal-owner":   40,
	"vm-async-any-carrier": 14,
	"llvm-pointer-word-ir": 1,
}

// migrationCarriersStillPresent is the live count of manifest migration
// identities the scan still finds: the 27 RV2-DEBT-318 paths, all resolving to
// `Frame.Locals -> LocalSlot.V -> Value`. Pinned 2026-09-03.
const migrationCarriersStillPresent = 27

// TestLiveCarrierRatchetSeesAMigrationCarrierMove is the negative control for
// the pin above: a tracked identity appearing once more, or one leaving, is a
// different number, never an absorbed change.
func TestLiveCarrierRatchetSeesAMigrationCarrierMove(t *testing.T) {
	manifest, err := LoadManifest(legacyManifestPath)
	if err != nil {
		t.Fatalf("load legacy carrier manifest: %v", err)
	}
	actual, err := Scan(repositoryRoot(t))
	if err != nil {
		t.Fatalf("scan live repository: %v", err)
	}
	var tracked *Finding
	for i := range manifest.Categories {
		category := &manifest.Categories[i]
		for j := range category.Migration {
			key := keyFor(&category.Migration[j].Finding)
			for k := range actual {
				if keyFor(&actual[k]) == key {
					tracked = &actual[k]
					break
				}
			}
			if tracked != nil {
				break
			}
		}
		if tracked != nil {
			break
		}
	}
	if tracked == nil {
		t.Fatal("no tracked migration carrier is present in the live scan; the pin above must read zero")
	}
	fewer := make([]Finding, 0, len(actual)-1)
	for i := range actual {
		if keyFor(&actual[i]) != keyFor(tracked) {
			fewer = append(fewer, actual[i])
		}
	}
	if got := Compare(&manifest, fewer).MigrationTracked; got != migrationCarriersStillPresent-1 {
		t.Fatalf("one tracked carrier removed reads %d, want %d", got, migrationCarriersStillPresent-1)
	}
	more := append(append([]Finding(nil), actual...), *tracked)
	if got := Compare(&manifest, more).MigrationTracked; got != migrationCarriersStillPresent+1 {
		t.Fatalf("one tracked carrier appearing once more reads %d, want %d", got, migrationCarriersStillPresent+1)
	}
	if got := Compare(&manifest, actual).MigrationTracked; got != migrationCarriersStillPresent {
		t.Fatalf("live scan reads %d tracked carriers, want %d", got, migrationCarriersStillPresent)
	}
}

func TestExactBaseScanRejectsArchivedSymlink(t *testing.T) {
	archiveFS := fstest.MapFS{}
	for _, scope := range requiredScopes {
		archiveFS[scope.Root] = &fstest.MapFile{Mode: fs.ModeDir | 0o755}
	}
	const linkPath = "runtime/native/linked.c"
	archiveFS[linkPath] = &fstest.MapFile{
		Data: []byte("../outside.c"),
		Mode: fs.ModeSymlink | 0o777,
	}
	_, err := scanFS(archiveFS)
	if err == nil || !strings.Contains(err.Error(), "symlink") ||
		!strings.Contains(err.Error(), linkPath) {
		t.Fatalf("archived symlink error = %v, want actionable path", err)
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
	nativeWords := 0
	frameOwners := 0
	captureStates := 0
	structuralAsyncOwners := 0
	structuralVMOwners := 0
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
		case categoryNativeWord:
			nativeWords++
		case categoryFrameOwner:
			frameOwners++
		case categoryUntypedCaptureState:
			captureStates++
		}
		if strings.HasPrefix(finding.Evidence, "structural owner field ") {
			switch finding.Category {
			case categoryAsyncAny:
				structuralAsyncOwners++
			case categoryVMUniversalOwner:
				structuralVMOwners++
			}
		}
	}
	// The earlier raw-regex census was 143/133/21. A lexical scan excludes its
	// nine comment/string matches and freezes the actual source carrier sites.
	// Context/dataflow matching adds 16 native word aliases to the earlier 69.
	if llvmWords != 25 || nativeTokens != 134 || nativeWords != 85 || len(lines) != 124 || len(files) != 21 {
		t.Fatalf("known census = llvm:%d native payload/word/lines/files:%d/%d/%d/%d",
			llvmWords, nativeTokens, nativeWords, len(lines), len(files))
	}
	// Asserted against the SCAN, not against the manifest, because the manifest
	// was written from this scan: a token that stopped matching would leave the
	// two agreeing at zero, and a ratchet standing at zero over a live carrier
	// is the failure this whole gate was built to refuse.
	if frameOwners != 7 || captureStates != 15 {
		t.Fatalf("known census = frame owner/untyped capture state:%d/%d, want 7/15",
			frameOwners, captureStates)
	}
	if structuralAsyncOwners != 12 || structuralVMOwners != 45 {
		t.Fatalf("known structural async/VM owners = %d/%d, want 12/45",
			structuralAsyncOwners, structuralVMOwners)
	}
}
