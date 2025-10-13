package project

import (
	"errors"
	"surge/internal/source"
)

type ImportMeta struct {
	Path string
	Span source.Span
}

type ModuleMeta struct {
	Path    string       // нормализованный путь к модулю: "a/b"
	Span    source.Span  // span всего файла (или места объявления модуля)
	Imports []ImportMeta // нормализованные пути импортов с их спанами
}

// NormalizeModulePath приводит путь модуля (импорт/сам файл) к каноническому виду "a/b".
// Удаляет расширение .sg, переводит слэши к '/', запрещает пустые сегменты, ".", "..".
func NormalizeModulePath(path string) (string, error) {
	// Используем filepath пакет для разбора, потом заменим все слэши на '/'
	// Отсекаем .sg (только если оно на конце)
	ext := ".sg"
	if len(path) >= len(ext) && path[len(path)-len(ext):] == ext {
		path = path[:len(path)-len(ext)]
	}
	// filepath.Clean нормализует слэши и убирает ./, ../, но нам нужно проверить сегменты
	// Разделяем путь на сегменты
	cleaned := []string{}
	curr := ""
	for _, r := range path {
		if r == '\\' || r == '/' {
			if curr != "" {
				cleaned = append(cleaned, curr)
				curr = ""
			} else {
				// пустой сегмент, например "a//b"
				return "", errors.New("invalid module path")
			}
		} else {
			curr += string(r)
		}
	}
	if curr != "" {
		cleaned = append(cleaned, curr)
	}
	if len(cleaned) == 0 {
		return "", errors.New("invalid module path")
	}
	// Проверяем сегменты
	for _, seg := range cleaned {
		if seg == "" || seg == "." || seg == ".." {
			return "", errors.New("invalid module path")
		}
	}
	// Собираем обратно через "/"
	out := ""
	for i, seg := range cleaned {
		if i != 0 {
			out += "/"
		}
		out += seg
	}
	return out, nil
}
