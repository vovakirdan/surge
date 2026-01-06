package diagfmt

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"surge/internal/diag"
	"surge/internal/source"

	"fortio.org/safecast"
	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

// visualWidthUpTo вычисляет визуальную ширину подстроки до указанной колонки (1-based, в байтах).
// Учитывает табуляции и правильную ширину Unicode символов (восточноазиатские занимают 2 колонки).
func visualWidthUpTo(s string, byteCol uint32, tabWidth int) int {
	if byteCol <= 1 {
		return 0
	}

	bytePos := 0
	visualPos := 0

	for _, r := range s {
		if bytePos >= int(byteCol-1) {
			break
		}

		if r == '\t' {
			// Табуляция выравнивается до следующей позиции, кратной tabWidth
			visualPos = (visualPos + tabWidth) / tabWidth * tabWidth
		} else {
			// Используем runewidth для правильного подсчёта ширины Unicode символов
			visualPos += runewidth.RuneWidth(r)
		}

		bytePos += len(string(r))
	}

	return visualPos
}

// Pretty форматирует диагностики в человекочитаемый вид.
// Идёт по bag.Items() (ожидается bag.Sort() заранее).
// Для каждого diag печатает:
// <path>:<line>:<col>: <SEV> <CODE>: <Message>
// затем контекст строки с подчёркиванием ^~~~ по Span, затем Notes с аналогичным форматом.
// Цвет включается опцией.
func Pretty(w io.Writer, bag *diag.Bag, fs *source.FileSet, opts PrettyOpts) {
	// Настройка цветов
	var (
		errorColor     = color.New(color.FgRed, color.Bold)
		warningColor   = color.New(color.FgYellow, color.Bold)
		infoColor      = color.New(color.FgCyan, color.Bold)
		pathColor      = color.New(color.FgWhite, color.Bold)
		codeColor      = color.New(color.FgMagenta)
		lineNumColor   = color.New(color.FgBlue)
		underlineColor = color.New(color.FgRed, color.Bold)
		previewLabel   = color.New(color.FgCyan, color.Bold)
		beforeColor    = color.New(color.FgRed)
		afterColor     = color.New(color.FgGreen)
	)

	// Отключаем цвета если нужно
	prev := color.NoColor
	defer func() { color.NoColor = prev }()
	color.NoColor = !opts.Color

	context, err := safecast.Conv[uint32](opts.Context)
	if err != nil {
		panic(fmt.Errorf("context overflow: %w", err))
	}
	if context == 0 {
		context = 1
	}

	formatPath := func(f *source.File) string {
		switch opts.PathMode {
		case PathModeAbsolute:
			return f.FormatPath("absolute", "")
		case PathModeRelative:
			return f.FormatPath("relative", fs.BaseDir())
		case PathModeBasename:
			return f.FormatPath("basename", "")
		case PathModeAuto:
			return f.FormatPath("auto", "")
		default:
			return f.Path
		}
	}

	fixLabelColor := infoColor

	for idx, d := range bag.Items() {
		if idx > 0 {
			fmt.Fprintln(w) //nolint:errcheck // пустая строка между диагностиками
		}

		lineColStart, lineColEnd := fs.Resolve(d.Primary)
		f := fs.Get(d.Primary.File)

		// Форматируем путь в зависимости от PathMode
		displayPath := formatPath(f)

		// Заголовок: file.sg:23:7: ERROR LEX1002: message
		sevStr := d.Severity.String()
		var sevColored string
		switch d.Severity {
		case diag.SevError:
			sevColored = errorColor.Sprint(sevStr)
		case diag.SevWarning:
			sevColored = warningColor.Sprint(sevStr)
		case diag.SevInfo:
			sevColored = infoColor.Sprint(sevStr)
		default:
			sevColored = sevStr
		}

		fmt.Fprintf(w, "%s:%d:%d: %s %s: %s\n", //nolint:errcheck
			pathColor.Sprint(displayPath),
			lineColStart.Line,
			lineColStart.Col,
			sevColored,
			codeColor.Sprint(d.Code.ID()),
			d.Message,
		)

		// Вывод контекста с подчеркиванием
		totalLines, err := safecast.Conv[uint32](len(f.LineIdx))
		if err != nil {
			panic(fmt.Errorf("total lines overflow: %w", err))
		}
		totalLines++
		if len(f.LineIdx) == 0 && len(f.Content) > 0 {
			totalLines = 1
		}

		// Определяем диапазон строк для отображения
		startLine := lineColStart.Line
		if startLine > context {
			startLine = lineColStart.Line - uint32(context)
		} else {
			startLine = 1
		}

		endLine := min(lineColStart.Line+context, totalLines)

		// Если это не первая строка файла, показываем "..."
		if startLine > 1 {
			fmt.Fprintln(w, "...") //nolint:errcheck
		}

		// Выводим строки контекста
		const tabWidth = 8

		// Вычисляем ширину номеров строк для всего блока (для единообразия)
		lineNumWidth := max(len(fmt.Sprintf("%d", endLine)), 3)

		for lineNum := startLine; lineNum <= endLine; lineNum++ {
			lineText := f.GetLine(lineNum)

			// Формируем gutter (левую часть с номером строки)
			lineNumStr := fmt.Sprintf("%*d", lineNumWidth, lineNum)
			gutter := fmt.Sprintf("%s | ", lineNumColor.Sprint(lineNumStr))
			// Длина без ANSI escape-кодов: "lineNumWidth цифр + ' | '"
			gutterLen := lineNumWidth + 3

			_, err = io.WriteString(w, gutter)
			if err != nil {
				panic(fmt.Errorf("write gutter: %w", err))
			}
			_, err = io.WriteString(w, lineText)
			if err != nil {
				panic(fmt.Errorf("write line text: %w", err))
			}
			_, err = io.WriteString(w, "\n")
			if err != nil {
				panic(fmt.Errorf("write newline: %w", err))
			}

			// Если это строка с ошибкой, добавляем подчеркивание
			if lineNum == lineColStart.Line {
				// Вычисляем визуальную позицию подчеркивания
				startCol := lineColStart.Col
				endCol := lineColEnd.Col

				// Если ошибка на разных строках, подчеркиваем до конца текущей строки
				if lineColEnd.Line > lineColStart.Line {
					lenLineText, err := safecast.Conv[uint32](len(lineText))
					if err != nil {
						panic(fmt.Errorf("len line text overflow: %w", err))
					}
					endCol = lenLineText + 1
				}

				// Вычисляем визуальные позиции с учётом табуляций и Unicode
				visualStart := visualWidthUpTo(lineText, startCol, tabWidth)
				visualEnd := visualWidthUpTo(lineText, endCol, tabWidth)

				// Строим строку подчеркивания
				var underline strings.Builder

				// Пробелы для выравнивания с gutter
				for range gutterLen {
					underline.WriteByte(' ')
				}

				// Пробелы до начала подчеркивания
				for range visualStart {
					underline.WriteByte(' ')
				}

				// Подчеркивание: ~~~~~^
				spanLen := visualEnd - visualStart
				if spanLen <= 0 {
					underline.WriteByte('^')
				} else {
					for i := range spanLen {
						if i == spanLen-1 {
							underline.WriteByte('^')
						} else {
							underline.WriteByte('~')
						}
					}
				}

				fmt.Fprintln(w, underlineColor.Sprint(underline.String())) //nolint:errcheck
			}
		}

		// Если это не последняя строка файла, показываем "..."
		if endLine < totalLines {
			fmt.Fprintln(w, "...") //nolint:errcheck
		}

		// Заглушки для Notes и Fixes
		if opts.ShowNotes && len(d.Notes) > 0 {
			for _, note := range d.Notes {
				if d.Code == diag.ObsTimings && printTimingNote(w, note.Msg, infoColor) {
					continue
				}

				nf := fs.Get(note.Span.File)
				notePath := formatPath(nf)
				noteStart, _ := fs.Resolve(note.Span)
				fmt.Fprintf( //nolint:errcheck
					w,
					"  %s: %s:%d:%d: %s\n",
					infoColor.Sprint("note"),
					pathColor.Sprint(notePath),
					noteStart.Line,
					noteStart.Col,
					note.Msg,
				)
			}
		}

		if opts.ShowFixes && len(d.Fixes) > 0 {
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
			for i, fix := range fixes {
				resolved, err := fix.Resolve(ctx)
				if err != nil {
					fmt.Fprintf( //nolint:errcheck
						w,
						"  %s #%d: %s (build error: %v)\n",
						fixLabelColor.Sprint("fix"),
						i+1,
						fix.Title,
						err,
					)
					continue
				}

				meta := []string{
					resolved.Kind.String(),
					resolved.Applicability.String(),
				}
				if resolved.IsPreferred {
					meta = append(meta, "preferred")
				}
				if resolved.ID != "" {
					meta = append(meta, "id="+resolved.ID)
				}
				fmt.Fprintf( //nolint:errcheck
					w,
					"  %s #%d: %s (%s)\n",
					fixLabelColor.Sprint("fix"),
					i+1,
					resolved.Title,
					strings.Join(meta, ", "),
				)

				if len(resolved.Edits) == 0 {
					fmt.Fprintf(w, "      (no edits)\n") //nolint:errcheck
					continue
				}

				for _, edit := range resolved.Edits {
					ef := fs.Get(edit.Span.File)
					editPath := formatPath(ef)
					start, end := fs.Resolve(edit.Span)
					oldPreview := edit.OldText
					newPreview := edit.NewText
					if len(oldPreview) > 32 {
						oldPreview = oldPreview[:29] + "..."
					}
					if len(newPreview) > 32 {
						newPreview = newPreview[:29] + "..."
					}
					metaParts := []string{}
					if edit.OldText != "" {
						metaParts = append(metaParts, fmt.Sprintf("expect=%q", oldPreview))
					}
					metaParts = append(metaParts, fmt.Sprintf("apply=%q", newPreview))
					fmt.Fprintf( //nolint:errcheck
						w,
						"      %s:%d:%d-%d:%d %s\n",
						pathColor.Sprint(editPath),
						start.Line,
						start.Col,
						end.Line,
						end.Col,
						strings.Join(metaParts, ", "),
					)

					if opts.ShowPreview {
						preview, err := buildFixEditPreview(fs, edit)
						if err != nil {
							fmt.Fprintf( //nolint:errcheck
								w,
								"        preview unavailable: %v\n",
								err,
							)
							continue
						}

						fmt.Fprintf( //nolint:errcheck
							w,
							"      %s\n",
							previewLabel.Sprint("preview:"),
						)

						printPreviewSection := func(label string, marker string, lines []string, colorizer *color.Color) {
							if len(lines) == 0 {
								fmt.Fprintf( //nolint:errcheck
									w,
									"        %s %s\n",
									label,
									colorizer.Sprint("<empty>"),
								)
								return
							}
							fmt.Fprintf( //nolint:errcheck
								w,
								"        %s\n",
								label,
							)
							for _, line := range lines {
								display := line
								if display == "" {
									display = "(blank)"
								}
								fmt.Fprintf( //nolint:errcheck
									w,
									"          %s %s\n",
									colorizer.Sprint(marker),
									colorizer.Sprint(display),
								)
							}
						}

						printPreviewSection("before:", "-", preview.before, beforeColor)
						printPreviewSection("after:", "+", preview.after, afterColor)
					}
				}
			}
		}
	}
}

type timingNotePayload struct {
	Kind    string  `json:"kind"`
	Path    string  `json:"path"`
	TotalMS float64 `json:"total_ms"`
	Phases  []struct {
		Name       string  `json:"name"`
		DurationMS float64 `json:"duration_ms"`
		Note       string  `json:"note"`
	} `json:"phases"`
}

func printTimingNote(w io.Writer, payload string, infoColor *color.Color) bool {
	var data timingNotePayload
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return false
	}
	kind := data.Kind
	if kind == "" {
		kind = "pipeline"
	}
	fmt.Fprintf( //nolint:errcheck
		w,
		"  %s: timings (%s) total %.2f ms",
		infoColor.Sprint("note"),
		kind,
		data.TotalMS,
	)
	if data.Path != "" {
		fmt.Fprintf(w, " — %s", data.Path) //nolint:errcheck
	}
	fmt.Fprintln(w) //nolint:errcheck
	for _, phase := range data.Phases {
		if phase.Name == "" {
			continue
		}
		fmt.Fprintf(w, "      %-20s %7.2f ms", phase.Name, phase.DurationMS) //nolint:errcheck
		if phase.Note != "" {
			fmt.Fprintf(w, "  // %s", phase.Note) //nolint:errcheck
		}
		fmt.Fprintln(w) //nolint:errcheck
	}
	return true
}
