package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// DirectCloneRequest is one `clone(&value)` on a concrete non-Copy type,
// recorded while checking a file and answered once against the whole-program
// callable catalog. Owner is the enclosing function: without it the selected
// body is unreachable and dead-code elimination drops it.
type DirectCloneRequest struct {
	Use          ast.ExprID
	Owner        symbols.SymbolID
	Receiver     types.TypeID
	AccessModule string
	Site         source.Span
	SourceKey    string
	TypeLabel    string
}

// DirectCloneBinding names the canonical implementation chosen for one direct
// use. Use stays in the vocabulary of the file that owns the expression.
type DirectCloneBinding struct {
	Use          ast.ExprID
	Callee       symbols.SymbolID
	CalleeKey    string
	TemplateArgs []types.TypeID
	Site         source.Span
	SourceKey    string
}

func (tc *typeChecker) recordDirectCloneRequest(request *DirectCloneRequest) {
	if tc == nil || tc.result == nil || !request.Use.IsValid() || request.Receiver == types.NoTypeID {
		return
	}
	tc.result.DirectCloneRequests = append(tc.result.DirectCloneRequests, *request)
}

func canonicalizeDirectCloneSources(result *Result, resolve func(source.FileID) (string, error)) error {
	for i := range result.DirectCloneRequests {
		request := &result.DirectCloneRequests[i]
		key, err := resolve(request.Site.File)
		if err != nil {
			return fmt.Errorf("direct clone source: %w", err)
		}
		if key == "" {
			return fmt.Errorf("direct clone has empty canonical source identity")
		}
		request.SourceKey = key
	}
	return nil
}

// mergeDirectCloneRequests carries per-file requests into the merged authority.
// Use is deliberately not remapped: it addresses an expression in the file that
// owns it, and (SourceKey, Use) is that expression's portable identity.
func mergeDirectCloneRequests(dst, src *Result, mapping map[symbols.SymbolID]symbols.SymbolID) {
	for i := range src.DirectCloneRequests {
		request := src.DirectCloneRequests[i]
		request.Owner = remapInstantiationSymbol(request.Owner, mapping)
		dst.DirectCloneRequests = append(dst.DirectCloneRequests, request)
	}
}

func cloneDirectCloneRequests(input []DirectCloneRequest) []DirectCloneRequest {
	return slices.Clone(input)
}

func cloneDirectCloneBindings(input []DirectCloneBinding) []DirectCloneBinding {
	out := make([]DirectCloneBinding, len(input))
	for i := range input {
		out[i] = input[i]
		out[i].TemplateArgs = slices.Clone(input[i].TemplateArgs)
	}
	return out
}

func compareDirectCloneRequests(left, right *DirectCloneRequest) int {
	if left.SourceKey != right.SourceKey {
		if left.SourceKey < right.SourceKey {
			return -1
		}
		return 1
	}
	if cmp := compareSpanOffsets(left.Site, right.Site); cmp != 0 {
		return cmp
	}
	if left.Use == right.Use {
		return 0
	}
	if left.Use < right.Use {
		return -1
	}
	return 1
}
