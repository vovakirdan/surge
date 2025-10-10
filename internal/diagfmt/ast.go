package diagfmt

import (
	"encoding/json"
	"fmt"
	"io"

	"surge/internal/ast"
	"surge/internal/source"
)

type ASTNodeOutput struct {
	Type     string             `json:"type"`
	Kind     string             `json:"kind,omitempty"`
	Span     source.Span        `json:"span"`
	Text     string             `json:"text,omitempty"`
	Children []ASTNodeOutput    `json:"children,omitempty"`
	Fields   map[string]any     `json:"fields,omitempty"`
}

func FormatASTPretty(w io.Writer, builder *ast.Builder, fileID ast.FileID, fs *source.FileSet) error {
	file := builder.Files.Get(fileID)
	if file == nil {
		return fmt.Errorf("file not found")
	}

	// todo печатать название файла
	fmt.Fprintf(w, "File (span: %s)\n", formatSpan(file.Span, fs))

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

func FormatASTJSON(w io.Writer, builder *ast.Builder, fileID ast.FileID) error {
	file := builder.Files.Get(fileID)
	if file == nil {
		return fmt.Errorf("file not found")
	}

	var children []ASTNodeOutput
	for _, itemID := range file.Items {
		itemNode, err := formatItemJSON(builder, itemID)
		if err != nil {
			return err
		}
		children = append(children, itemNode)
	}

	output := ASTNodeOutput{
		Type:     "File",
		Span:     file.Span,
		Children: children,
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
