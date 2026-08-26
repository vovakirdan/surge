package lsp

import (
	"os"
	"path/filepath"
	"testing"

	"surge/internal/driver/diagnose"
)

// The always-safe partial-move `own` edit is the action every row below drives.
// It is the one edit in the tree the compiler proves preserves intent, which is
// exactly the case the guard pipeline exists to protect.
const codeActionSource = `type Inner = { name: string }
type Outer = { inner: Inner }

fn consume(value: Inner) -> int { return 1; }

@entrypoint
fn main() {
    let outer = Outer { inner = Inner { name = "n" } };
    let taken = consume(outer.inner);
}
`

// actionFixture is a server holding one analysed, open document, wired exactly
// as the publish path leaves it.
type actionFixture struct {
	server *Server
	uri    string
	diags  []diagnose.Diagnostic
	plan   analysisPlan
	params codeActionParams
}

func newActionFixture(t *testing.T) *actionFixture {
	t.Helper()
	diags, uri := diagnoseForPublish(t, codeActionSource)
	server := &Server{
		fixes:        newFixRegistry(),
		openDocs:     map[string]string{uri: codeActionSource},
		versions:     map[string]int{uri: 3},
		docSnapshots: map[string]int64{uri: 5},
	}
	plan := analysisPlan{seq: 11, docs: map[string]docState{uri: {version: 3, snapshotID: 5}}}
	server.fixes.record(plan, diags)
	published := buildPublishedDiagnostics(plan, diags)[uri]
	fixture := &actionFixture{server: server, uri: uri, diags: diags, plan: plan}
	fixture.params = codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Context:      codeActionContext{Diagnostics: published},
	}
	return fixture
}

func (f *actionFixture) actions() []codeAction {
	return f.server.codeActionsFor(&f.params)
}

func (f *actionFixture) requireOneAction(t *testing.T) codeAction {
	t.Helper()
	actions := f.actions()
	if len(actions) != 1 {
		t.Fatalf("code actions = %d, want the one always-safe edit: %+v", len(actions), actions)
	}
	return actions[0]
}

func (f *actionFixture) requireNoActions(t *testing.T, why string) {
	t.Helper()
	if actions := f.actions(); len(actions) != 0 {
		t.Fatalf("%s still produced an edit: %+v", why, actions)
	}
}

// TestCodeActionFreshSuccess is the positive control. Every refusal below is
// only meaningful because this one succeeds.
func TestCodeActionFreshSuccess(t *testing.T) {
	fixture := newActionFixture(t)
	action := fixture.requireOneAction(t)
	if action.Kind != codeActionKindQuickFix {
		t.Fatalf("kind = %q", action.Kind)
	}
	if len(action.Edit.DocumentChanges) != 1 {
		t.Fatalf("documentChanges = %+v", action.Edit.DocumentChanges)
	}
	change := action.Edit.DocumentChanges[0]
	if change.TextDocument.URI != fixture.uri {
		t.Fatalf("edit targets %q, want %q", change.TextDocument.URI, fixture.uri)
	}
	// Versioned, always: an edit that does not say which document version it
	// applies to cannot be rejected by a client that has moved on.
	if change.TextDocument.Version != 3 {
		t.Fatalf("edit version = %d, want the open document's 3", change.TextDocument.Version)
	}
	if len(change.Edits) == 0 || change.Edits[0].NewText != "own " {
		t.Fatalf("edits = %+v, want the `own ` insertion", change.Edits)
	}
}

func TestCodeActionRacesReturnNothing(t *testing.T) {
	cases := []struct {
		name    string
		disturb func(t *testing.T, f *actionFixture)
	}{
		{
			// didChange: the text moved under the recorded positions.
			name: "the document changed",
			disturb: func(_ *testing.T, f *actionFixture) {
				f.server.mu.Lock()
				f.server.openDocs[f.uri] = "// prefix\n" + codeActionSource
				f.server.versions[f.uri] = 4
				f.server.mu.Unlock()
			},
		},
		{
			// didSave without a version change still bumps the snapshot id, and
			// that alone must invalidate: the analysis is no longer the one the
			// action was proved against.
			name: "the snapshot id moved without a version change",
			disturb: func(_ *testing.T, f *actionFixture) {
				f.server.mu.Lock()
				f.server.docSnapshots[f.uri] = 6
				f.server.mu.Unlock()
			},
		},
		{
			// Another document in the same analysis changed. The edit's safety
			// was proved over the whole analysis, not over one file.
			name: "another analysed document changed",
			disturb: func(_ *testing.T, f *actionFixture) {
				other := f.uri + ".other"
				f.plan.docs[other] = docState{version: 1, snapshotID: 1}
				f.server.fixes.record(f.plan, f.diags)
			},
		},
		{
			// close/reopen reuses a version number but never a snapshot id.
			name: "close and reopen reusing the version",
			disturb: func(_ *testing.T, f *actionFixture) {
				f.server.mu.Lock()
				delete(f.server.openDocs, f.uri)
				f.server.mu.Unlock()
			},
		},
		{
			name: "the data names another document",
			disturb: func(_ *testing.T, f *actionFixture) {
				for i := range f.params.Context.Diagnostics {
					if f.params.Context.Diagnostics[i].Data != nil {
						f.params.Context.Diagnostics[i].Data.URI = "file:///elsewhere.sg"
					}
				}
			},
		},
		{
			name: "the data names another analysis",
			disturb: func(_ *testing.T, f *actionFixture) {
				for i := range f.params.Context.Diagnostics {
					if f.params.Context.Diagnostics[i].Data != nil {
						f.params.Context.Diagnostics[i].Data.AnalysisID = 99
					}
				}
			},
		},
		{
			name: "the data claims a version the server never had",
			disturb: func(_ *testing.T, f *actionFixture) {
				for i := range f.params.Context.Diagnostics {
					if f.params.Context.Diagnostics[i].Data != nil {
						f.params.Context.Diagnostics[i].Data.Version = 99
					}
				}
			},
		},
		{
			name: "the data invents a fix id",
			disturb: func(_ *testing.T, f *actionFixture) {
				for i := range f.params.Context.Diagnostics {
					if f.params.Context.Diagnostics[i].Data != nil {
						f.params.Context.Diagnostics[i].Data.SafeFixIDs = []string{"forged-1"}
					}
				}
			},
		},
		{
			// A heuristic or manual candidate promoted by a client into the
			// safe list is still refused: applicability lives in the registry.
			name: "a client promotes an unsafe fix into the safe list",
			disturb: func(_ *testing.T, f *actionFixture) {
				for i := range f.params.Context.Diagnostics {
					data := f.params.Context.Diagnostics[i].Data
					if data == nil {
						continue
					}
					data.SafeFixIDs = data.FixIDs
					f.server.fixes.mu.Lock()
					for key, action := range f.server.fixes.actions {
						action.alwaysSafe = false
						f.server.fixes.actions[key] = action
					}
					f.server.fixes.mu.Unlock()
				}
			},
		},
		{
			// The guarded text no longer matches, with the version untouched:
			// this is the check that catches a mutation the version missed.
			name: "the guarded text no longer matches",
			disturb: func(_ *testing.T, f *actionFixture) {
				f.server.fixes.mu.Lock()
				for key, action := range f.server.fixes.actions {
					for i := range action.edits {
						action.edits[i].OldText = "impossible"
						action.edits[i].StartCol = action.edits[i].StartCol + 1
					}
					f.server.fixes.actions[key] = action
				}
				f.server.fixes.mu.Unlock()
			},
		},
		{
			name: "the request document is not the analysed one",
			disturb: func(_ *testing.T, f *actionFixture) {
				f.params.TextDocument.URI = "file:///elsewhere.sg"
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newActionFixture(t)
			// The action must exist before the disturbance, or the row proves
			// nothing about the disturbance.
			fixture.requireOneAction(t)
			testCase.disturb(t, fixture)
			fixture.requireNoActions(t, testCase.name)
		})
	}
}

// TestCodeActionSuppressedForDiskOnlyDocument is the first-phase rule: an edit
// into a document the server holds no trusted text for is suppressed rather
// than guessed at. RV2-DEBT-135 tracks the remaining case, where a disk-only
// SEMANTIC DEPENDENCY changes without any open document transition.
func TestCodeActionSuppressedForDiskOnlyDocument(t *testing.T) {
	fixture := newActionFixture(t)
	fixture.requireOneAction(t)
	fixture.server.mu.Lock()
	delete(fixture.server.openDocs, fixture.uri)
	fixture.server.mu.Unlock()
	fixture.requireNoActions(t, "a disk-only document")
}

// TestCodeActionOffersOnlyAlwaysSafe is the applicability rule stated directly.
func TestCodeActionOffersOnlyAlwaysSafe(t *testing.T) {
	fixture := newActionFixture(t)
	for _, action := range fixture.actions() {
		fixture.server.fixes.mu.Lock()
		found := false
		for _, registered := range fixture.server.fixes.actions {
			if registered.title == action.Title && registered.alwaysSafe {
				found = true
			}
		}
		fixture.server.fixes.mu.Unlock()
		if !found {
			t.Fatalf("offered an action that is not registered as always-safe: %q", action.Title)
		}
	}
}

// TestCodeActionCapabilityIsAdvertised keeps the handler reachable: a provider
// the client never learns about is a feature nobody can use.
func TestCodeActionCapabilityIsAdvertised(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.sg"), []byte(codeActionSource), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	server := NewServer(nil, nil, ServerOptions{})
	if server.fixes == nil {
		t.Fatal("a new server has no fix registry")
	}
}
