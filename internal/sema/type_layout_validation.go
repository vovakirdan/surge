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
		if cycle := tc.formatLayoutCycle(layoutErr.Cycle); cycle != "" {
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

func (tc *typeChecker) formatLayoutCycle(cycle []types.TypeID) string {
	if len(cycle) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cycle))
	for _, id := range cycle {
		parts = append(parts, tc.typeLabel(id))
	}
	return strings.Join(parts, " -> ")
}
