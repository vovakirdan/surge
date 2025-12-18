package layout

import "surge/internal/types"

type cacheKey struct {
	Type  types.TypeID
	Attrs uint64
}

type cache struct {
	byType map[cacheKey]TypeLayout
}

func newCache() *cache {
	return &cache{byType: make(map[cacheKey]TypeLayout, 256)}
}

func (c *cache) get(key cacheKey) (TypeLayout, bool) {
	if c == nil {
		return TypeLayout{}, false
	}
	l, ok := c.byType[key]
	return l, ok
}

func (c *cache) put(key cacheKey, l *TypeLayout) {
	if c == nil {
		return
	}
	if l == nil {
		delete(c.byType, key)
		return
	}
	c.byType[key] = *l
}
