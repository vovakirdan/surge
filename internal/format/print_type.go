package format

import "surge/internal/ast"

func (p *printer) printTypeItem(item *ast.Item, typeItem *ast.TypeItem) {
	switch typeItem.Kind {
	case ast.TypeDeclStruct:
		decl := p.builder.Items.TypeStruct(typeItem)
		if decl == nil || !spanValid(decl.BodySpan) {
			p.writer.CopySpan(item.Span)
			return
		}
		p.printStructDecl(item, typeItem, decl)
	case ast.TypeDeclUnion:
		decl := p.builder.Items.TypeUnion(typeItem)
		if decl == nil || !spanValid(decl.BodySpan) {
			p.writer.CopySpan(item.Span)
			return
		}
		p.printUnionDecl(item, typeItem, decl)
	default:
		p.writer.CopySpan(item.Span)
	}
}

func (p *printer) printStructDecl(item *ast.Item, typeItem *ast.TypeItem, decl *ast.TypeStructDecl) {
	bodyStart := int(decl.BodySpan.Start)
	bodyEnd := int(decl.BodySpan.End)
	if bodyStart < int(item.Span.Start) || bodyEnd > int(item.Span.End) {
		p.writer.CopySpan(item.Span)
		return
	}

	// Prefix before '{'
	p.writer.CopyRange(int(item.Span.Start), bodyStart)

	cursor := bodyStart
	fieldCount := int(decl.FieldsCount)
	fields := make([]*ast.TypeStructField, 0, fieldCount)
	if decl.FieldsCount > 0 && decl.FieldsStart.IsValid() {
		start := uint32(decl.FieldsStart)
		end := start + decl.FieldsCount
		for rawID := start; rawID < end; rawID++ {
			fieldID := ast.TypeFieldID(rawID)
			if field := p.builder.Items.StructField(fieldID); field != nil {
				fields = append(fields, field)
			}
		}
	}

	commaIndex := 0
	for idx, field := range fields {
		fieldStart := int(field.Span.Start)
		fieldEnd := int(field.Span.End)

		if cursor < fieldStart {
			p.writer.CopyRange(cursor, fieldStart)
		}
		cursor = fieldStart

		attrs := p.builder.Items.CollectAttrs(field.AttrStart, field.AttrCount)
		fieldBodyStart := fieldStart
		if len(attrs) > 0 {
			lastAttrEnd := int(attrs[len(attrs)-1].Span.End)
			p.writer.CopyRange(cursor, lastAttrEnd)
			cursor = lastAttrEnd
			fieldBodyStart = lastAttrEnd
		}

		if cursor < fieldBodyStart {
			p.writer.CopyRange(cursor, fieldBodyStart)
		}

		p.writeStructFieldBody(field)

		cursor = fieldEnd

		if commaIndex < len(decl.FieldCommas) {
			commaSpan := decl.FieldCommas[commaIndex]
			commaIndex++
			if int(commaSpan.End) > cursor {
				cursor = int(commaSpan.End)
			}
		}

		if idx < len(fields)-1 || decl.HasTrailing {
			p.writer.WriteString(",")
		}
	}

	p.writer.CopyRange(cursor, bodyEnd)

	// copy tail after body (semicolon, comments)
	if int(item.Span.End) > bodyEnd {
		p.writer.CopyRange(bodyEnd, int(item.Span.End))
	}
}

func (p *printer) writeStructFieldBody(field *ast.TypeStructField) {
	name := p.string(field.Name)
	if name == "" {
		name = "_"
	}
	p.writer.WriteString(name)
	p.writer.WriteString(": ")
	p.printTypeID(field.Type)
	if field.Default.IsValid() {
		p.writer.WriteString(" = ")
		p.printExpr(field.Default)
	}
}

func (p *printer) printUnionDecl(item *ast.Item, typeItem *ast.TypeItem, decl *ast.TypeUnionDecl) {
	bodyStart := int(decl.BodySpan.Start)
	bodyEnd := int(decl.BodySpan.End)
	if bodyStart < int(item.Span.Start) || bodyEnd > int(item.Span.End) {
		p.writer.CopySpan(item.Span)
		return
	}

	p.writer.CopyRange(int(item.Span.Start), bodyStart)

	members := make([]*ast.TypeUnionMember, 0, int(decl.MembersCount))
	if decl.MembersCount > 0 && decl.MembersStart.IsValid() {
		start := uint32(decl.MembersStart)
		end := start + decl.MembersCount
		for rawID := start; rawID < end; rawID++ {
			memberID := ast.TypeUnionMemberID(rawID)
			if member := p.builder.Items.UnionMember(memberID); member != nil {
				members = append(members, member)
			}
		}
	}

	for idx, member := range members {
		if idx > 0 {
			p.writer.WriteString(" | ")
		}
		p.writeUnionMember(member)
	}

	tailStart := bodyEnd
	if tailStart < int(item.Span.End) {
		content := p.writer.sf.Content
		found := false
		for i := tailStart; i < int(item.Span.End); i++ {
			if content[i] == ';' {
				tailStart = i
				found = true
				break
			}
		}
		if !found {
			tailStart = int(item.Span.End)
		}
	}
	p.writer.CopyRange(tailStart, int(item.Span.End))
}

func (p *printer) writeUnionMember(member *ast.TypeUnionMember) {
	switch member.Kind {
	case ast.TypeUnionMemberTag:
		name := p.string(member.TagName)
		p.writer.WriteString(name)
		if err := p.writer.WriteByte('('); err != nil {
			p.writer.CopySpan(member.Span)
			return
		}
		for i, arg := range member.TagArgs {
			if i > 0 {
				p.writer.WriteString(", ")
			}
			p.printTypeID(arg)
		}
		if member.HasTrailing && len(member.TagArgs) > 0 {
			p.writer.WriteString(",")
		}
		if err := p.writer.WriteByte(')'); err != nil {
			p.writer.CopySpan(member.Span)
			return
		}
	case ast.TypeUnionMemberType, ast.TypeUnionMemberNothing:
		p.printTypeID(member.Type)
	default:
		p.writer.CopySpan(member.Span)
	}
}
