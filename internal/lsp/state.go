package lsp

import "surge/internal/driver/diagnose"

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
