package lsp

import (
	"encoding/json"

	"surge/internal/driver/diagnose"
	"surge/internal/token"
)

const (
	completionItemKindText        = 1
	completionItemKindMethod      = 2
	completionItemKindFunction    = 3
	completionItemKindConstructor = 4
	completionItemKindField       = 5
	completionItemKindVariable    = 6
	completionItemKindClass       = 7
	completionItemKindInterface   = 8
	completionItemKindModule      = 9
	completionItemKindEnum        = 13
	completionItemKindEnumMember  = 20
	completionItemKindConstant    = 21
	completionItemKindStruct      = 22
	completionItemKindTypeParam   = 25
)

func (s *Server) handleCompletion(msg *rpcMessage) error {
	var params completionParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.sendError(msg.ID, -32602, "invalid params")
		}
	}
	snapshot := s.currentSnapshot()
	if snapshot == nil {
		return s.sendResponse(msg.ID, completionList{IsIncomplete: false, Items: nil})
	}
	result := buildCompletion(snapshot, params.TextDocument.URI, params.Position)
	return s.sendResponse(msg.ID, result)
}

func buildCompletion(snapshot *diagnose.AnalysisSnapshot, uri string, pos position) completionList {
	af, file := snapshotFile(snapshot, uri)
	if af == nil || file == nil {
		return completionList{IsIncomplete: false, Items: nil}
	}
	offset := offsetForPositionInFile(file, pos)
	tokens := af.Tokens
	tokenIdx := tokenIndexAtOffset(tokens, offset)

	if ctx, ok := importContextAt(tokens, file, offset, tokenIdx); ok {
		if ctx.afterColonColon {
			items := importMemberCompletions(snapshot, af, file, ctx.moduleText)
			return completionList{IsIncomplete: false, Items: items}
		}
		items := importPathCompletions(snapshot, file, ctx.prefix)
		return completionList{IsIncomplete: false, Items: items}
	}

	trigger, targetOffset := completionTrigger(tokens, offset, tokenIdx)
	switch trigger {
	case token.Dot:
		items := memberCompletions(snapshot, af, file, targetOffset)
		return completionList{IsIncomplete: false, Items: items}
	case token.ColonColon:
		items := staticCompletions(snapshot, af, file, targetOffset)
		return completionList{IsIncomplete: false, Items: items}
	}

	if isTypePosition(tokens, offset) {
		items := typeCompletions(af, file, offset)
		return completionList{IsIncomplete: false, Items: items}
	}
	items := generalCompletions(af, file, offset)
	return completionList{IsIncomplete: false, Items: items}
}
