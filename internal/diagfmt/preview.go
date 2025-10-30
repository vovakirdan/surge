package diagfmt

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/diag"
	"surge/internal/source"
)

type fixEditPreview struct {
	before []string
	after  []string
}

func buildFixEditPreview(fs *source.FileSet, edit diag.TextEdit) (fixEditPreview, error) {
	if fs == nil {
		return fixEditPreview{}, fmt.Errorf("nil FileSet")
	}
	file := fs.Get(edit.Span.File)
	if file == nil {
		return fixEditPreview{}, fmt.Errorf("file %d not found in FileSet", edit.Span.File)
	}

	startPos, endPos := fs.Resolve(edit.Span)
	startLine := startPos.Line
	endLine := endPos.Line
	if endLine < startLine {
		endLine = startLine
	}

	blockStart := lineStartOffset(file, startLine)
	blockEnd := max(lineEndOffsetInclusive(file, endLine), blockStart)

	lenFileContent, err := safecast.Conv[uint32](len(file.Content))
	if err != nil {
		return fixEditPreview{}, fmt.Errorf("len file content overflow: %w", err)
	}
	blockEnd = min(blockEnd, lenFileContent)

	original := make([]byte, blockEnd-blockStart)
	copy(original, file.Content[blockStart:blockEnd])

	relStart := int(edit.Span.Start - blockStart)
	relEnd := int(edit.Span.End - blockStart)

	if relStart < 0 || relStart > len(original) {
		return fixEditPreview{}, fmt.Errorf("edit span start %d out of range for preview block", relStart)
	}
	if relEnd < relStart || relEnd > len(original) {
		return fixEditPreview{}, fmt.Errorf("edit span end %d out of range for preview block", relEnd)
	}

	after := make([]byte, 0, len(original)+len(edit.NewText))
	after = append(after, original[:relStart]...)
	after = append(after, edit.NewText...)
	after = append(after, original[relEnd:]...)

	return fixEditPreview{
		before: splitPreviewLines(original),
		after:  splitPreviewLines(after),
	}, nil
}

func splitPreviewLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	text := string(content)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	// strings.Split("a", "\n") returns ["a"], but if string ends with \n, last element empty.
	//// We keep trailing empty string to reflect blank lines in preview.
	// so we trim the trailing \n
	return lines
}

func lineStartOffset(f *source.File, line uint32) uint32 {
	if line <= 1 {
		return 0
	}
	idx := line - 2
	if int(idx) < len(f.LineIdx) {
		return f.LineIdx[idx] + 1
	}
	lenFileContent, err := safecast.Conv[uint32](len(f.Content))
	if err != nil {
		panic(fmt.Errorf("len file content overflow: %w", err))
	}
	return lenFileContent
}

func lineEndOffsetInclusive(f *source.File, line uint32) uint32 {
	if line == 0 {
		return 0
	}
	idx := line - 1
	if int(idx) < len(f.LineIdx) {
		return f.LineIdx[idx] + 1
	}
	lenFileContent, err := safecast.Conv[uint32](len(f.Content))
	if err != nil {
		panic(fmt.Errorf("len file content overflow: %w", err))
	}
	return lenFileContent
}
