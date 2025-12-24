package diagfmt

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

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
						for idx := range structDecl.FieldsCount {
							idxUint32, err := safecast.Conv[uint32](idx)
							if err != nil {
								panic(fmt.Errorf("fields count overflow: %w", err))
							}
							field := builder.Items.StructField(ast.TypeFieldID(start + idxUint32))
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
						for idx := range unionDecl.MembersCount {
							idxUint32, err := safecast.Conv[uint32](idx)
							if err != nil {
								panic(fmt.Errorf("members count overflow: %w", err))
							}
							member := builder.Items.UnionMember(ast.TypeUnionMemberID(start + idxUint32))
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
	case ast.ItemContract:
		if contractItem, ok := builder.Items.Contract(itemID); ok {
			fields := map[string]any{
				"name":       lookupStringOr(builder, contractItem.Name, "<anon>"),
				"visibility": contractItem.Visibility.String(),
			}

			if len(contractItem.Generics) > 0 {
				genericNames := make([]string, 0, len(contractItem.Generics))
				for _, gid := range contractItem.Generics {
					genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
				}
				fields["generics"] = genericNames
			}

			if contractItem.AttrCount > 0 {
				attrs := builder.Items.CollectAttrs(contractItem.AttrStart, contractItem.AttrCount)
				if len(attrs) > 0 {
					fields["attributes"] = buildAttrsJSON(builder, attrs)
				}
			}

			members := make([]map[string]any, 0, contractItem.ItemsCount)
			for _, cid := range builder.Items.GetContractItemIDs(contractItem) {
				member := builder.Items.ContractItem(cid)
				if member == nil {
					continue
				}
				entry := map[string]any{
					"kind": formatContractItemKind(member.Kind),
				}
				switch member.Kind {
				case ast.ContractItemField:
					if field := builder.Items.ContractField(ast.ContractFieldID(member.Payload)); field != nil {
						entry["name"] = lookupStringOr(builder, field.Name, "<field>")
						entry["type"] = formatTypeExprInline(builder, field.Type)
						if field.AttrCount > 0 {
							attrs := builder.Items.CollectAttrs(field.AttrStart, field.AttrCount)
							if len(attrs) > 0 {
								entry["attributes"] = buildAttrsJSON(builder, attrs)
							}
						}
					}
				case ast.ContractItemFn:
					if fn := builder.Items.ContractFn(ast.ContractFnID(member.Payload)); fn != nil {
						entry["name"] = lookupStringOr(builder, fn.Name, "<fn>")
						entry["params"] = formatContractFnParamsInline(builder, fn)
						entry["returnType"] = formatTypeExprInline(builder, fn.ReturnType)
						if fn.AttrCount > 0 {
							fnAttrs := builder.Items.CollectAttrs(fn.AttrStart, fn.AttrCount)
							if len(fnAttrs) > 0 {
								entry["attributes"] = buildAttrsJSON(builder, fnAttrs)
							}
						}
						if fn.Flags&ast.FnModifierPublic != 0 {
							entry["public"] = true
						}
						if fn.Flags&ast.FnModifierAsync != 0 {
							entry["async"] = true
						}
					}
				}
				members = append(members, entry)
			}
			fields["memberCount"] = contractItem.ItemsCount
			if len(members) > 0 {
				fields["members"] = members
			}

			output.Fields = fields
		}
	case ast.ItemTag:
		if tagItem, ok := builder.Items.Tag(itemID); ok {
			fields := map[string]any{
				"name":       lookupStringOr(builder, tagItem.Name, "<anon>"),
				"visibility": tagItem.Visibility.String(),
			}

			if len(tagItem.Generics) > 0 {
				genericNames := make([]string, 0, len(tagItem.Generics))
				for _, gid := range tagItem.Generics {
					genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
				}
				fields["generics"] = genericNames
			}

			if len(tagItem.Payload) > 0 {
				payload := make([]string, 0, len(tagItem.Payload))
				for _, pid := range tagItem.Payload {
					payload = append(payload, formatTypeExprInline(builder, pid))
				}
				fields["payload"] = payload
			}

			if tagItem.AttrCount > 0 {
				attrs := builder.Items.CollectAttrs(tagItem.AttrStart, tagItem.AttrCount)
				if len(attrs) > 0 {
					fields["attributes"] = buildAttrsJSON(builder, attrs)
				}
			}

			output.Fields = fields
		}
	case ast.ItemExtern:
		if externItem, ok := builder.Items.Extern(itemID); ok {
			fields := map[string]any{
				"target": formatTypeExprInline(builder, externItem.Target),
			}
			if externItem.AttrCount > 0 {
				attrs := builder.Items.CollectAttrs(externItem.AttrStart, externItem.AttrCount)
				if len(attrs) > 0 {
					fields["attributes"] = buildAttrsJSON(builder, attrs)
				}
			}

			memberNames := make([]string, 0, externItem.MembersCount)
			if externItem.MembersCount > 0 && externItem.MembersStart.IsValid() {
				start := uint32(externItem.MembersStart)
				for idx := range externItem.MembersCount {
					idxUint32, err := safecast.Conv[uint32](idx)
					if err != nil {
						panic(fmt.Errorf("members count overflow: %w", err))
					}
					member := builder.Items.ExternMember(ast.ExternMemberID(start + idxUint32))
					if member == nil {
						continue
					}
					switch member.Kind {
					case ast.ExternMemberFn:
						fnItem := builder.Items.FnByPayload(member.Fn)
						if fnItem == nil {
							continue
						}

						name := lookupStringOr(builder, fnItem.Name, "<anon>")
						memberNames = append(memberNames, name)

						memberFields := map[string]any{
							"name":       name,
							"returnType": formatTypeExprInline(builder, fnItem.ReturnType),
							"params":     formatFnParamsInline(builder, fnItem),
							"hasBody":    fnItem.Body.IsValid(),
						}
						if len(fnItem.Generics) > 0 {
							genericNames := make([]string, 0, len(fnItem.Generics))
							for _, gid := range fnItem.Generics {
								genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
							}
							memberFields["generics"] = genericNames
						}
						if fnItem.AttrCount > 0 {
							fnAttrs := builder.Items.CollectAttrs(fnItem.AttrStart, fnItem.AttrCount)
							if len(fnAttrs) > 0 {
								memberFields["attributes"] = buildAttrsJSON(builder, fnAttrs)
							}
						}
						if fnItem.Flags&ast.FnModifierPublic != 0 {
							memberFields["public"] = true
						}
						if fnItem.Flags&ast.FnModifierAsync != 0 {
							memberFields["async"] = true
						}

						memberNode := ASTNodeOutput{
							Type:   "ExternMember",
							Kind:   "Fn",
							Span:   member.Span,
							Fields: memberFields,
						}

						if fnItem.Body.IsValid() {
							bodyNode, err := formatStmtJSON(builder, fnItem.Body)
							if err != nil {
								return ASTNodeOutput{}, err
							}
							memberNode.Children = append(memberNode.Children, bodyNode)
						}

						output.Children = append(output.Children, memberNode)
					case ast.ExternMemberField:
						field := builder.Items.ExternField(member.Field)
						if field == nil {
							continue
						}
						name := lookupStringOr(builder, field.Name, "<anon>")
						memberNames = append(memberNames, name)

						memberFields := map[string]any{
							"name":     name,
							"type":     formatTypeExprInline(builder, field.Type),
							"hasAttrs": field.AttrCount > 0,
						}
						if field.AttrCount > 0 {
							attrs := builder.Items.CollectAttrs(field.AttrStart, field.AttrCount)
							if len(attrs) > 0 {
								memberFields["attributes"] = buildAttrsJSON(builder, attrs)
							}
						}

						memberNode := ASTNodeOutput{
							Type:   "ExternMember",
							Kind:   "Field",
							Span:   member.Span,
							Fields: memberFields,
						}
						output.Children = append(output.Children, memberNode)
					}
				}
			}

			fields["memberCount"] = externItem.MembersCount
			if len(memberNames) > 0 {
				fields["memberNames"] = memberNames
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
	case ast.ItemContract:
		return "Contract"
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

func formatContractItemKind(kind ast.ContractItemKind) string {
	switch kind {
	case ast.ContractItemField:
		return "Field"
	case ast.ContractItemFn:
		return "Fn"
	default:
		return fmt.Sprintf("ContractItemKind(%d)", kind)
	}
}

func formatContractFnParamsInline(builder *ast.Builder, fn *ast.ContractFnReq) string {
	if builder == nil || fn == nil {
		return "()"
	}
	if fn.ParamsCount == 0 || !fn.ParamsStart.IsValid() {
		return "()"
	}
	params := make([]string, 0, fn.ParamsCount)
	start := uint32(fn.ParamsStart)
	for idx := range fn.ParamsCount {
		param := builder.Items.FnParam(ast.FnParamID(start + uint32(idx)))
		if param == nil {
			continue
		}
		name := lookupStringOr(builder, param.Name, "_")
		if param.Variadic {
			name = "..." + name
		}
		piece := fmt.Sprintf("%s: %s", name, formatTypeExprInline(builder, param.Type))
		attrs := builder.Items.CollectAttrs(param.AttrStart, param.AttrCount)
		if len(attrs) > 0 {
			attrStrings := make([]string, 0, len(attrs))
			for _, attr := range attrs {
				attrStrings = append(attrStrings, formatAttrInline(builder, attr))
			}
			piece = strings.Join(attrStrings, " ") + " " + piece
		}
		if param.Default.IsValid() {
			piece = fmt.Sprintf("%s = %s", piece, formatExprInline(builder, param.Default))
		}
		params = append(params, piece)
	}
	return fmt.Sprintf("(%s)", strings.Join(params, ", "))
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
