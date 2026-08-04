package layout

import "surge/internal/types"

type cacheKey struct {
	Type  types.TypeID
	Attrs uint64
}

type cacheEntry struct {
	Layout PhysicalLayout
	Err    *LayoutError
}

type cache struct {
	byType map[cacheKey]cacheEntry
}

func newCache() *cache { return &cache{byType: make(map[cacheKey]cacheEntry, 256)} }

func (c *cache) get(key cacheKey) (cacheEntry, bool) {
	if c == nil {
		return cacheEntry{}, false
	}
	e, ok := c.byType[key]
	if !ok {
		return cacheEntry{}, false
	}
	e.Layout = e.Layout.clone()
	e.Err = e.Err.clone()
	return e, true
}

func (c *cache) put(key cacheKey, entry *cacheEntry) {
	if c == nil || entry == nil {
		return
	}
	owned := *entry
	owned.Layout = owned.Layout.clone()
	owned.Err = owned.Err.clone()
	c.byType[key] = owned
}
