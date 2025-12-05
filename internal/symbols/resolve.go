package symbols

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
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
		Reporter:    opts.Reporter,
		Prelude:     prelude,
		CurrentFile: fileID,
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
	typeParamStack      [][]source.StringID
}

func (fr *fileResolver) pushTypeParams(params []source.StringID) {
	if len(params) == 0 {
		return
	}
	fr.typeParamStack = append(fr.typeParamStack, params)
}

func (fr *fileResolver) popTypeParams() {
	if len(fr.typeParamStack) == 0 {
		return
	}
	fr.typeParamStack = fr.typeParamStack[:len(fr.typeParamStack)-1]
}

func (fr *fileResolver) hasTypeParam(name source.StringID) bool {
	if name == source.NoStringID {
		return false
	}
	for i := len(fr.typeParamStack) - 1; i >= 0; i-- {
		for _, n := range fr.typeParamStack[i] {
			if n == name {
				return true
			}
		}
	}
	return false
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
