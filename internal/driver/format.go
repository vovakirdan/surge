package driver

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/format"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

type FormatOptions struct {
	Check          bool
	MaxDiagnostics int
	Options        format.Options
}

type FormatResult struct {
	Path    string
	Changed bool
	Err     error
}

// FormatPaths formats provided files or directories (recursively collecting .sg files).
// When opts.Check is true, files are not modified; Changed indicates whether formatting
// would update the file contents.
func FormatPaths(paths []string, opts FormatOptions) ([]FormatResult, error) {
	files, err := collectSourceFiles(paths)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("format: no source files found")
	}

	results := make([]FormatResult, 0, len(files))
	for _, path := range files {
		result := FormatResult{Path: path}
		formatted, changed, err := formatSingleFile(path, opts)
		if err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}

		if opts.Check {
			result.Changed = changed
			results = append(results, result)
			continue
		}

		if changed {
			mode := os.FileMode(0o644)
			if info, statErr := os.Stat(path); statErr == nil {
				mode = info.Mode()
			}
			if err := os.WriteFile(path, formatted, mode.Perm()); err != nil {
				result.Err = err
			} else {
				result.Changed = true
			}
		}
		results = append(results, result)
	}

	return results, nil
}

func formatSingleFile(path string, opts FormatOptions) (formatted []byte, changed bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}

	fileSet := source.NewFileSet()
	fileID := fileSet.Add(path, data, 0)
	sf := fileSet.Get(fileID)

	maxDiag := opts.MaxDiagnostics
	if maxDiag <= 0 {
		maxDiag = 256
	}
	bag := diag.NewBag(maxDiag)
	lx := lexer.New(sf, lexer.Options{Reporter: (&lexer.ReporterAdapter{Bag: bag}).Reporter()})
	builder := ast.NewBuilder(ast.Hints{}, nil)

	maxErrors, convErr := safecast.Conv[uint](bag.Cap())
	if convErr != nil {
		maxErrors = 0
	}
	parseRes := parser.ParseFile(fileSet, lx, builder, parser.Options{Reporter: &diag.BagReporter{Bag: bag}, MaxErrors: maxErrors})
	if bag.HasErrors() {
		return nil, false, errors.New("format: parse errors present")
	}

	formatted, err = format.FormatFile(sf, builder, parseRes.File, opts.Options)
	if err != nil {
		return nil, false, err
	}

	changed = !bytesEqual(sf.Content, formatted)
	return formatted, changed, nil
}

func collectSourceFiles(paths []string) ([]string, error) {
	var files []string
	seen := make(map[string]struct{})
	addFile := func(path string) {
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				if filepath.Ext(path) == ".sg" {
					addFile(path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}

		if filepath.Ext(p) == ".sg" {
			addFile(p)
		}
	}

	return files, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
