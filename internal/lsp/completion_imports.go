package lsp

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"surge/internal/driver/diagnose"
	"surge/internal/project"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/token"
)

type importContext struct {
	afterColonColon bool
	moduleText      string
	prefix          string
}

func importContextAt(tokens []token.Token, file *source.File, offset uint32, tokenIdx int) (importContext, bool) {
	if file == nil || len(tokens) == 0 {
		return importContext{}, false
	}
	searchIdx := tokenIdx
	if searchIdx < 0 {
		searchIdx = len(tokens) - 1
	}
	for i := searchIdx; i >= 0; i-- {
		tok := tokens[i]
		if tok.Span.End > offset {
			continue
		}
		switch tok.Kind {
		case token.Semicolon:
			return importContext{}, false
		case token.KwImport:
			colonIdx := -1
			for j := i + 1; j < len(tokens); j++ {
				next := tokens[j]
				if next.Span.Start >= offset {
					break
				}
				if next.Kind == token.Semicolon {
					break
				}
				if next.Kind == token.ColonColon {
					colonIdx = j
				}
			}
			if colonIdx >= 0 && tokens[colonIdx].Span.End <= offset {
				moduleText := sliceContent(file, tok.Span.End, tokens[colonIdx].Span.Start)
				prefix := sliceContent(file, tokens[colonIdx].Span.End, offset)
				return importContext{
					afterColonColon: true,
					moduleText:      strings.TrimSpace(moduleText),
					prefix:          strings.TrimSpace(prefix),
				}, true
			}
			prefix := sliceContent(file, tok.Span.End, offset)
			return importContext{
				afterColonColon: false,
				prefix:          strings.TrimSpace(prefix),
			}, true
		}
	}
	return importContext{}, false
}

func importPathCompletions(snapshot *diagnose.AnalysisSnapshot, file *source.File, prefix string) []completionItem {
	if snapshot == nil || snapshot.ModuleExports == nil {
		return nil
	}
	currentModule := modulePathForFile(snapshot, file)
	useRelative := strings.HasPrefix(prefix, ".")
	paths := make([]string, 0, len(snapshot.ModuleExports))
	for modulePath := range snapshot.ModuleExports {
		if modulePath == "" {
			continue
		}
		label := modulePath
		if useRelative {
			rel := relativeImportPath(currentModule, modulePath)
			if rel == "" {
				continue
			}
			label = rel
		}
		if prefix != "" && !strings.HasPrefix(label, prefix) {
			continue
		}
		paths = append(paths, label)
	}
	sort.Strings(paths)
	items := make([]completionItem, 0, len(paths))
	for _, label := range paths {
		items = append(items, completionItem{
			Label:    label,
			Kind:     completionItemKindModule,
			SortText: "1_" + label,
		})
	}
	return items
}

func importMemberCompletions(snapshot *diagnose.AnalysisSnapshot, af *diagnose.AnalysisFile, file *source.File, moduleText string) []completionItem {
	modulePath := resolveImportPath(snapshot, file, moduleText)
	if modulePath == "" {
		return nil
	}
	exports := lookupModuleExports(snapshot, modulePath)
	if exports == nil {
		return nil
	}
	names := make([]string, 0, len(exports.Symbols))
	for name := range exports.Symbols {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]completionItem, 0, len(names))
	for _, name := range names {
		symbolsList := exports.Symbols[name]
		if len(symbolsList) == 0 {
			continue
		}
		kind := completionItemKindFunction
		if k := completionKindForExport(af, &symbolsList[0]); k != 0 {
			kind = k
		}
		items = append(items, completionItem{
			Label:    name,
			Kind:     kind,
			SortText: "2_" + name,
		})
	}
	return items
}

func modulePathForFile(snapshot *diagnose.AnalysisSnapshot, file *source.File) string {
	if file == nil || snapshot == nil {
		return ""
	}
	baseDir := snapshot.ProjectRoot
	if baseDir == "" {
		baseDir = filepath.Dir(file.Path)
	}
	pathValue := file.Path
	if baseDir != "" {
		if rel, err := source.RelativePath(pathValue, baseDir); err == nil {
			pathValue = rel
		}
	}
	if norm, err := project.NormalizeModulePath(pathValue); err == nil {
		return norm
	}
	return ""
}

func relativeImportPath(fromModule, toModule string) string {
	baseDir := strings.Trim(path.Dir(fromModule), "/")
	if baseDir == "." {
		baseDir = ""
	}
	rel, err := filepath.Rel(filepath.FromSlash(baseDir), filepath.FromSlash(toModule))
	if err != nil || rel == "" {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, ".") {
		return rel
	}
	return "./" + rel
}

func resolveImportPath(snapshot *diagnose.AnalysisSnapshot, file *source.File, raw string) string {
	if raw == "" {
		return ""
	}
	segs := splitImportPath(raw)
	if len(segs) == 0 {
		return ""
	}
	modulePath := modulePathForFile(snapshot, file)
	baseDir := ""
	if file != nil {
		baseDir = filepath.Dir(file.Path)
	}
	if norm, err := project.ResolveImportPath(modulePath, baseDir, segs); err == nil {
		return norm
	}
	if norm, err := project.NormalizeModulePath(strings.Join(segs, "/")); err == nil {
		return norm
	}
	return strings.Join(segs, "/")
}

func splitImportPath(raw string) []string {
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func lookupModuleExports(snapshot *diagnose.AnalysisSnapshot, modulePath string) *symbols.ModuleExports {
	if snapshot == nil || snapshot.ModuleExports == nil || modulePath == "" {
		return nil
	}
	if norm, err := project.NormalizeModulePath(modulePath); err == nil {
		modulePath = norm
	}
	if exp, ok := snapshot.ModuleExports[modulePath]; ok {
		return exp
	}
	return snapshot.ModuleExports[strings.Trim(modulePath, "/")]
}

func exportCompletions(af *diagnose.AnalysisFile, exports *symbols.ModuleExports, sortPrefix string) []completionItem {
	if exports == nil {
		return nil
	}
	names := make([]string, 0, len(exports.Symbols))
	for name := range exports.Symbols {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]completionItem, 0, len(names))
	for _, name := range names {
		list := exports.Symbols[name]
		if len(list) == 0 {
			continue
		}
		kind := completionKindForExport(af, &list[0])
		items = append(items, completionItem{
			Label:    name,
			Kind:     kind,
			SortText: sortPrefix + name,
		})
	}
	return items
}
