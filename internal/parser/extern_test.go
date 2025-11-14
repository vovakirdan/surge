package parser

import (
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
)

func TestParseExternItem_Basic(t *testing.T) {
	src := `
extern<Person> {
	fn age(self: &Person) -> int;
	pub async fn to_json(self: &Person) -> string {
		return "{}";
	}
}
`
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		var b strings.Builder
		for _, d := range bag.Items() {
			b.WriteString(d.Code.String())
			b.WriteString(": ")
			b.WriteString(d.Message)
			b.WriteString("\n")
		}
		t.Fatalf("unexpected diagnostics:\n%s", b.String())
	}

	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatalf("file not found")
	}
	if len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(file.Items))
	}

	itemID := file.Items[0]
	item := builder.Items.Get(itemID)
	if item == nil {
		t.Fatalf("item missing")
	}
	if item.Kind != ast.ItemExtern {
		t.Fatalf("expected extern item, got %v", item.Kind)
	}

	externItem, ok := builder.Items.Extern(itemID)
	if !ok {
		t.Fatalf("extern payload missing")
	}

	target := builder.Types.Get(externItem.Target)
	if target == nil || target.Kind != ast.TypeExprPath {
		t.Fatalf("expected path type for extern target, got %v", target)
	}
	path, ok := builder.Types.Path(externItem.Target)
	if !ok || len(path.Segments) != 1 {
		t.Fatalf("unexpected extern target segments: %+v", path)
	}
	targetName := builder.StringsInterner.MustLookup(path.Segments[0].Name)
	if targetName != "Person" {
		t.Fatalf("expected extern target Person, got %s", targetName)
	}

	if externItem.MembersCount != 2 {
		t.Fatalf("expected 2 members, got %d", externItem.MembersCount)
	}

	checkMember := func(idx uint32, wantName string, wantPublic, wantAsync, wantBody bool) {
		member := builder.Items.ExternMember(ast.ExternMemberID(uint32(externItem.MembersStart) + idx))
		if member == nil {
			t.Fatalf("member %d missing", idx)
		}
		if member.Kind != ast.ExternMemberFn {
			t.Fatalf("member %d unexpected kind %v", idx, member.Kind)
		}
		fnItem := builder.Items.FnByPayload(member.Fn)
		if fnItem == nil {
			t.Fatalf("member %d function payload missing", idx)
		}
		name := builder.StringsInterner.MustLookup(fnItem.Name)
		if name != wantName {
			t.Fatalf("member %d name mismatch: got %q want %q", idx, name, wantName)
		}
		isPublic := fnItem.Flags&ast.FnModifierPublic != 0
		if isPublic != wantPublic {
			t.Fatalf("member %d public flag: got %v want %v", idx, isPublic, wantPublic)
		}
		isAsync := fnItem.Flags&ast.FnModifierAsync != 0
		if isAsync != wantAsync {
			t.Fatalf("member %d async flag: got %v want %v", idx, isAsync, wantAsync)
		}
		hasBody := fnItem.Body.IsValid()
		if hasBody != wantBody {
			t.Fatalf("member %d body presence: got %v want %v", idx, hasBody, wantBody)
		}
		params := builder.Items.GetFnParamIDs(fnItem)
		if len(params) != 1 {
			t.Fatalf("member %d expected 1 parameter, got %d", idx, len(params))
		}
		param := builder.Items.FnParam(params[0])
		if param == nil {
			t.Fatalf("member %d parameter missing", idx)
		}
		if builder.StringsInterner.MustLookup(param.Name) != "self" {
			t.Fatalf("member %d parameter name mismatch", idx)
		}
	}

	checkMember(0, "age", false, false, false)
	checkMember(1, "to_json", true, true, true)
}

func TestParseExternItem_IllegalMember(t *testing.T) {
	src := `
extern<Person> {
	let x = 1;
}
`
	builder, fileID, bag := parseSource(t, src)
	if !bag.HasErrors() {
		t.Fatalf("expected diagnostics for illegal extern member")
	}
	items := bag.Items()
	if len(items) == 0 || items[0].Code != diag.SynIllegalItemInExtern {
		t.Fatalf("expected SynIllegalItemInExtern, got %+v", items)
	}

	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatalf("file not found")
	}
	if len(file.Items) != 0 {
		t.Fatalf("extern item should be discarded on fatal error, got %d items", len(file.Items))
	}
}

func TestParseExternItem_OverrideRequiresPub(t *testing.T) {
	src := `
extern<Person> {
	@override fn __to(self: &Person, target: string) -> string { return ""; }
}
`
	builder, fileID, bag := parseSource(t, src)
	if !bag.HasErrors() {
		t.Fatalf("expected visibility diagnostics")
	}
	found := false
	for _, d := range bag.Items() {
		if d.Code == diag.SynVisibilityReduction {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected SynVisibilityReduction diagnostic, got %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatalf("file not found")
	}
	if len(file.Items) != 1 {
		t.Fatalf("extern item should remain in AST, got %d items", len(file.Items))
	}

	itemID := file.Items[0]
	externItem, ok := builder.Items.Extern(itemID)
	if !ok {
		t.Fatalf("extern payload missing")
	}
	if externItem.MembersCount != 1 {
		t.Fatalf("expected 1 member, got %d", externItem.MembersCount)
	}
	member := builder.Items.ExternMember(externItem.MembersStart)
	if member == nil {
		t.Fatalf("member missing")
	}
	fnItem := builder.Items.FnByPayload(member.Fn)
	if fnItem == nil {
		t.Fatalf("function payload missing")
	}
	if fnItem.Flags&ast.FnModifierPublic != 0 {
		t.Fatalf("override without pub should remain non-public in AST")
	}
}
