package diagfmt

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
)

type ASTNodeOutput struct {
	Type     string          `json:"type"`
	Kind     string          `json:"kind,omitempty"`
	Span     source.Span     `json:"span"`
	Text     string          `json:"text,omitempty"`
	Children []ASTNodeOutput `json:"children,omitempty"`
	Fields   map[string]any  `json:"fields,omitempty"`
}

func FormatASTPretty(w io.Writer, builder *ast.Builder, fileID ast.FileID, fs *source.FileSet) error {
	file := builder.Files.Get(fileID)
	if file == nil {
		return fmt.Errorf("file not found")
	}

	header := "File"
	if fs != nil {
		srcFile := fs.Get(file.Span.File)
		header = srcFile.FormatPath("auto", fs.BaseDir())
	}
	fmt.Fprintf(w, "%s (span: %s)\n", header, formatSpan(file.Span, fs))

	for i, itemID := range file.Items {
		isLast := i == len(file.Items)-1
		var prefix string
		if isLast {
			fmt.Fprintf(w, "└─ Item[%d]: ", i)
			prefix = "   "
		} else {
			fmt.Fprintf(w, "├─ Item[%d]: ", i)
			prefix = "│  "
		}
		if err := formatItemPretty(w, builder, itemID, fs, prefix); err != nil {
			return err
		}
	}

	return nil
}

func FormatASTTree(w io.Writer, builder *ast.Builder, fileID ast.FileID, fs *source.FileSet) error {
	file := builder.Files.Get(fileID)
	if file == nil {
		return fmt.Errorf("file not found")
	}

	root := buildFileTreeNode(builder, fileID, fs)
	block := renderTree(root)
	for _, line := range block.lines {
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
	return nil
}

// BuildASTJSON формирует JSON-представление AST для заданного файла.
func BuildASTJSON(builder *ast.Builder, fileID ast.FileID) (ASTNodeOutput, error) {
	file := builder.Files.Get(fileID)
	if file == nil {
		return ASTNodeOutput{}, fmt.Errorf("file not found")
	}

	var children []ASTNodeOutput
	for _, itemID := range file.Items {
		itemNode, err := formatItemJSON(builder, itemID)
		if err != nil {
			return ASTNodeOutput{}, err
		}
		children = append(children, itemNode)
	}

	output := ASTNodeOutput{
		Type:     "File",
		Span:     file.Span,
		Children: children,
	}

	return output, nil
}

func FormatASTJSON(w io.Writer, builder *ast.Builder, fileID ast.FileID) error {
	output, err := BuildASTJSON(builder, fileID)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
