package diagfmt

import (
	"encoding/json"
	"io"
	"sort"

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
	Location    LocationJSON `json:"location"`
	NewText     string       `json:"new_text"`
	OldText     string       `json:"old_text,omitempty"`
	BeforeLines []string     `json:"before_lines,omitempty"`
	AfterLines  []string     `json:"after_lines,omitempty"`
}

// FixJSON представляет предложение по исправлению для JSON
type FixJSON struct {
	ID            string        `json:"id,omitempty"`
	Title         string        `json:"title"`
	Kind          string        `json:"kind"`
	Applicability string        `json:"applicability"`
	IsPreferred   bool          `json:"is_preferred,omitempty"`
	BuildError    string        `json:"build_error,omitempty"`
	Edits         []FixEditJSON `json:"edits,omitempty"`
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

// BuildDiagnosticsOutput формирует структуру JSON-вывода без сериализации.
func BuildDiagnosticsOutput(bag *diag.Bag, fs *source.FileSet, opts JSONOpts) (DiagnosticsOutput, error) {
	diagnostics := make([]DiagnosticJSON, 0, bag.Len())

	items := bag.Items()
	maxItems := len(items)
	if opts.Max > 0 && opts.Max < maxItems {
		maxItems = opts.Max
	}

	for i := range maxItems {
		d := items[i]

		diagJSON := DiagnosticJSON{
			Severity: d.Severity.String(),
			Code:     d.Code.ID(),
			Message:  d.Message,
			Location: makeLocation(d.Primary, fs, opts.PathMode, opts.IncludePositions),
		}

		includeNotes := opts.IncludeNotes || d.Code == diag.ObsTimings
		if includeNotes && len(d.Notes) > 0 {
			diagJSON.Notes = make([]NoteJSON, len(d.Notes))
			for j, note := range d.Notes {
				diagJSON.Notes[j] = NoteJSON{
					Message:  note.Msg,
					Location: makeLocation(note.Span, fs, opts.PathMode, opts.IncludePositions),
				}
			}
		}

		if opts.IncludeFixes && len(d.Fixes) > 0 {
			fixes := append([]*diag.Fix(nil), d.Fixes...)
			sort.SliceStable(fixes, func(i, j int) bool {
				fi, fj := fixes[i], fixes[j]
				if fi.IsPreferred != fj.IsPreferred {
					return fi.IsPreferred && !fj.IsPreferred
				}
				if fi.Applicability != fj.Applicability {
					return fi.Applicability < fj.Applicability
				}
				if fi.Kind != fj.Kind {
					return fi.Kind < fj.Kind
				}
				if fi.Title != fj.Title {
					return fi.Title < fj.Title
				}
				return fi.ID < fj.ID
			})

			ctx := diag.FixBuildContext{FileSet: fs}
			diagJSON.Fixes = make([]FixJSON, 0, len(fixes))
			for _, fix := range fixes {
				resolved, err := fix.Resolve(ctx)
				fixJSON := FixJSON{
					ID:            resolved.ID,
					Title:         resolved.Title,
					Kind:          resolved.Kind.String(),
					Applicability: resolved.Applicability.String(),
					IsPreferred:   resolved.IsPreferred,
				}
				if err != nil {
					fixJSON.BuildError = err.Error()
				} else if len(resolved.Edits) > 0 {
					fixJSON.Edits = make([]FixEditJSON, len(resolved.Edits))
					for k, edit := range resolved.Edits {
						editJSON := FixEditJSON{
							Location: makeLocation(edit.Span, fs, opts.PathMode, opts.IncludePositions),
							NewText:  edit.NewText,
							OldText:  edit.OldText,
						}
						if opts.IncludePreviews {
							if preview, err := buildFixEditPreview(fs, edit); err == nil {
								editJSON.BeforeLines = append([]string(nil), preview.before...)
								editJSON.AfterLines = append([]string(nil), preview.after...)
							}
						}
						fixJSON.Edits[k] = editJSON
					}
				}
				diagJSON.Fixes = append(diagJSON.Fixes, fixJSON)
			}
		}

		diagnostics = append(diagnostics, diagJSON)
	}

	output := DiagnosticsOutput{
		Diagnostics: diagnostics,
		Count:       len(diagnostics),
	}

	return output, nil
}

// JSON форматирует диагностики в JSON формат.
// Выводит массив диагностик с полной информацией о местоположении, заметках и исправлениях.
func JSON(w io.Writer, bag *diag.Bag, fs *source.FileSet, opts JSONOpts) error {
	output, err := BuildDiagnosticsOutput(bag, fs, opts)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
