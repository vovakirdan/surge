package project

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"surge/internal/source"
)

type ImportMeta struct {
	Path string
	Span source.Span
}

type ModuleKind uint8

const (
	ModuleKindUnknown ModuleKind = iota
	ModuleKindModule
	ModuleKindBinary
)

type ModuleFileMeta struct {
	Path string
	Span source.Span
	Hash Digest
}

type ModuleMeta struct {
	Name            string
	Path            string     // нормализованный путь к модулю: "a/b"
	Dir             string     // нормализованный путь к каталогу модуля: "a/b"
	Kind            ModuleKind // module или binary
	HasModulePragma bool
	Span            source.Span  // span всего файла (или места объявления модуля)
	Imports         []ImportMeta // нормализованные пути импортов с их спанами
	Files           []ModuleFileMeta
	ContentHash     Digest // хеш содержимого файла (из FileSet)
	ModuleHash      Digest // агрегированный хеш модуля с учётом зависимостей
}

func IsValidModuleIdent(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r > unicode.MaxASCII {
			return false
		}
		if i == 0 && r != '_' && !unicode.IsLetter(r) {
			return false
		}
		if i > 0 && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
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
	for path != "" && (path[0] == '/' || path[0] == '\\') {
		path = path[1:]
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

// ResolveImportPath нормализует путь импорта относительно модуля modulePath и базового каталога basePath.
// segments — список сегментов (включая ".", "..").
func ResolveImportPath(modulePath, basePath string, segments []string) (string, error) {
	if len(segments) == 0 {
		return "", errors.New("empty import path")
	}

	joined := strings.Join(segments, "/")
	if len(segments) > 0 && (segments[0] == "stdlib" || segments[0] == "core") {
		return NormalizeModulePath(joined)
	}

	var baseSegments []string
	if basePath != "" {
		clean := strings.Trim(basePath, "/")
		if clean != "" {
			baseSegments = strings.Split(strings.ReplaceAll(clean, "\\", "/"), "/")
		}
	}

	var moduleDir []string
	if modulePath != "" {
		parts := strings.Split(modulePath, "/")
		if len(parts) > 1 {
			moduleDir = append(moduleDir, parts[:len(parts)-1]...)
		}
	}

	target := make([]string, 0, len(moduleDir)+len(segments))
	if len(moduleDir) > 0 {
		target = append(target, moduleDir...)
	}

	useRelative := segments[0] == "." || segments[0] == ".."
	if !useRelative {
		absolute := false
		if len(baseSegments) > 0 && len(segments) >= len(baseSegments) {
			absolute = true
			for i := range baseSegments {
				if segments[i] != baseSegments[i] {
					absolute = false
					break
				}
			}
		}
		if !absolute && len(segments) >= len(moduleDir) {
			absolute = true
			for i := range moduleDir {
				if moduleDir[i] != segments[i] {
					absolute = false
					break
				}
			}
		}
		if !absolute && len(moduleDir) > 0 {
			parent := moduleDir[:len(moduleDir)-1]
			if len(parent) > 0 && len(segments) >= len(parent) {
				absolute = true
				for i := range parent {
					if parent[i] != segments[i] {
						absolute = false
						break
					}
				}
			}
		}
		if absolute {
			target = target[:0]
		}
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
