package lsp

import (
	"encoding/json"

	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/token"
)

func (s *Server) handleDefinition(msg *rpcMessage) error {
	var params definitionParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.sendError(msg.ID, -32602, "invalid params")
		}
	}
	snapshot := s.snapshotForURI(params.TextDocument.URI)
	if snapshot == nil {
		return s.sendResponse(msg.ID, []location{})
	}
	result := buildDefinition(snapshot, params.TextDocument.URI, params.Position)
	return s.sendResponse(msg.ID, result)
}

func buildDefinition(snapshot *diagnose.AnalysisSnapshot, uri string, pos position) []location {
	af, file := snapshotFile(snapshot, uri)
	if af == nil || file == nil {
		return nil
	}
	offset := offsetForPositionInFile(file, pos)
	tok, tokOK := tokenAtOffset(af.Tokens, offset)
	if !tokOK || tok.Kind != token.Ident {
		return nil
	}
	resolved := resolveSymbolAt(af, file, offset, tok)
	if resolved.Sym == nil || snapshot.FileSet == nil {
		return nil
	}
	span := resolved.Sym.Span
	if span == (source.Span{}) {
		return nil
	}
	if !snapshot.FileSet.HasFile(span.File) {
		return nil
	}
	defFile := snapshot.FileSet.Get(span.File)
	if defFile == nil {
		return nil
	}
	loc := location{
		URI:   pathToURI(defFile.Path),
		Range: rangeForSpan(defFile, span),
	}
	return []location{loc}
}
