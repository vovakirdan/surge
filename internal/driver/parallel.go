package driver

import (
	"context"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

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
	Path   string         // Относительный путь к файлу
	FileID source.FileID  // ID файла в FileSet
	Tokens []token.Token  // Токены файла
	Bag    *diag.Bag      // Диагностики
}

// ParseDirResult содержит результат парсинга одного файла
type ParseDirResult struct {
	Path    string       // Относительный путь к файлу
	FileID  ast.FileID   // ID файла в AST
	Builder *ast.Builder // AST builder с распарсенным файлом
	Bag     *diag.Bag    // Диагностики
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

	for _, path := range files {
		fileID, err := fs.Load(path)
		if err != nil {
			// Если файл не загрузился, всё равно продолжаем
			// Диагностика будет создана при попытке токенизации
			continue
		}
		fileIDs[path] = fileID
	}

	// Настраиваем параллелизм
	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}

	// Результаты
	results := make([]TokenizeDirResult, len(files))
	var mu sync.Mutex

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

			// Получаем файл
			fileID, ok := fileIDs[path]
			if !ok {
				// Файл не загрузился, создаём пустой результат с ошибкой
				mu.Lock()
				results[i] = TokenizeDirResult{
					Path:   path,
					FileID: 0,
					Tokens: nil,
					Bag:    bag,
				}
				mu.Unlock()
				return nil
			}

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

			// Сохраняем результат
			mu.Lock()
			results[i] = TokenizeDirResult{
				Path:   path,
				FileID: fileID,
				Tokens: tokens,
				Bag:    bag,
			}
			mu.Unlock()

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

	for _, path := range files {
		fileID, err := fs.Load(path)
		if err != nil {
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

	// Результаты
	results := make([]ParseDirResult, len(files))
	var mu sync.Mutex

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

			// Получаем файл
			fileID, ok := fileIDs[path]
			if !ok {
				mu.Lock()
				results[i] = ParseDirResult{
					Path:    path,
					FileID:  0,
					Builder: nil,
					Bag:     bag,
				}
				mu.Unlock()
				return nil
			}

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

			// Сохраняем результат
			mu.Lock()
			results[i] = ParseDirResult{
				Path:    path,
				FileID:  result.File,
				Builder: builder,
				Bag:     bag,
			}
			mu.Unlock()

			return nil
		})
	}

	// Ждём завершения всех горутин
	if err := g.Wait(); err != nil {
		return fs, interner, results, err
	}

	return fs, interner, results, nil
}
