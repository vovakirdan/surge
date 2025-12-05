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
	"surge/internal/observ"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
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
	Symbols *symbols.Result
	Sema    *sema.Result
	Timing  *observ.Report
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

				result := parser.ParseFile(ctx, fileSet, lx, builder, opts)

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
