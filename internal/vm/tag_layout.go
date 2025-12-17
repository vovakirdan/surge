package vm

import (
	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

type TagCase struct {
	TagName      string
	TagSym       symbols.SymbolID
	PayloadTypes []types.TypeID
}

type TagLayout struct {
	TypeID types.TypeID
	Cases  []TagCase

	byName map[string]int
	bySym  map[symbols.SymbolID]int
}

type TagLayouts struct {
	byType   map[types.TypeID]*TagLayout
	anyBySym map[symbols.SymbolID]string
}

func NewTagLayouts(m *mir.Module) *TagLayouts {
	tl := &TagLayouts{
		byType:   make(map[types.TypeID]*TagLayout),
		anyBySym: make(map[symbols.SymbolID]string),
	}
	if m == nil || m.Meta == nil || len(m.Meta.TagLayouts) == 0 {
		return tl
	}

	for typeID, cases := range m.Meta.TagLayouts {
		if typeID == types.NoTypeID || len(cases) == 0 {
			continue
		}
		layout := &TagLayout{
			TypeID: typeID,
			Cases:  make([]TagCase, 0, len(cases)),
			byName: make(map[string]int, len(cases)),
			bySym:  make(map[symbols.SymbolID]int, len(cases)),
		}
		for _, c := range cases {
			tc := TagCase{
				TagName:      c.TagName,
				TagSym:       c.TagSym,
				PayloadTypes: append([]types.TypeID(nil), c.PayloadTypes...),
			}
			idx := len(layout.Cases)
			layout.Cases = append(layout.Cases, tc)
			if _, ok := layout.byName[tc.TagName]; !ok {
				layout.byName[tc.TagName] = idx
			}
			if _, ok := layout.bySym[tc.TagSym]; !ok {
				layout.bySym[tc.TagSym] = idx
			}
			if tc.TagSym.IsValid() {
				if _, ok := tl.anyBySym[tc.TagSym]; !ok {
					tl.anyBySym[tc.TagSym] = tc.TagName
				}
			}
		}
		tl.byType[typeID] = layout
	}

	return tl
}

func (tl *TagLayouts) Layout(typeID types.TypeID) (*TagLayout, bool) {
	if tl == nil || typeID == types.NoTypeID {
		return nil, false
	}
	layout, ok := tl.byType[typeID]
	return layout, ok && layout != nil
}

func (tl *TagLayouts) IsTagType(typeID types.TypeID) bool {
	_, ok := tl.Layout(typeID)
	return ok
}

func (tl *TagLayouts) KnownTagSym(sym symbols.SymbolID) bool {
	if tl == nil || !sym.IsValid() {
		return false
	}
	_, ok := tl.anyBySym[sym]
	return ok
}

func (tl *TagLayouts) AnyTagName(sym symbols.SymbolID) (string, bool) {
	if tl == nil || !sym.IsValid() {
		return "", false
	}
	name, ok := tl.anyBySym[sym]
	return name, ok
}

func (l *TagLayout) CaseByName(tagName string) (TagCase, bool) {
	if l == nil {
		return TagCase{}, false
	}
	idx, ok := l.byName[tagName]
	if !ok || idx < 0 || idx >= len(l.Cases) {
		return TagCase{}, false
	}
	return l.Cases[idx], true
}

func (l *TagLayout) CaseBySym(sym symbols.SymbolID) (TagCase, bool) {
	if l == nil {
		return TagCase{}, false
	}
	idx, ok := l.bySym[sym]
	if !ok || idx < 0 || idx >= len(l.Cases) {
		return TagCase{}, false
	}
	return l.Cases[idx], true
}
