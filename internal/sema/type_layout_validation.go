package sema

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/types"
)

type layoutObligation struct {
	typeID        types.TypeID
	span          source.Span
	substitutions map[types.TypeID]types.TypeID
}

func (tc *typeChecker) validateTypeLayouts() {
	if tc == nil || tc.types == nil || tc.builder == nil || tc.reporter == nil || tc.result == nil {
		return
	}

	obligations := tc.collectLayoutObligations()
	sort.SliceStable(obligations, func(i, j int) bool {
		a, b := obligations[i], obligations[j]
		if a.span.File != b.span.File {
			return a.span.File < b.span.File
		}
		if a.span.Start != b.span.Start {
			return a.span.Start < b.span.Start
		}
		if a.span.End != b.span.End {
			return a.span.End < b.span.End
		}
		return tc.layoutTypeLabel(a.typeID, a.substitutions) < tc.layoutTypeLabel(b.typeID, b.substitutions)
	})

	reported := make(map[string]struct{}, len(obligations))
	for _, obligation := range obligations {
		if tc.skipLayoutObligation(obligation.typeID) {
			continue
		}
		key := fmt.Sprintf("%d:%s", obligation.typeID, tc.layoutTypeLabel(obligation.typeID, obligation.substitutions))
		if _, ok := reported[key]; ok {
			continue
		}
		le := layout.NewWithSubstitutions(layout.X86_64LinuxGNU(), tc.types, obligation.substitutions)
		_, err := le.LayoutOf(obligation.typeID)
		if err == nil {
			continue
		}
		var layoutErr *layout.LayoutError
		if !errors.As(err, &layoutErr) {
			continue
		}
		if layoutErr.Kind == layout.ErrDeferred && types.ContainsGenericParam(tc.types, obligation.typeID) {
			continue
		}
		if tc.reportLayoutError(obligation, layoutErr) {
			reported[key] = struct{}{}
		}
	}
}

func (tc *typeChecker) skipLayoutObligation(typeID types.TypeID) bool {
	if typeID == types.NoTypeID {
		return true
	}
	t, ok := tc.types.Lookup(typeID)
	if !ok || t.Kind == types.KindInvalid || t.Kind == types.KindConst {
		return true
	}
	if attrs, ok := tc.types.TypeLayoutAttrs(typeID); ok {
		return attrs.Packed && attrs.AlignOverride != nil
	}
	return false
}

func (tc *typeChecker) reportLayoutError(obligation layoutObligation, err *layout.LayoutError) bool {
	if err == nil {
		return false
	}
	label := tc.layoutTypeLabel(obligation.typeID, obligation.substitutions)
	var code diag.Code
	message := ""
	switch err.Kind {
	case layout.ErrRecursiveUnsized:
		code = diag.SemaRecursiveUnsized
		message = fmt.Sprintf("recursive value type %s has infinite size", label)
		if cycle := tc.formatLayoutCycle(obligation.typeID, err.Cycle(), obligation.substitutions); cycle != "" {
			message += ": " + cycle
		}
	case layout.ErrOverflow:
		code = diag.SemaLayoutOverflow
		message = fmt.Sprintf("physical layout of %s exceeds the target address space", label)
	case layout.ErrUnsupportedAlignment:
		code = diag.SemaLayoutUnsupportedAlignment
		message = fmt.Sprintf("alignment %d for %s is not supported by this target", err.Value, label)
	case layout.ErrDeferred:
		code = diag.SemaLayoutDeferred
		message = fmt.Sprintf("physical layout of %s is not concrete after type checking", label)
	default:
		return false
	}
	b := diag.ReportError(tc.reporter, code, obligation.span, message)
	if b == nil {
		return false
	}
	if cycle := tc.formatLayoutCycle(obligation.typeID, err.Cycle(), obligation.substitutions); cycle != "" {
		b.WithNote(obligation.span, "by-value cycle: "+cycle)
	}
	if path := tc.formatLayoutPath(obligation.typeID, err.Path(), obligation.substitutions); path != "" {
		b.WithNote(obligation.span, "layout path: "+path)
	}
	switch err.Kind {
	case layout.ErrOverflow:
		b.WithNote(obligation.span, "stored fields and payloads must fit in the target's address space")
	case layout.ErrUnsupportedAlignment:
		b.WithNote(obligation.span, fmt.Sprintf("use a power-of-two alignment no greater than %d", err.Limit))
	case layout.ErrDeferred:
		b.WithNote(obligation.span, "all stored fields and payloads must have concrete types before code generation")
	}
	b.Emit()
	return true
}

func (tc *typeChecker) itemSpan(itemID ast.ItemID) source.Span {
	if !itemID.IsValid() || tc.builder == nil {
		return source.Span{}
	}
	item := tc.builder.Items.Get(itemID)
	if item == nil {
		return source.Span{}
	}
	return item.Span
}

func (tc *typeChecker) fallbackTypeSpan(typeID types.TypeID) source.Span {
	if tc == nil || tc.typeIDItems == nil {
		return source.Span{}
	}
	itemID := tc.typeIDItems[typeID]
	if !itemID.IsValid() {
		return source.Span{}
	}
	return tc.itemSpan(itemID)
}

func (tc *typeChecker) formatLayoutPath(
	root types.TypeID,
	path []layout.PathElement,
	substitutions map[types.TypeID]types.TypeID,
) string { //nolint:gocyclo
	if len(path) == 0 || tc == nil || tc.types == nil {
		return ""
	}
	parts := []string{tc.layoutTypeLabel(root, substitutions)}
	current := root
	pendingUnion := types.NoTypeID
	pendingCase := -1
	for _, edge := range path {
		switch edge.Kind {
		case layout.PathAliasTarget:
			if target, ok := tc.types.AliasTarget(tc.layoutSubstitute(current, substitutions)); ok {
				current = target
				parts = append(parts, "alias target "+tc.layoutTypeLabel(current, substitutions))
			} else {
				parts = append(parts, edge.String())
			}
		case layout.PathOwnValue:
			if t, ok := tc.types.Lookup(tc.layoutSubstitute(current, substitutions)); ok {
				current = t.Elem
				parts = append(parts, "owned value "+tc.layoutTypeLabel(current, substitutions))
			} else {
				parts = append(parts, edge.String())
			}
		case layout.PathArrayElement:
			resolved := tc.resolveAlias(tc.layoutSubstitute(current, substitutions))
			if elem, _, ok := tc.types.ArrayFixedInfo(resolved); ok {
				current = elem
			} else if t, ok := tc.types.Lookup(resolved); ok {
				current = t.Elem
			}
			parts = append(parts, "array element "+tc.layoutTypeLabel(current, substitutions))
		case layout.PathStructField:
			resolved := tc.resolveAlias(tc.layoutSubstitute(current, substitutions))
			info, ok := tc.types.StructInfo(resolved)
			if !ok || info == nil || int(edge.Index) >= len(info.Fields) {
				parts = append(parts, edge.String())
				continue
			}
			field := info.Fields[edge.Index]
			name := tc.lookupName(field.Name)
			if name == "" {
				name = fmt.Sprintf("#%d", edge.Index+1)
			}
			current = field.Type
			parts = append(parts, fmt.Sprintf("field %s (%s)", name, tc.layoutTypeLabel(current, substitutions)))
		case layout.PathTupleElement:
			resolved := tc.resolveAlias(tc.layoutSubstitute(current, substitutions))
			info, ok := tc.types.TupleInfo(resolved)
			if !ok || info == nil || int(edge.Index) >= len(info.Elems) {
				parts = append(parts, edge.String())
				continue
			}
			current = info.Elems[edge.Index]
			parts = append(parts, fmt.Sprintf("tuple element %d (%s)", edge.Index, tc.layoutTypeLabel(current, substitutions)))
		case layout.PathUnionCase:
			resolved := tc.resolveAlias(tc.layoutSubstitute(current, substitutions))
			info, ok := tc.types.UnionInfo(resolved)
			if !ok || info == nil || int(edge.Index) >= len(info.Members) {
				parts = append(parts, edge.String())
				continue
			}
			pendingUnion, pendingCase = resolved, int(edge.Index)
			member := info.Members[edge.Index]
			caseName := fmt.Sprintf("#%d", edge.Index+1)
			switch member.Kind {
			case types.UnionMemberType:
				current = member.Type
				caseName = tc.layoutTypeLabel(current, substitutions)
			case types.UnionMemberTag:
				if name := tc.lookupName(member.TagName); name != "" {
					caseName = name
				}
			case types.UnionMemberNothing:
				caseName = "nothing"
			}
			parts = append(parts, "union case "+caseName)
		case layout.PathUnionPayload:
			info, ok := tc.types.UnionInfo(pendingUnion)
			if !ok || info == nil || pendingCase < 0 || pendingCase >= len(info.Members) {
				parts = append(parts, edge.String())
				continue
			}
			member := info.Members[pendingCase]
			if int(edge.Index) >= len(member.TagArgs) {
				parts = append(parts, edge.String())
				continue
			}
			current = member.TagArgs[edge.Index]
			parts = append(parts, fmt.Sprintf("payload %d (%s)", edge.Index+1, tc.layoutTypeLabel(current, substitutions)))
		case layout.PathEnumBase:
			resolved := tc.resolveAlias(tc.layoutSubstitute(current, substitutions))
			if info, ok := tc.types.EnumInfo(resolved); ok && info != nil {
				current = info.BaseType
				parts = append(parts, "enum base "+tc.layoutTypeLabel(current, substitutions))
			} else {
				parts = append(parts, edge.String())
			}
		default:
			parts = append(parts, edge.String())
		}
	}
	return strings.Join(parts, " -> ")
}

func (tc *typeChecker) formatLayoutCycle(
	typeID types.TypeID,
	cycle []types.TypeID,
	substitutions map[types.TypeID]types.TypeID,
) string {
	if len(cycle) == 0 {
		return ""
	}
	normalized := append([]types.TypeID(nil), cycle...)
	if len(normalized) > 1 && normalized[0] == normalized[len(normalized)-1] {
		normalized = normalized[:len(normalized)-1]
	}
	if len(normalized) == 0 {
		return ""
	}
	start := -1
	if typeID != types.NoTypeID {
		for i, id := range normalized {
			if id == typeID {
				start = i
				break
			}
		}
	}
	if start == -1 && typeID != types.NoTypeID {
		label := tc.layoutTypeLabel(typeID, substitutions)
		if label != "" {
			for i, id := range normalized {
				if tc.layoutTypeLabel(id, substitutions) == label {
					start = i
					break
				}
			}
		}
	}
	if start == -1 {
		start = 0
		minLabel := tc.layoutTypeLabel(normalized[0], substitutions)
		for i := 1; i < len(normalized); i++ {
			label := tc.layoutTypeLabel(normalized[i], substitutions)
			if label < minLabel {
				minLabel = label
				start = i
			}
		}
	}
	if start != 0 {
		rotated := make([]types.TypeID, 0, len(normalized))
		rotated = append(rotated, normalized[start:]...)
		rotated = append(rotated, normalized[:start]...)
		normalized = rotated
	}
	parts := make([]string, 0, len(normalized)+1)
	for _, id := range normalized {
		parts = append(parts, tc.layoutTypeLabel(id, substitutions))
	}
	parts = append(parts, tc.layoutTypeLabel(normalized[0], substitutions))
	return strings.Join(parts, " -> ")
}
