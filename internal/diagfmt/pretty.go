package diagfmt

import (
	"fmt"
	"io"
	"strings"
	"surge/internal/diag"
	"surge/internal/source"

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
	)

	// Отключаем цвета если нужно
	prev := color.NoColor
	defer func() { color.NoColor = prev }()
	color.NoColor = !opts.Color

	context := int(opts.Context)
	if context <= 0 {
		context = 1
	}

	for idx, d := range bag.Items() {
		if idx > 0 {
			fmt.Fprintln(w) // пустая строка между диагностиками
		}

		lineColStart, lineColEnd := fs.Resolve(d.Primary)
		f := fs.Get(d.Primary.File)

		// Форматируем путь в зависимости от PathMode
		var displayPath string
		switch opts.PathMode {
		case PathModeAbsolute:
			displayPath = f.FormatPath("absolute", "")
		case PathModeRelative:
			displayPath = f.FormatPath("relative", fs.BaseDir())
		case PathModeBasename:
			displayPath = f.FormatPath("basename", "")
		case PathModeAuto:
			displayPath = f.FormatPath("auto", "")
		default:
			displayPath = f.Path
		}

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

		fmt.Fprintf(w, "%s:%d:%d: %s %s: %s\n",
			pathColor.Sprint(displayPath),
			lineColStart.Line,
			lineColStart.Col,
			sevColored,
			codeColor.Sprint(d.Code.ID()),
			d.Message,
		)

		// Вывод контекста с подчеркиванием
		totalLines := uint32(len(f.LineIdx)) + 1
		if len(f.LineIdx) == 0 && len(f.Content) > 0 {
			totalLines = 1
		}

		// Определяем диапазон строк для отображения
		startLine := lineColStart.Line
		if int(startLine) > context {
			startLine = lineColStart.Line - uint32(context)
		} else {
			startLine = 1
		}

		endLine := min(lineColStart.Line+uint32(context), totalLines)

		// Если это не первая строка файла, показываем "..."
		if startLine > 1 {
			fmt.Fprintln(w, "...")
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

			io.WriteString(w, gutter)
			io.WriteString(w, lineText)
			io.WriteString(w, "\n")

			// Если это строка с ошибкой, добавляем подчеркивание
			if lineNum == lineColStart.Line {
				// Вычисляем визуальную позицию подчеркивания
				startCol := lineColStart.Col
				endCol := lineColEnd.Col

				// Если ошибка на разных строках, подчеркиваем до конца текущей строки
				if lineColEnd.Line > lineColStart.Line {
					endCol = uint32(len(lineText)) + 1
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

				fmt.Fprintln(w, underlineColor.Sprint(underline.String()))
			}
		}

		// Если это не последняя строка файла, показываем "..."
		if endLine < totalLines {
			fmt.Fprintln(w, "...")
		}

		// Заглушки для Notes и Fixes
		if len(d.Notes) > 0 {
			fmt.Fprintf(w, "Notes: (TODO: implement notes formatting)\n")
		}

		if len(d.Fixes) > 0 {
			fmt.Fprintf(w, "Fixes: (TODO: implement fixes formatting)\n")
		}
	}
}
