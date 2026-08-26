package lsp

import (
	"encoding/json"
	"sort"
)

// codeActionKindQuickFix is the only kind this server offers.
const codeActionKindQuickFix = "quickfix"

func (s *Server) handleCodeAction(msg *rpcMessage) error {
	var params codeActionParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.sendError(msg.ID, -32602, "invalid params")
		}
	}
	// An empty result is the answer to everything that does not check out.
	// A stale action must not be an error the client can retry into, and it
	// must never be an edit.
	return s.sendResponse(msg.ID, s.codeActionsFor(&params))
}

func (s *Server) codeActionsFor(params *codeActionParams) []codeAction {
	actions := make([]codeAction, 0, 1)
	requestURI := params.TextDocument.URI
	if requestURI == "" {
		return actions
	}
	for i := range params.Context.Diagnostics {
		reported := &params.Context.Diagnostics[i]
		data := reported.Data
		if data == nil {
			continue
		}
		// Only the always-safe subset is ever offered without being asked for.
		// The list is the server's own, published beside the diagnostic; a
		// client that adds ids to it names actions the registry does not hold.
		for _, fixID := range data.SafeFixIDs {
			action, ok := s.buildCodeAction(requestURI, data, fixID)
			if !ok {
				continue
			}
			actions = append(actions, action)
		}
	}
	sort.SliceStable(actions, func(i, j int) bool { return actions[i].Title < actions[j].Title })
	return actions
}

// buildCodeAction verifies one action end to end and materialises it.
//
// The guards run BEFORE the edits are built and again AFTER, because the
// document can change between the two: a check that only ran first would prove
// something about a document that no longer exists by the time the edit is
// returned.
func (s *Server) buildCodeAction(requestURI string, data *diagnosticData, fixID string) (codeAction, bool) {
	key := registeredFixKey{diagnosticID: data.DiagnosticID, fixID: fixID}
	action, analyzed, known := s.fixes.lookup(data.AnalysisID, key)
	if !known || !action.alwaysSafe {
		return codeAction{}, false
	}
	if !sameURI(action.uri, requestURI) || !sameURI(data.URI, requestURI) {
		// The data names another document. Whether that is a mistake or a
		// forgery, the answer is the same.
		return codeAction{}, false
	}
	if !s.snapshotStillMatches(analyzed, data) {
		return codeAction{}, false
	}
	edits, version, ok := s.materializeEdits(action)
	if !ok {
		return codeAction{}, false
	}
	if !s.snapshotStillMatches(analyzed, data) {
		return codeAction{}, false
	}
	return codeAction{
		Title:       action.title,
		Kind:        codeActionKindQuickFix,
		IsPreferred: true,
		Edit: workspaceEdit{DocumentChanges: []textDocumentEdit{{
			TextDocument: versionedTextDocumentIdentifier{URI: action.uri, Version: version},
			Edits:        edits,
		}}},
	}, true
}

// snapshotStillMatches requires every document the analysis was computed over
// to be exactly as it was, and the requesting document to match the version and
// snapshot the diagnostic was published with.
//
// A weaker rule — checking only the document being edited — would offer an
// action whose safety was proved against an imported file that has since
// changed underneath it.
func (s *Server) snapshotStillMatches(analyzed map[string]docState, data *diagnosticData) bool {
	if len(analyzed) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for uri, want := range analyzed {
		got, open := s.docStateLocked(uri)
		if !open || got != want {
			return false
		}
	}
	requested, open := s.docStateLocked(data.URI)
	if !open {
		return false
	}
	return requested.version == data.Version && uint64(requested.snapshotID) == data.SnapshotID //nolint:gosec // an id
}

// materializeEdits turns the recorded positions into edits against the text the
// server currently holds, refusing anything it cannot prove safe.
func (s *Server) materializeEdits(action registeredFix) (edits []textEdit, version int, ok bool) {
	s.mu.Lock()
	text, open := s.openDocs[action.uri]
	version = s.versions[action.uri]
	s.mu.Unlock()
	if !open {
		// A closed or disk-only document has no trusted text to check against,
		// so a first-phase edit into one is suppressed rather than guessed at.
		return nil, 0, false
	}
	resolved := make([]resolvedEdit, 0, len(action.edits))
	for i := range action.edits {
		recorded := &action.edits[i]
		start := position{Line: maxZero(recorded.StartLine - 1), Character: maxZero(recorded.StartCol - 1)}
		end := position{Line: maxZero(recorded.EndLine - 1), Character: maxZero(recorded.EndCol - 1)}
		startOffset := offsetForPosition(text, start)
		endOffset := offsetForPosition(text, end)
		if startOffset > endOffset || endOffset > len(text) {
			return nil, 0, false
		}
		if !guardedTextMatches(text[startOffset:endOffset], recorded.OldText) {
			return nil, 0, false
		}
		resolved = append(resolved, resolvedEdit{
			start: startOffset, end: endOffset,
			edit: textEdit{Range: lspRange{Start: start, End: end}, NewText: recorded.NewText},
		})
	}
	if len(resolved) == 0 || editsOverlap(resolved) {
		return nil, 0, false
	}
	edits = make([]textEdit, 0, len(resolved))
	for i := range resolved {
		edits = append(edits, resolved[i].edit)
	}
	return edits, version, true
}

// resolvedEdit is one recorded edit placed against the current text, keeping
// the byte offsets the overlap check needs.
type resolvedEdit struct {
	start int
	end   int
	edit  textEdit
}

// guardedTextMatches enforces the old-text contract.
//
// A replace or a delete destroys what is there, so it must say what it expects
// to find; an empty guard is valid only for an insertion, whose span is a point
// and which therefore destroys nothing.
func guardedTextMatches(current, guard string) bool {
	if guard == "" {
		return current == ""
	}
	return current == guard
}

// editsOverlap refuses a set whose members touch each other. Applying two
// overlapping edits produces a document neither of them described, and no
// ordering makes that safe.
func editsOverlap(resolved []resolvedEdit) bool {
	ordered := make([]resolvedEdit, len(resolved))
	copy(ordered, resolved)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].start < ordered[j].start })
	for i := 1; i < len(ordered); i++ {
		previous, current := ordered[i-1], ordered[i]
		if previous.start == previous.end && current.start == current.end {
			// Two insertions at one point are ordered, not conflicting.
			continue
		}
		if current.start < previous.end {
			return true
		}
	}
	return false
}

func sameURI(left, right string) bool {
	if left == right {
		return true
	}
	return canonicalPath(uriToPath(left)) == canonicalPath(uriToPath(right))
}
