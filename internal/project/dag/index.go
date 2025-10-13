package dag

import (
	"sort"
	"surge/internal/project"
)

type ModuleID uint32

type ModuleIndex struct {
	NameToID map[string]ModuleID
	IDToName []string
}

// собрать уникальные пути, sort.Strings, раздать ID по порядку
func BuildIndex(metas []project.ModuleMeta) ModuleIndex {
	uniq := make(map[string]struct{}, len(metas))
	for _, meta := range metas {
		if meta.Path != "" {
			uniq[meta.Path] = struct{}{}
		}
		for _, dep := range meta.Imports {
			if dep.Path == "" {
				continue
			}
			uniq[dep.Path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(uniq))
	for path := range uniq {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	nameToID := make(map[string]ModuleID, len(paths))
	for i, path := range paths {
		nameToID[path] = ModuleID(i)
	}

	return ModuleIndex{
		NameToID: nameToID,
		IDToName: paths,
	}
}
