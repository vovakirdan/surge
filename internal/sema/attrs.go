package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
)

// attrNames collects raw attribute name identifiers from the AST range.
func (tc *typeChecker) attrNames(start ast.AttrID, count uint32) []source.StringID {
	if count == 0 || !start.IsValid() || tc.builder == nil {
		return nil
	}
	attrs := tc.builder.Items.CollectAttrs(start, count)
	if len(attrs) == 0 {
		return nil
	}
	names := make([]source.StringID, 0, len(attrs))
	for _, attr := range attrs {
		if attr.Name != source.NoStringID {
			names = append(names, attr.Name)
		}
	}
	return names
}
