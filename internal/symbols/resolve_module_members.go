package symbols

import (
	"fmt"
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

func (fr *fileResolver) resolveMember(exprID ast.ExprID, member *ast.ExprMemberData) {
	if member == nil {
		return
	}
	targetSymID, ok := fr.result.ExprSymbols[member.Target]
	if !ok {
		return
	}
	sym := fr.result.Table.Symbols.Get(targetSymID)
	if sym == nil {
		return
	}
	if sym.Kind == SymbolModule {
		fr.resolveModuleMember(exprID, member, sym)
	}
}

func (fr *fileResolver) resolveModuleMember(exprID ast.ExprID, member *ast.ExprMemberData, moduleSym *Symbol) {
	if moduleSym == nil {
		return
	}
	exports := fr.aliasExports[moduleSym.Name]
	modulePath := fr.aliasModulePaths[moduleSym.Name]
	if modulePath == "" && moduleSym.ModulePath != "" {
		modulePath = moduleSym.ModulePath
	}
	if exports == nil && modulePath != "" {
		exports = fr.moduleExports[modulePath]
	}
	memberName := fr.lookupString(member.Field)
	useSpan := source.Span{}
	if expr := fr.builder.Exprs.Get(exprID); expr != nil {
		useSpan = expr.Span
	}
	if exports == nil {
		fr.reportModuleMemberNotFound(modulePath, member.Field, useSpan)
		return
	}
	exported := exports.Lookup(memberName)
	if len(exported) == 0 {
		fr.reportModuleMemberNotFound(modulePath, member.Field, useSpan)
		return
	}
	var candidate *ExportedSymbol
	for i := range exported {
		if exported[i].Flags&SymbolFlagPublic != 0 {
			candidate = &exported[i]
			break
		}
	}
	if candidate == nil {
		refSpan := exported[0].Span
		fr.reportModuleMemberNotPublic(modulePath, member.Field, useSpan, refSpan)
		return
	}
	symID := fr.syntheticSymbolForExport(modulePath, memberName, candidate, useSpan)
	if symID.IsValid() {
		fr.result.ExprSymbols[exprID] = symID
	}
}

func signatureKey(sig *FunctionSignature) string {
	if sig == nil {
		return "nosig"
	}
	b := strings.Builder{}
	for _, p := range sig.Params {
		b.WriteString(string(p))
		b.WriteByte(',')
	}
	b.WriteString("->")
	b.WriteString(string(sig.Result))
	return b.String()
}

func (fr *fileResolver) syntheticSymbolForExport(modulePath, name string, export *ExportedSymbol, fallback source.Span) SymbolID {
	if fr.syntheticImportSyms == nil {
		fr.syntheticImportSyms = make(map[string]SymbolID)
	}
	if export == nil {
		return NoSymbolID
	}
	key := modulePath + "::" + name + fmt.Sprintf("#%d:%s", export.Kind, signatureKey(export.Signature))
	if id, ok := fr.syntheticImportSyms[key]; ok {
		return id
	}
	nameID := source.NoStringID
	if fr.builder != nil && fr.builder.StringsInterner != nil {
		nameID = fr.builder.StringsInterner.Intern(name)
	}
	span := export.Span
	if span == (source.Span{}) {
		span = fallback
	}
	var typeParams []source.StringID
	if fr.builder != nil && fr.builder.StringsInterner != nil {
		for _, paramName := range export.TypeParamNames {
			if paramName == "" {
				continue
			}
			typeParams = append(typeParams, fr.builder.StringsInterner.Intern(paramName))
		}
	}
	sym := Symbol{
		Name:          nameID,
		Kind:          export.Kind,
		Span:          span,
		Flags:         export.Flags | SymbolFlagImported,
		Type:          export.Type,
		Signature:     export.Signature,
		Scope:         fr.result.FileScope,
		ModulePath:    modulePath,
		ImportName:    nameID,
		TypeParams:    typeParams,
		TypeParamSpan: export.TypeParamSpan,
		ReceiverKey:   export.ReceiverKey,
	}
	id := fr.result.Table.Symbols.New(&sym)
	if scope := fr.result.Table.Scopes.Get(fr.result.FileScope); scope != nil {
		scope.Symbols = append(scope.Symbols, id)
		scope.NameIndex[nameID] = append(scope.NameIndex[nameID], id)
	}
	fr.syntheticImportSyms[key] = id
	return id
}

func (fr *fileResolver) injectCoreExports() {
	if fr.noStd || fr.moduleExports == nil || fr.builder == nil || fr.result == nil {
		return
	}
	var fileSpan source.Span
	if file := fr.builder.Files.Get(fr.fileID); file != nil {
		fileSpan = file.Span
	}
	for modulePath, exports := range fr.moduleExports {
		trimmed := strings.Trim(modulePath, "/")
		if trimmed != "core" && !strings.HasPrefix(trimmed, "core/") {
			continue
		}
		for name, overloads := range exports.Symbols {
			for i := range overloads {
				exp := &overloads[i]
				if exp.Flags&SymbolFlagPublic == 0 && exp.Flags&SymbolFlagBuiltin == 0 {
					continue
				}
				fr.syntheticSymbolForExport(modulePath, name, exp, fileSpan)
			}
		}
	}
}

func (fr *fileResolver) reportModuleMemberNotFound(modulePath string, field source.StringID, span source.Span) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	name := fr.lookupString(field)
	if name == "" {
		name = "_"
	}
	msg := fmt.Sprintf("module %q has no member %q", modulePath, name)
	if b := diag.ReportError(fr.resolver.reporter, diag.SemaModuleMemberNotFound, span, msg); b != nil {
		b.Emit()
	}
}

func (fr *fileResolver) reportModuleMemberNotPublic(modulePath string, field source.StringID, useSpan, defSpan source.Span) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	name := fr.lookupString(field)
	if name == "" {
		name = "_"
	}
	msg := fmt.Sprintf("member %q of module %q is not public", name, modulePath)
	b := diag.ReportError(fr.resolver.reporter, diag.SemaModuleMemberNotPublic, useSpan, msg)
	if b == nil {
		return
	}
	if defSpan != (source.Span{}) {
		b.WithNote(defSpan, "declared here")
	}
	b.Emit()
}

func exportsPrelude(exports map[string]*ModuleExports) []PreludeEntry {
	if len(exports) == 0 {
		return nil
	}
	entries := make([]PreludeEntry, 0, 8)
	modulePaths := make([]string, 0, len(exports))
	for modulePath, moduleExports := range exports {
		if moduleExports == nil {
			continue
		}
		trimmed := strings.Trim(modulePath, "/")
		if trimmed != "core" && !strings.HasPrefix(trimmed, "core/") {
			continue
		}
		modulePaths = append(modulePaths, modulePath)
	}
	sort.Strings(modulePaths)
	for _, modulePath := range modulePaths {
		moduleExports := exports[modulePath]
		if moduleExports == nil {
			continue
		}
		names := make([]string, 0, len(moduleExports.Symbols))
		for name := range moduleExports.Symbols {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for i := range moduleExports.Symbols[name] {
				exp := &moduleExports.Symbols[name][i]
				if exp.Flags&SymbolFlagPublic == 0 && exp.Flags&SymbolFlagBuiltin == 0 {
					continue
				}
				entries = append(entries, PreludeEntry{
					Name:          name,
					Kind:          exp.Kind,
					Flags:         exp.Flags | SymbolFlagBuiltin | SymbolFlagImported,
					Span:          exp.Span,
					Signature:     exp.Signature,
					Type:          exp.Type,
					TypeParams:    append([]string(nil), exp.TypeParamNames...),
					TypeParamSpan: exp.TypeParamSpan,
					ReceiverKey:   exp.ReceiverKey,
				})
			}
		}
	}
	return entries
}
