package diagfmt

import (
	"fmt"
	"strings"

	"surge/internal/ast"
)

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
	case ast.ItemType:
		if typeItem, ok := builder.Items.Type(itemID); ok {
			fields := map[string]any{
				"name":       lookupStringOr(builder, typeItem.Name, "<anon>"),
				"kind":       formatTypeDeclKind(typeItem.Kind),
				"visibility": typeItem.Visibility.String(),
			}

			if len(typeItem.Generics) > 0 {
				genericNames := make([]string, 0, len(typeItem.Generics))
				for _, gid := range typeItem.Generics {
					genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
				}
				fields["generics"] = genericNames
			}

			if typeItem.AttrCount > 0 {
				attrs := builder.Items.CollectAttrs(typeItem.AttrStart, typeItem.AttrCount)
				if len(attrs) > 0 {
					fields["attributes"] = buildAttrsJSON(builder, attrs)
				}
			}

			switch typeItem.Kind {
			case ast.TypeDeclAlias:
				if aliasDecl := builder.Items.TypeAlias(typeItem); aliasDecl != nil {
					fields["target"] = formatTypeExprInline(builder, aliasDecl.Target)
				}
			case ast.TypeDeclStruct:
				if structDecl := builder.Items.TypeStruct(typeItem); structDecl != nil {
					if structDecl.Base.IsValid() {
						fields["base"] = formatTypeExprInline(builder, structDecl.Base)
					}
					jsonFields := make([]map[string]any, 0, structDecl.FieldsCount)
					if structDecl.FieldsCount > 0 && structDecl.FieldsStart.IsValid() {
						start := uint32(structDecl.FieldsStart)
						for idx := uint32(0); idx < structDecl.FieldsCount; idx++ {
							field := builder.Items.StructField(ast.TypeFieldID(start + idx))
							if field == nil {
								continue
							}
							entry := map[string]any{
								"name": lookupStringOr(builder, field.Name, "<field>"),
								"type": formatTypeExprInline(builder, field.Type),
							}
							if field.Default.IsValid() {
								entry["default"] = formatExprInline(builder, field.Default)
							}
							if field.AttrCount > 0 {
								attrs := builder.Items.CollectAttrs(field.AttrStart, field.AttrCount)
								if len(attrs) > 0 {
									entry["attributes"] = buildAttrsJSON(builder, attrs)
								}
							}
							jsonFields = append(jsonFields, entry)
						}
					}
					fields["fields"] = jsonFields
				}
			case ast.TypeDeclUnion:
				if unionDecl := builder.Items.TypeUnion(typeItem); unionDecl != nil {
					members := make([]map[string]any, 0, unionDecl.MembersCount)
					if unionDecl.MembersCount > 0 && unionDecl.MembersStart.IsValid() {
						start := uint32(unionDecl.MembersStart)
						for idx := uint32(0); idx < unionDecl.MembersCount; idx++ {
							member := builder.Items.UnionMember(ast.TypeUnionMemberID(start + idx))
							if member == nil {
								continue
							}
							entry := map[string]any{
								"kind": formatUnionMemberKind(member.Kind),
							}
							switch member.Kind {
							case ast.TypeUnionMemberType:
								entry["type"] = formatTypeExprInline(builder, member.Type)
							case ast.TypeUnionMemberTag:
								entry["tag"] = lookupStringOr(builder, member.TagName, "<tag>")
								if len(member.TagArgs) > 0 {
									argStrings := make([]string, 0, len(member.TagArgs))
									for _, arg := range member.TagArgs {
										argStrings = append(argStrings, formatTypeExprInline(builder, arg))
									}
									entry["args"] = argStrings
								}
							}
							members = append(members, entry)
						}
					}
					fields["members"] = members
				}
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

func formatTypeDeclKind(kind ast.TypeDeclKind) string {
	switch kind {
	case ast.TypeDeclAlias:
		return "Alias"
	case ast.TypeDeclStruct:
		return "Struct"
	case ast.TypeDeclUnion:
		return "Union"
	default:
		return fmt.Sprintf("TypeDeclKind(%d)", kind)
	}
}

func formatUnionMemberKind(kind ast.TypeUnionMemberKind) string {
	switch kind {
	case ast.TypeUnionMemberType:
		return "Type"
	case ast.TypeUnionMemberNothing:
		return "Nothing"
	case ast.TypeUnionMemberTag:
		return "Tag"
	default:
		return fmt.Sprintf("UnionMemberKind(%d)", kind)
	}
}

func formatUnionMemberInline(builder *ast.Builder, member *ast.TypeUnionMember, idx int) string {
	if member == nil {
		return fmt.Sprintf("Member[%d]: <nil>", idx)
	}
	switch member.Kind {
	case ast.TypeUnionMemberNothing:
		return fmt.Sprintf("Member[%d]: nothing", idx)
	case ast.TypeUnionMemberType:
		return fmt.Sprintf("Member[%d]: %s", idx, formatTypeExprInline(builder, member.Type))
	case ast.TypeUnionMemberTag:
		name := lookupStringOr(builder, member.TagName, "<tag>")
		if len(member.TagArgs) == 0 {
			return fmt.Sprintf("Member[%d]: %s", idx, name)
		}
		args := make([]string, 0, len(member.TagArgs))
		for _, arg := range member.TagArgs {
			args = append(args, formatTypeExprInline(builder, arg))
		}
		return fmt.Sprintf("Member[%d]: %s(%s)", idx, name, strings.Join(args, ", "))
	default:
		return fmt.Sprintf("Member[%d]: <unknown>", idx)
	}
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
