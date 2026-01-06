package diagfmt

import (
	"fmt"
	"io"
	"strings"

	"fortio.org/safecast"

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
		fmt.Fprintf(w, "nil item\n") //nolint:errcheck
		return nil
	}

	kindStr := formatItemKind(item.Kind)
	fmt.Fprintf(w, "%s (span: %s)\n", kindStr, formatSpan(item.Span, fs)) //nolint:errcheck

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
			fmt.Fprintf(w, "%s%s Module: ", prefix, modulePrefix) //nolint:errcheck
			for i, stringID := range importItem.Module {
				if i > 0 {
					fmt.Fprintf(w, "::") //nolint:errcheck
				}
				fmt.Fprintf(w, "%s", builder.StringsInterner.MustLookup(stringID)) //nolint:errcheck
			}
			fmt.Fprintf(w, "\n") //nolint:errcheck

			if hasAlias {
				currentField++
				aliasPrefix := "├─"
				if currentField == fieldsCount {
					aliasPrefix = "└─"
				}
				fmt.Fprintf(w, "%s%s Alias: %s\n", prefix, aliasPrefix, builder.StringsInterner.MustLookup(importItem.ModuleAlias)) //nolint:errcheck
			}

			if hasOne {
				currentField++
				onePrefix := "├─"
				if currentField == fieldsCount {
					onePrefix = "└─"
				}
				fmt.Fprintf(w, "%s%s One: %s", prefix, onePrefix, formatImportOne(importItem.One, builder)) //nolint:errcheck
				if importItem.One.Alias != 0 {
					fmt.Fprintf(w, " as %s", builder.StringsInterner.MustLookup(importItem.One.Alias)) //nolint:errcheck
				}
				fmt.Fprintf(w, "\n") //nolint:errcheck
			}

			if hasGroup {
				fmt.Fprintf(w, "%s└─ Group:\n", prefix) //nolint:errcheck
				for i, pair := range importItem.Group {
					isLastInGroup := i == len(importItem.Group)-1
					groupItemPrefix := "├─"
					if isLastInGroup {
						groupItemPrefix = "└─"
					}
					fmt.Fprintf(w, "%s   %s [%d] %s", prefix, groupItemPrefix, i, builder.StringsInterner.MustLookup(pair.Name)) //nolint:errcheck
					if pair.Alias != 0 {
						fmt.Fprintf(w, " as %s", builder.StringsInterner.MustLookup(pair.Alias)) //nolint:errcheck
					}
					fmt.Fprintf(w, "\n") //nolint:errcheck
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
				fmt.Fprintf(w, "%s%s %s: %s\n", prefix, fieldPrefix, f.label, f.value) //nolint:errcheck
			}
		}
	case ast.ItemType:
		if typeItem, ok := builder.Items.Type(itemID); ok {
			fields := []struct {
				label string
				value string
				show  bool
			}{
				{"Name", lookupStringOr(builder, typeItem.Name, "<anon>"), true},
				{"Kind", formatTypeDeclKind(typeItem.Kind), true},
				{"Visibility", typeItem.Visibility.String(), true},
			}

			if len(typeItem.Generics) > 0 {
				genericNames := make([]string, 0, len(typeItem.Generics))
				for _, gid := range typeItem.Generics {
					genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
				}
				fields = append(fields, struct {
					label string
					value string
					show  bool
				}{
					label: "Generics",
					value: "<" + strings.Join(genericNames, ", ") + ">",
					show:  true,
				})
			}

			if typeItem.AttrCount > 0 {
				attrs := builder.Items.CollectAttrs(typeItem.AttrStart, typeItem.AttrCount)
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

			structPrinter := func(_ string) error { return nil }
			unionPrinter := func(_ string) error { return nil }
			hasStructBody := false
			hasUnionBody := false

			switch typeItem.Kind {
			case ast.TypeDeclAlias:
				if aliasDecl := builder.Items.TypeAlias(typeItem); aliasDecl != nil {
					fields = append(fields, struct {
						label string
						value string
						show  bool
					}{
						label: "Target",
						value: formatTypeExprInline(builder, aliasDecl.Target),
						show:  true,
					})
				}
			case ast.TypeDeclStruct:
				if structDecl := builder.Items.TypeStruct(typeItem); structDecl != nil {
					hasStructBody = true
					structPrinter = func(bodyPrefix string) error {
						entries := 0
						if structDecl.Base.IsValid() {
							entries++
						}
						var structFields []*ast.TypeStructField
						if structDecl.FieldsCount > 0 && structDecl.FieldsStart.IsValid() {
							start := uint32(structDecl.FieldsStart)
							for idx := range structDecl.FieldsCount {
								idxUint32, err := safecast.Conv[uint32](idx)
								if err != nil {
									panic(fmt.Errorf("fields count overflow: %w", err))
								}
								field := builder.Items.StructField(ast.TypeFieldID(start + idxUint32))
								if field != nil {
									structFields = append(structFields, field)
								}
							}
						}
						entries += len(structFields)
						if entries == 0 {
							fmt.Fprintf(w, "%s<empty>\n", bodyPrefix) //nolint:errcheck
							return nil
						}
						current := 0
						if structDecl.Base.IsValid() {
							current++
							marker := "├─"
							if current == entries {
								marker = "└─"
							}
							fmt.Fprintf(w, "%s%s Base: %s\n", bodyPrefix, marker, formatTypeExprInline(builder, structDecl.Base)) //nolint:errcheck
						}
						for idx, field := range structFields {
							current++
							marker := "├─"
							if current == entries {
								marker = "└─"
							}
							fieldLine := fmt.Sprintf("Field[%d]: %s: %s", idx, lookupStringOr(builder, field.Name, "<field>"), formatTypeExprInline(builder, field.Type))
							if field.Default.IsValid() {
								fieldLine += " = " + formatExprInline(builder, field.Default)
							}
							if field.AttrCount > 0 {
								attrList := builder.Items.CollectAttrs(field.AttrStart, field.AttrCount)
								if len(attrList) > 0 {
									attrStrings := make([]string, 0, len(attrList))
									for _, attr := range attrList {
										attrStrings = append(attrStrings, formatAttrInline(builder, attr))
									}
									fieldLine += " [" + strings.Join(attrStrings, ", ") + "]"
								}
							}
							fmt.Fprintf(w, "%s%s %s\n", bodyPrefix, marker, fieldLine) //nolint:errcheck
						}
						return nil
					}
				}
			case ast.TypeDeclUnion:
				if unionDecl := builder.Items.TypeUnion(typeItem); unionDecl != nil {
					hasUnionBody = true
					unionPrinter = func(bodyPrefix string) error {
						var members []*ast.TypeUnionMember
						if unionDecl.MembersCount > 0 && unionDecl.MembersStart.IsValid() {
							start := uint32(unionDecl.MembersStart)
							for idx := range unionDecl.MembersCount {
								idxUint32, err := safecast.Conv[uint32](idx)
								if err != nil {
									panic(fmt.Errorf("members count overflow: %w", err))
								}
								member := builder.Items.UnionMember(ast.TypeUnionMemberID(start + idxUint32))
								if member != nil {
									members = append(members, member)
								}
							}
						}
						if len(members) == 0 {
							fmt.Fprintf(w, "%s<empty>\n", bodyPrefix) //nolint:errcheck
							return nil
						}
						for idx, member := range members {
							marker := "├─"
							if idx == len(members)-1 {
								marker = "└─"
							}
							fmt.Fprintf(w, "%s%s %s\n", bodyPrefix, marker, formatUnionMemberInline(builder, member, idx)) //nolint:errcheck
						}
						return nil
					}
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
				hasMore := current < visible || hasStructBody || hasUnionBody
				marker := "├─"
				if !hasMore {
					marker = "└─"
				}
				fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, f.label, f.value) //nolint:errcheck
			}

			if hasStructBody {
				marker := "├─"
				if !hasUnionBody {
					marker = "└─"
				}
				childPrefix := prefix + "│  "
				if marker == "└─" {
					childPrefix = prefix + "   "
				}
				fmt.Fprintf(w, "%s%s Struct:\n", prefix, marker) //nolint:errcheck
				if err := structPrinter(childPrefix); err != nil {
					return err
				}
			}

			if hasUnionBody {
				marker := "└─"
				childPrefix := prefix + "   "
				fmt.Fprintf(w, "%s%s Union:\n", prefix, marker) //nolint:errcheck
				if err := unionPrinter(childPrefix); err != nil {
					return err
				}
			}
		}
	case ast.ItemTag:
		if tagItem, ok := builder.Items.Tag(itemID); ok {
			fields := []struct {
				label string
				value string
				show  bool
			}{
				{"Name", lookupStringOr(builder, tagItem.Name, "<anon>"), true},
				{"Visibility", tagItem.Visibility.String(), true},
			}

			if len(tagItem.Generics) > 0 {
				genericNames := make([]string, 0, len(tagItem.Generics))
				for _, gid := range tagItem.Generics {
					genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
				}
				fields = append(fields, struct {
					label string
					value string
					show  bool
				}{
					label: "Generics",
					value: "<" + strings.Join(genericNames, ", ") + ">",
					show:  true,
				})
			}

			if len(tagItem.Payload) > 0 {
				payloadTypes := make([]string, 0, len(tagItem.Payload))
				for _, pid := range tagItem.Payload {
					payloadTypes = append(payloadTypes, formatTypeExprInline(builder, pid))
				}
				fields = append(fields, struct {
					label string
					value string
					show  bool
				}{
					label: "Payload",
					value: strings.Join(payloadTypes, ", "),
					show:  true,
				})
			}

			if tagItem.AttrCount > 0 {
				attrs := builder.Items.CollectAttrs(tagItem.AttrStart, tagItem.AttrCount)
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
				marker := "├─"
				if current == visible {
					marker = "└─"
				}
				fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, f.label, f.value) //nolint:errcheck
			}
		}
	case ast.ItemExtern:
		return formatExternPretty(w, builder, itemID, fs, prefix)
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

			fields = append(fields,
				fnField{
					label: "Params",
					value: formatFnParamsInline(builder, fnItem),
				},
				fnField{
					label: "Return",
					value: formatTypeExprInline(builder, fnItem.ReturnType),
				},
				fnField{
					label:  "Body",
					isBody: true,
				},
			)

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
						fmt.Fprintf(w, "%s%s Body:\n", prefix, marker) //nolint:errcheck
						fmt.Fprintf(w, "%s└─ Stmt[0]: ", childPrefix)  //nolint:errcheck
						if err := formatStmtPretty(w, builder, fnItem.Body, fs, childPrefix+"   "); err != nil {
							return err
						}
					} else {
						fmt.Fprintf(w, "%s%s Body: <none>\n", prefix, marker) //nolint:errcheck
					}
					continue
				}

				fmt.Fprintf(w, "%s%s %s: %s\n", prefix, marker, field.label, field.value) //nolint:errcheck
			}
		}
	}

	return nil
}
