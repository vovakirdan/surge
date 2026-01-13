package lsp

import "unicode/utf8"

func applyChanges(text string, changes []textDocumentContentChangeEvent) string {
	if len(changes) == 0 {
		return text
	}
	for _, change := range changes {
		if change.Range == nil {
			text = change.Text
			continue
		}
		start := offsetForPosition(text, change.Range.Start)
		end := offsetForPosition(text, change.Range.End)
		if start < 0 {
			start = 0
		}
		if end < start {
			end = start
		}
		if start > len(text) {
			start = len(text)
		}
		if end > len(text) {
			end = len(text)
		}
		text = text[:start] + change.Text + text[end:]
	}
	return text
}

func offsetForPosition(text string, pos position) int {
	if pos.Line < 0 || pos.Character < 0 {
		return 0
	}
	line := 0
	i := 0
	for i < len(text) && line < pos.Line {
		if text[i] == '\n' {
			line++
		}
		i++
	}
	if line < pos.Line {
		return len(text)
	}
	utf16Units := 0
	for i < len(text) {
		if text[i] == '\n' {
			break
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 1 {
			size = 1
		}
		need := 1
		if r > 0xFFFF {
			need = 2
		}
		if utf16Units+need > pos.Character {
			break
		}
		utf16Units += need
		i += size
		if utf16Units == pos.Character {
			break
		}
	}
	return i
}
