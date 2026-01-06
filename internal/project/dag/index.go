package dag

import (
	"fmt"
	"sort"

	"fortio.org/safecast"

	"surge/internal/project"
)

// ModuleID is a unique identifier for a module in the graph.
type ModuleID uint32

// ModuleIndex maps module paths to their numeric IDs.
type ModuleIndex struct {
	NameToID map[string]ModuleID
	IDToName []string
}

// BuildIndex collects unique module paths, sorts them, and assigns IDs sequentially.
func BuildIndex(metas []*project.ModuleMeta) ModuleIndex {
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
		mID, err := safecast.Conv[ModuleID](i)
		if err != nil {
			panic(fmt.Errorf("module id overflow: %w", err))
		}
		nameToID[path] = mID
	}

	return ModuleIndex{
		NameToID: nameToID,
		IDToName: paths,
	}
}
