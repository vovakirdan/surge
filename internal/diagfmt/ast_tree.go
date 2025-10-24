package diagfmt

import (
	"fmt"
	"strings"
	"surge/internal/ast"
	"surge/internal/source"
)

type treeNode struct {
	label    string
	children []*treeNode
}

type treeBlock struct {
	lines []string
	width int
	root  int
}

// buildFileTreeNode constructs a treeNode representing the file identified by fileID,
// labeling the root with a header and the file span and appending a child node for each item in the file.
// If fs is non-nil the header is the source file's formatted path; otherwise the header is "File".
func buildFileTreeNode(builder *ast.Builder, fileID ast.FileID, fs *source.FileSet) *treeNode {
	file := builder.Files.Get(fileID)
	if file == nil {
		return &treeNode{label: fmt.Sprintf("File[%d]: <nil>", fileID)}
	}
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

// buildItemTreeNode constructs a treeNode for the item identified by itemID (displayed as Item[idx]) and populates children that describe the item's key components.
//
// The returned node's label includes the item kind and its span. For import items, children include a "Module" subtree with path segments and optional "Alias", "One" (with alias), and "Group" entries. For let items, children include "Name", "Mutable", optional "Type", and "Value". For function items, children include "Name", optional "Generics", "Params", "Return", and either a "Body" subtree or "Body: <none>". If the item is nil, a node labeled "Item[idx]: <nil>" is returned.
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
	case ast.ItemLet:
		if letItem, ok := builder.Items.Let(itemID); ok {
			nameNode := &treeNode{label: fmt.Sprintf("Name: %s", builder.StringsInterner.MustLookup(letItem.Name))}
			mutNode := &treeNode{label: fmt.Sprintf("Mutable: %v", letItem.IsMut)}
			node.children = append(node.children, nameNode, mutNode)

			if letItem.Type.IsValid() {
				typeNode := &treeNode{label: fmt.Sprintf("Type: %s", formatTypeExprInline(builder, letItem.Type))}
				node.children = append(node.children, typeNode)
			}

			valueLabel := fmt.Sprintf("Value: %s", formatExprSummary(builder, letItem.Value))
			node.children = append(node.children, &treeNode{label: valueLabel})
		}
	case ast.ItemFn:
		if fnItem, ok := builder.Items.Fn(itemID); ok {
			nameNode := &treeNode{label: fmt.Sprintf("Name: %s", lookupStringOr(builder, fnItem.Name, "<anon>"))}
			node.children = append(node.children, nameNode)

			if len(fnItem.Generics) > 0 {
				genericNames := make([]string, 0, len(fnItem.Generics))
				for _, gid := range fnItem.Generics {
					genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
				}
				node.children = append(node.children, &treeNode{
					label: fmt.Sprintf("Generics: <%s>", strings.Join(genericNames, ", ")),
				})
			}

			paramsNode := &treeNode{label: fmt.Sprintf("Params: %s", formatFnParamsInline(builder, fnItem))}
			retNode := &treeNode{label: fmt.Sprintf("Return: %s", formatTypeExprInline(builder, fnItem.ReturnType))}
			node.children = append(node.children, paramsNode, retNode)

			if fnItem.Body.IsValid() {
				bodyNode := &treeNode{label: "Body"}
				bodyNode.children = append(bodyNode.children, buildStmtTreeNode(builder, fnItem.Body, fs, 0))
				node.children = append(node.children, bodyNode)
			} else {
				node.children = append(node.children, &treeNode{label: "Body: <none>"})
			}
		}
	}

	return node
}

// renderTree converts a treeNode into a treeBlock containing an ASCII-art representation.
//
// The returned treeBlock.lines is a slice of strings representing the rendered lines of
// the node and its descendants arranged as a tree with connector characters. The block's
// width is the horizontal extent of the rendered lines and root is the column index of
// the root node's vertical connector within those lines.
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
