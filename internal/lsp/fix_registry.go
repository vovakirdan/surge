package lsp

import (
	"sync"

	"surge/internal/driver/diagnose"
)

// fixRegistry is the server's trusted record of what one analysis offered.
//
// A Code Action request arrives carrying `Diagnostic.data`, which a client may
// replay, edit or invent. Nothing in it is believed: it is used only to LOOK UP
// an entry here, and every guard is then checked against this record and the
// server's own document state. That is what makes the data opaque rather than
// merely obscure.
type fixRegistry struct {
	mu sync.Mutex
	// analysisID is the analysis these actions came from. A request naming any
	// other analysis is stale by construction.
	analysisID uint64
	// docs is every document that analysis was computed over, with the version
	// and snapshot it saw. An action is offered only while ALL of them still
	// match: an edit whose safety was proved against one file cannot be trusted
	// after a different file in the same analysis has changed.
	docs    map[string]docState
	actions map[registeredFixKey]registeredFix
}

type registeredFixKey struct {
	diagnosticID string
	fixID        string
}

type registeredFix struct {
	uri        string
	title      string
	alwaysSafe bool
	edits      []diagnose.FixEditLocation
}

func newFixRegistry() *fixRegistry {
	return &fixRegistry{docs: make(map[string]docState), actions: make(map[registeredFixKey]registeredFix)}
}

// record replaces the registry with what this analysis offered.
//
// It replaces rather than accumulates: an action from a superseded analysis is
// not merely unlikely to be valid, it is unverifiable, because the snapshot its
// guards were proved against is gone.
func (r *fixRegistry) record(plan analysisPlan, diags []diagnose.Diagnostic) {
	actions := make(map[registeredFixKey]registeredFix)
	for i := range diags {
		d := &diags[i]
		uri := pathToURI(d.FilePath)
		if uri == "" {
			continue
		}
		diagnosticID := diagnosticIdentity(d, i)
		for j := range d.Fixes {
			offer := &d.Fixes[j]
			actions[registeredFixKey{diagnosticID: diagnosticID, fixID: offer.ID}] = registeredFix{
				uri: uri, title: offer.Title, alwaysSafe: offer.AlwaysSafe, edits: offer.Edits,
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.analysisID = plan.seq
	r.docs = cloneDocStates(plan.docs)
	if r.docs == nil {
		r.docs = make(map[string]docState)
	}
	r.actions = actions
}

// lookup returns the recorded action, and whether the request names the
// analysis this registry holds.
func (r *fixRegistry) lookup(analysisID uint64, key registeredFixKey) (registeredFix, map[string]docState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.analysisID == 0 || r.analysisID != analysisID {
		return registeredFix{}, nil, false
	}
	action, ok := r.actions[key]
	if !ok {
		return registeredFix{}, nil, false
	}
	return action, cloneDocStates(r.docs), true
}

func (r *fixRegistry) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.analysisID = 0
	r.docs = make(map[string]docState)
	r.actions = make(map[registeredFixKey]registeredFix)
}
