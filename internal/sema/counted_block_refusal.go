package sema

import (
	"fmt"
	"strings"

	"surge/internal/source"
	"surge/internal/types"
)

type countedBlockCulprit struct {
	path     string
	kind     types.Kind
	widths   uint8
	complete bool
}

type countedBlockVisit struct {
	typeID       types.TypeID
	behindHandle bool
}

type countedBlockPathWalk struct {
	in     *types.Interner
	names  *source.Interner
	memo   map[countedBlockVisit]countedBlockCulprit
	active map[countedBlockVisit]bool
}

// CountedBlockRefusalMessage explains an already-refused crossing. It never
// decides admission: callers retain CountedBlockStaysShared and their existing
// diagnostic order. Width advice is useful only if every other site constraint
// would accept the same shape after replacing its inaccessible numeric leaves.
func (r *Result) CountedBlockRefusalMessage(names *source.Interner, id types.TypeID, subject string, allowFixedWidth bool) string {
	culprit := r.countedBlockCulprit(names, id)
	detail := "arbitrary-precision values"
	if culprit.widths != 0 {
		detail = fmt.Sprintf("an arbitrary-precision `%s` at `%s`", culprit.kind, culprit.path)
	}
	message := fmt.Sprintf("%s: it holds %s in storage its current owner keeps "+
		"(a map's table, a channel's ring, a task's result slot), so the counted heap "+
		"blocks behind them cannot be made private before the value crosses, and "+
		"their count is not safe to share across threads", subject, detail)
	if !allowFixedWidth || !culprit.complete || culprit.widths == 0 {
		return message
	}
	var replacements []string
	for _, kind := range []types.Kind{types.KindInt, types.KindUint, types.KindFloat} {
		if culprit.widths&countedNumericWidth(kind) != 0 {
			replacements = append(replacements, fmt.Sprintf("`%s` with `%s64`", kind, kind))
		}
	}
	return message + ". If fixed precision is sufficient, replace " +
		strings.Join(replacements, " and ") + " for all such stored values"
}

func (r *Result) countedBlockCulprit(names *source.Interner, id types.TypeID) countedBlockCulprit {
	if r == nil || r.TypeInterner == nil {
		return countedBlockCulprit{}
	}
	walk := countedBlockPathWalk{
		in: r.TypeInterner, names: names,
		memo: make(map[countedBlockVisit]countedBlockCulprit), active: make(map[countedBlockVisit]bool),
	}
	return walk.visit(id, false)
}

func countedNumericWidth(kind types.Kind) uint8 {
	switch kind {
	case types.KindInt:
		return 1
	case types.KindUint:
		return 2
	case types.KindFloat:
		return 4
	default:
		return 0
	}
}

func (w *countedBlockPathWalk) visit(id types.TypeID, behindHandle bool) countedBlockCulprit {
	key := countedBlockVisit{id, behindHandle}
	if cached, ok := w.memo[key]; ok {
		return cached
	}
	if w.active[key] {
		// A cycle leaves part of the shape unknown; a repeated completed DAG
		// node is instead served by memo, retaining its relative culprit path.
		return countedBlockCulprit{}
	}
	w.active[key] = true
	result := w.inspect(id, behindHandle)
	delete(w.active, key)
	w.memo[key] = result
	return result
}

func (w *countedBlockPathWalk) inspect(id types.TypeID, behindHandle bool) countedBlockCulprit {
	tt, ok := w.in.Lookup(id)
	if !ok || id == types.NoTypeID {
		return countedBlockCulprit{}
	}
	result := countedBlockCulprit{complete: true}
	switch tt.Kind {
	case types.KindAlias:
		target, exists := w.in.AliasTarget(id)
		if !exists {
			return countedBlockCulprit{}
		}
		return w.visit(target, behindHandle)
	case types.KindOwn:
		return w.visit(tt.Elem, behindHandle)
	case types.KindReference, types.KindPointer, types.KindFar, types.KindFn:
		return result
	case types.KindInvalid, types.KindGenericParam:
		return countedBlockCulprit{}
	}
	if w.in.IsRefCountedScalar(id) {
		if behindHandle {
			result.kind, result.widths = tt.Kind, countedNumericWidth(tt.Kind)
			result.complete = result.widths != 0
		}
		return result
	}
	appendMember := func(member types.TypeID, path string, behind bool) {
		child := w.visit(member, behind)
		result.complete = result.complete && child.complete
		if result.widths == 0 && child.widths != 0 {
			result.path, result.kind = path, child.kind
			if child.path != "" {
				result.path += "." + child.path
			}
		}
		result.widths |= child.widths
	}
	if elem, ok := w.in.DynamicArrayElem(id); ok {
		appendMember(elem, "element", behindHandle)
		return result
	}
	if w.in.RangeBoundsAreArbitraryPrecision(id) {
		bound, _ := w.in.RangeBoundType(id)
		appendMember(bound, "bound", behindHandle)
		return result
	}
	if elem, _, ok := w.in.ArrayFixedInfo(id); ok {
		appendMember(elem, "element", behindHandle)
		return result
	}
	if key, value, ok := w.in.MapInfo(id); ok {
		appendMember(key, "key", true)
		appendMember(value, "value", true)
		return result
	}
	if payloads, handle := w.in.RuntimeHandlePayloads(id); handle {
		for i, payload := range payloads {
			appendMember(payload, fmt.Sprintf("payload[%d]", i), true)
		}
		return result
	}
	switch tt.Kind {
	case types.KindArray:
		appendMember(tt.Elem, "element", behindHandle)
	case types.KindStruct:
		info, found := w.in.StructInfo(id)
		if !found || info == nil {
			return countedBlockCulprit{}
		}
		for i, field := range info.Fields {
			name := w.name(field.Name, fmt.Sprintf("field[%d]", i))
			appendMember(field.Type, name, behindHandle)
		}
	case types.KindTuple:
		info, found := w.in.TupleInfo(id)
		if !found || info == nil {
			return countedBlockCulprit{}
		}
		for i, elem := range info.Elems {
			appendMember(elem, fmt.Sprintf("%d", i), behindHandle)
		}
	case types.KindUnion:
		info, found := w.in.UnionInfo(id)
		if !found || info == nil || len(info.Members) == 0 {
			return countedBlockCulprit{}
		}
		for i, member := range info.Members {
			if member.Kind == types.UnionMemberType {
				appendMember(member.Type, fmt.Sprintf("member[%d]", i), behindHandle)
			}
			for j, arg := range member.TagArgs {
				name := w.name(member.TagName, fmt.Sprintf("tag[%d]", i))
				appendMember(arg, fmt.Sprintf("%s[%d]", name, j), behindHandle)
			}
		}
	}
	return result
}

func (w *countedBlockPathWalk) name(id source.StringID, fallback string) string {
	if w.names != nil {
		if name, ok := w.names.Lookup(id); ok && name != "" {
			return name
		}
	}
	return fallback
}
