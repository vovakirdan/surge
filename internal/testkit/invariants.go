package testkit

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
)

// CheckSpanInvariants runs a minimal set of span invariants on a parsed file:
// 1) file.Span is non-empty and within file content bounds
// 2) every item span is non-empty and fully contained in file.Span
// 3) file.Span covers the union of item spans (if any items exist)
func CheckSpanInvariants(b *ast.Builder, fileID ast.FileID, sf *source.File) error {
	if b == nil || sf == nil {
		return fmt.Errorf("nil builder or file")
	}
	f := b.Files.Get(fileID)
	if f == nil {
		return fmt.Errorf("file node not found")
	}

	// 1) file span sanity
	if f.Span.End <= f.Span.Start {
		return fmt.Errorf("file span is empty: %v", f.Span)
	}
	if f.Span.File != sf.ID {
		return fmt.Errorf("file span points to different file id: got=%d want=%d", f.Span.File, sf.ID)
	}
	lenContent, err := safecast.Conv[uint32](len(sf.Content))
	if err != nil {
		return fmt.Errorf("len content overflow: %w", err)
	}
	if f.Span.End > lenContent {
		return fmt.Errorf("file span end beyond content: %d > %d", f.Span.End, lenContent)
	}

	// 2) item spans within file span; 3) file covers union
	var union source.Span
	var haveItem bool
	for _, it := range f.Items {
		item := b.Items.Get(it)
		if item == nil {
			return fmt.Errorf("nil item for id=%d", it)
		}
		sp := item.Span
		if sp.End <= sp.Start {
			return fmt.Errorf("empty item span: %v", sp)
		}
		if sp.File != sf.ID {
			return fmt.Errorf("item span file mismatch: got=%d want=%d", sp.File, sf.ID)
		}
		// item inside file
		if sp.Start < f.Span.Start || sp.End > f.Span.End {
			return fmt.Errorf("item span %v is outside file span %v", sp, f.Span)
		}
		if !haveItem {
			union = sp
			haveItem = true
		} else {
			union = union.Cover(sp)
		}
	}

	if haveItem {
		// file covers union
		if union.Start < f.Span.Start || union.End > f.Span.End {
			return fmt.Errorf("file span %v does not cover union of items %v", f.Span, union)
		}
	}
	return nil
}
