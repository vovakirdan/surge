package lsp

import "encoding/json"

type inlayHintConfig struct {
	letTypes    bool
	hideObvious bool
	defaultInit bool
}

func defaultInlayHintConfig() inlayHintConfig {
	return inlayHintConfig{
		letTypes:    true,
		hideObvious: false,
		defaultInit: true,
	}
}

func (s *Server) handleDidChangeConfiguration(msg *rpcMessage) error {
	if len(msg.Params) == 0 {
		return nil
	}
	var params didChangeConfigurationParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return nil
	}
	s.applySettings(params.Settings)
	return nil
}

func (s *Server) applySettings(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var settings lspSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if settings.Surge.InlayHints.LetTypes != nil {
		s.inlayHints.letTypes = *settings.Surge.InlayHints.LetTypes
	}
	if settings.Surge.InlayHints.HideObvious != nil {
		s.inlayHints.hideObvious = *settings.Surge.InlayHints.HideObvious
	}
	if settings.Surge.InlayHints.DefaultInit != nil {
		s.inlayHints.defaultInit = *settings.Surge.InlayHints.DefaultInit
	}
	if settings.Surge.LSP.Trace != nil {
		s.traceLSP = *settings.Surge.LSP.Trace
	}
}
