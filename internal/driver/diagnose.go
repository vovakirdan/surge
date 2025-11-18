package driver

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/observ"
	"surge/internal/parser"
	"surge/internal/project"
	"surge/internal/project/dag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"

	"fortio.org/safecast"
)

var (
	errModuleNotFound        = errors.New("module not found")
	errCoreNamespaceReserved = errors.New("core namespace reserved")
)

type DiagnoseResult struct {
	FileSet *source.FileSet
	File    *source.File
	FileID  ast.FileID
	Bag     *diag.Bag
	Builder *ast.Builder
	Symbols *symbols.Result
	Sema    *sema.Result
}

// DiagnoseStage определяет уровень диагностики
type DiagnoseStage string

const (
	DiagnoseStageTokenize DiagnoseStage = "tokenize"
	DiagnoseStageSyntax   DiagnoseStage = "syntax"
	DiagnoseStageSema     DiagnoseStage = "sema"
	DiagnoseStageAll      DiagnoseStage = "all"
)

// DiagnoseOptions содержит опции для диагностики
type DiagnoseOptions struct {
	Stage            DiagnoseStage
	MaxDiagnostics   int
	IgnoreWarnings   bool
	WarningsAsErrors bool
	EnableTimings    bool
}

// Diagnose запускает диагностику файла до указанного уровня
func Diagnose(path string, stage DiagnoseStage, maxDiagnostics int) (*DiagnoseResult, error) {
	opts := DiagnoseOptions{
		Stage:          stage,
		MaxDiagnostics: maxDiagnostics,
	}
	return DiagnoseWithOptions(path, opts)
}

// DiagnoseWithOptions запускает диагностику файла с указанными опциями
func DiagnoseWithOptions(path string, opts DiagnoseOptions) (*DiagnoseResult, error) {
	var timer *observ.Timer
	if opts.EnableTimings {
		timer = observ.NewTimer()
	}
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

	loadIdx := begin("load_file")
	// Создаём FileSet и загружаем файл
	fs := source.NewFileSet()
	sharedTypes := types.NewInterner()
	fileID, err := fs.Load(path)
	end(loadIdx, "")
	if err != nil {
		return nil, err
	}
	file := fs.Get(fileID)
	baseDir := fs.BaseDir()
	modulePath := modulePathForFile(fs, file)

	// Создаём диагностический пакет
	bag := diag.NewBag(opts.MaxDiagnostics)

	var (
		builder    *ast.Builder
		astFile    ast.FileID
		symbolsRes *symbols.Result
		semaRes    *sema.Result
	)
	// per-call cache (следующим шагом добавим его в параллельный обход директорий)
	cache := NewModuleCache(256)

	tokenIdx := begin("tokenize")
	err = diagnoseTokenize(file, bag)
	tokenNote := ""
	if timer != nil {
		tokenNote = fmt.Sprintf("diags=%d", bag.Len())
	}
	end(tokenIdx, tokenNote)
	if err != nil {
		return nil, err
	}

	if opts.Stage != DiagnoseStageTokenize {
		parseIdx := begin("parse")
		builder, astFile = diagnoseParse(fs, file, bag)
		parseNote := ""
		if timer != nil && builder != nil && builder.Files != nil {
			fileNode := builder.Files.Get(astFile)
			if fileNode != nil {
				parseNote = fmt.Sprintf("items=%d", len(fileNode.Items))
			}
		}
		end(parseIdx, parseNote)

		graphIdx := begin("imports_graph")
		var moduleExports map[string]*symbols.ModuleExports
		moduleExports, err = runModuleGraph(fs, file, builder, astFile, bag, opts, cache, sharedTypes)
		end(graphIdx, "")
		if err != nil {
			return nil, err
		}
		if opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll {
			symbolIdx := begin("symbols")
			filePath := ""
			if file != nil {
				filePath = file.Path
			}
			symbolsRes = diagnoseSymbols(builder, astFile, bag, modulePath, filePath, baseDir, moduleExports)
			if moduleExports != nil && symbolsRes != nil {
				if rootExports := symbols.CollectExports(builder, *symbolsRes, modulePath); rootExports != nil {
					moduleExports[modulePath] = rootExports
				}
			}
			symbolNote := ""
			if timer != nil && symbolsRes != nil && symbolsRes.Table != nil {
				symbolNote = fmt.Sprintf("symbols=%d", symbolsRes.Table.Symbols.Len())
			}
			end(symbolIdx, symbolNote)

			semaIdx := begin("sema")
			semaRes = diagnoseSemaWithTypes(builder, astFile, bag, moduleExports, symbolsRes, sharedTypes)
			end(semaIdx, "")
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

	if timer != nil && opts.EnableTimings {
		report := timer.Report()
		appendTimingDiagnostic(bag, timingPayload{
			Kind:    "file",
			Path:    file.Path,
			TotalMS: report.TotalMS,
			Phases:  report.Phases,
		})
	}

	return &DiagnoseResult{
		FileSet: fs,
		File:    file,
		FileID:  astFile,
		Bag:     bag,
		Builder: builder,
		Symbols: symbolsRes,
		Sema:    semaRes,
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

func diagnoseSema(builder *ast.Builder, fileID ast.FileID, bag *diag.Bag, exports map[string]*symbols.ModuleExports, symbolsRes *symbols.Result) *sema.Result {
	if builder == nil || fileID == ast.NoFileID {
		return nil
	}
	opts := sema.Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  symbolsRes,
		Exports:  exports,
	}
	res := sema.Check(builder, fileID, opts)
	return &res
}

func diagnoseSemaWithTypes(builder *ast.Builder, fileID ast.FileID, bag *diag.Bag, exports map[string]*symbols.ModuleExports, symbolsRes *symbols.Result, typeInterner *types.Interner) *sema.Result {
	if builder == nil || fileID == ast.NoFileID {
		return nil
	}
	opts := sema.Options{
		Reporter: &diag.BagReporter{Bag: bag},
		Symbols:  symbolsRes,
		Exports:  exports,
		Types:    typeInterner,
	}
	res := sema.Check(builder, fileID, opts)
	return &res
}

// diagnoseTokenize выполняет диагностику на уровне лексера
func diagnoseTokenize(file *source.File, bag *diag.Bag) error {
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

	return nil
}

func diagnoseParse(fs *source.FileSet, file *source.File, bag *diag.Bag) (*ast.Builder, ast.FileID) {
	lx := lexer.New(file, lexer.Options{})
	arenas := ast.NewBuilder(ast.Hints{}, nil)

	maxErrors := uint(bag.Cap())
	if maxErrors == 0 {
		maxErrors = 0 // без лимита для парсера
	}

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: maxErrors,
	}

	result := parser.ParseFile(fs, lx, arenas, opts)

	return arenas, result.File
}

type ParseResult struct {
	FileSet *source.FileSet
	File    *source.File
	Builder *ast.Builder
	FileID  ast.FileID
	Bag     *diag.Bag
}

func Parse(path string, maxDiagnostics int) (*ParseResult, error) {
	fs := source.NewFileSet()
	fileID, err := fs.Load(path)
	if err != nil {
		return nil, err
	}
	file := fs.Get(fileID)

	bag := diag.NewBag(maxDiagnostics)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, nil)

	var maxErrors uint
	maxErrors, err = safecast.Conv[uint](maxDiagnostics)
	if err != nil {
		return nil, err
	}

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: maxErrors,
	}

	result := parser.ParseFile(fs, lx, builder, opts)

	return &ParseResult{
		FileSet: fs,
		File:    file,
		Builder: builder,
		FileID:  result.File,
		Bag:     bag,
	}, nil
}

type moduleRecord struct {
	Meta     *project.ModuleMeta
	Bag      *diag.Bag
	Broken   bool
	FirstErr *diag.Diagnostic
	Builder  *ast.Builder
	FileID   ast.FileID
	File     *source.File
	Sema     *sema.Result
	Exports  *symbols.ModuleExports
}

func runModuleGraph(
	fs *source.FileSet,
	file *source.File,
	builder *ast.Builder,
	astFile ast.FileID,
	bag *diag.Bag,
	opts DiagnoseOptions,
	cache *ModuleCache,
	typeInterner *types.Interner,
) (map[string]*symbols.ModuleExports, error) {
	if builder == nil {
		return nil, nil
	}

	baseDir := fs.BaseDir()
	stdlibRoot := detectStdlibRoot(baseDir)
	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fs, builder, astFile, baseDir, reporter)
	if !ok {
		meta = fallbackModuleMeta(file, baseDir)
	}
	if !validateCoreModule(meta, file, stdlibRoot, reporter) {
		return nil, fmt.Errorf("core namespace reserved")
	}

	records := make(map[string]*moduleRecord)
	broken, firstErr := moduleStatus(bag)
	records[meta.Path] = &moduleRecord{
		Meta:     meta,
		Bag:      bag,
		Broken:   broken,
		FirstErr: firstErr,
		Builder:  builder,
		FileID:   astFile,
		File:     file,
	}
	// cache roor
	if cache != nil {
		cache.Put(meta, broken, firstErr)
	}

	missing := make(map[string]struct{})
	queue := []string{meta.Path}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		rec := records[cur]
		for _, imp := range rec.Meta.Imports {
			if _, ok := records[imp.Path]; ok {
				continue
			}
			if _, miss := missing[imp.Path]; miss {
				continue
			}

			depRec, err := analyzeDependencyModule(fs, imp.Path, baseDir, opts, cache)
			if err != nil {
				if errors.Is(err, errModuleNotFound) {
					missing[imp.Path] = struct{}{}
					continue
				}
				return nil, err
			}
			records[depRec.Meta.Path] = depRec
			queue = append(queue, depRec.Meta.Path)
		}
	}

	if err := ensureStdlibModules(fs, records, opts, cache, stdlibRoot, typeInterner); err != nil {
		return nil, err
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

	exports := collectModuleExports(records, idx, topo, baseDir, meta.Path)

	return exports, nil
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
	if !strings.HasPrefix(meta.Path, "core/") {
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
	path := file.Path
	if baseDir != "" {
		if rel, err := source.RelativePath(path, baseDir); err == nil {
			path = rel
		}
	}
	if norm, err := project.NormalizeModulePath(path); err == nil {
		path = norm
	}
	lenFileContent, err := safecast.Conv[uint32](len(file.Content))
	if err != nil {
		panic(fmt.Errorf("len file content overflow: %w", err))
	}
	return &project.ModuleMeta{
		Path: path,
		Span: source.Span{File: file.ID, Start: 0, End: lenFileContent},
	}
}
