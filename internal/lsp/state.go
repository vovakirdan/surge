package lsp

import "surge/internal/driver/diagnose"

func (s *Server) currentSnapshot() *diagnose.AnalysisSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSnapshot
}

func (s *Server) currentInlayConfig() inlayHintConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inlayHints
}
