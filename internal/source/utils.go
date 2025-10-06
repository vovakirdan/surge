package source

import (
	"path/filepath"
	"slices"
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
	// Если LineIdx пустой, то весь файл - одна строка
	if len(lineIdx) == 0 {
		return LineCol{Line: 1, Col: off + 1}
	}

	// бинпоиск: находим наибольший lineIdx[i] <= off
	lo, hi := 0, len(lineIdx)-1
	for lo <= hi {
		mid := (lo + hi) >> 1
		if lineIdx[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	line := hi // индекс строки (0-based)

	// Если off меньше первого элемента LineIdx, то это первая строка
	if line < 0 {
		return LineCol{Line: 1, Col: off + 1}
	}

	// hi == индекс последнего '\n' с позицией <= off; может быть -1 если off до первого \n
    if hi < 0 {
        return LineCol{Line: 1, Col: off + 1}
    }
    startOff := lineIdx[hi] + 1
    return LineCol{Line: uint32(hi + 2), Col: off - startOff + 1}
}

func normalizePath(p string) string {
	// единый вид в кроссплатформенных дифах
	return filepath.ToSlash(filepath.Clean(p))
}
