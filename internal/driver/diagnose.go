package driver

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/directive"
	"surge/internal/fix"
	"surge/internal/hir"
	"surge/internal/lexer"
	"surge/internal/mono"
	"surge/internal/observ"
	"surge/internal/parser"
	"surge/internal/project"
	"surge/internal/project/dag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/trace"
	"surge/internal/types"

	"fortio.org/safecast"
)

var (
	errModuleNotFound        = errors.New("module not found")
	errCoreNamespaceReserved = errors.New("core namespace reserved")
)

// DiagnoseResult encapsulates the artifacts and diagnostics from a compilation phase.
type DiagnoseResult struct {
	FileSet           *source.FileSet
	File              *source.File
	FileID            ast.FileID
	Bag               *diag.Bag
	Builder           *ast.Builder
	Symbols           *symbols.Result
	Sema              *sema.Result
	Instantiations    *mono.InstantiationMap
	DirectiveRegistry *directive.Registry // Collected directive scenarios
	HIR               *hir.Module         // HIR module (if EmitHIR is enabled)
	TimingReport      observ.Report       // Phase timing report (if enabled)
	rootRecord        *moduleRecord
	moduleRecords     map[string]*moduleRecord
}

// DiagnoseStage определяет уровень диагностики
type DiagnoseStage string

const (
	// DiagnoseStageTokenize runs only the lexer.
	DiagnoseStageTokenize DiagnoseStage = "tokenize"
	// DiagnoseStageSyntax runs up to the parser.
	DiagnoseStageSyntax DiagnoseStage = "syntax"
	// DiagnoseStageSema runs semantic analysis.
	DiagnoseStageSema DiagnoseStage = "sema"
	// DiagnoseStageAll runs the full compilation pipeline.
	DiagnoseStageAll DiagnoseStage = "all"
)

// DiagnoseOptions содержит опции для диагностики
type DiagnoseOptions struct {
	Stage              DiagnoseStage
	MaxDiagnostics     int
	IgnoreWarnings     bool
	WarningsAsErrors   bool
	NoAlienHints       bool // Disable extra alien-hint diagnostics (enabled by default)
	BaseDir            string
	ReadFile           func(string) ([]byte, error)
	RootKind           project.ModuleKind
	EnableTimings      bool
	PhaseObserver      PhaseObserver
	EnableDiskCache    bool                 // Enable persistent disk cache (experimental, adds I/O overhead)
	DirectiveMode      parser.DirectiveMode // Directive processing mode (off, collect, gen, run)
	DirectiveFilter    []string             // Directive namespaces to process (empty = all)
	EmitHIR            bool                 // Build HIR (High-level IR) from AST + sema
	EmitInstantiations bool                 // Capture generic instantiation map (sema artefact)
	KeepArtifacts      bool                 // Retain AST/symbol/semantic data (for analysis snapshots)
	FullModuleGraph    bool                 // Resolve full module graph for directory diagnostics (LSP analysis)
}

// Diagnose запускает диагностику файла до указанного уровня
func Diagnose(ctx context.Context, filePath string, stage DiagnoseStage, maxDiagnostics int) (*DiagnoseResult, error) {
	opts := DiagnoseOptions{
		Stage:          stage,
		MaxDiagnostics: maxDiagnostics,
	}
	return DiagnoseWithOptions(ctx, filePath, &opts)
}

// DiagnoseWithOptions запускает диагностику файла с указанными опциями
func DiagnoseWithOptions(ctx context.Context, filePath string, opts *DiagnoseOptions) (*DiagnoseResult, error) {
	if opts == nil {
		opts = &DiagnoseOptions{}
	}
	// Get tracer from context
	tracer := trace.FromContext(ctx)
	diagSpan := trace.Begin(tracer, trace.ScopeDriver, "diagnose", 0)
	defer diagSpan.End("")

	var timer *observ.Timer
	if opts.EnableTimings {
		timer = observ.NewTimer()
	}
	alienHintsEnabled := !opts.NoAlienHints
	phaseObserver := opts.PhaseObserver
	phaseStarts := make(map[string]time.Time, 8)
	phaseBegin := func(name string) {
		if phaseObserver == nil {
			return
		}
		phaseStarts[name] = time.Now()
		phaseObserver(PhaseEvent{Name: name, Status: PhaseStart})
	}
	phaseEnd := func(name string) {
		if phaseObserver == nil {
			return
		}
		elapsed := time.Duration(0)
		if start, ok := phaseStarts[name]; ok {
			elapsed = time.Since(start)
		}
		phaseObserver(PhaseEvent{Name: name, Status: PhaseEnd, Elapsed: elapsed})
	}
	sharedStrings := source.NewInterner()
	begin := func(name string) int {
		if timer == nil {
			return -1
		}
		return timer.Begin(name)
	}
	end := func(idx int, note string) {
		if timer == nil || idx < 0 {
			return
		}
		timer.End(idx, note)
	}

	phaseBegin("load_file")
	loadIdx := begin("load_file")
	loadSpan := trace.Begin(tracer, trace.ScopePass, "load_file", diagSpan.ID())
	// Создаём FileSet и загружаем файл
	fs := source.NewFileSet()
	if opts.BaseDir != "" {
		fs.SetBaseDir(opts.BaseDir)
	}
	if opts.ReadFile != nil {
		fs.SetReadFile(opts.ReadFile)
	}
	sharedTypes := types.NewInterner()
	fileID, err := fs.Load(filePath)
	loadSpan.End("")
	end(loadIdx, "")
	phaseEnd("load_file")
	if err != nil {
		return nil, err
	}
	file := fs.Get(fileID)
	baseDir := fs.BaseDir()
	modulePath := modulePathForFile(fs, file)

	// Создаём диагностический пакет
	bag := diag.NewBag(opts.MaxDiagnostics)

	var (
		builder       *ast.Builder
		astFile       ast.FileID
		symbolsRes    *symbols.Result
		semaRes       *sema.Result
		rootRec       *moduleRecord
		moduleRecords map[string]*moduleRecord
	)
	var instMap *mono.InstantiationMap
	var instRecorder sema.InstantiationRecorder
	if opts.EmitInstantiations && (opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll) {
		instMap = mono.NewInstantiationMap()
		instRecorder = mono.NewInstantiationMapRecorder(instMap)
	}
	// per-call cache (следующим шагом добавим его в параллельный обход директорий)
	cache := NewModuleCache(256)

	phaseBegin("tokenize")
	tokenIdx := begin("tokenize")
	tokenSpan := trace.Begin(tracer, trace.ScopePass, "tokenize", diagSpan.ID())
	diagnoseTokenize(file, bag)
	tokenNote := ""
	if timer != nil {
		tokenNote = fmt.Sprintf("diags=%d", bag.Len())
	}
	tokenSpan.End(tokenNote)
	end(tokenIdx, tokenNote)
	phaseEnd("tokenize")

	if opts.Stage != DiagnoseStageTokenize {
		phaseBegin("parse")
		parseIdx := begin("parse")
		parseSpan := trace.Begin(tracer, trace.ScopePass, "parse", diagSpan.ID())
		builder, astFile = diagnoseParseWithStrings(ctx, fs, file, bag, sharedStrings, opts.DirectiveMode)
		parseNote := ""
		if timer != nil && builder != nil && builder.Files != nil {
			fileNode := builder.Files.Get(astFile)
			if fileNode != nil {
				parseNote = fmt.Sprintf("items=%d", len(fileNode.Items))
			}
		}
		parseSpan.End(parseNote)
		end(parseIdx, parseNote)
		phaseEnd("parse")

		phaseBegin("imports_graph")
		graphIdx := begin("imports_graph")
		graphSpan := trace.Begin(tracer, trace.ScopePass, "imports_graph", diagSpan.ID())
		var moduleExports map[string]*symbols.ModuleExports
		moduleExports, rootRec, moduleRecords, err = runModuleGraph(ctx, fs, file, builder, astFile, bag, opts, cache, sharedTypes, sharedStrings)
		graphSpan.End("")
		end(graphIdx, "")
		phaseEnd("imports_graph")
		if err != nil {
			return nil, err
		}
		if rootRec != nil && rootRec.Meta != nil && rootRec.Meta.Path != "" {
			modulePath = rootRec.Meta.Path
		}
		if opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll {
			phaseBegin("symbols")
			symbolIdx := begin("symbols")
			symbolSpan := trace.Begin(tracer, trace.ScopePass, "symbols", diagSpan.ID())
			if rootRec != nil {
				if moduleExports == nil {
					moduleExports = make(map[string]*symbols.ModuleExports)
				}
				if exp := resolveModuleRecord(ctx, rootRec, baseDir, moduleExports, sharedTypes, opts, instRecorder); exp != nil {
					moduleExports[rootRec.Meta.Path] = exp
				}
				if sym, ok := rootRec.Symbols[astFile]; ok {
					symbolsRes = &sym
				}
				if sem, ok := rootRec.Sema[astFile]; ok {
					semaRes = sem
				}
			}
			if symbolsRes == nil {
				symFilePath := ""
				if file != nil {
					symFilePath = file.Path
				}
				symbolsRes = diagnoseSymbols(builder, astFile, bag, modulePath, symFilePath, baseDir, moduleExports)
				if moduleExports != nil && symbolsRes != nil {
					if rootExports := symbols.CollectExports(builder, *symbolsRes, modulePath); rootExports != nil {
						moduleExports[modulePath] = rootExports
					}
				}
			}
			symbolNote := ""
			if timer != nil && symbolsRes != nil && symbolsRes.Table != nil {
				symbolNote = fmt.Sprintf("symbols=%d", symbolsRes.Table.Symbols.Len())
			}
			symbolSpan.End(symbolNote)
			end(symbolIdx, symbolNote)
			phaseEnd("symbols")

			if semaRes == nil {
				phaseBegin("sema")
				semaIdx := begin("sema")
				semaSpan := trace.Begin(tracer, trace.ScopePass, "sema", diagSpan.ID())
				semaRes = diagnoseSemaWithTypes(ctx, builder, astFile, bag, moduleExports, symbolsRes, sharedTypes, alienHintsEnabled, instRecorder)
				semaSpan.End("")
				end(semaIdx, "")
				phaseEnd("sema")
			}
		}
	}

	// Применяем фильтрацию и трансформацию диагностик
	if opts.IgnoreWarnings {
		bag.Filter(func(d *diag.Diagnostic) bool {
			return d.Severity != diag.SevWarning && d.Severity != diag.SevInfo
		})
	}

	if opts.WarningsAsErrors {
		bag.Transform(func(d *diag.Diagnostic) *diag.Diagnostic {
			if d.Severity == diag.SevWarning {
				d.Severity = diag.SevError
			}
			return d
		})
		// Пересортировываем после изменения severity
		bag.Sort()
	}

	var timingReport observ.Report
	if timer != nil && opts.EnableTimings {
		report := timer.Report()
		timingReport = report
		appendTimingDiagnostic(bag, timingPayload{
			Kind:    "file",
			Path:    file.Path,
			TotalMS: report.TotalMS,
			Phases:  report.Phases,
		})
	}

	// Collect directive scenarios if directives are enabled
	var directiveRegistry *directive.Registry
	if opts.DirectiveMode >= parser.DirectiveModeCollect && builder != nil && astFile != ast.NoFileID {
		directiveRegistry = directive.NewRegistry()
		directiveRegistry.CollectFromFile(builder, astFile, modulePath, filePath)
	}

	// Build HIR if requested and sema succeeded
	var hirModule *hir.Module
	if opts.EmitHIR && semaRes != nil && builder != nil && astFile != ast.NoFileID {
		phaseBegin("hir")
		hirIdx := begin("hir")
		hirSpan := trace.Begin(tracer, trace.ScopePass, "hir", diagSpan.ID())
		hirModule, _ = hir.Lower(ctx, builder, astFile, semaRes, symbolsRes) //nolint:errcheck // HIR errors are non-fatal
		hirSpan.End("")
		end(hirIdx, "")
		phaseEnd("hir")
	}

	return &DiagnoseResult{
		FileSet:           fs,
		File:              file,
		FileID:            astFile,
		Bag:               bag,
		Builder:           builder,
		Symbols:           symbolsRes,
		Sema:              semaRes,
		Instantiations:    instMap,
		DirectiveRegistry: directiveRegistry,
		HIR:               hirModule,
		TimingReport:      timingReport,
		rootRecord:        rootRec,
		moduleRecords:     moduleRecords,
	}, nil
}

func diagnoseSymbols(builder *ast.Builder, fileID ast.FileID, bag *diag.Bag, modulePath, filePath, baseDir string, exports map[string]*symbols.ModuleExports) *symbols.Result {
	if builder == nil || fileID == ast.NoFileID {
		return nil
	}
	res := symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter:      &diag.BagReporter{Bag: bag},
		Validate:      true,
		ModulePath:    modulePath,
		FilePath:      filePath,
		BaseDir:       baseDir,
		ModuleExports: exports,
	})
	return &res
}

func diagnoseSema(ctx context.Context, builder *ast.Builder, fileID ast.FileID, bag *diag.Bag, exports map[string]*symbols.ModuleExports, symbolsRes *symbols.Result, alienHints bool, insts sema.InstantiationRecorder) *sema.Result {
	if builder == nil || fileID == ast.NoFileID {
		return nil
	}
	opts := sema.Options{
		Reporter:       &diag.BagReporter{Bag: bag},
		Symbols:        symbolsRes,
		Exports:        exports,
		AlienHints:     alienHints,
		Bag:            bag,
		Instantiations: insts,
	}
	res := sema.Check(ctx, builder, fileID, opts)
	return &res
}

func diagnoseSemaWithTypes(ctx context.Context, builder *ast.Builder, fileID ast.FileID, bag *diag.Bag, exports map[string]*symbols.ModuleExports, symbolsRes *symbols.Result, typeInterner *types.Interner, alienHints bool, insts sema.InstantiationRecorder) *sema.Result {
	if builder == nil || fileID == ast.NoFileID {
		return nil
	}
	opts := sema.Options{
		Reporter:       &diag.BagReporter{Bag: bag},
		Symbols:        symbolsRes,
		Exports:        exports,
		Types:          typeInterner,
		AlienHints:     alienHints,
		Bag:            bag,
		Instantiations: insts,
	}
	res := sema.Check(ctx, builder, fileID, opts)
	return &res
}

// diagnoseTokenize выполняет диагностику на уровне лексера
func diagnoseTokenize(file *source.File, bag *diag.Bag) {
	reporterAdapter := &lexer.ReporterAdapter{Bag: bag}
	opts := lexer.Options{
		Reporter: reporterAdapter.Reporter(),
	}
	lx := lexer.New(file, opts)

	// Прогоняем весь файл через лексер
	for {
		tok := lx.Next()
		if tok.Kind.IsEOF() {
			break
		}
	}
}

func diagnoseParse(ctx context.Context, fs *source.FileSet, file *source.File, bag *diag.Bag, directiveMode parser.DirectiveMode) (*ast.Builder, ast.FileID) {
	arenas := ast.NewBuilder(ast.Hints{}, nil)
	return diagnoseParseWithBuilder(ctx, fs, file, bag, arenas, directiveMode)
}

func diagnoseParseWithStrings(ctx context.Context, fs *source.FileSet, file *source.File, bag *diag.Bag, strs *source.Interner, directiveMode parser.DirectiveMode) (*ast.Builder, ast.FileID) {
	arenas := ast.NewBuilder(ast.Hints{}, strs)
	return diagnoseParseWithBuilder(ctx, fs, file, bag, arenas, directiveMode)
}

func diagnoseParseWithBuilder(ctx context.Context, fs *source.FileSet, file *source.File, bag *diag.Bag, arenas *ast.Builder, directiveMode parser.DirectiveMode) (*ast.Builder, ast.FileID) {
	if arenas == nil {
		arenas = ast.NewBuilder(ast.Hints{}, nil)
	}
	lx := lexer.New(file, lexer.Options{})

	maxErrors := uint(bag.Cap())
	if maxErrors == 0 {
		maxErrors = 0 // без лимита для парсера
	}

	opts := parser.Options{
		Reporter:      &diag.BagReporter{Bag: bag},
		MaxErrors:     maxErrors,
		DirectiveMode: directiveMode,
	}

	result := parser.ParseFile(ctx, fs, lx, arenas, opts)

	return arenas, result.File
}

type moduleRecord struct {
	Meta               *project.ModuleMeta
	Bag                *diag.Bag
	Broken             bool
	FirstErr           *diag.Diagnostic
	Builder            *ast.Builder
	Table              *symbols.Table
	FileIDs            []ast.FileID
	Files              []*source.File
	Sema               map[ast.FileID]*sema.Result
	Symbols            map[ast.FileID]symbols.Result
	Exports            *symbols.ModuleExports
	checkedEntrypoints bool
}

func moduleHasExplicitName(meta *project.ModuleMeta) bool {
	if meta == nil {
		return false
	}
	if !meta.HasModulePragma {
		return false
	}
	if meta.Dir == "" {
		return false
	}
	dirBase := filepath.Base(meta.Dir)
	return dirBase != "" && dirBase != meta.Name
}

func runModuleGraph(
	ctx context.Context,
	fs *source.FileSet,
	file *source.File,
	builder *ast.Builder,
	astFile ast.FileID,
	bag *diag.Bag,
	opts *DiagnoseOptions,
	cache *ModuleCache,
	typeInterner *types.Interner,
	strs *source.Interner,
) (exports map[string]*symbols.ModuleExports, rootRec *moduleRecord, moduleRecords map[string]*moduleRecord, err error) {
	if builder == nil {
		return nil, nil, nil, nil
	}

	tracer := trace.FromContext(ctx)
	graphSpan := trace.Begin(tracer, trace.ScopeModule, "module_graph", 0)
	defer graphSpan.End("")

	baseDir := fs.BaseDir()
	stdlibRoot := detectStdlibRootFrom(fs.BaseDir())
	reporter := &diag.BagReporter{Bag: bag}
	dirPath := filepath.Dir(file.Path)
	preloaded := map[string]ast.FileID{
		filepath.ToSlash(file.Path): astFile,
	}
	builder, rootFileIDs, rootFiles, err := parseModuleDir(ctx, fs, dirPath, bag, strs, builder, preloaded, opts.DirectiveMode)
	if err != nil {
		return nil, nil, nil, err
	}
	meta, ok := buildModuleMeta(fs, builder, rootFileIDs, baseDir, reporter)
	if !ok && len(rootFiles) > 0 && rootFiles[0] != nil {
		meta = fallbackModuleMeta(rootFiles[0], baseDir)
	}
	if meta != nil && !meta.HasModulePragma && len(rootFileIDs) > 1 {
		rootFileIDs = []ast.FileID{astFile}
		rootFiles = []*source.File{file}
		meta, ok = buildModuleMeta(fs, builder, rootFileIDs, baseDir, reporter)
		if !ok {
			meta = fallbackModuleMeta(file, baseDir)
		}
		if bag != nil && file != nil {
			targetFileID := file.ID
			bag.Filter(func(d *diag.Diagnostic) bool {
				return d.Primary.File == targetFileID
			})
		}
	}
	if meta != nil && opts.RootKind != project.ModuleKindUnknown {
		meta.Kind = opts.RootKind
	}
	if !validateCoreModule(meta, file, stdlibRoot, reporter) {
		return nil, nil, nil, fmt.Errorf("core namespace reserved")
	}

	records := make(map[string]*moduleRecord)
	broken, firstErr := moduleStatus(bag)
	records[meta.Path] = &moduleRecord{
		Meta:     meta,
		Bag:      bag,
		Broken:   broken,
		FirstErr: firstErr,
		Builder:  builder,
		FileIDs:  rootFileIDs,
		Files:    rootFiles,
	}
	// cache roor
	if cache != nil {
		cache.Put(meta, broken, firstErr)
	}

	missing := make(map[string]struct{})
	seen := make(map[string]struct{})
	processedImports := make(map[string]struct{})
	aliasExports := make(map[string]string)
	queue := []string{meta.Path}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		seen[cur] = struct{}{}
		rec := records[cur]

		moduleSpan := trace.Begin(tracer, trace.ScopeModule, "process_module", graphSpan.ID())
		moduleSpan.WithExtra("path", cur)

		importsCount := 0
		for i := range rec.Meta.Imports {
			imp := rec.Meta.Imports[i]
			if _, ok := processedImports[imp.Path]; ok {
				continue
			}
			if _, ok := records[imp.Path]; ok {
				continue
			}
			if _, miss := missing[imp.Path]; miss {
				continue
			}

			depRec, err := analyzeDependencyModule(ctx, fs, imp.Path, baseDir, stdlibRoot, opts, cache, strs)
			if err != nil {
				if errors.Is(err, errModuleNotFound) {
					missing[imp.Path] = struct{}{}
					continue
				}
				return nil, nil, nil, err
			}
			importedPath := normalizeExportsKey(imp.Path)
			actualPath := normalizeExportsKey(depRec.Meta.Path)
			if importedPath != "" && actualPath != "" && importedPath != actualPath {
				if moduleHasExplicitName(depRec.Meta) && rec != nil && rec.Bag != nil && path.Base(importedPath) != depRec.Meta.Name {
					reporter := &diag.BagReporter{Bag: rec.Bag}
					msg := fmt.Sprintf("module is named %q, not %q", depRec.Meta.Path, imp.Path)
					if b := diag.ReportError(reporter, diag.ProjWrongModuleNameInImport, imp.Span, msg); b != nil {
						fixID := fix.MakeFixID(diag.ProjWrongModuleNameInImport, imp.Span)
						b.WithFixSuggestion(fix.ReplaceSpan(
							"update import path",
							imp.Span,
							depRec.Meta.Path,
							imp.Path,
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindQuickFix),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						))
						b.Emit()
					}
				}
				aliasExports[importedPath] = depRec.Meta.Path
				if rec != nil {
					rec.Meta.Imports[i].Path = depRec.Meta.Path
				}
			}
			processedImports[imp.Path] = struct{}{}
			records[depRec.Meta.Path] = depRec
			if _, ok := seen[depRec.Meta.Path]; !ok {
				queue = append(queue, depRec.Meta.Path)
			}
			importsCount++
		}

		moduleSpan.End(fmt.Sprintf("imports=%d", importsCount))
	}

	if err := ensureStdlibModules(ctx, fs, records, opts, cache, stdlibRoot, typeInterner, strs); err != nil {
		return nil, nil, nil, err
	}

	paths := make([]string, 0, len(records))
	for p := range records {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	metas := make([]*project.ModuleMeta, 0, len(paths))
	nodes := make([]*dag.ModuleNode, 0, len(paths))
	for _, p := range paths {
		rec := records[p]
		metas = append(metas, rec.Meta)
		nodes = append(nodes, &dag.ModuleNode{
			Meta:     rec.Meta,
			Reporter: &diag.BagReporter{Bag: rec.Bag},
			Broken:   rec.Broken,
			FirstErr: rec.FirstErr,
		})
	}

	idx := dag.BuildIndex(metas)
	graph, slots := dag.BuildGraph(idx, nodes)
	topo := dag.ToposortKahn(graph)
	dag.ReportCycles(idx, slots, topo)
	ComputeModuleHashes(idx, graph, slots, topo)
	for i := range slots {
		reporter, ok := slots[i].Reporter.(*diag.BagReporter)
		if !ok || reporter.Bag == nil {
			continue
		}
		brokenNow, firstErrNow := moduleStatus(reporter.Bag)
		if brokenNow {
			slots[i].Broken = true
			if slots[i].FirstErr == nil && firstErrNow != nil {
				slots[i].FirstErr = firstErrNow
			}
		}
	}
	dag.ReportBrokenDeps(idx, slots)

	exports = collectModuleExports(ctx, records, idx, topo, baseDir, meta.Path, typeInterner, opts)
	for alias, target := range aliasExports {
		if exp, ok := exports[normalizeExportsKey(target)]; ok {
			exports[normalizeExportsKey(alias)] = exp
		}
	}

	return exports, records[meta.Path], records, nil
}

func markSymbolsBuiltin(res *symbols.Result) {
	if res == nil || res.Table == nil || res.Table.Symbols == nil {
		return
	}
	count := res.Table.Symbols.Len()
	for i := 1; i <= count; i++ {
		value, convErr := safecast.Conv[uint32](i)
		if convErr != nil {
			panic(fmt.Errorf("symbol id overflow: %w", convErr))
		}
		id := symbols.SymbolID(value)
		if sym := res.Table.Symbols.Get(id); sym != nil {
			sym.Flags |= symbols.SymbolFlagBuiltin
		}
	}
}

func validateCoreModule(meta *project.ModuleMeta, file *source.File, stdlibRoot string, reporter diag.Reporter) bool {
	if meta == nil || file == nil {
		return true
	}
	if meta.Path != "core" && !strings.HasPrefix(meta.Path, "core/") {
		return true
	}
	if stdlibRoot != "" && pathWithin(stdlibRoot, file.Path) {
		return true
	}
	if reporter != nil {
		msg := fmt.Sprintf("module %q is reserved for the standard library", meta.Path)
		span := source.Span{File: file.ID}
		if b := diag.ReportError(reporter, diag.ProjInvalidModulePath, span, msg); b != nil {
			b.Emit()
		}
	}
	return false
}

func fallbackModuleMeta(file *source.File, baseDir string) *project.ModuleMeta {
	filePath := file.Path
	if baseDir != "" {
		if rel, err := source.RelativePath(filePath, baseDir); err == nil {
			filePath = rel
		}
	}
	if norm, err := project.NormalizeModulePath(filePath); err == nil {
		filePath = norm
	}
	lenFileContent, err := safecast.Conv[uint32](len(file.Content))
	if err != nil {
		panic(fmt.Errorf("len file content overflow: %w", err))
	}
	return &project.ModuleMeta{
		Name: filepath.Base(filePath),
		Path: filePath,
		Dir:  filepath.Dir(filePath),
		Kind: project.ModuleKindModule,
		Span: source.Span{File: file.ID, Start: 0, End: lenFileContent},
	}
}
