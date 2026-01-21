package lsp

import (
	"encoding/json"
	"sort"

	"surge/internal/ast"
	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/token"
)

type braceEntry struct {
	line int
	span source.Span
	skip bool
}

func (s *Server) handleFoldingRange(msg *rpcMessage) error {
	var params foldingRangeParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.sendError(msg.ID, -32602, "invalid params")
		}
	}
	snapshot := s.snapshotForURI(params.TextDocument.URI)
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
	itemRanges, skipBraces := itemFoldingRanges(af, file)
	stack := make([]braceEntry, 0, 8)
	ranges := make([]foldingRange, 0, len(itemRanges))
	ranges = append(ranges, itemRanges...)
	for _, tok := range tokens {
		if tok.Span.File != file.ID {
			continue
		}
		switch tok.Kind {
		case token.LBrace:
			_, skip := skipBraces[tok.Span.Start]
			stack = append(stack, braceEntry{line: lineForOffset(file, tok.Span.Start), span: tok.Span, skip: skip})
		case token.RBrace:
			if len(stack) == 0 {
				continue
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if open.skip {
				continue
			}
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

func itemFoldingRanges(af *diagnose.AnalysisFile, file *source.File) (ranges []foldingRange, skipBraces map[uint32]struct{}) {
	if af == nil || af.Builder == nil {
		return nil, map[uint32]struct{}{}
	}
	fileNode := af.Builder.Files.Get(af.ASTFile)
	if fileNode == nil {
		return nil, map[uint32]struct{}{}
	}
	ranges = make([]foldingRange, 0)
	skipBraces = make(map[uint32]struct{})

	addRange := func(startSpan, bodySpan source.Span) {
		if startSpan == (source.Span{}) || bodySpan == (source.Span{}) {
			return
		}
		if startSpan.File != file.ID || bodySpan.File != file.ID {
			return
		}
		startLine := lineForOffset(file, startSpan.Start)
		endLine := lineForOffset(file, spanLastOffset(bodySpan))
		if startLine >= endLine {
			return
		}
		ranges = append(ranges, foldingRange{StartLine: startLine, EndLine: endLine})
		skipBraces[bodySpan.Start] = struct{}{}
	}

	for _, itemID := range fileNode.Items {
		item := af.Builder.Items.Get(itemID)
		if item == nil {
			continue
		}
		switch item.Kind {
		case ast.ItemFn:
			fnItem, ok := af.Builder.Items.Fn(itemID)
			if !ok || fnItem == nil || !fnItem.Body.IsValid() {
				continue
			}
			bodyStmt := af.Builder.Stmts.Get(fnItem.Body)
			if bodyStmt == nil {
				continue
			}
			startSpan := fnItem.FnKeywordSpan
			if startSpan == (source.Span{}) {
				startSpan = fnItem.Span
			}
			addRange(startSpan, bodyStmt.Span)
		case ast.ItemType:
			typeItem, ok := af.Builder.Items.Type(itemID)
			if !ok || typeItem == nil {
				continue
			}
			startSpan := typeItem.TypeKeywordSpan
			if startSpan == (source.Span{}) {
				startSpan = typeItem.Span
			}
			switch typeItem.Kind {
			case ast.TypeDeclStruct:
				if decl := af.Builder.Items.TypeStruct(typeItem); decl != nil {
					addRange(startSpan, decl.BodySpan)
				}
			case ast.TypeDeclUnion:
				if decl := af.Builder.Items.TypeUnion(typeItem); decl != nil {
					addRange(startSpan, decl.BodySpan)
				}
			case ast.TypeDeclEnum:
				if decl := af.Builder.Items.TypeEnum(typeItem); decl != nil {
					addRange(startSpan, decl.BodySpan)
				}
			}
		case ast.ItemContract:
			contract, ok := af.Builder.Items.Contract(itemID)
			if !ok || contract == nil {
				continue
			}
			startSpan := contract.ContractKeywordSpan
			if startSpan == (source.Span{}) {
				startSpan = contract.Span
			}
			addRange(startSpan, contract.BodySpan)
		case ast.ItemExtern:
			ext, ok := af.Builder.Items.Extern(itemID)
			if !ok || ext == nil {
				continue
			}
			addRange(ext.Span, ext.Span)
		}
	}

	return ranges, skipBraces
}

func spanLastOffset(span source.Span) uint32 {
	if span.End == 0 {
		return span.End
	}
	if span.End > span.Start {
		return span.End - 1
	}
	return span.End
}
