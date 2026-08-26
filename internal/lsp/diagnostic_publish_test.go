package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/driver"
	"surge/internal/driver/diagnose"
	"surge/internal/parser"
)

// diagnoseForPublish runs the same workspace analysis the server runs and hands
// back the simplified diagnostics the publish path consumes.
func diagnoseForPublish(t *testing.T, content string) ([]diagnose.Diagnostic, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	opts := diagnose.DiagnoseOptions{
		ProjectRoot:    path,
		BaseDir:        dir,
		Stage:          driver.DiagnoseStageAll,
		MaxDiagnostics: 20,
		DirectiveMode:  parser.DirectiveModeOff,
	}
	_, diags, err := diagnose.AnalyzeWorkspace(context.Background(), &opts, diagnose.FileOverlay{})
	if err != nil {
		t.Fatalf("analyze workspace: %v", err)
	}
	return diags, pathToURI(path)
}

const publishProbeSource = `
type Widget = { name: string }

async fn make_widget() -> Widget {
    return Widget { name = "w" };
}

@entrypoint
fn main() {
    let first = spawn make_widget();
    let second = first.clone();
    let a = first.await();
    let b = second.await();
}
`

func publishPlanFor(uri string) analysisPlan {
	return analysisPlan{
		seq:  7,
		docs: map[string]docState{uri: {version: 42, snapshotID: 9}},
	}
}

// TestPublishedDiagnosticsCarryRelatedInformation is the parity proof for the
// language server.
//
// The compiler assembles notes and help for every diagnostic; the server used
// to publish the message and the range and drop both, which made the editor
// the least informative of the three user-facing paths.
func TestPublishedDiagnosticsCarryRelatedInformation(t *testing.T) {
	diags, uri := diagnoseForPublish(t, publishProbeSource)
	grouped := buildPublishedDiagnostics(publishPlanFor(uri), diags)
	published := grouped[uri]
	if len(published) == 0 {
		t.Fatalf("no diagnostics were published for %s", uri)
	}
	var subject *lspDiagnostic
	for i := range published {
		if published[i].Code == "SEM3116" {
			subject = &published[i]
		}
	}
	if subject == nil {
		t.Fatalf("the clone refusal was not published: %+v", published)
	}
	if len(subject.RelatedInformation) < 2 {
		t.Fatalf("relatedInformation = %+v, want the note and the help", subject.RelatedInformation)
	}
	joined := ""
	for _, related := range subject.RelatedInformation {
		if related.Location.URI == "" {
			t.Fatalf("a related entry has no location: %+v", related)
		}
		joined += related.Message + "\n"
	}
	if !strings.Contains(joined, "no `__clone` declaration claims this type") {
		t.Fatalf("the explanation channel did not reach the client:\n%s", joined)
	}
	if !strings.Contains(joined, "consume this one by awaiting it") {
		t.Fatalf("the actionable channel did not reach the client:\n%s", joined)
	}
}

// TestPublishedDiagnosticDataBindsToADocumentVersion pins the identity a later
// Code Action is checked against.
//
// Nothing in it is an edit and nothing in it is trusted on the way back. It
// says which analysis, which document, which version, and which fixes existed;
// the snapshot and the guards stay on the server, so a replayed value can only
// fail to match.
func TestPublishedDiagnosticDataBindsToADocumentVersion(t *testing.T) {
	diags, uri := diagnoseForPublish(t, publishProbeSource)
	grouped := buildPublishedDiagnostics(publishPlanFor(uri), diags)
	for _, entry := range grouped[uri] {
		if entry.Data == nil {
			t.Fatalf("a published diagnostic carries no data: %+v", entry)
		}
		if entry.Data.URI != uri {
			t.Fatalf("data names %q, want the canonical %q", entry.Data.URI, uri)
		}
		if entry.Data.Version != 42 || entry.Data.SnapshotID != 9 || entry.Data.AnalysisID != 7 {
			t.Fatalf("data does not bind to this analysis: %+v", entry.Data)
		}
		if entry.Data.DiagnosticID == "" {
			t.Fatalf("a published diagnostic has no id: %+v", entry.Data)
		}
		// Whatever a client sends back names ids, never edits.
		encoded, err := json.Marshal(entry.Data)
		if err != nil {
			t.Fatalf("marshal data: %v", err)
		}
		for _, forbidden := range []string{"newText", "oldText", "edits", "range"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("Diagnostic.data leaked %q to the client: %s", forbidden, encoded)
			}
		}
	}
}

// TestClonableRefusalOffersNoFixIDs keeps the contract's "no automatic fix"
// visible at the protocol boundary: an editor cannot offer an action the
// compiler refused to attach.
func TestClonableRefusalOffersNoFixIDs(t *testing.T) {
	diags, uri := diagnoseForPublish(t, publishProbeSource)
	grouped := buildPublishedDiagnostics(publishPlanFor(uri), diags)
	for _, entry := range grouped[uri] {
		if entry.Code != "SEM3116" || entry.Data == nil {
			continue
		}
		if len(entry.Data.FixIDs) != 0 || len(entry.Data.SafeFixIDs) != 0 {
			t.Fatalf("the clone refusal published fix ids: %+v", entry.Data)
		}
	}
}

// TestSafeFixIDsAreASubsetOfFixIDs is what makes the AlwaysSafe-only rule
// checkable at the boundary rather than only inside the fix engine.
func TestSafeFixIDsAreASubsetOfFixIDs(t *testing.T) {
	diags, uri := diagnoseForPublish(t, `
type Inner = { name: string }
type Outer = { inner: Inner }

fn consume(value: Inner) -> int { return 1; }

@entrypoint
fn main() {
    let outer = Outer { inner = Inner { name = "n" } };
    let taken = consume(outer.inner);
}
`)
	grouped := buildPublishedDiagnostics(publishPlanFor(uri), diags)
	sawSafe := false
	for _, entry := range grouped[uri] {
		if entry.Data == nil {
			continue
		}
		all := make(map[string]struct{}, len(entry.Data.FixIDs))
		for _, id := range entry.Data.FixIDs {
			all[id] = struct{}{}
		}
		for _, id := range entry.Data.SafeFixIDs {
			if _, ok := all[id]; !ok {
				t.Fatalf("safe fix %q is not among the published fixes %v", id, entry.Data.FixIDs)
			}
			sawSafe = true
		}
	}
	if !sawSafe {
		t.Fatal("the always-safe partial-move edit did not reach the protocol boundary")
	}
}

// TestPublishParamsStateTheDocumentVersion proves the version reaches the wire,
// not just the struct.
func TestPublishParamsStateTheDocumentVersion(t *testing.T) {
	version := 42
	encoded, err := json.Marshal(publishDiagnosticsParams{
		URI: "file:///probe.sg", Version: &version, Diagnostics: []lspDiagnostic{},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if !strings.Contains(string(encoded), `"version":42`) {
		t.Fatalf("publishDiagnostics omitted the version: %s", encoded)
	}
}
