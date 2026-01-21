package lsp

import "surge/internal/driver/diagnose"

type docState struct {
	version    int
	snapshotID int64
}

func (s *Server) currentSnapshot() *diagnose.AnalysisSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastGoodSnapshot
}

func (s *Server) currentInlayConfig() inlayHintConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inlayHints
}

func (s *Server) currentSnapshotVersion() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotVersion
}

func (s *Server) currentTrace() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.traceLSP
}

func (s *Server) snapshotForURI(uri string) *diagnose.AnalysisSnapshot {
	uri = canonicalURI(uri)
	if uri == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastGoodSnapshot == nil {
		return nil
	}
	if _, ok := s.snapshotDocs[uri]; !ok {
		return nil
	}
	if _, mismatch := s.firstDocMismatchLocked(s.snapshotDocs); mismatch {
		return nil
	}
	return s.lastGoodSnapshot
}

func (s *Server) currentDocState(uri string) (docState, bool) {
	uri = canonicalURI(uri)
	if uri == "" {
		return docState{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.docStateLocked(uri)
}

func (s *Server) snapshotDocState(uri string) (docState, bool) {
	uri = canonicalURI(uri)
	if uri == "" {
		return docState{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.snapshotDocs[uri]
	return state, ok
}

func (s *Server) docStateLocked(uri string) (docState, bool) {
	version, ok := s.versions[uri]
	if !ok {
		return docState{}, false
	}
	snapshotID, ok := s.docSnapshots[uri]
	if !ok {
		return docState{}, false
	}
	return docState{
		version:    version,
		snapshotID: snapshotID,
	}, true
}
