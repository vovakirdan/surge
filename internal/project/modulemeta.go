package project

import (
	"errors"
	"fmt"
	"strings"

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

// ResolveImportPath нормализует путь импорта относительно модуля modulePath.
// segments — список сегментов (включая ".", "..").
func ResolveImportPath(modulePath string, segments []string) (string, error) {
	if len(segments) == 0 {
		return "", errors.New("empty import path")
	}

	var moduleDir []string
	if modulePath != "" {
		parts := strings.Split(modulePath, "/")
		if len(parts) > 1 {
			moduleDir = append(moduleDir, parts[:len(parts)-1]...)
		}
	}

	useBase := len(segments) > 0 && (segments[0] == "." || segments[0] == "..")
	target := make([]string, 0, len(moduleDir)+len(segments))
	if useBase {
		target = append(target, moduleDir...)
	}

	for _, seg := range segments {
		switch seg {
		case "":
			return "", errors.New("empty import segment")
		case ".":
			continue
		case "..":
			if len(target) == 0 {
				return "", errors.New("import path escapes project root")
			}
			target = target[:len(target)-1]
		default:
			if strings.Contains(seg, "/") {
				return "", fmt.Errorf("import segment %q contains '/'", seg)
			}
			target = append(target, seg)
		}
	}

	if len(target) == 0 {
		return "", errors.New("import resolves to empty path")
	}

	return NormalizeModulePath(strings.Join(target, "/"))
}
