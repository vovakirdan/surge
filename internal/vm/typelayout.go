package vm

import (
	"fmt"
	"unicode"

	"surge/internal/types"
)

// StructLayout represents the layout of a struct type.
type StructLayout struct {
	TypeID       types.TypeID
	FieldNames   []string
	FieldTypes   []types.TypeID
	IndexByName  map[string]int
	FieldsByName map[string]types.TypeID
}

type layoutCache struct {
	vm      *VM
	structs map[types.TypeID]*StructLayout
}

func newLayoutCache(vm *VM) *layoutCache {
	return &layoutCache{
		vm:      vm,
		structs: make(map[types.TypeID]*StructLayout, 64),
	}
}

func (lc *layoutCache) Struct(typeID types.TypeID) (*StructLayout, *VMError) {
	if lc == nil || lc.vm == nil {
		return nil, &VMError{Code: PanicUnimplemented, Message: "no layout provider"}
	}
	typeID = lc.vm.valueType(typeID)
	if typeID == types.NoTypeID {
		return nil, lc.vm.eb.makeError(PanicUnimplemented, "invalid struct type")
	}
	if cached, ok := lc.structs[typeID]; ok && cached != nil {
		return cached, nil
	}

	if lc.vm.Types == nil {
		return nil, lc.vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("no type interner for struct layout type#%d", typeID))
	}
	info, ok := lc.vm.Types.StructInfo(typeID)
	if !ok || info == nil {
		return nil, lc.vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("missing struct info for type#%d", typeID))
	}

	fields := lc.vm.Types.StructFields(typeID)
	names, ok := lc.structFieldNames(fields)
	if !ok {
		if lc.vm.Files == nil {
			return nil, lc.vm.eb.makeError(PanicUnimplemented, "no fileset for struct layout")
		}
		file := lc.vm.Files.Get(info.Decl.File)
		if file == nil {
			return nil, lc.vm.eb.makeError(PanicUnimplemented, "missing source file for struct layout")
		}
		if int(info.Decl.End) > len(file.Content) || info.Decl.Start > info.Decl.End {
			return nil, lc.vm.eb.makeError(PanicUnimplemented, "invalid struct decl span")
		}
		decl := file.Content[info.Decl.Start:info.Decl.End]

		body, ok := extractFirstBracedBody(decl)
		if !ok {
			return nil, lc.vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("failed to parse struct layout for type#%d", typeID))
		}
		names = parseStructFieldNames(body)
	}
	if len(names) != len(fields) {
		return nil, lc.vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("struct layout mismatch for type#%d: parsed %d fields, interner has %d", typeID, len(names), len(fields)))
	}

	fieldTypes := make([]types.TypeID, len(fields))
	byName := make(map[string]types.TypeID, len(fields))
	indexByName := make(map[string]int, len(fields))
	for i := range fields {
		fieldTypes[i] = fields[i].Type
		byName[names[i]] = fields[i].Type
		indexByName[names[i]] = i
	}
	layout := &StructLayout{
		TypeID:       typeID,
		FieldNames:   append([]string(nil), names...),
		FieldTypes:   fieldTypes,
		IndexByName:  indexByName,
		FieldsByName: byName,
	}
	lc.structs[typeID] = layout
	return layout, nil
}

func (lc *layoutCache) structFieldNames(fields []types.StructField) ([]string, bool) {
	if lc == nil || lc.vm == nil || lc.vm.Types == nil || lc.vm.Types.Strings == nil {
		return nil, false
	}
	if len(fields) == 0 {
		return nil, true
	}
	names := make([]string, len(fields))
	for i, f := range fields {
		name, ok := lc.vm.Types.Strings.Lookup(f.Name)
		if !ok || name == "" {
			return nil, false
		}
		names[i] = name
	}
	return names, true
}

// skipStringOrComment skips over a string literal or comment starting at position i.
// Returns the new position after the string/comment, or i unchanged if not at one.
func skipStringOrComment(src []byte, i int) int {
	if i >= len(src) {
		return i
	}
	switch src[i] {
	case '"':
		// Double-quoted string with escapes
		i++
		for i < len(src) && src[i] != '"' {
			if src[i] == '\\' && i+1 < len(src) {
				i += 2
				continue
			}
			i++
		}
		if i < len(src) {
			i++ // skip closing quote
		}
		return i
	case '\'':
		// Single-quoted string with escapes
		i++
		for i < len(src) && src[i] != '\'' {
			if src[i] == '\\' && i+1 < len(src) {
				i += 2
				continue
			}
			i++
		}
		if i < len(src) {
			i++ // skip closing quote
		}
		return i
	case '`':
		// Raw/backtick string (no escapes)
		i++
		for i < len(src) && src[i] != '`' {
			i++
		}
		if i < len(src) {
			i++ // skip closing backtick
		}
		return i
	case '/':
		if i+1 < len(src) && src[i+1] == '/' {
			// Line comment
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
			return i
		}
		if i+1 < len(src) && src[i+1] == '*' {
			// Block comment (with nesting)
			i += 2
			depth := 1
			for i < len(src) && depth > 0 {
				if i+1 < len(src) {
					if src[i] == '/' && src[i+1] == '*' {
						depth++
						i += 2
						continue
					}
					if src[i] == '*' && src[i+1] == '/' {
						depth--
						i += 2
						continue
					}
				}
				i++
			}
			return i
		}
	}
	return i
}

func extractFirstBracedBody(src []byte) ([]byte, bool) {
	depth := 0
	bodyStart := -1
	for i := 0; i < len(src); {
		// Skip over strings and comments
		newI := skipStringOrComment(src, i)
		if newI > i {
			i = newI
			continue
		}

		switch src[i] {
		case '{':
			depth++
			if depth == 1 {
				bodyStart = i + 1
			}
		case '}':
			depth--
			if depth == 0 && bodyStart >= 0 {
				return src[bodyStart:i], true
			}
		}
		i++
	}
	return nil, false
}

func parseStructFieldNames(body []byte) []string {
	var out []string
	i := 0
	for i < len(body) {
		i = skipSpaceAndComments(body, i)
		if i >= len(body) {
			break
		}
		if !isIdentStart(body[i]) {
			i++
			continue
		}
		start := i
		i++
		for i < len(body) && isIdentContinue(body[i]) {
			i++
		}
		name := string(body[start:i])
		i = skipSpaceAndComments(body, i)
		if i >= len(body) || body[i] != ':' {
			continue
		}
		out = append(out, name)
		i++ // skip ':'

		paren := 0
		brack := 0
		angle := 0
		brace := 0
		for i < len(body) {
			i = skipSpaceAndComments(body, i)
			if i >= len(body) {
				break
			}
			ch := body[i]
			switch ch {
			case '(':
				paren++
			case ')':
				if paren > 0 {
					paren--
				}
			case '[':
				brack++
			case ']':
				if brack > 0 {
					brack--
				}
			case '<':
				angle++
			case '>':
				if angle > 0 {
					angle--
				}
			case '{':
				brace++
			case '}':
				if brace > 0 {
					brace--
				}
			case ',':
				if paren == 0 && brack == 0 && angle == 0 && brace == 0 {
					i++
					goto nextField
				}
			}
			i++
		}
	nextField:
	}
	return out
}

func skipSpaceAndComments(src []byte, i int) int {
	for i < len(src) {
		switch src[i] {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				i += 2
				for i < len(src) && src[i] != '\n' {
					i++
				}
				continue
			}
			if i+1 < len(src) && src[i+1] == '*' {
				i += 2
				depth := 1
				for i < len(src) && depth > 0 {
					if i+1 < len(src) {
						if src[i] == '/' && src[i+1] == '*' {
							depth++
							i += 2
							continue
						}
						if src[i] == '*' && src[i+1] == '/' {
							depth--
							i += 2
							continue
						}
					}
					i++
				}
				continue
			}
		}
		break
	}
	return i
}

func isIdentStart(b byte) bool {
	return b == '_' || unicode.IsLetter(rune(b))
}

func isIdentContinue(b byte) bool {
	return b == '_' || unicode.IsLetter(rune(b)) || unicode.IsDigit(rune(b))
}
