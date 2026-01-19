package lsp

import (
	"strings"
	"testing"
)

func TestCompletionMember(t *testing.T) {
	src := strings.Join([]string{
		"type Point = { x: int, y: int }",
		"",
		"extern<Point> {",
		"    fn length(self: &Point) -> float { return 0.0; }",
		"}",
		"",
		"fn main() {",
		"    let p = Point { x = 1, y = 2 };",
		"    p.x",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)
	offset := strings.Index(src, "p.x") + len("p.")
	items := buildCompletion(snapshot, uri, positionForOffsetUTF16(src, offset)).Items
	if len(items) == 0 {
		t.Fatal("expected completion items")
	}
	field := findCompletion(items, "x")
	if field == nil || field.Kind != completionItemKindField {
		t.Fatalf("expected field completion for x, got %+v", field)
	}
	method := findCompletion(items, "length")
	if method == nil || method.Kind != completionItemKindMethod {
		t.Fatalf("expected method completion for length, got %+v", method)
	}
}

func TestCompletionStaticEnum(t *testing.T) {
	src := strings.Join([]string{
		"enum Color = {",
		"    Red,",
		"    Green,",
		"}",
		"",
		"fn main() {",
		"    let c = Color::",
		"}",
		"",
	}, "\n")
	snapshot, uri := analyzeSnapshot(t, src)
	offset := strings.Index(src, "Color::") + len("Color::")
	items := buildCompletion(snapshot, uri, positionForOffsetUTF16(src, offset)).Items
	if item := findCompletion(items, "Red"); item == nil || item.Kind != completionItemKindEnumMember {
		t.Fatalf("expected enum member completion for Red, got %+v", item)
	}
}

func TestCompletionModuleAccess(t *testing.T) {
	t.Setenv("SURGE_STDLIB", stdlibRoot(t))
	util := strings.Join([]string{
		"pub fn greet(name: string) -> string {",
		"    return name;",
		"}",
		"",
	}, "\n")
	main := strings.Join([]string{
		"import util;",
		"",
		"fn main() {",
		"    util::",
		"}",
		"",
	}, "\n")
	snapshot, paths := analyzeWorkspaceSnapshot(t, map[string]string{
		"util.sg": util,
		"main.sg": main,
	}, nil)
	uri := pathToURI(paths["main.sg"])
	offset := strings.Index(main, "util::") + len("util::")
	items := buildCompletion(snapshot, uri, positionForOffsetUTF16(main, offset)).Items
	if item := findCompletion(items, "greet"); item == nil {
		t.Fatalf("expected module completion for greet, got %+v", item)
	}
}

func TestCompletionImport(t *testing.T) {
	t.Setenv("SURGE_STDLIB", stdlibRoot(t))
	util := strings.Join([]string{
		"pub fn greet(name: string) -> string {",
		"    return name;",
		"}",
		"",
	}, "\n")
	main := strings.Join([]string{
		"import util::greet;",
		"",
		"fn main() {}",
		"",
	}, "\n")
	snapshot, paths := analyzeWorkspaceSnapshot(t, map[string]string{
		"util.sg": util,
		"main.sg": main,
	}, nil)
	uri := pathToURI(paths["main.sg"])
	offset := strings.Index(main, "greet")
	items := buildCompletion(snapshot, uri, positionForOffsetUTF16(main, offset)).Items
	if item := findCompletion(items, "greet"); item == nil {
		t.Fatalf("expected import member completion for greet, got %+v", item)
	}
}

func findCompletion(items []completionItem, label string) *completionItem {
	for i := range items {
		if items[i].Label == label {
			return &items[i]
		}
	}
	return nil
}
