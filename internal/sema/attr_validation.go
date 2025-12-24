package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

// AttrInfo holds information about a parsed attribute including its spec and arguments
type AttrInfo struct {
	Spec ast.AttrSpec // Attribute specification from catalog
	Attr *ast.Attr    // The actual attribute node
	Span source.Span  // Source location
	Args []ast.ExprID // Argument expressions
}

// collectAttrs gathers all attributes from the given range and returns parsed AttrInfo
func (tc *typeChecker) collectAttrs(start ast.AttrID, count uint32) []AttrInfo {
	if count == 0 || !start.IsValid() {
		return nil
	}

	attrs := tc.builder.Items.CollectAttrs(start, count)
	result := make([]AttrInfo, 0, len(attrs))

	for _, attr := range attrs {
		spec, ok := ast.LookupAttrID(tc.builder.StringsInterner, attr.Name)
		if !ok {
			// Unknown attribute - will be reported by validateAttrs
			continue
		}

		// Collect arguments
		args := make([]ast.ExprID, 0, len(attr.Args))
		args = append(args, attr.Args...)

		result = append(result, AttrInfo{
			Spec: spec,
			Attr: &attr,
			Span: attr.Span,
			Args: args,
		})
	}

	return result
}

// hasAttr checks if the given attribute name exists in the list
// Returns the AttrInfo and true if found, zero value and false otherwise
func hasAttr(infos []AttrInfo, attrName string) (AttrInfo, bool) {
	for _, info := range infos {
		if strings.EqualFold(info.Spec.Name, attrName) {
			return info, true
		}
	}
	return AttrInfo{}, false
}

// checkConflict detects if two conflicting attributes appear together
func (tc *typeChecker) checkConflict(infos []AttrInfo, attr1, attr2 string, code diag.Code) {
	_, has1 := hasAttr(infos, attr1)
	info2, has2 := hasAttr(infos, attr2)

	if has1 && has2 {
		tc.report(code, info2.Span,
			"attribute '@%s' conflicts with '@%s'", attr2, attr1)
	}
}

// checkPackedAlignConflict is a special handler for @packed + @align conflicts
// @packed and @align can coexist if alignment is natural, but we reject them
// together to keep validation simple
func (tc *typeChecker) checkPackedAlignConflict(infos []AttrInfo) {
	packedInfo, hasPacked := hasAttr(infos, "packed")
	alignInfo, hasAlign := hasAttr(infos, "align")

	if hasPacked && hasAlign {
		tc.report(diag.SemaAttrPackedAlign, alignInfo.Span,
			"@align conflicts with @packed on the same declaration")
		// Also report on packed for clarity
		tc.report(diag.SemaAttrPackedAlign, packedInfo.Span,
			"@packed conflicts with @align on the same declaration")
	}
}

// validateAllConflicts checks for all known conflicting attribute pairs
func (tc *typeChecker) validateAllConflicts(infos []AttrInfo) {
	// @send vs @nosend
	tc.checkConflict(infos, "send", "nosend", diag.SemaAttrSendNosend)

	// @nonblocking vs @waits_on
	tc.checkConflict(infos, "nonblocking", "waits_on", diag.SemaAttrNonblockingWaitsOn)

	// @packed vs @align (special handler)
	tc.checkPackedAlignConflict(infos)
}
