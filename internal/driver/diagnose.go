package driver

import (
	"path/filepath"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/project"
	"surge/internal/project/dag"
	"surge/internal/source"
)

type DiagnoseResult struct {
	FileSet *source.FileSet
	File    *source.File
	Bag     *diag.Bag
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
	// Создаём FileSet и загружаем файл
	fs := source.NewFileSet()
	fileID, err := fs.Load(path)
	if err != nil {
		return nil, err
	}
	file := fs.Get(fileID)

	// Создаём диагностический пакет
	bag := diag.NewBag(opts.MaxDiagnostics)

	var (
		builder *ast.Builder
		astFile ast.FileID
	)

	// Запускаем диагностику по стадиям
	switch opts.Stage {
	case DiagnoseStageTokenize:
		err = diagnoseTokenize(file, bag)
	case DiagnoseStageSyntax:
		err = diagnoseTokenize(file, bag)
		if err == nil {
			builder, astFile, err = diagnoseParse(fs, file, bag)
		}
	case DiagnoseStageSema:
		fallthrough // пока что обрабатываем как syntax
	case DiagnoseStageAll:
		err = diagnoseTokenize(file, bag)
		if err == nil {
			// TODO: добавить диагностику парсера и семантики
			builder, astFile, err = diagnoseParse(fs, file, bag)
			// if err == nil {
			//     err = diagnoseSema(file, bag)
			// }
		}
	}

	if err != nil {
		return nil, err
	}

	if builder != nil {
		baseDir := fs.BaseDir()
		if baseDir == "" {
			baseDir = filepath.Dir(file.Path)
		}
		reporter := &diag.BagReporter{Bag: bag}
		if meta, ok := buildModuleMeta(fs, builder, astFile, baseDir, reporter); ok {
			metas := []project.ModuleMeta{meta}
			idx := dag.BuildIndex(metas)
			graph, slots := dag.BuildGraph(idx, []dag.ModuleNode{
				{Meta: meta, Reporter: reporter},
			})
			topo := dag.ToposortKahn(graph)
			dag.ReportCycles(idx, slots, topo)
		}
	}

	// Применяем фильтрацию и трансформацию диагностик
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
		// Пересортировываем после изменения severity
		bag.Sort()
	}

	return &DiagnoseResult{
		FileSet: fs,
		File:    file,
		Bag:     bag,
	}, nil
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

	maxErrors := uint(maxDiagnostics)
	if maxErrors == 0 {
		maxErrors = 0
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
