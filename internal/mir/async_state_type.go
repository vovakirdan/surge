package mir

import (
	"fmt"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const (
	asyncStatePcField      = "__pc"
	asyncStatePayloadField = "__payload"
)

func buildAsyncPayloadUnion(m *Module, typesIn *types.Interner, symTable *symbols.Table, f, pollFn *Func, variants []stateVariant) (types.TypeID, error) {
	if typesIn == nil || typesIn.Strings == nil {
		return types.NoTypeID, fmt.Errorf("mir: async: missing type interner strings")
	}
	name := asyncPayloadTypePrefix + f.Name
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

func buildAsyncStateStruct(typesIn *types.Interner, f, pollFn *Func, payloadType types.TypeID, residents residentSet) (types.TypeID, error) {
	if typesIn == nil || typesIn.Strings == nil {
		return types.NoTypeID, fmt.Errorf("mir: async: missing type interner strings")
	}
	name := asyncStateTypePrefix + f.Name
	nameID := typesIn.Strings.Intern(name)
	stateID := typesIn.RegisterStruct(nameID, source.Span{})

	// The lifecycle word leads, so it lands at offset zero (see FrameStateField).
	// The resume point and the packed payload follow it; both are read only by
	// code that already knows this is a frame, while the word answers a reader
	// that knows nothing else about the storage it is holding.
	fields := []types.StructField{
		{Name: typesIn.Strings.Intern(FrameStateField), Type: typesIn.Builtins().Int},
		{Name: typesIn.Strings.Intern(asyncStatePcField), Type: typesIn.Builtins().Int},
		{Name: typesIn.Strings.Intern(asyncStatePayloadField), Type: payloadType},
	}
	// Residents sit BESIDE the payload, never inside it. Inside, they would be
	// members of one union arm and would therefore change address -- or cease to
	// exist -- the moment a suspension selected a different arm, which is the very
	// thing the promotion exists to prevent. Beside it, each keeps one offset for
	// the life of the activation.
	for _, id := range residents.order {
		if pollFn == nil || int(id) >= len(pollFn.Locals) {
			return types.NoTypeID, fmt.Errorf("mir: async: resident local %d is not a local of %s", int64(id), f.Name)
		}
		fields = append(fields, types.StructField{
			Name: typesIn.Strings.Intern(residents.fields[id]),
			Type: pollFn.Locals[id].Type,
		})
	}
	typesIn.SetStructFields(stateID, fields)
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
