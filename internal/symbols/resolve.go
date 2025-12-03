package symbols

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
)

var intrinsicAllowedNamesList = []string{
	"rt_alloc",
	"rt_free",
	"rt_realloc",
	"rt_memcpy",
	"rt_memmove",
	"next",
	"await",
	"__abs",
	"__add",
	"__sub",
	"__mul",
	"__div",
	"__mod",
	"__index",
	"__index_set",
	"__range",
	"__bit_and",
	"__bit_or",
	"__bit_xor",
	"__shl",
	"__shr",
	"__lt",
	"__le",
	"__eq",
	"__ne",
	"__ge",
	"__gt",
	"__pos",
	"__neg",
	"__not",
	"__min_value",
	"__max_value",
	"__to",
	"__is",
	"__heir",
	"exit",
	"default",
}

var (
	intrinsicAllowedNames = func() map[string]struct{} {
		m := make(map[string]struct{}, len(intrinsicAllowedNamesList))
		for _, name := range intrinsicAllowedNamesList {
			m[name] = struct{}{}
		}
		return m
	}()
	intrinsicAllowedNamesDisplay = strings.Join(intrinsicAllowedNamesList, ", ")
)

// ResolveOptions controls a resolve pass for a single AST file.
type ResolveOptions struct {
	Table         *Table
	Hints         Hints
	Prelude       []PreludeEntry
	Reporter      diag.Reporter
	Validate      bool
	ModulePath    string
	FilePath      string
	BaseDir       string
	ModuleExports map[string]*ModuleExports
	NoStd         bool
	ModuleScope   ScopeID
	DeclareOnly   bool
	ReuseDecls    bool
}

// Result captures resolve artefacts for one file.
type Result struct {
	Table       *Table
	File        ast.FileID
	FileScope   ScopeID
	ItemSymbols map[ast.ItemID][]SymbolID
	ExprSymbols map[ast.ExprID]SymbolID
	ExternSyms  map[ast.ExternMemberID]SymbolID
	ModuleFiles map[ast.FileID]struct{}
}

// ResolveFile walks the AST file and populates the symbol table.
func ResolveFile(builder *ast.Builder, fileID ast.FileID, opts *ResolveOptions) Result {
	if opts == nil {
		opts = &ResolveOptions{}
	}
	noStd := opts.NoStd
	if !noStd && builder != nil {
		if file := builder.Files.Get(fileID); file != nil && file.Pragma.Flags&ast.PragmaFlagNoStd != 0 {
			noStd = true
		}
	}
	var table *Table
	if opts.Table != nil {
		table = opts.Table
	} else {
		table = NewTable(opts.Hints, builder.StringsInterner)
	}

	result := Result{
		Table:       table,
		File:        fileID,
		ItemSymbols: make(map[ast.ItemID][]SymbolID),
		ExprSymbols: make(map[ast.ExprID]SymbolID),
		ExternSyms:  make(map[ast.ExternMemberID]SymbolID),
	}

	file := builder.Files.Get(fileID)
	if file == nil {
		return result
	}

	sourceFile := file.Span.File
	rootScope := opts.ModuleScope
	if !rootScope.IsValid() {
		rootScope = table.FileRoot(sourceFile, file.Span)
	}
	result.FileScope = rootScope

	var prelude []PreludeEntry
	if noStd {
		prelude = mergePrelude(opts.Prelude)
	} else {
		importsPrelude := exportsPrelude(opts.ModuleExports)
		prelude = mergePrelude(append(importsPrelude, opts.Prelude...))
	}
	resolver := NewResolver(table, rootScope, ResolverOptions{
		Reporter: opts.Reporter,
		Prelude:  prelude,
	})

	fr := fileResolver{
		builder:             builder,
		result:              &result,
		resolver:            resolver,
		fileID:              fileID,
		sourceFile:          sourceFile,
		modulePath:          opts.ModulePath,
		filePath:            opts.FilePath,
		baseDir:             opts.BaseDir,
		moduleImports:       make(map[string]source.Span),
		moduleExports:       opts.ModuleExports,
		aliasExports:        make(map[source.StringID]*ModuleExports),
		aliasModulePaths:    make(map[source.StringID]string),
		syntheticImportSyms: make(map[string]SymbolID),
		noStd:               noStd,
		declareOnly:         opts.DeclareOnly,
		reuseDecls:          opts.ReuseDecls,
	}
	fr.injectCoreExports()
	fr.predeclareConstItems(file.Items)
	for _, itemID := range file.Items {
		fr.handleItem(itemID)
	}

	if opts.Validate {
		if err := table.Validate(); err != nil {
			if opts.Reporter != nil {
				msg := fmt.Sprintf("symbol table invariant violation: %v", err)
				diag.ReportError(opts.Reporter, diag.SemaError, file.Span, msg).Emit()
			} else {
				panic(err)
			}
		}
	}

	return result
}

type fileResolver struct {
	builder             *ast.Builder
	result              *Result
	resolver            *Resolver
	fileID              ast.FileID
	sourceFile          source.FileID
	modulePath          string
	filePath            string
	baseDir             string
	moduleImports       map[string]source.Span
	moduleExports       map[string]*ModuleExports
	aliasExports        map[source.StringID]*ModuleExports
	aliasModulePaths    map[source.StringID]string
	syntheticImportSyms map[string]SymbolID
	noStd               bool
	declareOnly         bool
	reuseDecls          bool
}

func (fr *fileResolver) handleExtern(itemID ast.ItemID, block *ast.ExternBlock) {
	if block.MembersCount == 0 || !block.MembersStart.IsValid() {
		return
	}
	receiverKey := makeTypeKey(fr.builder, block.Target)
	start := uint32(block.MembersStart)
	for offset := range block.MembersCount {
		memberID := ast.ExternMemberID(start + offset)
		member := fr.builder.Items.ExternMember(memberID)
		if member == nil || member.Kind != ast.ExternMemberFn {
			continue
		}
		fn := fr.builder.Items.FnByPayload(member.Fn)
		if fn == nil {
			continue
		}
		fr.declareExternFn(itemID, memberID, receiverKey, fn)
		fr.walkFn(ScopeOwner{
			Kind:       ScopeOwnerItem,
			SourceFile: fr.sourceFile,
			ASTFile:    fr.fileID,
			Item:       itemID,
			Extern:     memberID,
		}, fn)
	}
}

func (fr *fileResolver) reportMissingOverload(
	name source.StringID,
	span, keywordSpan source.Span,
	existing []SymbolID,
	newSig *FunctionSignature,
) {
	reporter := fr.resolver.reporter
	if reporter == nil {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	msg := fmt.Sprintf("function '%s' redeclared without @overload or @override", nameStr)
	b := diag.ReportError(reporter, diag.SemaFnOverride, keywordSpan.Cover(span), msg)
	if b == nil {
		return
	}
	insert := keywordSpan
	if insert == (source.Span{}) {
		insert = span
	}
	insert = insert.ZeroideToStart()
	fixID := fix.MakeFixID(diag.SemaFnOverride, insert)

	suggestionText := "@overload "
	suggestionTitle := "mark function as overload"
	if newSig != nil && fr.result != nil && fr.result.Table != nil {
		for _, id := range existing {
			sym := fr.result.Table.Symbols.Get(id)
			if sym == nil {
				continue
			}
			if sym.Flags&SymbolFlagBuiltin != 0 {
				continue
			}
			if signaturesEqual(sym.Signature, newSig) {
				suggestionText = "@override "
				suggestionTitle = "mark function as override"
				break
			}
		}
	}

	b.WithFixSuggestion(fix.InsertText(
		suggestionTitle,
		insert,
		suggestionText,
		"",
		fix.WithID(fixID),
		fix.WithKind(diag.FixKindRefactor),
		fix.WithApplicability(diag.FixApplicabilitySafeWithHeuristics),
	))
	fr.attachPreviousNotes(b, existing)
	b.Emit()
}

func (fr *fileResolver) predeclareConstItems(items []ast.ItemID) {
	if fr.builder == nil || fr.resolver == nil {
		return
	}
	for _, itemID := range items {
		item := fr.builder.Items.Get(itemID)
		if item == nil || item.Kind != ast.ItemConst {
			continue
		}
		constItem, ok := fr.builder.Items.Const(itemID)
		if !ok || constItem == nil {
			continue
		}
		fr.declareConstItem(itemID, constItem)
	}
}

func (fr *fileResolver) reportInvalidOverride(name source.StringID, span source.Span, message string, existing []SymbolID) {
	reporter := fr.resolver.reporter
	if reporter == nil {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	msg := fmt.Sprintf("invalid override for '%s': %s", nameStr, message)
	b := diag.ReportError(reporter, diag.SemaFnOverride, span, msg)
	if b == nil {
		return
	}
	fr.attachPreviousNotes(b, existing)
	b.Emit()
}

func (fr *fileResolver) attachPreviousNotes(b *diag.ReportBuilder, existing []SymbolID) {
	if b == nil {
		return
	}
	for _, id := range existing {
		sym := fr.result.Table.Symbols.Get(id)
		if sym == nil || sym.Span == (source.Span{}) {
			continue
		}
		b.WithNote(sym.Span, "previous declaration here")
	}
}

func (fr *fileResolver) appendItemSymbol(item ast.ItemID, id SymbolID) {
	if !id.IsValid() {
		return
	}
	fr.result.ItemSymbols[item] = append(fr.result.ItemSymbols[item], id)
}

func (fr *fileResolver) appendExternSymbol(member ast.ExternMemberID, id SymbolID) {
	if !member.IsValid() || !id.IsValid() {
		return
	}
	if fr.result.ExternSyms == nil {
		fr.result.ExternSyms = make(map[ast.ExternMemberID]SymbolID)
	}
	fr.result.ExternSyms[member] = id
}

func preferSpan(primary, fallback source.Span) source.Span {
	if primary != (source.Span{}) {
		return primary
	}
	return fallback
}

func fnNameSpan(fn *ast.FnItem) source.Span {
	if fn == nil {
		return source.Span{}
	}
	if fn.NameSpan != (source.Span{}) {
		return fn.NameSpan
	}
	if fn.FnKeywordSpan != (source.Span{}) && fn.ParamsSpan != (source.Span{}) && fn.FnKeywordSpan.File == fn.ParamsSpan.File {
		if fn.ParamsSpan.Start >= fn.FnKeywordSpan.End {
			return source.Span{
				File:  fn.FnKeywordSpan.File,
				Start: fn.FnKeywordSpan.End,
				End:   fn.ParamsSpan.Start,
			}
		}
	}
	return fn.Span
}

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
		if !strings.HasPrefix(modulePath, "core/") {
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

func (fr *fileResolver) moduleAllowsIntrinsic() bool {
	if isCoreIntrinsicsModule(fr.modulePath) {
		return true
	}
	if strings.Trim(fr.modulePath, "/") == "core/task" {
		return true
	}
	if fr.filePath == "" {
		return false
	}
	path := filepath.ToSlash(fr.filePath)
	path = strings.TrimSuffix(path, ".sg")
	path = strings.TrimSuffix(path, "/")
	return strings.HasSuffix(path, "/core/intrinsics") || path == "core/intrinsics"
}

func isCoreIntrinsicsModule(path string) bool {
	if path == "" {
		return false
	}
	return strings.Trim(path, "/") == "core/intrinsics"
}

func isProtectedModule(path string) bool {
	if path == "" {
		return false
	}
	trimmed := strings.Trim(path, "/")
	return trimmed == "core" || strings.HasPrefix(trimmed, "core/") || trimmed == "stdlib" || strings.HasPrefix(trimmed, "stdlib/")
}

func exportsPrelude(exports map[string]*ModuleExports) []PreludeEntry {
	if len(exports) == 0 {
		return nil
	}
	entries := make([]PreludeEntry, 0, 8)
	modulePaths := make([]string, 0, len(exports))
	for modulePath, moduleExports := range exports {
		if moduleExports == nil || !strings.HasPrefix(modulePath, "core/") {
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

func (fr *fileResolver) intrinsicNameAllowed(name source.StringID) bool {
	if name == source.NoStringID || fr.builder == nil || fr.builder.StringsInterner == nil {
		return false
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	_, ok := intrinsicAllowedNames[nameStr]
	return ok
}

func (fr *fileResolver) reportIntrinsicError(name source.StringID, span source.Span, code diag.Code, detail string) {
	if fr.resolver == nil || fr.resolver.reporter == nil {
		return
	}
	nameStr := fr.builder.StringsInterner.MustLookup(name)
	msg := fmt.Sprintf("invalid intrinsic '%s': %s", nameStr, detail)
	if b := diag.ReportError(fr.resolver.reporter, code, span, msg); b != nil {
		b.Emit()
	}
}
