package lsp

import (
	"encoding/json"
	"sort"

	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/token"
)

type braceEntry struct {
	line int
	span source.Span
}

func (s *Server) handleFoldingRange(msg *rpcMessage) error {
	var params foldingRangeParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.sendError(msg.ID, -32602, "invalid params")
		}
	}
	snapshot := s.currentSnapshot()
	if snapshot == nil {
		return s.sendResponse(msg.ID, []foldingRange{})
	}
	ranges := buildFoldingRanges(snapshot, params.TextDocument.URI)
	return s.sendResponse(msg.ID, ranges)
}

func buildFoldingRanges(snapshot *diagnose.AnalysisSnapshot, uri string) []foldingRange {
	af, file := snapshotFile(snapshot, uri)
	if af == nil || file == nil {
		return nil
	}
	tokens := af.Tokens
	if len(tokens) == 0 {
		return nil
	}
	stack := make([]braceEntry, 0, 8)
	ranges := make([]foldingRange, 0)
	for _, tok := range tokens {
		if tok.Span.File != file.ID {
			continue
		}
		switch tok.Kind {
		case token.LBrace:
			stack = append(stack, braceEntry{line: lineForOffset(file, tok.Span.Start), span: tok.Span})
		case token.RBrace:
			if len(stack) == 0 {
				continue
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			endLine := lineForOffset(file, tok.Span.Start)
			if open.line >= endLine {
				continue
			}
			ranges = append(ranges, foldingRange{StartLine: open.line, EndLine: endLine})
		}
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].StartLine == ranges[j].StartLine {
			return ranges[i].EndLine < ranges[j].EndLine
		}
		return ranges[i].StartLine < ranges[j].StartLine
	})
	return ranges
}

func lineForOffset(file *source.File, offset uint32) int {
	return positionForOffsetInFile(file, offset).Line
}
