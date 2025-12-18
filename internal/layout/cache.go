package layout

import "surge/internal/types"

type cache struct {
	byType map[types.TypeID]TypeLayout
}

func newCache() *cache {
	return &cache{byType: make(map[types.TypeID]TypeLayout, 256)}
}

func (c *cache) get(id types.TypeID) (TypeLayout, bool) {
	if c == nil {
		return TypeLayout{}, false
	}
	l, ok := c.byType[id]
	return l, ok
}

func (c *cache) put(id types.TypeID, l *TypeLayout) {
	if c == nil {
		return
	}
	if l == nil {
		delete(c.byType, id)
		return
	}
	c.byType[id] = *l
}
