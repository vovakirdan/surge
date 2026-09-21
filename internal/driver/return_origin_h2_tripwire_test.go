package driver

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"surge/internal/sema"
)

// RV2-DEBT-365 tripwire, compile rows. Return-origin does not model what a running task
// borrows, and the task check that owns it has a measured hole. Each program below leaks a
// task that borrows a dead frame and would run it; it is refused today by exactly one
// incidental rule. A row pins that rule's exact rows, so the change that removes the rule
// turns the row red before the task check has closed the hole.
//
// The three core Task rows may still be present (a tree below census 0); no other core row may.

type h2TripwireOwnRow struct {
	start, end      int
	snippet, reason string
}

// The frozen probe sources and digests are in return_origin_h2_tripwire_sources_test.go.

var h2TripwireTaskCoreRows = [][2]int{{9752, 9757}, {10046, 10056}, {10154, 10159}}

// h2TripwirePending diagnoses one frozen program through the public path and splits what
// keeps it unfinished into its own rows and core rows. built reports a finished analysis.
func h2TripwirePending(t *testing.T, text, digest string) (own, core []sema.ReturnOriginPending, built bool) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != digest {
		t.Fatalf("PRECONDITION: frozen probe changed: %s", got)
	}
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "origin.sg")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: dir, MaxDiagnostics: 64, IgnoreWarnings: true, KeepArtifacts: true}
	res, err := DiagnoseWithOptions(t.Context(), path, &opts)
	var unfinished *returnOriginUnfinishedError
	if err != nil && !errors.As(err, &unfinished) {
		t.Fatalf("PRECONDITION: the probe stopped before return origins: %v", err)
	}
	if res != nil && res.Bag != nil && res.Bag.HasErrors() {
		t.Fatalf("PRECONDITION: the probe is refused by another rule: %+v", res.Bag.Items())
	}
	if unfinished == nil {
		return nil, nil, true
	}
	for _, p := range unfinished.Pending {
		if strings.HasPrefix(p.SourceKey, "core/") {
			core = append(core, p)
		} else {
			own = append(own, p)
		}
	}
	t.Logf("H2_TRIPWIRE own=%+v core_rows=%d", own, len(core))
	return own, core, false
}

// h2TripwireExactOwn answers "" when own is exactly want, in any order.
func h2TripwireExactOwn(text string, own []sema.ReturnOriginPending, want []h2TripwireOwnRow) string {
	if len(own) != len(want) {
		return fmt.Sprintf("own rows = %+v, want exactly %d", own, len(want))
	}
	for _, row := range want {
		if row.end > len(text) || text[row.start:row.end] != row.snippet {
			return fmt.Sprintf("PRECONDITION: frozen span %d:%d is not %q", row.start, row.end, row.snippet)
		}
		if !slices.ContainsFunc(own, func(p sema.ReturnOriginPending) bool {
			return int(p.Span.Start) == row.start && int(p.Span.End) == row.end && p.Reason == row.reason
		}) {
			return fmt.Sprintf("lost %q at %d:%d %q: %+v", row.reason, row.start, row.end, row.snippet, own)
		}
	}
	return ""
}

// h2TripwireOnlyTaskCore answers "" when every core row is one of the three core Task rows.
func h2TripwireOnlyTaskCore(core []sema.ReturnOriginPending) string {
	for _, p := range core {
		if p.SourceKey != "core/intrinsics.sg" || !slices.Contains(h2TripwireTaskCoreRows, [2]int{int(p.Span.Start), int(p.Span.End)}) {
			return fmt.Sprintf("a core row other than the Task rows keeps the probe refused: %+v", p)
		}
	}
	return ""
}

// h2TripwireImportDifferential answers "" while an explicit core import still adds core rows:
// the importing program has no row of its own, and it keeps core rows its import-free twin lacks.
func h2TripwireImportDifferential(own, core, twinCore []sema.ReturnOriginPending) (extra []sema.ReturnOriginPending, verdict string) {
	if len(own) != 0 {
		return nil, fmt.Sprintf("the importing program has rows of its own: %+v", own)
	}
	for _, p := range core {
		if !slices.Contains(twinCore, p) {
			extra = append(extra, p)
		}
	}
	if len(extra) == 0 {
		return nil, "the explicit core import adds no core row: the importing program builds"
	}
	return extra, ""
}

func checkH2TripwireRefused(t *testing.T, text, digest string, want []h2TripwireOwnRow, taskCoreOnly bool) {
	t.Helper()
	own, core, built := h2TripwirePending(t, text, digest)
	if built {
		t.Fatal("the probe builds: its barrier is gone (RV2-DEBT-365)")
	}
	if verdict := h2TripwireExactOwn(text, own, want); verdict != "" {
		t.Error(verdict)
	}
	if taskCoreOnly {
		if verdict := h2TripwireOnlyTaskCore(core); verdict != "" {
			t.Error(verdict)
		}
	}
}

const h2TripwireG1 = "generic original call argument disagrees with its substituted source signature"

// RV2-DEBT-365: the G1 row on a non-own `.await()` receiver (removed by W4-G1).
func TestH2TripwireRefusedG0Await(t *testing.T) {
	checkH2TripwireRefused(t, h2TripwireG0Await, h2TripwireG0AwaitDigest,
		[]h2TripwireOwnRow{{232, 241, "t.await()", h2TripwireG1}}, true)
}

// RV2-DEBT-365: the same G1 row, with the result carried in the exit code.
func TestH2TripwireRefusedG0dAwaitDisc(t *testing.T) {
	checkH2TripwireRefused(t, h2TripwireG0dAwaitDisc, h2TripwireG0dAwaitDiscDigest,
		[]h2TripwireOwnRow{{232, 241, "t.await()", h2TripwireG1}}, true)
}

// RV2-DEBT-365: a module-qualified core `timeout` has no callable identity.
func TestH2TripwireRefusedG5ModuleTimeout(t *testing.T) {
	checkH2TripwireRefused(t, h2TripwireG5ModuleTimeout, h2TripwireG5ModuleTimeoutDigest, []h2TripwireOwnRow{
		{243, 267, "ci.timeout(leak(), 1000)", "generic use lacks its exact original callable declarations"},
		{243, 267, "ci.timeout(leak(), 1000)", "selected callable lacks its published callable authority"},
	}, false)
}

// RV2-DEBT-365: an explicit `import core/intrinsics` re-adds core rows, so an aliased
// `timeout` called from a sync main does not build. The twin is the same leak without the import.
func TestH2TripwireImportAddsCoreRowsG5b(t *testing.T) {
	twinOwn, twinCore, _ := h2TripwirePending(t, h2TripwireG4dTwin, h2TripwireG4dTwinDigest)
	if len(twinOwn) != 0 {
		t.Fatalf("PRECONDITION: the import-free twin has rows of its own: %+v", twinOwn)
	}
	if verdict := h2TripwireOnlyTaskCore(twinCore); verdict != "" {
		t.Fatalf("PRECONDITION: %s", verdict)
	}
	own, core, built := h2TripwirePending(t, h2TripwireG5bAliasTimeout, h2TripwireG5bAliasTimeoutDigest)
	if built {
		t.Fatal("the importing program builds: its barrier is gone (RV2-DEBT-365)")
	}
	extra, verdict := h2TripwireImportDifferential(own, core, twinCore)
	if verdict != "" {
		t.Fatal(verdict)
	}
	units := make(map[string]int)
	for _, p := range extra {
		units[p.SourceKey]++
	}
	t.Logf("H2_TRIPWIRE import-induced core rows=%d by unit=%v", len(extra), units)
}

// The differential must refuse an importing program that has become its own twin.
func TestH2TripwireImportDifferentialRejectsTheTwin(t *testing.T) {
	task := sema.ReturnOriginPending{SourceKey: "core/intrinsics.sg", Reason: genericConditionUnsupported}
	added := sema.ReturnOriginPending{SourceKey: "core/string.sg", Reason: genericConditionUnsupported}
	for _, tc := range []struct {
		name            string
		own, core, twin []sema.ReturnOriginPending
		accepted        bool
	}{
		{"import_adds_rows", nil, []sema.ReturnOriginPending{task, added}, []sema.ReturnOriginPending{task}, true},
		{"same_as_twin", nil, []sema.ReturnOriginPending{task}, []sema.ReturnOriginPending{task}, false},
		{"builds", nil, nil, nil, false},
		{"own_row", []sema.ReturnOriginPending{{SourceKey: "origin.sg"}}, []sema.ReturnOriginPending{task, added}, []sema.ReturnOriginPending{task}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, verdict := h2TripwireImportDifferential(tc.own, tc.core, tc.twin); (verdict == "") != tc.accepted {
				t.Errorf("verdict %q, accepted want %v", verdict, tc.accepted)
			}
		})
	}
}
