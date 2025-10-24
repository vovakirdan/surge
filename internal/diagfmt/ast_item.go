package diagfmt

import (
	"fmt"
	"io"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
)

// formatItemPretty writes a tree-style, human-readable representation of the AST item
// identified by itemID to the provided writer.
//
// The output includes the item's kind and span and expands payloads for Import, Let,
// and Fn items with hierarchical prefixes (├─, └─). For functions, generics, parameters,
// return type, and the first body statement (if present) are shown. If nested formatters
// (for statements) return an error, that error is propagated. If the item is not found
// (nil), a "nil item" line is written and no error is returned.
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
				{"Visibility", letItem.Visibility.String(), true},
				{"Type", formatTypeExprInline(builder, letItem.Type), letItem.Type.IsValid()},
				{"Value", formatExprSummary(builder, letItem.Value), true},
			}

			if letItem.AttrCount > 0 {
				attrs := builder.Items.CollectAttrs(letItem.AttrStart, letItem.AttrCount)
				if len(attrs) > 0 {
					attrStrings := make([]string, 0, len(attrs))
					for _, attr := range attrs {
						attrStrings = append(attrStrings, formatAttrInline(builder, attr))
					}
					fields = append(fields, struct {
						label string
						value string
						show  bool
					}{
						label: "Attributes",
						value: strings.Join(attrStrings, ", "),
						show:  true,
					})
				}
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

// formatItemJSON builds an ASTNodeOutput for the item identified by itemID in builder.
// The output contains Type "Item", a human-readable Kind, the item's Span, and a
// Fields map populated according to the item's payload. For imports the fields may
// include "module", "moduleAlias", "one" (with "name" and optional "alias"), and
// "group" (list of name/alias entries). For let bindings the fields include
// "name", "isMut", "value", "valueSet", "type", "typeSet" and, if present, "valueExprID".
// For functions the fields include "name", "returnType", "params", "hasBody" and,
// when generics are present, "generics"; when a body exists the function also
// appends the formatted body as a child node.
// Returns an error if the item is not found or if nested formatting fails.
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
				"name":       builder.StringsInterner.MustLookup(letItem.Name),
				"isMut":      letItem.IsMut,
				"value":      formatExprInline(builder, letItem.Value),
				"valueSet":   letItem.Value.IsValid(),
				"type":       formatTypeExprInline(builder, letItem.Type),
				"typeSet":    letItem.Type.IsValid(),
				"visibility": letItem.Visibility.String(),
			}
			if letItem.Value.IsValid() {
				fields["valueExprID"] = uint32(letItem.Value)
			}
			if letItem.AttrCount > 0 {
				attrs := builder.Items.CollectAttrs(letItem.AttrStart, letItem.AttrCount)
				fields["attributes"] = buildAttrsJSON(builder, attrs)
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

// formatItemKind returns a short human-readable label for the given ast.ItemKind.
// Known kinds are mapped to concise names such as "Fn", "Let", "Type", "Import", etc.
// For an unrecognized kind it returns "Unknown(<value>)" where <value> is the numeric kind.
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

// formatImportOne returns the import identifier as a string.
// It returns "*" when the import is a glob (one.Name == 0); otherwise it looks up the interned name.
func formatImportOne(one ast.ImportOne, builder *ast.Builder) string {
	if one.Name == 0 {
		return "*"
	}
	return builder.StringsInterner.MustLookup(one.Name)
}

func formatAttrInline(builder *ast.Builder, attr ast.Attr) string {
	name := lookupStringOr(builder, attr.Name, "<attr>")
	if len(attr.Args) == 0 {
		return "@" + name
	}
	argStrs := make([]string, 0, len(attr.Args))
	for _, arg := range attr.Args {
		argStrs = append(argStrs, formatExprInline(builder, arg))
	}
	return fmt.Sprintf("@%s(%s)", name, strings.Join(argStrs, ", "))
}

func buildAttrsJSON(builder *ast.Builder, attrs []ast.Attr) []map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(attrs))
	for _, attr := range attrs {
		entry := map[string]any{
			"name": lookupStringOr(builder, attr.Name, "<attr>"),
		}
		if len(attr.Args) > 0 {
			argStrs := make([]string, 0, len(attr.Args))
			for _, arg := range attr.Args {
				argStrs = append(argStrs, formatExprInline(builder, arg))
			}
			entry["args"] = argStrs
		}
		result = append(result, entry)
	}
	return result
}
