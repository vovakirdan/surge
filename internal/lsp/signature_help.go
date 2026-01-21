package lsp

import (
	"encoding/json"
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/driver/diagnose"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/token"
	"surge/internal/types"
)

type signatureCandidate struct {
	name     string
	sig      *symbols.FunctionSignature
	dropSelf bool
}

func (s *Server) handleSignatureHelp(msg *rpcMessage) error {
	var params signatureHelpParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return s.sendError(msg.ID, -32602, "invalid params")
		}
	}
	snapshot := s.snapshotForURI(params.TextDocument.URI)
	if snapshot == nil {
		return s.sendResponse(msg.ID, nil)
	}
	result := buildSignatureHelp(snapshot, params.TextDocument.URI, params.Position)
	return s.sendResponse(msg.ID, result)
}

func buildSignatureHelp(snapshot *diagnose.AnalysisSnapshot, uri string, pos position) *signatureHelp {
	af, file := snapshotFile(snapshot, uri)
	if af == nil || file == nil || af.Builder == nil {
		return nil
	}
	offset := offsetForPositionInFile(file, pos)
	_, call := findCallAtOffset(af.Builder, file.ID, offset)
	if call == nil {
		return nil
	}
	argIndex := callArgIndex(call, offset)

	candidates := resolveSignatureCandidates(snapshot, af, file, call)
	if len(candidates) == 0 {
		if fallback := signatureFromCallableType(af, call.Target); fallback != nil {
			return &signatureHelp{
				Signatures:      []signatureInformation{*fallback},
				ActiveSignature: 0,
				ActiveParameter: clampActiveParam(argIndex, len(fallback.Parameters)),
			}
		}
		return nil
	}
	infos := make([]signatureInformation, 0, len(candidates))
	for _, cand := range candidates {
		info := buildSignatureInformation(cand.name, cand.sig, func(id source.StringID) string {
			return lookupName(af, id)
		}, cand.dropSelf)
		infos = append(infos, info)
	}
	activeSig := selectActiveSignature(af, candidates, call)
	activeParam := argIndex
	if activeSig >= 0 && activeSig < len(candidates) {
		activeParam = activeParamForCandidate(activeParam, candidates[activeSig])
		activeParam = clampActiveParam(activeParam, len(infos[activeSig].Parameters))
	}
	return &signatureHelp{
		Signatures:      infos,
		ActiveSignature: activeSig,
		ActiveParameter: activeParam,
	}
}

func findCallAtOffset(builder *ast.Builder, fileID source.FileID, offset uint32) (ast.ExprID, *ast.ExprCallData) {
	if builder == nil || builder.Exprs == nil {
		return ast.NoExprID, nil
	}
	var bestID ast.ExprID
	var bestSpan source.Span
	for i := uint32(1); i <= builder.Exprs.Arena.Len(); i++ {
		exprID := ast.ExprID(i)
		expr := builder.Exprs.Get(exprID)
		if expr == nil || expr.Kind != ast.ExprCall {
			continue
		}
		if expr.Span.File != fileID {
			continue
		}
		if offset < expr.Span.Start || offset >= expr.Span.End {
			continue
		}
		if bestSpan != (source.Span{}) {
			if expr.Span.End-expr.Span.Start >= bestSpan.End-bestSpan.Start {
				continue
			}
		}
		bestID = exprID
		bestSpan = expr.Span
	}
	if bestID == ast.NoExprID {
		return ast.NoExprID, nil
	}
	call, ok := builder.Exprs.Call(bestID)
	if !ok || call == nil {
		return ast.NoExprID, nil
	}
	return bestID, call
}

func callArgIndex(call *ast.ExprCallData, offset uint32) int {
	if call == nil {
		return 0
	}
	idx := 0
	for _, comma := range call.ArgCommas {
		if comma.End <= offset {
			idx++
		}
	}
	if idx < 0 {
		return 0
	}
	if idx > len(call.Args) {
		return len(call.Args)
	}
	return idx
}

func resolveSignatureCandidates(snapshot *diagnose.AnalysisSnapshot, af *diagnose.AnalysisFile, file *source.File, call *ast.ExprCallData) []signatureCandidate {
	if af == nil || af.Builder == nil {
		return nil
	}
	targetID := unwrapGroupExpr(af.Builder, call.Target)
	targetExpr := af.Builder.Exprs.Get(targetID)
	if targetExpr == nil {
		return nil
	}
	if targetExpr.Kind == ast.ExprMember {
		member, ok := af.Builder.Exprs.Member(targetID)
		if !ok || member == nil {
			return nil
		}
		candidates := memberSignatureCandidates(snapshot, af, member, targetExpr.Span)
		return sortSignatureCandidates(dedupeSignatureCandidates(candidates))
	}
	if targetExpr.Kind == ast.ExprIdent {
		candidates := identSignatureCandidates(af, file, targetExpr.Span)
		return sortSignatureCandidates(dedupeSignatureCandidates(candidates))
	}
	return nil
}

func memberSignatureCandidates(snapshot *diagnose.AnalysisSnapshot, af *diagnose.AnalysisFile, member *ast.ExprMemberData, memberSpan source.Span) []signatureCandidate {
	if af == nil || af.Builder == nil || member == nil {
		return nil
	}
	name := lookupName(af, member.Field)
	if name == "" {
		return nil
	}
	if moduleSym := moduleSymbolForExpr(af, member.Target); moduleSym != nil {
		modulePath := moduleSym.ModulePath
		if modulePath == "" && moduleSym.ImportName != source.NoStringID {
			modulePath = lookupName(af, moduleSym.ImportName)
		}
		if modulePath == "" {
			return nil
		}
		exports := lookupModuleExports(snapshot, modulePath)
		if exports == nil {
			return nil
		}
		return exportSignatureCandidates(exports, name)
	}
	staticAccess := memberOperatorKind(af.Tokens, memberSpan, member.Target, af.Builder) == token.ColonColon
	if staticAccess {
		if enumType := enumTypeForExpr(af, member.Target); enumType != types.NoTypeID {
			return nil
		}
		if recvType := typeForStaticExpr(af, member.Target); recvType != types.NoTypeID {
			return methodSignatureCandidates(snapshot, af, recvType, name, true)
		}
		return nil
	}
	recvType := exprTypeForCompletion(af, member.Target)
	if recvType == types.NoTypeID {
		return nil
	}
	return methodSignatureCandidates(snapshot, af, recvType, name, false)
}

func identSignatureCandidates(af *diagnose.AnalysisFile, file *source.File, span source.Span) []signatureCandidate {
	if af == nil || af.Builder == nil || af.Symbols == nil || af.Symbols.Table == nil || file == nil {
		return nil
	}
	identID := findIdentExprBySpan(af.Builder, span)
	if identID == ast.NoExprID {
		return nil
	}
	ident, ok := af.Builder.Exprs.Ident(identID)
	if !ok || ident == nil || ident.Name == source.NoStringID {
		return nil
	}
	name := lookupName(af, ident.Name)
	if name == "" {
		return nil
	}
	scope := scopeAtOffset(af.Symbols.Table.Scopes, af.Symbols.FileScope, file.ID, span.Start)
	if !scope.IsValid() {
		scope = af.Symbols.FileScope
	}
	ids := overloadSymbolsForName(af.Symbols.Table, scope, ident.Name)
	if len(ids) == 0 {
		return nil
	}
	candidates := make([]signatureCandidate, 0, len(ids))
	for _, id := range ids {
		sym := af.Symbols.Table.Symbols.Get(id)
		if sym == nil || sym.Signature == nil {
			continue
		}
		if sym.Kind != symbols.SymbolFunction && sym.Kind != symbols.SymbolTag {
			continue
		}
		if sym.ReceiverKey != "" {
			continue
		}
		candidates = append(candidates, signatureCandidate{name: name, sig: sym.Signature, dropSelf: false})
	}
	return candidates
}

func methodSignatureCandidates(snapshot *diagnose.AnalysisSnapshot, af *diagnose.AnalysisFile, recvType types.TypeID, name string, staticOnly bool) []signatureCandidate {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil {
		return nil
	}
	recvKeys := typeKeyCandidates(af.Sema.TypeInterner, recvType)
	if len(recvKeys) == 0 {
		return nil
	}
	candidates := make([]signatureCandidate, 0)
	addCandidate := func(sig *symbols.FunctionSignature) {
		if sig == nil {
			return
		}
		candidates = append(candidates, signatureCandidate{name: name, sig: sig, dropSelf: !staticOnly})
	}
	if af.Symbols != nil && af.Symbols.Table != nil {
		if data := af.Symbols.Table.Symbols.Data(); data != nil {
			for i := range data {
				sym := &data[i]
				if sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
					continue
				}
				if sym.ReceiverKey == "" || lookupName(af, sym.Name) != name {
					continue
				}
				if staticOnly && sym.Signature.HasSelf {
					continue
				}
				if !staticOnly && !sym.Signature.HasSelf {
					continue
				}
				if !receiverKeyMatches(recvKeys, sym.ReceiverKey) {
					continue
				}
				addCandidate(sym.Signature)
			}
		}
	}
	if snapshot != nil && snapshot.ModuleExports != nil {
		for _, exp := range snapshot.ModuleExports {
			if exp == nil {
				continue
			}
			symbolsList, ok := exp.Symbols[name]
			if !ok {
				continue
			}
			for i := range symbolsList {
				item := &symbolsList[i]
				if item.Kind != symbols.SymbolFunction || item.Signature == nil || item.ReceiverKey == "" {
					continue
				}
				if item.Flags&symbols.SymbolFlagPublic == 0 && item.Flags&symbols.SymbolFlagBuiltin == 0 {
					continue
				}
				if staticOnly && item.Signature.HasSelf {
					continue
				}
				if !staticOnly && !item.Signature.HasSelf {
					continue
				}
				if !receiverKeyMatches(recvKeys, item.ReceiverKey) {
					continue
				}
				addCandidate(item.Signature)
			}
		}
	}
	return candidates
}

func exportSignatureCandidates(exports *symbols.ModuleExports, name string) []signatureCandidate {
	if exports == nil {
		return nil
	}
	list, ok := exports.Symbols[name]
	if !ok || len(list) == 0 {
		return nil
	}
	candidates := make([]signatureCandidate, 0, len(list))
	for i := range list {
		item := &list[i]
		if item.Signature == nil {
			continue
		}
		if item.Kind != symbols.SymbolFunction && item.Kind != symbols.SymbolTag {
			continue
		}
		candidates = append(candidates, signatureCandidate{name: name, sig: item.Signature, dropSelf: false})
	}
	return candidates
}

func signatureFromCallableType(af *diagnose.AnalysisFile, target ast.ExprID) *signatureInformation {
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil || af.Sema.ExprTypes == nil {
		return nil
	}
	ty := af.Sema.ExprTypes[target]
	if ty == types.NoTypeID {
		return nil
	}
	info, ok := af.Sema.TypeInterner.FnInfo(ty)
	if !ok || info == nil {
		return nil
	}
	params := make([]string, 0, len(info.Params))
	paramInfos := make([]parameterInformation, 0, len(info.Params))
	for _, param := range info.Params {
		label := types.Label(af.Sema.TypeInterner, param)
		params = append(params, label)
		paramInfos = append(paramInfos, parameterInformation{Label: label})
	}
	label := "fn(" + strings.Join(params, ", ") + ")"
	if info.Result != types.NoTypeID {
		label += " -> " + types.Label(af.Sema.TypeInterner, info.Result)
	}
	return &signatureInformation{Label: label, Parameters: paramInfos}
}

func unwrapGroupExpr(builder *ast.Builder, exprID ast.ExprID) ast.ExprID {
	for {
		expr := builder.Exprs.Get(exprID)
		if expr == nil || expr.Kind != ast.ExprGroup {
			return exprID
		}
		group, ok := builder.Exprs.Group(exprID)
		if !ok || group == nil {
			return exprID
		}
		exprID = group.Inner
	}
}

func overloadSymbolsForName(table *symbols.Table, scopeID symbols.ScopeID, name source.StringID) []symbols.SymbolID {
	for scopeID.IsValid() {
		scope := table.Scopes.Get(scopeID)
		if scope == nil {
			break
		}
		if ids := scope.NameIndex[name]; len(ids) > 0 {
			out := make([]symbols.SymbolID, 0, len(ids))
			out = append(out, ids...)
			return out
		}
		scopeID = scope.Parent
	}
	return nil
}

func memberOperatorKind(tokens []token.Token, memberSpan source.Span, targetID ast.ExprID, builder *ast.Builder) token.Kind {
	if len(tokens) == 0 || builder == nil {
		return token.Invalid
	}
	targetSpan := source.Span{}
	if target := builder.Exprs.Get(targetID); target != nil {
		targetSpan = target.Span
	}
	for _, tok := range tokens {
		if tok.Span.File != memberSpan.File {
			continue
		}
		if tok.Span.Start < targetSpan.End {
			continue
		}
		if tok.Span.End > memberSpan.End {
			break
		}
		if tok.Kind == token.Dot || tok.Kind == token.ColonColon {
			return tok.Kind
		}
	}
	return token.Invalid
}

func buildSignatureInformation(name string, sig *symbols.FunctionSignature, lookup func(source.StringID) string, dropSelf bool) signatureInformation {
	params := make([]string, 0, len(sig.Params))
	paramInfos := make([]parameterInformation, 0, len(sig.Params))
	start := 0
	if dropSelf && sig.HasSelf && len(sig.Params) > 0 {
		start = 1
	}
	for i := start; i < len(sig.Params); i++ {
		paramLabel := string(sig.Params[i])
		if lookup != nil && i < len(sig.ParamNames) {
			if pname := lookup(sig.ParamNames[i]); pname != "" {
				paramLabel = pname + ": " + paramLabel
			}
		}
		if i < len(sig.Variadic) && sig.Variadic[i] {
			paramLabel = "[" + paramLabel + "]"
		}
		params = append(params, paramLabel)
		paramInfos = append(paramInfos, parameterInformation{Label: paramLabel})
	}
	label := "fn("
	if name != "" {
		label = "fn " + name + "("
	}
	label += strings.Join(params, ", ") + ")"
	if sig.Result != "" {
		label += " -> " + string(sig.Result)
	}
	return signatureInformation{Label: label, Parameters: paramInfos}
}

func selectActiveSignature(af *diagnose.AnalysisFile, candidates []signatureCandidate, call *ast.ExprCallData) int {
	if len(candidates) == 0 {
		return 0
	}
	if af == nil || af.Sema == nil || af.Sema.TypeInterner == nil || af.Sema.ExprTypes == nil {
		return 0
	}
	argTypes := callArgumentTypes(af, call)
	if len(argTypes) == 0 {
		return 0
	}
	bestIdx := 0
	bestScore := -1
	for i, cand := range candidates {
		score := signatureMatchScore(af.Sema.TypeInterner, cand, argTypes)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return bestIdx
}

func callArgumentTypes(af *diagnose.AnalysisFile, call *ast.ExprCallData) []types.TypeID {
	if af == nil || af.Sema == nil || af.Sema.ExprTypes == nil || call == nil {
		return nil
	}
	argTypes := make([]types.TypeID, 0, len(call.Args))
	for _, arg := range call.Args {
		argTypes = append(argTypes, af.Sema.ExprTypes[arg.Value])
	}
	return argTypes
}

func signatureMatchScore(interner *types.Interner, cand signatureCandidate, argTypes []types.TypeID) int {
	if interner == nil || cand.sig == nil || len(argTypes) == 0 {
		return 0
	}
	start := 0
	if cand.dropSelf && cand.sig.HasSelf && len(cand.sig.Params) > 0 {
		start = 1
	}
	params := cand.sig.Params
	count := len(params) - start
	if count < 0 {
		count = 0
	}
	limit := len(argTypes)
	if limit > count {
		limit = count
	}
	score := 0
	for i := range limit {
		paramKey := params[i+start]
		argType := argTypes[i]
		if argType == types.NoTypeID {
			continue
		}
		argKey := typeKeyForType(interner, argType)
		if argKey == "" {
			continue
		}
		if typeKeyMatchesWithGenerics(paramKey, argKey) {
			score += 2
		} else {
			score -= 1
		}
	}
	if score < 0 {
		return 0
	}
	return score
}

func activeParamForCandidate(argIndex int, cand signatureCandidate) int {
	active := argIndex
	if !cand.dropSelf {
		if cand.sig != nil && cand.sig.HasSelf {
			active++
		}
	}
	return active
}

func clampActiveParam(value, limit int) int {
	if limit <= 0 {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value >= limit {
		return limit - 1
	}
	return value
}

func dedupeSignatureCandidates(candidates []signatureCandidate) []signatureCandidate {
	if len(candidates) < 2 {
		return candidates
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]signatureCandidate, 0, len(candidates))
	for _, cand := range candidates {
		key := signatureKey(cand.sig)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cand)
	}
	return out
}

func signatureKey(sig *symbols.FunctionSignature) string {
	if sig == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range sig.Params {
		b.WriteString(string(p))
		b.WriteByte(',')
	}
	b.WriteString("->")
	b.WriteString(string(sig.Result))
	return b.String()
}

func sortSignatureCandidates(candidates []signatureCandidate) []signatureCandidate {
	if len(candidates) < 2 {
		return candidates
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return signatureKey(candidates[i].sig) < signatureKey(candidates[j].sig)
	})
	return candidates
}
