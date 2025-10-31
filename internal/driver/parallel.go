package driver

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"fortio.org/safecast"
	"golang.org/x/sync/errgroup"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/project"
	"surge/internal/project/dag"
	"surge/internal/source"
	"surge/internal/token"
)

// TokenizeDirResult содержит результат токенизации одного файла
type TokenizeDirResult struct {
	Path   string        // Относительный путь к файлу
	FileID source.FileID // ID файла в FileSet
	Tokens []token.Token // Токены файла
	Bag    *diag.Bag     // Диагностики
}

// ParseDirResult содержит результат парсинга одного файла
type ParseDirResult struct {
	Path    string       // Относительный путь к файлу
	FileID  ast.FileID   // ID файла в AST
	Builder *ast.Builder // AST builder с распарсенным файлом
	Bag     *diag.Bag    // Диагностики
}

type DiagnoseDirResult struct {
	Path    string
	FileID  source.FileID
	Bag     *diag.Bag
	Builder *ast.Builder
	ASTFile ast.FileID
}

func DiagnoseDirWithOptions(ctx context.Context, dir string, opts DiagnoseOptions, jobs int) (*source.FileSet, []DiagnoseDirResult, error) {
	files, err := listSGFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	fileSet := source.NewFileSetWithBase(dir)
	fileIDs := make(map[string]source.FileID, len(files))
	loadErrors := make(map[string]error, len(files))

	for _, p := range files {
		id, err := fileSet.Load(p)
		if err != nil {
			loadErrors[p] = err
			continue
		}
		fileIDs[p] = id
	}

	if len(files) == 0 {
		return fileSet, nil, nil
	}

	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}

	results := make([]DiagnoseDirResult, len(files))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(min(jobs, len(files)))

	for i, path := range files {
		g.Go(func(i int, path string) func() error {
			return func() error {
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}

				bag := diag.NewBag(opts.MaxDiagnostics)
				var (
					builder *ast.Builder
					astFile ast.FileID
				)

				if loadErr, hadErr := loadErrors[path]; hadErr {
					bag.Add(&diag.Diagnostic{
						Severity: diag.SevError,
						Code:     diag.IOLoadFileError,
						Message:  "failed to load file: " + loadErr.Error(),
						Primary:  source.Span{},
					})
					results[i] = DiagnoseDirResult{Path: path, FileID: 0, Bag: bag}
					return nil
				}

				fileID, ok := fileIDs[path]
				if !ok {
					results[i] = DiagnoseDirResult{Path: path, FileID: 0, Bag: bag}
					return nil
				}
				file := fileSet.Get(fileID)

				switch opts.Stage {
				case DiagnoseStageTokenize:
					if err := diagnoseTokenize(file, bag); err != nil {
						return err
					}
				case DiagnoseStageSyntax:
					if err := diagnoseTokenize(file, bag); err == nil {
						builder, astFile, err = diagnoseParse(fileSet, file, bag)
						if err != nil {
							return err
						}
					}
				case DiagnoseStageSema, DiagnoseStageAll:
					if err := diagnoseTokenize(file, bag); err == nil {
						builder, astFile, err = diagnoseParse(fileSet, file, bag)
						if err != nil {
							return err
						}
						// TODO: добавить семантическую диагностику
					}
				default:
					if err := diagnoseTokenize(file, bag); err == nil {
						builder, astFile, err = diagnoseParse(fileSet, file, bag)
						if err != nil {
							return err
						}
					}
				}

				results[i] = DiagnoseDirResult{
					Path:    path,
					FileID:  fileID,
					Bag:     bag,
					Builder: builder,
					ASTFile: astFile,
				}
				return nil
			}
		}(i, path))
	}

	if err := g.Wait(); err != nil {
		return fileSet, results, err
	}

	if opts.Stage == DiagnoseStageSyntax || opts.Stage == DiagnoseStageSema || opts.Stage == DiagnoseStageAll {
		baseDir := fileSet.BaseDir()
		type entry struct {
			meta project.ModuleMeta
			node dag.ModuleNode
		}
		entries := make([]*entry, 0, len(results))
		for i := range results {
			res := &results[i]
			if res.Bag == nil || res.Builder == nil {
				continue
			}
			reporter := &diag.BagReporter{Bag: res.Bag}
			meta, ok := buildModuleMeta(fileSet, res.Builder, res.ASTFile, baseDir, reporter)
			if !ok {
				file := fileSet.Get(res.FileID)
				meta = fallbackModuleMeta(file, baseDir)
			}
			broken, firstErr := moduleStatus(res.Bag)
			entries = append(entries, &entry{
				meta: meta,
				node: dag.ModuleNode{
					Meta:     meta,
					Reporter: reporter,
					Broken:   broken,
					FirstErr: firstErr,
				},
			})
		}
		if len(entries) > 0 {
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].meta.Path < entries[j].meta.Path
			})
			metas := make([]project.ModuleMeta, 0, len(entries))
			nodes := make([]*dag.ModuleNode, 0, len(entries))
			for _, e := range entries {
				metas = append(metas, e.meta)
				nodes = append(nodes, &e.node)
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
		}
	}

	for i := range results {
		bag := results[i].Bag
		if bag == nil {
			continue
		}
		results[i].Builder = nil
		results[i].ASTFile = 0
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
			bag.Sort()
		}
	}

	return fileSet, results, nil
}

// listSGFiles возвращает отсортированный список всех *.sg файлов в директории
func listSGFiles(dir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".sg") {
			files = append(files, path)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Сортируем для детерминированного порядка
	sort.Strings(files)
	return files, nil
}

// TokenizeDir токенизирует все *.sg файлы в директории параллельно
func TokenizeDir(ctx context.Context, dir string, maxDiagnostics, jobs int) (*source.FileSet, []TokenizeDirResult, error) {
	// Собираем список файлов
	files, err := listSGFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	if len(files) == 0 {
		return source.NewFileSetWithBase(dir), nil, nil
	}

	// Создаём FileSet и предзагружаем все файлы
	fileSet := source.NewFileSetWithBase(dir)
	fileIDs := make(map[string]source.FileID, len(files))
	loadErrors := make(map[string]error, len(files))

	for _, path := range files {
		fileID, err := fileSet.Load(path)
		if err != nil {
			// Сохраняем ошибку загрузки для последующей обработки
			loadErrors[path] = err
			continue
		}
		fileIDs[path] = fileID
	}

	// Настраиваем параллелизм
	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}

	// Результаты (индексы уникальны для каждой горутины, мьютекс не нужен)
	results := make([]TokenizeDirResult, len(files))

	// Параллельная токенизация
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(min(jobs, len(files)))

	for i, path := range files {
		g.Go(func(i int, path string) func() error {
			return func() error {
				// Проверка отмены
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}

				// Создаём bag для диагностик
				bag := diag.NewBag(maxDiagnostics)

				// Проверяем ошибку загрузки
				if loadErr, hadError := loadErrors[path]; hadError {
					// Файл не загрузился, создаём результат с ошибкой I/O
					results[i] = TokenizeDirResult{
						Path:   path,
						FileID: 0,
						Tokens: nil,
						Bag:    bag,
					}
					// Добавляем диагностику в bag
					bag.Add(&diag.Diagnostic{
						Severity: diag.SevError,
						Code:     diag.IOLoadFileError, // Generic I/O error
						Message:  "failed to load file: " + loadErr.Error(),
						Primary:  source.Span{}, // Empty span for I/O errors
					})
					return nil
				}

				fileID := fileIDs[path]
				file := fileSet.Get(fileID)

				// Создаём лексер
				reporter := (&lexer.ReporterAdapter{Bag: bag}).Reporter()
				lx := lexer.New(file, lexer.Options{Reporter: reporter})

				// Собираем токены
				var tokens []token.Token
				for {
					tok := lx.Next()
					tokens = append(tokens, tok)
					if tok.Kind == token.EOF {
						break
					}
				}

				// Сохраняем результат (мьютекс не нужен — индекс i уникален)
				results[i] = TokenizeDirResult{
					Path:   path,
					FileID: fileID,
					Tokens: tokens,
					Bag:    bag,
				}

				return nil
			}
		}(i, path))
	}

	// Ждём завершения всех горутин
	if err := g.Wait(); err != nil {
		return fileSet, results, err
	}

	return fileSet, results, nil
}

// ParseDir парсит все *.sg файлы в директории параллельно
func ParseDir(ctx context.Context, dir string, maxDiagnostics, jobs int) (*source.FileSet, *source.Interner, []ParseDirResult, error) {
	// Собираем список файлов
	files, err := listSGFiles(dir)
	if err != nil {
		return nil, nil, nil, err
	}

	if len(files) == 0 {
		return source.NewFileSetWithBase(dir), source.NewInterner(), nil, nil
	}

	// Создаём FileSet и предзагружаем все файлы
	fileSet := source.NewFileSetWithBase(dir)
	fileIDs := make(map[string]source.FileID, len(files))
	loadErrors := make(map[string]error, len(files))

	for _, path := range files {
		var fileID source.FileID
		fileID, err = fileSet.Load(path)
		if err != nil {
			// Сохраняем ошибку загрузки для последующей обработки
			loadErrors[path] = err
			continue
		}
		fileIDs[path] = fileID
	}

	// Создаём общий потокобезопасный interner
	interner := source.NewInterner()

	// Настраиваем параллелизм
	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}

	// Результаты (индексы уникальны для каждой горутины, мьютекс не нужен)
	results := make([]ParseDirResult, len(files))

	// Параллельный парсинг
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(min(jobs, len(files)))

	for i, path := range files {
		g.Go(func(i int, path string) func() error {
			return func() error {
				// Проверка отмены
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}

				// Создаём bag для диагностик
				bag := diag.NewBag(maxDiagnostics)

				// Проверяем ошибку загрузки
				if loadErr, hadError := loadErrors[path]; hadError {
					// Файл не загрузился, создаём результат с ошибкой I/O
					results[i] = ParseDirResult{
						Path:    path,
						FileID:  0,
						Builder: nil,
						Bag:     bag,
					}
					// Добавляем диагностику в bag
					bag.Add(&diag.Diagnostic{
						Severity: diag.SevError,
						Code:     diag.IOLoadFileError, // Generic I/O error
						Message:  "failed to load file: " + loadErr.Error(),
						Primary:  source.Span{}, // Empty span for I/O errors
					})
					return nil
				}

				fileID := fileIDs[path]
				file := fileSet.Get(fileID)

				// Создаём builder с общим interner'ом
				builder := ast.NewBuilder(ast.Hints{}, interner)

				// Создаём лексер
				lx := lexer.New(file, lexer.Options{})

				// Парсим файл
				var maxErrors uint
				maxErrors, err = safecast.Conv[uint](maxDiagnostics)
				if err != nil {
					panic(fmt.Errorf("maxDiagnostics overflow: %w", err))
				}

				opts := parser.Options{
					Reporter:  &diag.BagReporter{Bag: bag},
					MaxErrors: maxErrors,
				}

				result := parser.ParseFile(fileSet, lx, builder, opts)

				// Сохраняем результат (мьютекс не нужен — индекс i уникален)
				results[i] = ParseDirResult{
					Path:    path,
					FileID:  result.File,
					Builder: builder,
					Bag:     bag,
				}

				return nil
			}
		}(i, path))
	}

	// Ждём завершения всех горутин
	if err := g.Wait(); err != nil {
		return fileSet, interner, results, err
	}

	return fileSet, interner, results, nil
}
