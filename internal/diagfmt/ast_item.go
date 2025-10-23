package diagfmt

import (
	"fmt"
	"io"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
)

// formatItemPretty writes a tree-like, human-readable representation of the AST item
// identified by itemID to w.
// It prints the item's kind and span, and expands payloads for import, let, and
// function items (including module/alias/group entries for imports; name, mutability,
// type, and value for lets; and name, generics, params, return type, and optional body
// for functions). If the referenced item is nil it writes "nil item" and returns nil.
// Returns any error produced while formatting nested statements.
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
	case ast.ItemLet:
		if letItem, ok := builder.Items.Let(itemID); ok {
			fields := []struct {
				label string
				value string
				show  bool
			}{
				{"Name", builder.StringsInterner.MustLookup(letItem.Name), true},
				{"Mutable", fmt.Sprintf("%v", letItem.IsMut), true},
				{"Type", formatTypeExprInline(builder, letItem.Type), letItem.Type.IsValid()},
				{"Value", formatExprSummary(builder, letItem.Value), true},
			}

			visible := 0
			for _, f := range fields {
				if f.show {
					visible++
				}
			}

			current := 0
			for _, f := range fields {
				if !f.show {
					continue
				}
				current++
				fieldPrefix := "├─"
				if current == visible {
					fieldPrefix = "└─"
				}
				fmt.Fprintf(w, "%s%s %s: %s\n", prefix, fieldPrefix, f.label, f.value)
			}
		}
	case ast.ItemFn:
		if fnItem, ok := builder.Items.Fn(itemID); ok {
			type fnField struct {
				label  string
				value  string
				isBody bool
			}

			fields := make([]fnField, 0, 5)

			fields = append(fields, fnField{
				label: "Name",
				value: lookupStringOr(builder, fnItem.Name, "<anon>"),
			})

			if len(fnItem.Generics) > 0 {
				genericNames := make([]string, 0, len(fnItem.Generics))
				for _, gid := range fnItem.Generics {
					genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
				}
				fields = append(fields, fnField{
					label: "Generics",
					value: "<" + strings.Join(genericNames, ", ") + ">",
				})
			}

			fields = append(fields, fnField{
				label: "Params",
				value: formatFnParamsInline(builder, fnItem),
			})

			fields = append(fields, fnField{
				label: "Return",
				value: formatTypeExprInline(builder, fnItem.ReturnType),
			})

			fields = append(fields, fnField{
				label:  "Body",
				isBody: true,
			})

			for idx, field := range fields {
				isLast := idx == len(fields)-1
				marker := "├─"
				childPrefix := prefix + "│  "
				if isLast {
					marker = "└─"
					childPrefix = prefix + "   "
				}

				if field.isBody {
					if fnItem.Body.IsValid() {
						fmt.Fprintf(w, "%s%s Body:\n", prefix, marker)
						fmt.Fprintf(w, "%s└─ Stmt[0]: ", childPrefix)
						if err := formatStmtPretty(w, builder, fnItem.Body, fs, childPrefix+"   "); err != nil {
							return err
						}
					} else {
						fmt.Fprintf(w, "%s%s Body: <none>\n", prefix, marker)
					}
					continue
				}

				fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, field.label, field.value)
			}
		}
	}

	return nil
}

// formatItemJSON returns an ASTNodeOutput representing the item identified by itemID.
// 
// The returned node has Type "Item", a human-readable Kind string, the item's Span,
// a Fields map containing payload-specific entries (e.g., module, moduleAlias, one, group
// for imports; name, isMut, value, valueExprID, type for lets; name, returnType, params,
// generics, hasBody for functions), and optional Children (the function body node when present).
// 
// If the item ID is not found, an error with message "item not found" is returned.
// Any error produced while formatting nested statements (e.g., formatting a function body)
// is propagated.
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
	case ast.ItemLet:
		if letItem, ok := builder.Items.Let(itemID); ok {
			fields := map[string]any{
				"name":     builder.StringsInterner.MustLookup(letItem.Name),
				"isMut":    letItem.IsMut,
				"value":    formatExprInline(builder, letItem.Value),
				"valueSet": letItem.Value.IsValid(),
				"type":     formatTypeExprInline(builder, letItem.Type),
				"typeSet":  letItem.Type.IsValid(),
			}
			if letItem.Value.IsValid() {
				fields["valueExprID"] = uint32(letItem.Value)
			}
			output.Fields = fields
		}
	case ast.ItemFn:
		if fnItem, ok := builder.Items.Fn(itemID); ok {
			fields := map[string]any{
				"name":       lookupStringOr(builder, fnItem.Name, "<anon>"),
				"returnType": formatTypeExprInline(builder, fnItem.ReturnType),
				"params":     formatFnParamsInline(builder, fnItem),
				"hasBody":    fnItem.Body.IsValid(),
			}

			if len(fnItem.Generics) > 0 {
				genericNames := make([]string, 0, len(fnItem.Generics))
				for _, gid := range fnItem.Generics {
					genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
				}
				fields["generics"] = genericNames
			}

			output.Fields = fields

			if fnItem.Body.IsValid() {
				bodyNode, err := formatStmtJSON(builder, fnItem.Body)
				if err != nil {
					return ASTNodeOutput{}, err
				}
				output.Children = append(output.Children, bodyNode)
			}
		}
	}

	return output, nil
}

// formatItemKind returns a short, human-readable label for the given ast.ItemKind.
// For unknown kinds it returns a string of the form "Unknown(<n>)" where <n> is the numeric kind value.
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

// formatImportOne returns the display string for an import "one" entry.
// If the import specifies no name (Name == 0) it returns "*" to denote a wildcard.
// Otherwise it returns the name resolved from the builder's string interner.
func formatImportOne(one ast.ImportOne, builder *ast.Builder) string {
	if one.Name == 0 {
		return "*"
	}
	return builder.StringsInterner.MustLookup(one.Name)
}