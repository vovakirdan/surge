package parser

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
)

func TestParseTypeAlias(t *testing.T) {
	src := "type Alias = int;"
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if file == nil || len(file.Items) != 1 {
		t.Fatalf("expected single item, got %+v", file)
	}

	typeItem, ok := builder.Items.Type(file.Items[0])
	if !ok {
		t.Fatalf("expected type item, got %v", builder.Items.Get(file.Items[0]).Kind)
	}
	if typeItem.Kind != ast.TypeDeclAlias {
		t.Fatalf("expected alias kind, got %v", typeItem.Kind)
	}
	if typeItem.Visibility != ast.VisPrivate {
		t.Fatalf("expected private visibility, got %v", typeItem.Visibility)
	}

	alias := builder.Items.TypeAlias(typeItem)
	if alias == nil {
		t.Fatalf("alias payload missing")
	}
	path, ok := builder.Types.Path(alias.Target)
	if !ok || path == nil || len(path.Segments) != 1 {
		t.Fatalf("unexpected alias target path: %+v", path)
	}
	name := builder.StringsInterner.MustLookup(path.Segments[0].Name)
	if name != "int" {
		t.Fatalf("expected alias target 'int', got %q", name)
	}
}

func TestParseTypeStruct(t *testing.T) {
	src := "type Person = { name: string, age: int };"
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if len(file.Items) != 1 {
		t.Fatalf("expected single item, got %d", len(file.Items))
	}

	typeItem, ok := builder.Items.Type(file.Items[0])
	if !ok || typeItem.Kind != ast.TypeDeclStruct {
		t.Fatalf("expected struct type, got %v", builder.Items.Get(file.Items[0]).Kind)
	}

	structDecl := builder.Items.TypeStruct(typeItem)
	if structDecl == nil {
		t.Fatalf("struct payload missing")
	}
	if structDecl.Base.IsValid() {
		t.Fatalf("unexpected base type")
	}
	if structDecl.FieldsCount != 2 {
		t.Fatalf("expected 2 fields, got %d", structDecl.FieldsCount)
	}

	firstField := builder.Items.StructField(structDecl.FieldsStart)
	if firstField == nil {
		t.Fatalf("first field missing")
	}
	name := builder.StringsInterner.MustLookup(firstField.Name)
	if name != "name" {
		t.Fatalf("expected first field 'name', got %q", name)
	}
}

func TestParseTypeStructWithoutSemicolon(t *testing.T) {
	src := "type Shape = { width: int, height: int }"
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if len(file.Items) != 1 {
		t.Fatalf("expected single item, got %d", len(file.Items))
	}

	typeItem, ok := builder.Items.Type(file.Items[0])
	if !ok || typeItem.Kind != ast.TypeDeclStruct {
		t.Fatalf("expected struct type, got %v", builder.Items.Get(file.Items[0]).Kind)
	}
}

func TestParseTypeStructWithBase(t *testing.T) {
	src := "type Child = Parent : { extra: int };"
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	typeItem, ok := builder.Items.Type(file.Items[0])
	if !ok || typeItem.Kind != ast.TypeDeclStruct {
		t.Fatalf("expected struct type, got %v", builder.Items.Get(file.Items[0]).Kind)
	}

	structDecl := builder.Items.TypeStruct(typeItem)
	if structDecl == nil || !structDecl.Base.IsValid() {
		t.Fatalf("expected base type")
	}
	path, ok := builder.Types.Path(structDecl.Base)
	if !ok || path == nil || len(path.Segments) != 1 {
		t.Fatalf("unexpected base path: %+v", path)
	}
	name := builder.StringsInterner.MustLookup(path.Segments[0].Name)
	if name != "Parent" {
		t.Fatalf("expected base 'Parent', got %q", name)
	}
}

func TestParseTypeUnionWithTags(t *testing.T) {
	src := "type Result = Ok(int) | Err(string);"
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	typeItem, ok := builder.Items.Type(file.Items[0])
	if !ok || typeItem.Kind != ast.TypeDeclUnion {
		t.Fatalf("expected union type, got %v", builder.Items.Get(file.Items[0]).Kind)
	}

	union := builder.Items.TypeUnion(typeItem)
	if union == nil || union.MembersCount != 2 {
		t.Fatalf("expected 2 union members, got %d", union.MembersCount)
	}
	first := builder.Items.UnionMember(union.MembersStart)
	if first == nil || first.Kind != ast.TypeUnionMemberTag {
		t.Fatalf("expected first member tag, got %+v", first)
	}
	tagName := builder.StringsInterner.MustLookup(first.TagName)
	if tagName != "Ok" {
		t.Fatalf("expected tag 'Ok', got %q", tagName)
	}
}

func TestParseTypeUnionTypes(t *testing.T) {
	src := "type Maybe = nothing | int;"
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	typeItem, ok := builder.Items.Type(file.Items[0])
	if !ok || typeItem.Kind != ast.TypeDeclUnion {
		t.Fatalf("expected union type, got %v", builder.Items.Get(file.Items[0]).Kind)
	}

	union := builder.Items.TypeUnion(typeItem)
	if union == nil || union.MembersCount != 2 {
		t.Fatalf("expected 2 members, got %d", union.MembersCount)
	}
}

func TestParsePubType(t *testing.T) {
	src := "pub type Visible = int;"
	builder, fileID, bag := parseSource(t, src)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	typeItem, ok := builder.Items.Type(builder.Files.Get(fileID).Items[0])
	if !ok {
		t.Fatalf("expected type item")
	}
	if typeItem.Visibility != ast.VisPublic {
		t.Fatalf("expected public visibility, got %v", typeItem.Visibility)
	}
}

func TestParseTypeMissingEquals(t *testing.T) {
	src := "type Broken int;"
	_, _, bag := parseSource(t, src)
	if !bag.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
	if !hasDiagnostic(bag, diag.SynTypeExpectEquals) {
		t.Fatalf("expected SynTypeExpectEquals, got %+v", bag.Items())
	}
}

func TestParseTypeUnionTrailingPipe(t *testing.T) {
	src := "type U = A |;"
	_, _, bag := parseSource(t, src)
	if !bag.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
	if !hasDiagnostic(bag, diag.SynTypeExpectUnionMember) {
		t.Fatalf("expected SynTypeExpectUnionMember, got %+v", bag.Items())
	}
}

func TestParseTypeStructDuplicateField(t *testing.T) {
	src := "type Dup = { x: int, x: int };"
	_, _, bag := parseSource(t, src)
	if !bag.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
	if !hasDiagnostic(bag, diag.SynTypeFieldConflict) {
		t.Fatalf("expected SynTypeFieldConflict, got %+v", bag.Items())
	}
}

func hasDiagnostic(bag *diag.Bag, code diag.Code) bool {
	for _, item := range bag.Items() {
		if item.Code == code {
			return true
		}
	}
	return false
}
