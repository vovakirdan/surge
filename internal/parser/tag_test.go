package parser

import (
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
)

func TestParseTagItem_Basic(t *testing.T) {
	src := "tag Ping();"
	builder, fileID, bag := parseSource(t, src)

	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatalf("file not found")
	}
	if len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(file.Items))
	}

	tagItem, ok := builder.Items.Tag(file.Items[0])
	if !ok {
		t.Fatalf("expected tag item, got %v", builder.Items.Get(file.Items[0]).Kind)
	}

	if lookupNameOr(builder, tagItem.Name, "") != "Ping" {
		t.Fatalf("unexpected tag name: %s", lookupNameOr(builder, tagItem.Name, ""))
	}
	if tagItem.Visibility != ast.VisPrivate {
		t.Fatalf("expected private visibility, got %v", tagItem.Visibility)
	}
	if len(tagItem.Generics) != 0 {
		t.Fatalf("expected no generics, got %d", len(tagItem.Generics))
	}
	if len(tagItem.Payload) != 0 {
		t.Fatalf("expected empty payload, got %d", len(tagItem.Payload))
	}
}

func TestParseTagItem_PublicGenericsPayload(t *testing.T) {
	src := "pub tag Some<T>(T, string);"
	builder, fileID, bag := parseSource(t, src)

	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if file == nil || len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %v", len(file.Items))
	}

	tagItem, ok := builder.Items.Tag(file.Items[0])
	if !ok {
		t.Fatalf("expected tag item")
	}

	if tagItem.Visibility != ast.VisPublic {
		t.Fatalf("expected public visibility, got %v", tagItem.Visibility)
	}
	if len(tagItem.Generics) != 1 || lookupNameOr(builder, tagItem.Generics[0], "") != "T" {
		t.Fatalf("unexpected generics: %+v", tagItem.Generics)
	}
	if len(tagItem.Payload) != 2 {
		t.Fatalf("expected two payload types, got %d", len(tagItem.Payload))
	}

	payloadStrings := make([]string, 0, len(tagItem.Payload))
	for _, pid := range tagItem.Payload {
		payloadStrings = append(payloadStrings, stringifyType(builder, pid))
	}

	if payloadStrings[0] != "T" || payloadStrings[1] != "string" {
		t.Fatalf("unexpected payload types: %v", payloadStrings)
	}
}

func TestParseTagItem_InvalidModifier(t *testing.T) {
	src := "async tag Foo();"
	_, _, bag := parseSource(t, src)
	if !bag.HasErrors() {
		t.Fatalf("expected diagnostics for invalid modifier")
	}

	found := false
	for _, d := range bag.Items() {
		if d.Code == diag.SynUnexpectedModifier {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected SynUnexpectedModifier diagnostic, got %+v", bag.Items())
	}
}

func TestParseTagItem_MissingSemicolon(t *testing.T) {
	src := "tag Foo()"
	builder, fileID, bag := parseSource(t, src)
	if !bag.HasErrors() {
		t.Fatalf("expected diagnostics for missing semicolon")
	}

	found := false
	for _, d := range bag.Items() {
		if d.Code == diag.SynExpectSemicolon {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected SynExpectSemicolon diagnostic, got %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatalf("file missing")
	}
	if len(file.Items) != 0 {
		t.Fatalf("expected no recorded items on parse failure, got %d", len(file.Items))
	}
}

func stringifyType(builder *ast.Builder, typeID ast.TypeID) string {
	if !typeID.IsValid() {
		return "<invalid>"
	}
	typ := builder.Types.Get(typeID)
	if typ == nil {
		return "<nil>"
	}
	if typ.Kind != ast.TypeExprPath {
		return "<non-path>"
	}
	path, ok := builder.Types.Path(typeID)
	if !ok || path == nil || len(path.Segments) == 0 {
		return "<path>"
	}
	names := make([]string, 0, len(path.Segments))
	for _, seg := range path.Segments {
		names = append(names, lookupNameOr(builder, seg.Name, "<seg>"))
	}
	return strings.Join(names, "::")
}
