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

func formatItemPretty(w io.Writer, builder *ast.Builder, itemID ast.ItemID, fs *source.FileSet, prefix string) error {
	item := builder.Items.Get(itemID)
	if item == nil {
		fmt.Fprintf(w, "nil item\n")
		return nil
	}

	kindStr := formatItemKind(item.Kind)
	fmt.Fprintf(w, "%s (span: %s)\n", kindStr, formatSpan(item.Span, fs))

	// Handle special items with payload
	switch item.Kind {
	case ast.ItemImport:
		if importItem, ok := builder.Items.Import(itemID); ok {
			// Count how many fields we have to determine which one is last
			hasAlias := importItem.ModuleAlias != 0
			hasOne := importItem.HasOne
			hasGroup := len(importItem.Group) > 0

			fieldsCount := 1 // always have Module
			if hasAlias {
				fieldsCount++
			}
			if hasOne {
				fieldsCount++
			}
			if hasGroup {
				fieldsCount++
			}

			currentField := 0

			// Module is always first
			currentField++
			modulePrefix := "├─"
			if currentField == fieldsCount {
				modulePrefix = "└─"
			}
			fmt.Fprintf(w, "%s%s Module: ", prefix, modulePrefix)
			for i, stringID := range importItem.Module {
				if i > 0 {
					fmt.Fprintf(w, "::")
				}
				fmt.Fprintf(w, "%s", builder.StringsInterner.MustLookup(stringID))
			}
			fmt.Fprintf(w, "\n")

			if hasAlias {
				currentField++
				aliasPrefix := "├─"
				if currentField == fieldsCount {
					aliasPrefix = "└─"
				}
				fmt.Fprintf(w, "%s%s Alias: %s\n", prefix, aliasPrefix, builder.StringsInterner.MustLookup(importItem.ModuleAlias))
			}

			if hasOne {
				currentField++
				onePrefix := "├─"
				if currentField == fieldsCount {
					onePrefix = "└─"
				}
				fmt.Fprintf(w, "%s%s One: %s", prefix, onePrefix, formatImportOne(importItem.One, builder))
				if importItem.One.Alias != 0 {
					fmt.Fprintf(w, " as %s", builder.StringsInterner.MustLookup(importItem.One.Alias))
				}
				fmt.Fprintf(w, "\n")
			}

			if hasGroup {
				fmt.Fprintf(w, "%s└─ Group:\n", prefix)
				for i, pair := range importItem.Group {
					isLastInGroup := i == len(importItem.Group)-1
					groupItemPrefix := "├─"
					if isLastInGroup {
						groupItemPrefix = "└─"
					}
					fmt.Fprintf(w, "%s   %s [%d] %s", prefix, groupItemPrefix, i, builder.StringsInterner.MustLookup(pair.Name))
					if pair.Alias != 0 {
						fmt.Fprintf(w, " as %s", builder.StringsInterner.MustLookup(pair.Alias))
					}
					fmt.Fprintf(w, "\n")
				}
			}
		}
	}

	return nil
}

func formatItemJSON(builder *ast.Builder, itemID ast.ItemID) (ASTNodeOutput, error) {
	item := builder.Items.Get(itemID)
	if item == nil {
		return ASTNodeOutput{}, fmt.Errorf("item not found")
	}

	output := ASTNodeOutput{
		Type: "Item",
		Kind: formatItemKind(item.Kind),
		Span: item.Span,
	}

	// Handle special items with payload
	switch item.Kind {
	case ast.ItemImport:
		if importItem, ok := builder.Items.Import(itemID); ok {
			fields := make(map[string]any)

			var moduleStrs []string
			for _, stringID := range importItem.Module {
				moduleStrs = append(moduleStrs, builder.StringsInterner.MustLookup(stringID))
			}
			fields["module"] = moduleStrs

			if importItem.ModuleAlias != 0 {
				fields["moduleAlias"] = builder.StringsInterner.MustLookup(importItem.ModuleAlias)
			}

			if importItem.HasOne {
				oneMap := map[string]any{
					"name": formatImportOne(importItem.One, builder),
				}
				if importItem.One.Alias != 0 {
					oneMap["alias"] = builder.StringsInterner.MustLookup(importItem.One.Alias)
				}
				fields["one"] = oneMap
			}

			if len(importItem.Group) > 0 {
				var groupItems []map[string]any
				for _, pair := range importItem.Group {
					pairMap := map[string]any{
						"name": builder.StringsInterner.MustLookup(pair.Name),
					}
					if pair.Alias != 0 {
						pairMap["alias"] = builder.StringsInterner.MustLookup(pair.Alias)
					}
					groupItems = append(groupItems, pairMap)
				}
				fields["group"] = groupItems
			}

			output.Fields = fields
		}
	}

	return output, nil
}

func formatItemKind(kind ast.ItemKind) string {
	switch kind {
	case ast.ItemFn:
		return "Fn"
	case ast.ItemLet:
		return "Let"
	case ast.ItemType:
		return "Type"
	case ast.ItemNewtype:
		return "Newtype"
	case ast.ItemAlias:
		return "Alias"
	case ast.ItemLiteral:
		return "Literal"
	case ast.ItemTag:
		return "Tag"
	case ast.ItemExtern:
		return "Extern"
	case ast.ItemPragma:
		return "Pragma"
	case ast.ItemImport:
		return "Import"
	case ast.ItemMacro:
		return "Macro"
	default:
		return fmt.Sprintf("Unknown(%d)", kind)
	}
}

func formatImportOne(one ast.ImportOne, builder *ast.Builder) string {
	if one.Name == 0 {
		return "*"
	}
	return builder.StringsInterner.MustLookup(one.Name)
}

func formatSpan(span source.Span, fs *source.FileSet) string {
	if fs != nil {
		start, end := fs.Resolve(span)
		return fmt.Sprintf("%d:%d-%d:%d", start.Line, start.Col, end.Line, end.Col)
	}
	return fmt.Sprintf("span(%d-%d)", span.Start, span.End)
}

type treeNode struct {
	label    string
	children []*treeNode
}

func buildFileTreeNode(builder *ast.Builder, fileID ast.FileID, fs *source.FileSet) *treeNode {
	file := builder.Files.Get(fileID)
	header := "File"
	if fs != nil {
		srcFile := fs.Get(file.Span.File)
		header = srcFile.FormatPath("auto", fs.BaseDir())
	}
	root := &treeNode{
		label: fmt.Sprintf("%s (span: %s)", header, formatSpan(file.Span, fs)),
	}

	for idx, itemID := range file.Items {
		root.children = append(root.children, buildItemTreeNode(builder, itemID, fs, idx))
	}

	return root
}

func buildItemTreeNode(builder *ast.Builder, itemID ast.ItemID, fs *source.FileSet, idx int) *treeNode {
	item := builder.Items.Get(itemID)
	if item == nil {
		return &treeNode{label: fmt.Sprintf("Item[%d]: <nil>", idx)}
	}

	node := &treeNode{
		label: fmt.Sprintf("Item[%d]: %s (span: %s)", idx, formatItemKind(item.Kind), formatSpan(item.Span, fs)),
	}

	switch item.Kind {
	case ast.ItemImport:
		if importItem, ok := builder.Items.Import(itemID); ok {
			moduleNode := &treeNode{label: "Module"}
			for _, stringID := range importItem.Module {
				segment := builder.StringsInterner.MustLookup(stringID)
				moduleNode.children = append(moduleNode.children, &treeNode{label: segment})
			}
			node.children = append(node.children, moduleNode)

			if importItem.ModuleAlias != 0 {
				alias := builder.StringsInterner.MustLookup(importItem.ModuleAlias)
				node.children = append(node.children, &treeNode{label: fmt.Sprintf("Alias: %s", alias)})
			}

			if importItem.HasOne {
				label := fmt.Sprintf("One: %s", formatImportOne(importItem.One, builder))
				if importItem.One.Alias != 0 {
					alias := builder.StringsInterner.MustLookup(importItem.One.Alias)
					label = fmt.Sprintf("%s as %s", label, alias)
				}
				node.children = append(node.children, &treeNode{label: label})
			}

			if len(importItem.Group) > 0 {
				groupNode := &treeNode{label: "Group"}
				for i, pair := range importItem.Group {
					name := builder.StringsInterner.MustLookup(pair.Name)
					entry := fmt.Sprintf("[%d] %s", i, name)
					if pair.Alias != 0 {
						entry = fmt.Sprintf("%s as %s", entry, builder.StringsInterner.MustLookup(pair.Alias))
					}
					groupNode.children = append(groupNode.children, &treeNode{label: entry})
				}
				node.children = append(node.children, groupNode)
			}
		}
	}

	return node
}

type treeBlock struct {
	lines []string
	width int
	root  int
}

func renderTree(node *treeNode) treeBlock {
	label := node.label
	labelWidth := len(label)

	if len(node.children) == 0 {
		return treeBlock{
			lines: []string{label},
			width: labelWidth,
			root:  labelWidth / 2,
		}
	}

	childBlocks := make([]treeBlock, len(node.children))
	maxChildHeight := 0
	for i, child := range node.children {
		childBlocks[i] = renderTree(child)
		if len(childBlocks[i].lines) > maxChildHeight {
			maxChildHeight = len(childBlocks[i].lines)
		}
	}

	const spacing = 3

	positions := make([]int, len(childBlocks))
	totalWidth := 0
	for i, block := range childBlocks {
		positions[i] = totalWidth + block.root
		totalWidth += block.width
		if i != len(childBlocks)-1 {
			totalWidth += spacing
		}
	}

	childrenCenter := (positions[0] + positions[len(positions)-1]) / 2
	rootPos := labelWidth / 2
	shift := childrenCenter - rootPos

	childPrefix := 0
	if shift < 0 {
		childPrefix = -shift
		for i := range positions {
			positions[i] += childPrefix
		}
		totalWidth += childPrefix
		shift = 0
		rootPos = labelWidth / 2
	} else {
		rootPos += shift
	}

	width := totalWidth
	rootLine := label
	if shift > 0 {
		rootLine = strings.Repeat(" ", shift) + label
	}
	if len(rootLine) < width {
		rootLine += strings.Repeat(" ", width-len(rootLine))
	} else if len(rootLine) > width {
		width = len(rootLine)
		for i := range positions {
			if positions[i] >= width {
				width = positions[i] + 1
			}
		}
		if len(rootLine) < width {
			rootLine += strings.Repeat(" ", width-len(rootLine))
		}
	}

	connector := make([]byte, width)
	for i := range connector {
		connector[i] = ' '
	}
	if rootPos >= width {
		needed := rootPos - width + 1
		rootLine += strings.Repeat(" ", needed)
		connector = append(connector, make([]byte, needed)...)
		for i := width; i < len(connector); i++ {
			connector[i] = ' '
		}
		width = len(connector)
	}
	connector[rootPos] = '|'
	for _, pos := range positions {
		if pos < rootPos {
			connector[pos] = '/'
		} else if pos > rootPos {
			connector[pos] = '\\'
		} else {
			connector[pos] = '|'
		}
	}
	connectorLine := string(connector)

	childLines := make([]string, maxChildHeight)
	for row := 0; row < maxChildHeight; row++ {
		var sb strings.Builder
		if childPrefix > 0 {
			sb.WriteString(strings.Repeat(" ", childPrefix))
		}
		for i, block := range childBlocks {
			line := ""
			if row < len(block.lines) {
				line = block.lines[row]
			}
			if len(line) < block.width {
				line += strings.Repeat(" ", block.width-len(line))
			}
			sb.WriteString(line)
			if i != len(childBlocks)-1 {
				sb.WriteString(strings.Repeat(" ", spacing))
			}
		}
		rowStr := sb.String()
		if len(rowStr) < width {
			rowStr += strings.Repeat(" ", width-len(rowStr))
		}
		childLines[row] = rowStr
	}

	lines := make([]string, 0, 2+len(childLines))
	lines = append(lines, rootLine, connectorLine)
	lines = append(lines, childLines...)

	return treeBlock{
		lines: lines,
		width: width,
		root:  rootPos,
	}
}
