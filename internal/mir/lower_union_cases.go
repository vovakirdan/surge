package mir

import (
	"fmt"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// What a union CONTAINS, as opposed to what a tag switch over it looks like.
//
// The membership is the direct, unflattened truth: one entry per
// UnionInfo.Members entry, in that order, including bare type members that the
// flattened tag view drops entirely. It lives apart from lower_meta.go because
// the two answer different questions, and a reader who takes one for the other
// gets a number that indexes the wrong list.

// buildUnionCases records the FULL direct membership of every union the module
// mentions, in UnionInfo.Members order.
//
// It shares nothing with buildTagLayouts on purpose. That one flattens nested
// unions into the outer tag list and deduplicates by name, which is right for a
// tag switch and wrong for anything asking what a union contains — so the two
// answers must come from two functions rather than from one with a flag.
//
// The index recorded here is the index the physical layout uses, because both
// are positions in the same slice: the layout engine walks info.Members and
// stamps case i from payloads[i].
func buildUnionCases(
	typeIDs map[types.TypeID]struct{},
	typesIn *types.Interner,
	tagSymByName map[source.StringID]symbols.SymbolID,
) map[types.TypeID][]UnionCaseMeta {
	if typesIn == nil || typesIn.Strings == nil || len(typeIDs) == 0 {
		return nil
	}
	out := make(map[types.TypeID][]UnionCaseMeta)
	for typeID := range typeIDs {
		tt, ok := typesIn.Lookup(typeID)
		if !ok || tt.Kind != types.KindUnion {
			continue
		}
		info, ok := typesIn.UnionInfo(typeID)
		if !ok || info == nil || len(info.Members) == 0 {
			continue
		}
		cases := make([]UnionCaseMeta, 0, len(info.Members))
		for index := range info.Members {
			member := &info.Members[index]
			meta := UnionCaseMeta{PhysicalCaseIndex: index, BareType: types.NoTypeID}
			switch member.Kind {
			case types.UnionMemberTag:
				meta.Kind = UnionCaseTag
				meta.Name = typesIn.Strings.MustLookup(member.TagName)
				meta.TagSym = tagSymByName[member.TagName]
				meta.PayloadTypes = make([]types.TypeID, len(member.TagArgs))
				for i := range member.TagArgs {
					meta.PayloadTypes[i] = canonicalType(typesIn, member.TagArgs[i])
				}
			case types.UnionMemberNothing:
				meta.Kind = UnionCaseNothing
				meta.Name = "nothing"
			case types.UnionMemberType:
				memberType := canonicalType(typesIn, member.Type)
				if memberType == types.NoTypeID {
					continue
				}
				meta.Kind = UnionCaseBareType
				meta.Name = fmt.Sprintf("type#%d", memberType)
				meta.BareType = memberType
				meta.PayloadTypes = []types.TypeID{memberType}
			default:
				continue
			}
			cases = append(cases, meta)
		}
		// A case dropped above would silently shift every later index away from
		// the layout's, so refuse the whole union rather than record a list that
		// looks complete and is not.
		if len(cases) != len(info.Members) {
			continue
		}
		out[typeID] = cases
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
