package format

import (
	"bytes"
	"sort"

	"surge/internal/ast"
	"surge/internal/source"
)

type commaEdit struct {
	start int
	end   int
	data  []byte
}

// NormalizeCommas returns a copy of the original file content with whitespace
// around commas in function parameter lists and call argument lists normalized.
// The pass relies on lossless comma metadata captured in the AST to operate on
// the original bytes without re-printing entire constructs.
//
// Rules:
//   - tabs/spaces before ',' are removed;
//   - a single space is inserted after ',' unless the next non-space character
//     is ')', a newline, carriage return, another comma, or the comma is the
//     trailing separator stored by the parser.
func NormalizeCommas(sf *source.File, b *ast.Builder, fileID ast.FileID) []byte {
	if sf == nil {
		return nil
	}

	content := append([]byte(nil), sf.Content...)
	if b == nil || !fileID.IsValid() {
		return content
	}
	if b.Files == nil || b.Items == nil {
		return content
	}

	var edits []commaEdit

	file := b.Files.Get(fileID)
	if file != nil {
		for _, itemID := range file.Items {
			item := b.Items.Get(itemID)
			if item == nil {
				continue
			}
			if item.Span.File != sf.ID {
				continue
			}
			switch item.Kind {
			case ast.ItemFn:
				fn, ok := b.Items.Fn(itemID)
				if !ok || fn == nil {
					continue
				}
				lastIdx := len(fn.ParamCommas) - 1
				for idx, sp := range fn.ParamCommas {
					if sp.File != sf.ID {
						continue
					}
					addCommaEdit(&edits, content, int(sp.Start), int(sp.End), fn.ParamsTrailingComma && idx == lastIdx)
				}
			case ast.ItemExtern:
				block, ok := b.Items.Extern(itemID)
				if !ok || block == nil {
					continue
				}
				if block.MembersCount == 0 || !block.MembersStart.IsValid() {
					continue
				}
				base := uint32(block.MembersStart)
				for offset := range block.MembersCount {
					member := b.Items.ExternMember(ast.ExternMemberID(base + uint32(offset)))
					if member == nil || member.Kind != ast.ExternMemberFn {
						continue
					}
					fn := b.Items.FnByPayload(member.Fn)
					if fn == nil {
						continue
					}
					lastIdx := len(fn.ParamCommas) - 1
					for idx, sp := range fn.ParamCommas {
						if sp.File != sf.ID {
							continue
						}
						addCommaEdit(&edits, content, int(sp.Start), int(sp.End), fn.ParamsTrailingComma && idx == lastIdx)
					}
				}
			case ast.ItemContract:
				decl, ok := b.Items.Contract(itemID)
				if !ok || decl == nil {
					continue
				}
				for _, cid := range b.Items.GetContractItemIDs(decl) {
					member := b.Items.ContractItem(cid)
					if member == nil || member.Kind != ast.ContractItemFn {
						continue
					}
					fn := b.Items.ContractFn(ast.ContractFnID(member.Payload))
					if fn == nil {
						continue
					}
					lastIdx := len(fn.ParamCommas) - 1
					for idx, sp := range fn.ParamCommas {
						if sp.File != sf.ID {
							continue
						}
						addCommaEdit(&edits, content, int(sp.Start), int(sp.End), fn.ParamsTrailingComma && idx == lastIdx)
					}
				}
			}
		}
	}

	if exprs := b.Exprs; exprs != nil && exprs.Arena != nil {
		total := exprs.Arena.Len()
		for idx := uint32(1); idx <= total; idx++ {
			expr := exprs.Arena.Get(idx)
			if expr == nil || expr.Kind != ast.ExprCall || expr.Span.File != sf.ID {
				continue
			}
			call, ok := exprs.Call(ast.ExprID(idx))
			if !ok || call == nil {
				continue
			}
			lastIdx := len(call.ArgCommas) - 1
			for i, sp := range call.ArgCommas {
				if sp.File != sf.ID {
					continue
				}
				addCommaEdit(&edits, content, int(sp.Start), int(sp.End), call.HasTrailingComma && i == lastIdx)
			}
		}
	}

	if len(edits) == 0 {
		return content
	}

	sort.SliceStable(edits, func(i, j int) bool {
		return edits[i].start > edits[j].start
	})
	for _, e := range edits {
		if e.start < 0 || e.start > e.end || e.end > len(content) {
			continue
		}
		content = append(content[:e.start], append(e.data, content[e.end:]...)...)
	}
	return content
}

func addCommaEdit(out *[]commaEdit, buf []byte, start, end int, trailing bool) {
	if out == nil || start < 0 || end <= start || end > len(buf) {
		return
	}

	i := start - 1
	for i >= 0 && (buf[i] == ' ' || buf[i] == '\t') {
		i--
	}
	left := i + 1

	j := end
	for j < len(buf) && (buf[j] == ' ' || buf[j] == '\t') {
		j++
	}

	wantSpace := !trailing
	if wantSpace {
		if j >= len(buf) {
			wantSpace = false
		} else {
			switch buf[j] {
			case ')', '\n', '\r', ',':
				wantSpace = false
			}
		}
	}

	repl := []byte{','}
	if wantSpace {
		repl = append(repl, ' ')
	}

	if bytes.Equal(buf[left:j], repl) {
		return
	}

	*out = append(*out, commaEdit{
		start: left,
		end:   j,
		data:  repl,
	})
}
