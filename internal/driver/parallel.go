package driver

import (
	"context"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
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
	Path   string
	FileID source.FileID
	Bag    *diag.Bag
}

func DiagnoseDirWithOptions(ctx context.Context, dir string, opts DiagnoseOptions, jobs int) (*source.FileSet, []DiagnoseDirResult, error) {
	files, err := listSGFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	fs := source.NewFileSetWithBase(dir)
	fileIDs := make(map[string]source.FileID, len(files))
	loadErrors := make(map[string]error, len(files))

	for _, p := range files {
		id, err := fs.Load(p)
		if err != nil {
			loadErrors[p] = err
			continue
		}
		fileIDs[p] = id
	}

	if len(files) == 0 {
		return fs, nil, nil
	}

	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}

	results := make([]DiagnoseDirResult, len(files))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(min(jobs, len(files)))

	for i, path := range files {
		i, path := i, path

		g.Go(func() error {
			select {
			case <-gctx.Done():
				return gctx.Err()
			default:
			}

			bag := diag.NewBag(opts.MaxDiagnostics)

			if loadErr, hadErr := loadErrors[path]; hadErr {
				bag.Add(diag.Diagnostic{
					Severity: diag.SevError,
					Code:     diag.IOLoadFileError,
					Message:  "failed to load file: " + loadErr.Error(),
					Primary:  source.Span{},
				})
				if opts.IgnoreWarnings {
					bag.Filter(func(d diag.Diagnostic) bool {
						return d.Severity != diag.SevWarning && d.Severity != diag.SevInfo
					})
				}
				if opts.WarningsAsErrors {
					bag.Transform(func(d diag.Diagnostic) diag.Diagnostic {
						if d.Severity == diag.SevWarning {
							d.Severity = diag.SevError
						}
						return d
					})
					bag.Sort()
				}
				results[i] = DiagnoseDirResult{Path: path, FileID: 0, Bag: bag}
				return nil
			}

			fileID, ok := fileIDs[path]
			if !ok {
				results[i] = DiagnoseDirResult{Path: path, FileID: 0, Bag: bag}
				return nil
			}
			file := fs.Get(fileID)

			switch opts.Stage {
			case DiagnoseStageTokenize:
				_ = diagnoseTokenize(file, bag)
			case DiagnoseStageSyntax:
				if err := diagnoseTokenize(file, bag); err == nil {
					_ = diagnoseParse(fs, file, bag)
				}
			case DiagnoseStageSema, DiagnoseStageAll:
				if err := diagnoseTokenize(file, bag); err == nil {
					_ = diagnoseParse(fs, file, bag)
					// TODO: добавить семантическую диагностику
				}
			default:
				if err := diagnoseTokenize(file, bag); err == nil {
					_ = diagnoseParse(fs, file, bag)
				}
			}

			if opts.IgnoreWarnings {
				bag.Filter(func(d diag.Diagnostic) bool {
					return d.Severity != diag.SevWarning && d.Severity != diag.SevInfo
				})
			}
			if opts.WarningsAsErrors {
				bag.Transform(func(d diag.Diagnostic) diag.Diagnostic {
					if d.Severity == diag.SevWarning {
						d.Severity = diag.SevError
					}
					return d
				})
				bag.Sort()
			}

			results[i] = DiagnoseDirResult{Path: path, FileID: fileID, Bag: bag}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return fs, results, err
	}

	return fs, results, nil
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

// min возвращает минимум из двух чисел
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	fs := source.NewFileSetWithBase(dir)
	fileIDs := make(map[string]source.FileID, len(files))
	loadErrors := make(map[string]error, len(files))

	for _, path := range files {
		fileID, err := fs.Load(path)
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
		i, path := i, path // capture loop variables

		g.Go(func() error {
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
				bag.Add(diag.Diagnostic{
					Severity: diag.SevError,
					Code:     diag.IOLoadFileError, // Generic I/O error
					Message:  "failed to load file: " + loadErr.Error(),
					Primary:  source.Span{}, // Empty span for I/O errors
				})
				return nil
			}

			fileID := fileIDs[path]
			file := fs.Get(fileID)

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
		})
	}

	// Ждём завершения всех горутин
	if err := g.Wait(); err != nil {
		return fs, results, err
	}

	return fs, results, nil
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
	fs := source.NewFileSetWithBase(dir)
	fileIDs := make(map[string]source.FileID, len(files))
	loadErrors := make(map[string]error, len(files))

	for _, path := range files {
		fileID, err := fs.Load(path)
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
		i, path := i, path // capture loop variables

		g.Go(func() error {
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
				bag.Add(diag.Diagnostic{
					Severity: diag.SevError,
					Code:     diag.IOLoadFileError, // Generic I/O error
					Message:  "failed to load file: " + loadErr.Error(),
					Primary:  source.Span{}, // Empty span for I/O errors
				})
				return nil
			}

			fileID := fileIDs[path]
			file := fs.Get(fileID)

			// Создаём builder с общим interner'ом
			builder := ast.NewBuilder(ast.Hints{}, interner)

			// Создаём лексер
			lx := lexer.New(file, lexer.Options{})

			// Парсим файл
			maxErrors := uint(maxDiagnostics)
			if maxDiagnostics == 0 {
				maxErrors = 0 // без лимита
			}

			opts := parser.Options{
				Reporter:  &diag.BagReporter{Bag: bag},
				MaxErrors: maxErrors,
			}

			result := parser.ParseFile(fs, lx, builder, opts)

			// Сохраняем результат (мьютекс не нужен — индекс i уникален)
			results[i] = ParseDirResult{
				Path:    path,
				FileID:  result.File,
				Builder: builder,
				Bag:     bag,
			}

			return nil
		})
	}

	// Ждём завершения всех горутин
	if err := g.Wait(); err != nil {
		return fs, interner, results, err
	}

	return fs, interner, results, nil
}
