package driver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/observ"
	"surge/internal/parser"
	"surge/internal/project"
	"surge/internal/project/dag"
	"surge/internal/source"
	"surge/internal/symbols"

	"fortio.org/safecast"
)

type DiagnoseResult struct {
	FileSet *source.FileSet
	File    *source.File
	FileID  ast.FileID
	Bag     *diag.Bag
	Builder *ast.Builder
	Symbols *symbols.Result
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
		builder, astFile, err = diagnoseParse(fs, file, bag)
		parseNote := ""
		if timer != nil && err == nil && builder != nil && builder.Files != nil {
			fileNode := builder.Files.Get(astFile)
			if fileNode != nil {
				parseNote = fmt.Sprintf("items=%d", len(fileNode.Items))
			}
		}
		end(parseIdx, parseNote)
		if err != nil {
			return nil, err
		}

		graphIdx := begin("imports_graph")
		var moduleExports map[string]*symbols.ModuleExports
		moduleExports, err = runModuleGraph(fs, file, builder, astFile, bag, opts, cache)
		end(graphIdx, "")
		if err != nil {
			return nil, err
		}
		if opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll {
			semaIdx := begin("symbols")
			filePath := ""
			if file != nil {
				filePath = file.Path
			}
			symbolsRes = diagnoseSymbols(builder, astFile, bag, modulePath, filePath, baseDir, moduleExports)
			semaNote := ""
			if timer != nil && symbolsRes != nil && symbolsRes.Table != nil {
				semaNote = fmt.Sprintf("symbols=%d", symbolsRes.Table.Symbols.Len())
			}
			end(semaIdx, semaNote)
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

func diagnoseParse(fs *source.FileSet, file *source.File, bag *diag.Bag) (*ast.Builder, ast.FileID, error) {
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

	return arenas, result.File, nil
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
	Exports  *symbols.ModuleExports
}

var errModuleNotFound = errors.New("module not found")

func runModuleGraph(
	fs *source.FileSet,
	file *source.File,
	builder *ast.Builder,
	astFile ast.FileID,
	bag *diag.Bag,
	opts DiagnoseOptions,
	cache *ModuleCache,
) (map[string]*symbols.ModuleExports, error) {
	if builder == nil {
		return nil, nil
	}

	baseDir := fs.BaseDir()
	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fs, builder, astFile, baseDir, reporter)
	if !ok {
		meta = fallbackModuleMeta(file, baseDir)
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

	exports := collectModuleExports(records, idx, topo, baseDir)

	return exports, nil
}

func analyzeDependencyModule(
	fs *source.FileSet,
	modulePath string,
	baseDir string,
	opts DiagnoseOptions,
	cache *ModuleCache,
) (*moduleRecord, error) {
	filePath := modulePathToFilePath(baseDir, modulePath)
	fileID, err := fs.Load(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errModuleNotFound
		}
		return nil, err
	}
	file := fs.Get(fileID)
	// try cache by modulePath + content hash
	if cache != nil {
		if m, br, fe, ok := cache.Get(modulePath, file.Hash); ok {
			return &moduleRecord{
				Meta:     m,
				Bag:      diag.NewBag(opts.MaxDiagnostics), // пустой bag для согласованности
				Broken:   br,
				FirstErr: fe,
			}, nil
		}
	}
	bag := diag.NewBag(opts.MaxDiagnostics)
	err = diagnoseTokenize(file, bag)
	if err != nil {
		return nil, err
	}
	var builder *ast.Builder
	var astFile ast.FileID
	builder, astFile, err = diagnoseParse(fs, file, bag)
	if err != nil {
		return nil, err
	}
	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fs, builder, astFile, baseDir, reporter)
	if !ok {
		meta = fallbackModuleMeta(file, baseDir)
	}
	broken, firstErr := moduleStatus(bag)
	// fill content hash (на случай fallback)
	if meta.ContentHash == ([32]byte{}) {
		meta.ContentHash = file.Hash
	}
	if cache != nil {
		cache.Put(meta, broken, firstErr)
	}
	return &moduleRecord{
		Meta:     meta,
		Bag:      bag,
		Broken:   broken,
		FirstErr: firstErr,
		Builder:  builder,
		FileID:   astFile,
		File:     file,
	}, nil
}

func collectModuleExports(
	records map[string]*moduleRecord,
	idx dag.ModuleIndex,
	topo *dag.Topo,
	baseDir string,
) map[string]*symbols.ModuleExports {
	if topo == nil || len(topo.Order) == 0 {
		return nil
	}
	exports := make(map[string]*symbols.ModuleExports, len(records))
	for i := len(topo.Order) - 1; i >= 0; i-- {
		id := topo.Order[i]
		path := idx.IDToName[int(id)]
		rec := records[path]
		if rec == nil || rec.Builder == nil || rec.FileID == ast.NoFileID {
			continue
		}
		filePath := ""
		if rec.File != nil {
			filePath = rec.File.Path
		}
		opts := &symbols.ResolveOptions{
			Reporter:      &diag.BagReporter{Bag: rec.Bag},
			ModulePath:    path,
			FilePath:      filePath,
			BaseDir:       baseDir,
			ModuleExports: exports,
		}
		res := symbols.ResolveFile(rec.Builder, rec.FileID, opts)
		rec.Exports = symbols.CollectExports(rec.Builder, res, path)
		if rec.Exports != nil {
			exports[path] = rec.Exports
		}
	}
	return exports
}

func moduleStatus(bag *diag.Bag) (bool, *diag.Diagnostic) {
	if bag == nil {
		return false, nil
	}
	items := bag.Items()
	for i := range items {
		if items[i].Severity >= diag.SevError {
			first := items[i]
			copyFirst := first
			return true, copyFirst
		}
	}
	return false, nil
}

func modulePathToFilePath(baseDir, modulePath string) string {
	rel := filepath.FromSlash(modulePath) + ".sg"
	if baseDir == "" {
		return rel
	}
	return filepath.Join(baseDir, rel)
}

func modulePathForFile(fs *source.FileSet, file *source.File) string {
	if fs == nil || file == nil {
		return ""
	}
	path := file.Path
	baseDir := fs.BaseDir()
	if baseDir != "" {
		if rel, err := source.RelativePath(path, baseDir); err == nil {
			path = rel
		}
	}
	if norm, err := project.NormalizeModulePath(path); err == nil {
		return norm
	}
	return ""
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
