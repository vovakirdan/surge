package diagfmt

import (
	"encoding/json"
	"io"
	"surge/internal/diag"
	"surge/internal/source"
)

// LocationJSON представляет местоположение в файле для JSON
type LocationJSON struct {
	File      string `json:"file"`
	StartByte uint32 `json:"start_byte"`
	EndByte   uint32 `json:"end_byte"`
	StartLine uint32 `json:"start_line,omitempty"`
	StartCol  uint32 `json:"start_col,omitempty"`
	EndLine   uint32 `json:"end_line,omitempty"`
	EndCol    uint32 `json:"end_col,omitempty"`
}

// NoteJSON представляет дополнительную заметку для JSON
type NoteJSON struct {
	Message  string       `json:"message"`
	Location LocationJSON `json:"location"`
}

// FixEditJSON представляет одно редактирование для JSON
type FixEditJSON struct {
	Location LocationJSON `json:"location"`
	NewText  string       `json:"new_text"`
}

// FixJSON представляет предложение по исправлению для JSON
type FixJSON struct {
	Title string        `json:"title"`
	Edits []FixEditJSON `json:"edits"`
}

// DiagnosticJSON представляет диагностику в JSON формате
type DiagnosticJSON struct {
	Severity string       `json:"severity"`
	Code     string       `json:"code"`
	Message  string       `json:"message"`
	Location LocationJSON `json:"location"`
	Notes    []NoteJSON   `json:"notes,omitempty"`
	Fixes    []FixJSON    `json:"fixes,omitempty"`
}

// DiagnosticsOutput представляет корневую структуру JSON вывода
type DiagnosticsOutput struct {
	Diagnostics []DiagnosticJSON `json:"diagnostics"`
	Count       int              `json:"count"`
}

// makeLocation создаёт LocationJSON из Span
func makeLocation(span source.Span, fs *source.FileSet, pathMode PathMode, includePositions bool) LocationJSON {
	f := fs.Get(span.File)

	// Форматируем путь согласно режиму
	var path string
	switch pathMode {
	case PathModeAbsolute:
		path = f.FormatPath("absolute", "")
	case PathModeRelative:
		path = f.FormatPath("relative", fs.BaseDir())
	case PathModeBasename:
		path = f.FormatPath("basename", "")
	case PathModeAuto:
		path = f.FormatPath("auto", "")
	default:
		path = f.Path
	}

	loc := LocationJSON{
		File:      path,
		StartByte: span.Start,
		EndByte:   span.End,
	}

	// Добавляем позиции строк/колонок если требуется
	if includePositions {
		startPos, endPos := fs.Resolve(span)
		loc.StartLine = startPos.Line
		loc.StartCol = startPos.Col
		loc.EndLine = endPos.Line
		loc.EndCol = endPos.Col
	}

	return loc
}

// JSON форматирует диагностики в JSON формат.
// Выводит массив диагностик с полной информацией о местоположении, заметках и исправлениях.
func JSON(w io.Writer, bag *diag.Bag, fs *source.FileSet, opts JSONOpts) error {
	diagnostics := make([]DiagnosticJSON, 0, bag.Len())

	items := bag.Items()
	maxItems := len(items)
	if opts.Max > 0 && opts.Max < maxItems {
		maxItems = opts.Max
	}

	for i := 0; i < maxItems; i++ {
		d := items[i]

		// Основная диагностика
		diagJSON := DiagnosticJSON{
			Severity: d.Severity.String(),
			Code:     d.Code.ID(),
			Message:  d.Message,
			Location: makeLocation(d.Primary, fs, opts.PathMode, opts.IncludePositions),
		}

		// Добавляем заметки
		if len(d.Notes) > 0 {
			diagJSON.Notes = make([]NoteJSON, len(d.Notes))
			for j, note := range d.Notes {
				diagJSON.Notes[j] = NoteJSON{
					Message:  note.Msg,
					Location: makeLocation(note.Span, fs, opts.PathMode, opts.IncludePositions),
				}
			}
		}

		// Добавляем исправления
		if len(d.Fixes) > 0 {
			diagJSON.Fixes = make([]FixJSON, len(d.Fixes))
			for j, fix := range d.Fixes {
				fixJSON := FixJSON{
					Title: fix.Title,
					Edits: make([]FixEditJSON, len(fix.Edits)),
				}
				for k, edit := range fix.Edits {
					fixJSON.Edits[k] = FixEditJSON{
						Location: makeLocation(edit.Span, fs, opts.PathMode, opts.IncludePositions),
						NewText:  edit.NewText,
					}
				}
				diagJSON.Fixes[j] = fixJSON
			}
		}

		diagnostics = append(diagnostics, diagJSON)
	}

	output := DiagnosticsOutput{
		Diagnostics: diagnostics,
		Count:       len(diagnostics),
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
