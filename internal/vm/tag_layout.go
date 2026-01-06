package vm

import (
	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

// TagCase represents a case in a tagged union.
type TagCase struct {
	TagName      string
	TagSym       symbols.SymbolID
	PayloadTypes []types.TypeID
}

// TagLayout represents the layout of a tagged union type.
type TagLayout struct {
	TypeID types.TypeID
	Cases  []TagCase

	byName map[string]int
	bySym  map[symbols.SymbolID]int
}

// TagLayouts manages tag layouts for all types in a module.
type TagLayouts struct {
	byType     map[types.TypeID]*TagLayout
	anyBySym   map[symbols.SymbolID]string
	aliasBySym map[symbols.SymbolID]symbols.SymbolID
}

// NewTagLayouts creates a new TagLayouts instance from a MIR module.
func NewTagLayouts(m *mir.Module) *TagLayouts {
	tl := &TagLayouts{
		byType:   make(map[types.TypeID]*TagLayout),
		anyBySym: make(map[symbols.SymbolID]string),
	}
	if m != nil && m.Meta != nil && len(m.Meta.TagAliases) != 0 {
		tl.aliasBySym = make(map[symbols.SymbolID]symbols.SymbolID, len(m.Meta.TagAliases))
		for alias, orig := range m.Meta.TagAliases {
			if alias.IsValid() && orig.IsValid() {
				tl.aliasBySym[alias] = orig
			}
		}
	}
	var aliasesByOrig map[symbols.SymbolID][]symbols.SymbolID
	if m != nil && m.Meta != nil && len(m.Meta.TagAliases) != 0 {
		aliasesByOrig = make(map[symbols.SymbolID][]symbols.SymbolID, len(m.Meta.TagAliases))
		for alias, orig := range m.Meta.TagAliases {
			if !alias.IsValid() || !orig.IsValid() {
				continue
			}
			aliasesByOrig[orig] = append(aliasesByOrig[orig], alias)
		}
	}
	if m == nil || m.Meta == nil || len(m.Meta.TagLayouts) == 0 {
		if m != nil && m.Meta != nil && len(m.Meta.TagNames) != 0 {
			for sym, name := range m.Meta.TagNames {
				if sym.IsValid() && name != "" {
					tl.anyBySym[sym] = name
				}
			}
			if len(aliasesByOrig) != 0 {
				for orig, aliases := range aliasesByOrig {
					if name, ok := tl.anyBySym[orig]; ok {
						for _, alias := range aliases {
							if alias.IsValid() && name != "" {
								tl.anyBySym[alias] = name
							}
						}
					}
				}
			}
		}
		return tl
	}

	if len(m.Meta.TagNames) != 0 {
		for sym, name := range m.Meta.TagNames {
			if sym.IsValid() && name != "" {
				tl.anyBySym[sym] = name
			}
		}
	}
	if len(aliasesByOrig) != 0 {
		for orig, aliases := range aliasesByOrig {
			if name, ok := tl.anyBySym[orig]; ok {
				for _, alias := range aliases {
					if alias.IsValid() && name != "" {
						tl.anyBySym[alias] = name
					}
				}
			}
		}
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
			if len(aliasesByOrig) != 0 && tc.TagSym.IsValid() {
				if aliases := aliasesByOrig[tc.TagSym]; len(aliases) != 0 {
					for _, alias := range aliases {
						if alias.IsValid() {
							if _, ok := layout.bySym[alias]; !ok {
								layout.bySym[alias] = idx
							}
						}
					}
				}
			}
			if tc.TagSym.IsValid() {
				if _, ok := tl.anyBySym[tc.TagSym]; !ok {
					tl.anyBySym[tc.TagSym] = tc.TagName
				}
				if len(aliasesByOrig) != 0 {
					if aliases := aliasesByOrig[tc.TagSym]; len(aliases) != 0 {
						for _, alias := range aliases {
							if alias.IsValid() {
								if _, ok := tl.anyBySym[alias]; !ok {
									tl.anyBySym[alias] = tc.TagName
								}
							}
						}
					}
				}
			}
		}
		tl.byType[typeID] = layout
	}

	return tl
}

// Layout returns the tag layout for a given type ID.
func (tl *TagLayouts) Layout(typeID types.TypeID) (*TagLayout, bool) {
	if tl == nil || typeID == types.NoTypeID {
		return nil, false
	}
	layout, ok := tl.byType[typeID]
	return layout, ok && layout != nil
}

// IsTagType checks if a type ID represents a tagged union type.
func (tl *TagLayouts) IsTagType(typeID types.TypeID) bool {
	_, ok := tl.Layout(typeID)
	return ok
}

// KnownTagSym checks if a symbol ID is a known tag symbol.
func (tl *TagLayouts) KnownTagSym(sym symbols.SymbolID) bool {
	if tl == nil || !sym.IsValid() {
		return false
	}
	if tl.aliasBySym != nil {
		if orig, ok := tl.aliasBySym[sym]; ok && orig.IsValid() {
			sym = orig
		}
	}
	_, ok := tl.anyBySym[sym]
	return ok
}

// AnyTagName returns the tag name for a given symbol ID.
func (tl *TagLayouts) AnyTagName(sym symbols.SymbolID) (string, bool) {
	if tl == nil || !sym.IsValid() {
		return "", false
	}
	if tl.aliasBySym != nil {
		if orig, ok := tl.aliasBySym[sym]; ok && orig.IsValid() {
			sym = orig
		}
	}
	name, ok := tl.anyBySym[sym]
	return name, ok
}

// CanonicalTagSym returns the canonical symbol ID for a tag symbol, resolving aliases.
func (tl *TagLayouts) CanonicalTagSym(sym symbols.SymbolID) symbols.SymbolID {
	if tl == nil || !sym.IsValid() {
		return sym
	}
	if tl.aliasBySym == nil {
		return sym
	}
	if orig, ok := tl.aliasBySym[sym]; ok && orig.IsValid() {
		return orig
	}
	return sym
}

// CaseByName returns the tag case by name.
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

// CaseBySym returns the tag case by symbol ID.
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
