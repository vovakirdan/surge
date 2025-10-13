package source

import (
	"path/filepath"
	"slices"
	"sort"
)

// normalizeCRLF заменяет все \r\n на \n, не трогая одиночные \r.
// Возвращает новый слайс и флаг: были ли замены (true, если хотя бы одна).
func normalizeCRLF(content []byte) ([]byte, bool) {
	// Быстрый путь: если нет \r, возвращаем как есть.
	if !slices.Contains(content, '\r') {
		return content, false
	}

	// Новый слайс для результата (максимум такой же длины, может быть короче).
	out := make([]byte, 0, len(content))
	changed := false

	i := 0
	for i < len(content) {
		// Если встретили \r\n — заменяем на \n.
		if content[i] == '\r' && i+1 < len(content) && content[i+1] == '\n' {
			out = append(out, '\n')
			i += 2
			changed = true
		} else {
			out = append(out, content[i])
			i++
		}
	}
	return out, changed
}

func removeBOM(content []byte) ([]byte, bool) {
	if len(content) < 3 {
		return content, false
	}

	if content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		return content[3:], true
	}

	return content, false
}

// LineIdx хранит БАЙТОВЫЕ позиции всех '\n' в файле (0-based).
// Первая строка начинается в байте 0.
// Начало строки k > 1 = LineIdx[k-2] + 1.
func buildLineIndex(content []byte) []uint32 {
	out := make([]uint32, 0, len(content))
	for i, b := range content {
		if b == '\n' {
			out = append(out, uint32(i))
		}
	}
	return out
}

func toLineCol(lineIdx []uint32, off uint32) LineCol {
    if len(lineIdx) == 0 {
        return LineCol{Line: 1, Col: off + 1}
    }
    // ищем первый индекс '\n' > off
    i := sort.Search(len(lineIdx), func(k int) bool { return lineIdx[k] > off })
    if i == 0 {
        // off до первого \n
        return LineCol{Line: 1, Col: off + 1}
    }
    // последний '\n' <= off находится по индексу i-1
    last := lineIdx[i-1]
    if off == last {
        // позиция на '\n' — считаем концом предыдущей строки
        var start uint32
        if i-1 == 0 { start = 0 } else { start = lineIdx[i-2] + 1 }
        return LineCol{Line: uint32(i), Col: last - start + 1}
    }
    // обычный случай
    start := last + 1
    return LineCol{Line: uint32(i + 1), Col: off - start + 1}
}

func normalizePath(p string) string {
	// единый вид в кроссплатформенных дифах
	return filepath.ToSlash(filepath.Clean(p))
}

// AbsolutePath возвращает абсолютный путь к файлу.
// Если путь уже абсолютный, возвращает его нормализованным.
func AbsolutePath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path, err
	}
	return normalizePath(absPath), nil
}

// RelativePath возвращает путь относительно базовой директории.
// Если не удаётся вычислить относительный путь, возвращает абсолютный.
func RelativePath(path, base string) (string, error) {
	// Сначала делаем оба пути абсолютными
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path, err
	}

	absBase, err := filepath.Abs(base)
	if err != nil {
		return normalizePath(absPath), nil
	}

	// Вычисляем относительный путь
	relPath, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return normalizePath(absPath), nil
	}

	return normalizePath(relPath), nil
}

// BaseName возвращает только имя файла без директорий.
// Нормализует результат для консистентности (хотя обычно в basename нет слэшей).
func BaseName(path string) string {
	return normalizePath(filepath.Base(path))
}
