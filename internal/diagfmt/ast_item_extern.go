package diagfmt

import (
	"fmt"
	"io"
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
)

func formatExternPretty(w io.Writer, builder *ast.Builder, itemID ast.ItemID, fs *source.FileSet, prefix string) error {
	externItem, ok := builder.Items.Extern(itemID)
	if !ok {
		fmt.Fprintf(w, "<invalid extern>\n") //nolint:errcheck
		return nil
	}

	fields := []struct {
		label string
		value string
		show  bool
	}{
		{"Target", formatTypeExprInline(builder, externItem.Target), true},
	}

	if externItem.AttrCount > 0 {
		attrs := builder.Items.CollectAttrs(externItem.AttrStart, externItem.AttrCount)
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

	hasMembers := externItem.MembersCount > 0 && externItem.MembersStart.IsValid()
	visible := 0
	for _, f := range fields {
		if f.show {
			visible++
		}
	}
	if hasMembers {
		visible++
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

	if !hasMembers {
		return nil
	}

	marker := "└─"
	childPrefix := prefix + "   "
	if current < visible {
		marker = "├─"
		childPrefix = prefix + "│  "
	}
	fmt.Fprintf(w, "%s%s Members:\n", prefix, marker) //nolint:errcheck

	start := uint32(externItem.MembersStart)
	memberCount := int(externItem.MembersCount)
	for idx := range memberCount {
		idxUint32, err := safecast.Conv[uint32](idx)
		if err != nil {
			panic(fmt.Errorf("extern members count overflow: %w", err))
		}
		member := builder.Items.ExternMember(ast.ExternMemberID(start + idxUint32))
		if member == nil {
			continue
		}
		isLastMember := idx == memberCount-1
		memberMarker := "├─"
		if isLastMember {
			memberMarker = "└─"
		}
		memberPrefix := childPrefix + memberMarker + " "
		childChildPrefix := childPrefix + "│  "
		if isLastMember {
			childChildPrefix = childPrefix + "   "
		}

		switch member.Kind {
		case ast.ExternMemberFn:
			if err := formatExternFnPretty(w, builder, member, idx, childChildPrefix, memberPrefix, fs); err != nil {
				return err
			}
		case ast.ExternMemberField:
			formatExternFieldPretty(w, builder, member, idx, childChildPrefix, memberPrefix)
		}
	}

	return nil
}

func formatExternFnPretty(w io.Writer, builder *ast.Builder, member *ast.ExternMember, idx int, childChildPrefix, memberPrefix string, fs *source.FileSet) error {
	fnItem := builder.Items.FnByPayload(member.Fn)
	if fnItem == nil {
		fmt.Fprintf(w, "%sFn[%d]: <nil>\n", memberPrefix, idx) //nolint:errcheck
		return nil
	}
	name := lookupStringOr(builder, fnItem.Name, "<anon>")
	fmt.Fprintf(w, "%sFn[%d]: %s\n", memberPrefix, idx, name) //nolint:errcheck

	lines := []struct {
		label string
		value string
		show  bool
	}{
		{"Params", formatFnParamsInline(builder, fnItem), true},
		{"Return", formatTypeExprInline(builder, fnItem.ReturnType), true},
	}
	if len(fnItem.Generics) > 0 {
		genericNames := make([]string, 0, len(fnItem.Generics))
		for _, gid := range fnItem.Generics {
			genericNames = append(genericNames, lookupStringOr(builder, gid, "_"))
		}
		lines = append(lines, struct {
			label string
			value string
			show  bool
		}{
			label: "Generics",
			value: "<" + strings.Join(genericNames, ", ") + ">",
			show:  true,
		})
	}
	if fnItem.AttrCount > 0 {
		fnAttrs := builder.Items.CollectAttrs(fnItem.AttrStart, fnItem.AttrCount)
		if len(fnAttrs) > 0 {
			attrStrings := make([]string, 0, len(fnAttrs))
			for _, attr := range fnAttrs {
				attrStrings = append(attrStrings, formatAttrInline(builder, attr))
			}
			lines = append(lines, struct {
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

	lineCount := 0
	for _, l := range lines {
		if l.show {
			lineCount++
		}
	}

	lineIdx := 0
	for _, l := range lines {
		if !l.show {
			continue
		}
		lineIdx++
		lineMarker := "├─"
		if lineIdx == lineCount && !fnItem.Body.IsValid() {
			lineMarker = "└─"
		}
		fmt.Fprintf(w, "%s%s %s: %s\n", childChildPrefix, lineMarker, l.label, l.value) //nolint:errcheck
	}

	if fnItem.Body.IsValid() {
		bodyMarker := "└─"
		fmt.Fprintf(w, "%s%s Body:\n", childChildPrefix, bodyMarker) //nolint:errcheck
		fmt.Fprintf(w, "%s   Stmt[0]: ", childChildPrefix)           //nolint:errcheck
		if err := formatStmtPretty(w, builder, fnItem.Body, fs, childChildPrefix+"   "); err != nil {
			return err
		}
	}

	return nil
}

func formatExternFieldPretty(w io.Writer, builder *ast.Builder, member *ast.ExternMember, idx int, childChildPrefix, memberPrefix string) {
	field := builder.Items.ExternField(member.Field)
	if field == nil {
		fmt.Fprintf(w, "%sField[%d]: <nil>\n", memberPrefix, idx) //nolint:errcheck
		return
	}
	name := lookupStringOr(builder, field.Name, "<anon>")
	fmt.Fprintf(w, "%sField[%d]: %s\n", memberPrefix, idx, name) //nolint:errcheck

	lines := []struct {
		label string
		value string
		show  bool
	}{
		{"Type", formatTypeExprInline(builder, field.Type), true},
	}

	if field.AttrCount > 0 {
		attrs := builder.Items.CollectAttrs(field.AttrStart, field.AttrCount)
		if len(attrs) > 0 {
			attrStrings := make([]string, 0, len(attrs))
			for _, attr := range attrs {
				attrStrings = append(attrStrings, formatAttrInline(builder, attr))
			}
			lines = append(lines, struct {
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

	for lineIdx, l := range lines {
		if !l.show {
			continue
		}
		lineMarker := "├─"
		if lineIdx == len(lines)-1 {
			lineMarker = "└─"
		}
		fmt.Fprintf(w, "%s%s %s: %s\n", childChildPrefix, lineMarker, l.label, l.value) //nolint:errcheck
	}
}
