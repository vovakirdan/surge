package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// validateDirectiveNamespaces checks that all directive blocks reference
// imported modules that have pragma directive.
func (tc *typeChecker) validateDirectiveNamespaces() {
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil || len(file.Directives) == 0 {
		return
	}

	for i := range file.Directives {
		tc.validateDirectiveBlock(&file.Directives[i])
	}
}

func (tc *typeChecker) validateDirectiveBlock(block *ast.DirectiveBlock) {
	if block == nil || block.Namespace == source.NoStringID {
		return
	}

	namespace := tc.builder.StringsInterner.MustLookup(block.Namespace)

	// Check if namespace is an imported module with pragma directive
	if tc.exports == nil {
		tc.reportDirectiveError(
			diag.SemaDirectiveUnknownNamespace,
			block.Span,
			"directive namespace '%s' is not an imported module",
			namespace,
		)
		return
	}

	// Look for module by last path segment matching namespace
	var foundExports *symbols.ModuleExports
	for path, exp := range tc.exports {
		// Extract last segment of module path
		lastSeg := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			lastSeg = path[idx+1:]
		}
		if lastSeg == namespace {
			foundExports = exp
			break
		}
	}

	if foundExports == nil {
		tc.reportDirectiveError(
			diag.SemaDirectiveUnknownNamespace,
			block.Span,
			"directive namespace '%s' is not an imported module",
			namespace,
		)
		return
	}

	// Check if the imported module has pragma directive
	if foundExports.PragmaFlags&ast.PragmaFlagDirective == 0 {
		tc.reportDirectiveError(
			diag.SemaDirectiveNotDirectiveModule,
			block.Span,
			"module '%s' does not have 'pragma directive'",
			namespace,
		)
	}
}

func (tc *typeChecker) reportDirectiveError(code diag.Code, span source.Span, format string, args ...any) {
	if tc.reporter == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if b := diag.ReportError(tc.reporter, code, span, msg); b != nil {
		b.Emit()
	}
}
