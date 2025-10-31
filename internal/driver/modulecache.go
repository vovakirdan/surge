package driver

import (
	"sync"

	"surge/internal/diag"
	"surge/internal/project"
)

// minimal per-process cache by module path + content hash
type cached struct {
	content project.Digest
	meta    project.ModuleMeta
	broken  bool
	first   *diag.Diagnostic
}

type ModuleCache struct {
	mu    sync.RWMutex
	byMod map[string]cached // key: module path (canonical "a/b")
}

func NewModuleCache(capHint int) *ModuleCache {
	return &ModuleCache{byMod: make(map[string]cached, capHint)}
}

func (c *ModuleCache) Get(path string, content project.Digest) (project.ModuleMeta, bool, *diag.Diagnostic, bool) {
	c.mu.RLock()
	rec, ok := c.byMod[path]
	c.mu.RUnlock()
	if !ok || rec.content != content {
		return project.ModuleMeta{}, false, nil, false
	}
	return rec.meta, rec.broken, rec.first, true
}

func (c *ModuleCache) Put(m project.ModuleMeta, broken bool, first *diag.Diagnostic) {
	c.mu.Lock()
	c.byMod[m.Path] = cached{
		content: m.ContentHash,
		meta:    m,
		broken:  broken,
		first:   first,
	}
	c.mu.Unlock()
}
