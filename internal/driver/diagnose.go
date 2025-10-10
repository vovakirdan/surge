package driver

import (
	"surge/internal/diag"
	"surge/internal/lexer"
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

// Diagnose запускает диагностику файла до указанного уровня
func Diagnose(path string, stage DiagnoseStage, maxDiagnostics int) (*DiagnoseResult, error) {
	// Создаём FileSet и загружаем файл
	fs := source.NewFileSet()
	fileID, err := fs.Load(path)
	if err != nil {
		return nil, err
	}
	file := fs.Get(fileID)

	// Создаём диагностический пакет
	bag := diag.NewBag(maxDiagnostics)

	// Запускаем диагностику по стадиям
	switch stage {
	case DiagnoseStageTokenize:
		err = diagnoseTokenize(file, bag)
	case DiagnoseStageSyntax:
		err = diagnoseTokenize(file, bag)
		if err == nil {
			// TODO: добавить диагностику парсера когда он будет готов
			// err = diagnoseParse(file, bag)
		}
	case DiagnoseStageSema:
		fallthrough // пока что обрабатываем как syntax
	case DiagnoseStageAll:
		err = diagnoseTokenize(file, bag)
		if err == nil {
			// TODO: добавить диагностику парсера и семантики
			// err = diagnoseParse(file, bag)
			// if err == nil {
			//     err = diagnoseSema(file, bag)
			// }
		}
	}

	if err != nil {
		return nil, err
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
