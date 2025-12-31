package mir

import (
	"fmt"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func buildAsyncStateUnion(m *Module, typesIn *types.Interner, symTable *symbols.Table, f, pollFn *Func, variants []stateVariant) (types.TypeID, error) {
	if typesIn == nil || typesIn.Strings == nil {
		return types.NoTypeID, fmt.Errorf("mir: async: missing type interner strings")
	}
	name := fmt.Sprintf("__AsyncState$%s", f.Name)
	nameID := typesIn.Strings.Intern(name)
	stateID := typesIn.RegisterUnion(nameID, source.Span{})

	members := make([]types.UnionMember, 0, len(variants))
	for _, v := range variants {
		tagNameID := typesIn.Strings.Intern(v.name)
		payload := make([]types.TypeID, 0, len(v.locals))
		for _, localID := range v.locals {
			if pollFn == nil || localID == NoLocalID || int(localID) >= len(pollFn.Locals) {
				return types.NoTypeID, fmt.Errorf("mir: async: invalid local in state payload for %s", v.name)
			}
			payload = append(payload, pollFn.Locals[localID].Type)
		}
		members = append(members, types.UnionMember{Kind: types.UnionMemberTag, TagName: tagNameID, TagArgs: payload})
	}
	typesIn.SetUnionMembers(stateID, members)

	if m.Meta == nil {
		m.Meta = &ModuleMeta{}
	}
	if m.Meta.TagNames == nil {
		m.Meta.TagNames = make(map[symbols.SymbolID]string)
	}

	tagSymByName := make(map[string]symbols.SymbolID, len(variants))
	nextSym := nextSyntheticTagSym(m, symTable)
	for i := range variants {
		name := variants[i].name
		var symID symbols.SymbolID
		if symTable != nil && symTable.Symbols != nil && symTable.Strings != nil {
			nameID := symTable.Strings.Intern(name)
			symID = symTable.Symbols.New(&symbols.Symbol{Name: nameID, Kind: symbols.SymbolTag})
		} else {
			symID = nextSym
			nextSym++
		}
		variants[i].tagSym = symID
		tagSymByName[name] = symID
		if symID.IsValid() {
			m.Meta.TagNames[symID] = name
		}
	}

	if err := ensureTagLayout(m, typesIn, tagSymByName, stateID); err != nil {
		return types.NoTypeID, err
	}
	return stateID, nil
}

func nextSyntheticTagSym(m *Module, symTable *symbols.Table) symbols.SymbolID {
	maxSym := symbols.SymbolID(0)
	if symTable != nil && symTable.Symbols != nil {
		maxSym = symbols.SymbolID(symTable.Symbols.Len()) //nolint:gosec // bounded by symbol table size
	}
	if m != nil && m.Meta != nil {
		for sym := range m.Meta.TagNames {
			if sym > maxSym {
				maxSym = sym
			}
		}
		for _, cases := range m.Meta.TagLayouts {
			for _, c := range cases {
				if c.TagSym > maxSym {
					maxSym = c.TagSym
				}
			}
		}
	}
	return maxSym + 1
}
