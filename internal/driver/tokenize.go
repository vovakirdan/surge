package driver

import (
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
	"surge/internal/token"
)

type TokenizeResult struct {
	FileSet *source.FileSet
	File    *source.File
	Tokens  []token.Token
	Bag     *diag.Bag
}

func Tokenize(path string, maxDiagnostics int) (*TokenizeResult, error) {
	// Создаём FileSet и загружаем файл
	fs := source.NewFileSet()
	fileID, err := fs.Load(path)
	if err != nil {
		return nil, err
	}
	file := fs.Get(fileID)

	// Создаём диагностический пакет
	bag := diag.NewBag(maxDiagnostics)

	// Создаём лексер с reporter адаптером для диагностики
	reporterAdapter := &lexer.ReporterAdapter{Bag: bag}
	opts := lexer.Options{
		Reporter: reporterAdapter.Reporter(),
	}
	lx := lexer.New(file, opts)

	// Токенизация: собираем все токены до EOF
	var tokens []token.Token
	for {
		tok := lx.Next()
		tokens = append(tokens, tok)
		if tok.Kind == token.EOF {
			break
		}
	}

	return &TokenizeResult{
		FileSet: fs,
		File:    file,
		Tokens:  tokens,
		Bag:     bag,
	}, nil
}
