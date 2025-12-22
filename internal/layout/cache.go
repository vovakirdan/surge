package layout

import "surge/internal/types"

type cacheKey struct {
	Type  types.TypeID
	Attrs uint64
}

type cacheEntry struct {
	Layout TypeLayout
	Err    *LayoutError
}

type cache struct {
	byType map[cacheKey]cacheEntry
}

func newCache() *cache {
	return &cache{byType: make(map[cacheKey]cacheEntry, 256)}
}

func (c *cache) get(key cacheKey) (cacheEntry, bool) {
	if c == nil {
		return cacheEntry{}, false
	}
	e, ok := c.byType[key]
	return e, ok
}

func (c *cache) put(key cacheKey, entry *cacheEntry) {
	if c == nil {
		return
	}
	if entry == nil {
		delete(c.byType, key)
		return
	}
	c.byType[key] = *entry
}
