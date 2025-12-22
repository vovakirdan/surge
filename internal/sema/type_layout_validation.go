package sema

import (
	"errors"
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/types"
)

func (tc *typeChecker) validateTypeLayouts() {
	if tc == nil || tc.types == nil || tc.builder == nil || tc.reporter == nil || tc.typeIDItems == nil {
		return
	}

	le := layout.New(layout.X86_64LinuxGNU(), tc.types)
	reported := make(map[types.TypeID]struct{}, len(tc.typeIDItems))

	for typeID, itemID := range tc.typeIDItems {
		if typeID == types.NoTypeID {
			continue
		}
		if _, ok := reported[typeID]; ok {
			continue
		}

		if attrs, ok := tc.types.TypeLayoutAttrs(typeID); ok {
			if attrs.Packed && attrs.AlignOverride != nil {
				continue
			}
		}

		_, err := le.LayoutOf(typeID)
		if err == nil {
			continue
		}
		var layoutErr *layout.LayoutError
		if !errors.As(err, &layoutErr) || layoutErr.Kind != layout.LayoutErrRecursiveUnsized {
			continue
		}

		span := tc.itemSpan(itemID)
		if span == (source.Span{}) {
			span = tc.fallbackTypeSpan(typeID)
		}
		msg := fmt.Sprintf("recursive value type %s has infinite size", tc.typeLabel(typeID))
		if cycle := tc.formatLayoutCycle(typeID, layoutErr.Cycle); cycle != "" {
			msg += ": " + cycle
		}
		if b := diag.ReportError(tc.reporter, diag.SemaRecursiveUnsized, span, msg); b != nil {
			b.Emit()
		}
		reported[typeID] = struct{}{}
	}
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

func (tc *typeChecker) formatLayoutCycle(typeID types.TypeID, cycle []types.TypeID) string {
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
		label := tc.typeLabel(typeID)
		if label != "" {
			for i, id := range normalized {
				if tc.typeLabel(id) == label {
					start = i
					break
				}
			}
		}
	}
	if start == -1 {
		start = 0
		minLabel := tc.typeLabel(normalized[0])
		for i := 1; i < len(normalized); i++ {
			label := tc.typeLabel(normalized[i])
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
		parts = append(parts, tc.typeLabel(id))
	}
	parts = append(parts, tc.typeLabel(normalized[0]))
	return strings.Join(parts, " -> ")
}
