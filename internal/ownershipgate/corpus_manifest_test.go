//go:build runtime_v2_ownership_corpus

package ownershipgate_test

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"surge/internal/buildpipeline"
	"surge/internal/project"
)

const (
	corpusCensusReportVersion = 2
	corpusRepositoryRoot      = "repository_root"
	corpusRootKindUnknown     = "unknown"
	corpusInventoryDigestV1   = "sha256:path-lines-v1"
)

type corpusRootSpec struct {
	Path        string `json:"path"`
	PinnedCount int    `json:"pinned_count"`
}

// The counts are a tripwire, not a target: they force someone to look at what
// was added or removed before the corpus silently changes size. The golden
// count was raised from 966 to 1052 on 2026-08-28 after the run accounted for
// every one of the 1052 paths -- no unrecorded failure, no stale allowance and
// no untriaged finding -- so the growth is fixtures added since the pin was
// last set rather than coverage drifting away.
//
// Raised 1052 -> 1055 on 2026-09-02. The tripwire did its job: it fired on the
// dedicated machine after three fixtures landed, and the three are accounted
// for by name rather than by arithmetic --
// sema/invalid/task_borrow_mutated_while_child_runs,
// sema/invalid/task_borrow_join_on_one_branch_only and
// sema/valid/task_borrow_join_on_every_branch, the positive, the may-be-live
// negative and the control for the carrier-affine borrow pin. Nothing was
// removed, and the run reports normalized_findings=0 beside the new count.
//
// Raised 1055 -> 1057 on 2026-09-02, later the same day. The two are the
// SEM3209 port's fixtures (8882ff00): sema/invalid/concurrency/
// task_created_outside_scope, the refusal of a task created outside the
// scope that would count it, and sema/valid/concurrency/
// task_created_in_current_scope, its accepted twin. The port landed without
// this line, so the tripwire read 1057 against 1055 on every aggregate count
// since; the run beside the new count again reports normalized_findings=0.
//
// Raised 1057 -> 1058 on 2026-09-03. The one is E3's fixture (ab8be7c1):
// crossing/block02/invalid/on_negative_anchor_lease_misuse, the refusal of a
// body that treats its `on far_handle` anchor as anything but a lease
// (SEM3210). E3 landed without this line, so the tripwire read 1058 against
// 1057 on every aggregate count from that commit on -- the runner's W8 on
// ba7f13e4 is where it was finally read, four SHAs later. Nothing removed.
var corpusRoots = []corpusRootSpec{
	{Path: "testdata/golden", PinnedCount: 1058},
	{Path: "showcases", PinnedCount: 38},
	{Path: "core", PinnedCount: 10},
	{Path: "stdlib", PinnedCount: 32},
}

// corpusCompileProfile is both the report payload and the source of every
// invariant CompileRequest field. Per-fixture TargetPath is represented by the
// corpus inventory rather than repeated in this profile.
type corpusCompileProfile struct {
	Analysis                  bool                  `json:"analysis"`
	Backend                   buildpipeline.Backend `json:"backend"`
	MaxDiagnostics            int                   `json:"max_diagnostics"`
	AllowDiagnosticsError     bool                  `json:"allow_diagnostics_error"`
	RootKind                  string                `json:"root_kind"`
	DirInfoEnabled            bool                  `json:"dir_info_enabled"`
	CrossingFormsOverride     bool                  `json:"crossing_forms_override"`
	BaseDirPolicy             string                `json:"base_dir_policy"`
	StandardLibraryPathPolicy string                `json:"standard_library_path_policy"`
}

var ownershipCorpusCompileProfile = corpusCompileProfile{
	Analysis:                  true,
	Backend:                   buildpipeline.BackendLLVM,
	MaxDiagnostics:            500,
	AllowDiagnosticsError:     false,
	RootKind:                  corpusRootKindUnknown,
	DirInfoEnabled:            false,
	CrossingFormsOverride:     false,
	BaseDirPolicy:             corpusRepositoryRoot,
	StandardLibraryPathPolicy: corpusRepositoryRoot,
}

func (p corpusCompileProfile) request(root, fixture string) (*buildpipeline.CompileRequest, error) {
	if p.RootKind != corpusRootKindUnknown {
		return nil, fmt.Errorf("unsupported ownership corpus root kind %q", p.RootKind)
	}
	if p.BaseDirPolicy != corpusRepositoryRoot {
		return nil, fmt.Errorf("unsupported ownership corpus base-dir policy %q", p.BaseDirPolicy)
	}
	if p.StandardLibraryPathPolicy != corpusRepositoryRoot {
		return nil, fmt.Errorf("unsupported ownership corpus stdlib policy %q", p.StandardLibraryPathPolicy)
	}
	if p.DirInfoEnabled {
		return nil, fmt.Errorf("ownership corpus profile cannot enable directory targets")
	}
	if p.CrossingFormsOverride {
		return nil, fmt.Errorf("ownership corpus profile cannot override crossing forms")
	}
	return &buildpipeline.CompileRequest{
		TargetPath:            fixture,
		BaseDir:               root,
		RootKind:              project.ModuleKindUnknown,
		MaxDiagnostics:        p.MaxDiagnostics,
		AllowDiagnosticsError: p.AllowDiagnosticsError,
		Analysis:              p.Analysis,
		Backend:               p.Backend,
	}, nil
}

func corpusCompileRequest(root, fixture string) *buildpipeline.CompileRequest {
	request, err := ownershipCorpusCompileProfile.request(root, fixture)
	if err != nil {
		panic(fmt.Sprintf("invalid static ownership corpus compile profile: %v", err))
	}
	return request
}

func applyCorpusCompileEnvironment(t *testing.T, root string) {
	t.Helper()
	if ownershipCorpusCompileProfile.StandardLibraryPathPolicy != corpusRepositoryRoot {
		t.Fatalf("unsupported ownership corpus stdlib policy %q",
			ownershipCorpusCompileProfile.StandardLibraryPathPolicy)
	}
	t.Setenv("SURGE_STDLIB", root)
}

type corpusInventory struct {
	Roots           []corpusRootSpec `json:"roots"`
	PathCount       int              `json:"path_count"`
	DigestAlgorithm string           `json:"digest_algorithm"`
	PathDigest      string           `json:"path_digest"`
}

func snapshotCorpusInventory(root string, fixtures []string) (corpusInventory, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return corpusInventory{}, fmt.Errorf("resolve ownership corpus root: %w", err)
	}
	paths := make([]string, 0, len(fixtures))
	for _, fixture := range fixtures {
		absFixture, absErr := filepath.Abs(fixture)
		if absErr != nil {
			return corpusInventory{}, fmt.Errorf("resolve ownership fixture %q: %w", fixture, absErr)
		}
		rel, relErr := filepath.Rel(absRoot, absFixture)
		if relErr != nil {
			return corpusInventory{}, fmt.Errorf("relativize ownership fixture %q: %w", fixture, relErr)
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return corpusInventory{}, fmt.Errorf("ownership fixture %q is outside repository root %q", fixture, root)
		}
		if filepath.Ext(rel) != ".sg" {
			return corpusInventory{}, fmt.Errorf("ownership fixture %q is not a .sg path", fixture)
		}
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	var canonical strings.Builder
	for _, path := range paths {
		canonical.WriteString(path)
		canonical.WriteByte('\n')
	}
	digest := sha256.Sum256([]byte(canonical.String()))
	roots := append([]corpusRootSpec(nil), corpusRoots...)
	sort.Slice(roots, func(i, j int) bool { return roots[i].Path < roots[j].Path })
	return corpusInventory{
		Roots:           roots,
		PathCount:       len(paths),
		DigestAlgorithm: corpusInventoryDigestV1,
		PathDigest:      fmt.Sprintf("%x", digest),
	}, nil
}

func TestOwnershipCorpusCompileProfileContract(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "repo")
	fixture := filepath.Join(root, "showcases", "probe.sg")
	request, err := ownershipCorpusCompileProfile.request(root, fixture)
	if err != nil {
		t.Fatalf("build request from canonical profile: %v", err)
	}
	if request.TargetPath != fixture || request.BaseDir != root {
		t.Fatalf("request paths = target %q base %q", request.TargetPath, request.BaseDir)
	}
	if !request.Analysis || request.Backend != buildpipeline.BackendLLVM || request.MaxDiagnostics != 500 ||
		request.AllowDiagnosticsError || request.RootKind != project.ModuleKindUnknown {
		t.Fatalf("request differs from canonical profile: %+v", request)
	}
	if request.DirInfo != nil || request.CrossingFormsForTest != nil || request.Progress != nil || request.Files != nil {
		t.Fatalf("request enabled an unreported optional surface: %+v", request)
	}

	invalid := ownershipCorpusCompileProfile
	invalid.CrossingFormsOverride = true
	if _, err := invalid.request(root, fixture); err == nil || !strings.Contains(err.Error(), "cannot override crossing forms") {
		t.Fatalf("invalid profile error = %v", err)
	}
}

func TestOwnershipCorpusInventoryDigestContract(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.sg")
	b := filepath.Join(root, "b.sg")
	first, err := snapshotCorpusInventory(root, []string{b, a})
	if err != nil {
		t.Fatalf("snapshot inventory: %v", err)
	}
	second, err := snapshotCorpusInventory(root, []string{a, b})
	if err != nil {
		t.Fatalf("snapshot reversed inventory: %v", err)
	}
	const wantDigest = "023f60a39acab4d070001332d8aae3898e54798b73f8bf7bc1412b65472e3140"
	if first.PathDigest != wantDigest || second.PathDigest != wantDigest {
		t.Fatalf("path digest = %q / %q, want %q", first.PathDigest, second.PathDigest, wantDigest)
	}
	if first.PathCount != 2 || first.DigestAlgorithm != corpusInventoryDigestV1 || len(first.Roots) != len(corpusRoots) {
		t.Fatalf("inventory metadata = %+v", first)
	}

	changed, err := snapshotCorpusInventory(root, []string{a, filepath.Join(root, "c.sg")})
	if err != nil {
		t.Fatalf("snapshot changed inventory: %v", err)
	}
	if changed.PathDigest == first.PathDigest {
		t.Fatal("one-for-one fixture substitution did not change path digest")
	}
	if _, err := snapshotCorpusInventory(root, []string{filepath.Join(root, "..", "outside.sg")}); err == nil ||
		!strings.Contains(err.Error(), "outside repository root") {
		t.Fatalf("outside fixture error = %v", err)
	}
	if _, err := snapshotCorpusInventory(root, []string{filepath.Join(root, "not-source.txt")}); err == nil ||
		!strings.Contains(err.Error(), "not a .sg path") {
		t.Fatalf("non-source fixture error = %v", err)
	}
}
