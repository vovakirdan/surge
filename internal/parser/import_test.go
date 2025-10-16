package parser

// Тесты для парсинга import-деклараций.
//
// Покрытие:
//   - Простые импорты модулей: import foo; import foo/bar/baz;
//   - Импорты с алиасами модулей: import foo as F;
//   - Импорты конкретных элементов: import foo::Bar; import foo::Bar as B;
//   - Импорты групп элементов: import foo::{Bar, Baz}; import foo::{Bar as B, Baz};
//   - Обработка ошибок: отсутствующие сегменты, missing semicolons, unclosed braces
//   - Множественные импорты в одном файле
//   - Различные варианты пробелов и переносов строк

import (
	"strings"
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
	"testing"
)

// makeTestParser — хелпер для создания парсера с тестовой строкой
func makeTestParser(input string) (*Parser, *source.FileSet, *ast.Builder, *diag.Bag) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(input))
	file := fs.Get(fileID)

	bag := diag.NewBag(100)
	reporter := diag.BagReporter{Bag: bag}

	lxOpts := lexer.Options{Reporter: reporter}
	lx := lexer.New(file, lxOpts)

	arenas := ast.NewBuilder(ast.Hints{}, nil)

	opts := Options{
		MaxErrors: 100,
		Reporter:  reporter,
	}

	p := &Parser{
		lx:     lx,
		arenas: arenas,
		file:   arenas.Files.New(lx.EmptySpan()),
		fs:     fs,
		opts:   opts,
	}

	return p, fs, arenas, bag
}

// parseImportString — хелпер для парсинга одного импорта
func parseImportString(t *testing.T, input string) (*ast.ImportItem, *diag.Bag, *ast.Builder) {
	t.Helper()

	p, _, arenas, bag := makeTestParser(input)

	itemID, ok := p.parseImportItem()
	if !ok {
		return nil, bag, arenas
	}

	item := arenas.Items.Get(itemID)
	if item.Kind != ast.ItemImport {
		t.Fatalf("expected import item, got %v", item.Kind)
	}

	importItem, ok := arenas.Items.Import(itemID)
	if !ok {
		t.Fatal("failed to get import item")
	}
	return importItem, bag, arenas
}

func idsToStrings(t *testing.T, interner *source.Interner, ids []source.StringID) []string {
	t.Helper()
	out := make([]string, len(ids))
	for i, id := range ids {
		str, ok := interner.Lookup(id)
		if !ok {
			t.Fatalf("invalid string id %d", id)
		}
		out[i] = str
	}
	return out
}

func idToString(t *testing.T, interner *source.Interner, id source.StringID) (string, bool) {
	t.Helper()
	if id == source.NoStringID {
		return "", false
	}
	str, ok := interner.Lookup(id)
	if !ok {
		return "", false
	}
	return str, true
}

// TestParseImport_SimpleModule тестирует простейший импорт модуля
func TestParseImport_SimpleModule(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantSegs []string
	}{
		{
			name:     "single segment",
			input:    "import foo;",
			wantSegs: []string{"foo"},
		},
		{
			name:     "two segments",
			input:    "import foo/bar;",
			wantSegs: []string{"foo", "bar"},
		},
		{
			name:     "multiple segments",
			input:    "import std/io/file;",
			wantSegs: []string{"std", "io", "file"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp, bag, arenas := parseImportString(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			if imp == nil {
				t.Fatal("import item is nil")
			}

			actualSegs := idsToStrings(t, arenas.StringsInterner, imp.Module)
			if len(actualSegs) != len(tt.wantSegs) {
				t.Errorf("module segments count: got %d, want %d", len(actualSegs), len(tt.wantSegs))
			}

			for i, seg := range tt.wantSegs {
				if i >= len(actualSegs) {
					break
				}
				if actualSegs[i] != seg {
					t.Errorf("segment[%d]: got %q, want %q", i, actualSegs[i], seg)
				}
			}

			// Проверяем, что нет дополнительных элементов
			if alias, ok := idToString(t, arenas.StringsInterner, imp.ModuleAlias); ok && alias != "" {
				t.Errorf("expected no alias, got %q", alias)
			}
			if imp.HasOne {
				t.Errorf("expected no One, got %+v", imp.One)
			}
			if len(imp.Group) != 0 {
				t.Errorf("expected no Group, got %+v", imp.Group)
			}
		})
	}
}

// TestParseImport_ModuleWithAlias тестирует импорт модуля с алиасом
func TestParseImport_ModuleWithAlias(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantSegs  []string
		wantAlias string
	}{
		{
			name:      "simple alias",
			input:     "import foo as F;",
			wantSegs:  []string{"foo"},
			wantAlias: "F",
		},
		{
			name:      "alias for nested module",
			input:     "import std/io/file as File;",
			wantSegs:  []string{"std", "io", "file"},
			wantAlias: "File",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp, bag, arenas := parseImportString(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			if imp == nil {
				t.Fatal("import item is nil")
			}

			actualSegs := idsToStrings(t, arenas.StringsInterner, imp.Module)
			if len(actualSegs) != len(tt.wantSegs) {
				t.Errorf("module segments: got %v, want %v", actualSegs, tt.wantSegs)
			}

			alias, ok := idToString(t, arenas.StringsInterner, imp.ModuleAlias)
			if !ok || alias != tt.wantAlias {
				t.Errorf("alias: got %q, want %q", alias, tt.wantAlias)
			}

			// Проверяем, что нет One и Group
			if imp.HasOne {
				t.Errorf("expected no One, got %+v", imp.One)
			}
			if len(imp.Group) != 0 {
				t.Errorf("expected no Group, got %+v", imp.Group)
			}
		})
	}
}

// TestParseImport_SingleItem тестирует импорт конкретного элемента
func TestParseImport_SingleItem(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantSegs  []string
		wantName  string
		wantAlias string
	}{
		{
			name:      "item without alias",
			input:     "import foo::Bar;",
			wantSegs:  []string{"foo"},
			wantName:  "Bar",
			wantAlias: "",
		},
		{
			name:      "item with alias",
			input:     "import foo::Bar as B;",
			wantSegs:  []string{"foo"},
			wantName:  "Bar",
			wantAlias: "B",
		},
		{
			name:      "nested module item",
			input:     "import std/io::File;",
			wantSegs:  []string{"std", "io"},
			wantName:  "File",
			wantAlias: "",
		},
		{
			name:      "nested module item with alias",
			input:     "import std/io/file::Reader as FileReader;",
			wantSegs:  []string{"std", "io", "file"},
			wantName:  "Reader",
			wantAlias: "FileReader",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp, bag, arenas := parseImportString(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			if imp == nil {
				t.Fatal("import item is nil")
			}

			actualSegs := idsToStrings(t, arenas.StringsInterner, imp.Module)
			if len(actualSegs) != len(tt.wantSegs) {
				t.Errorf("module segments: got %v, want %v", actualSegs, tt.wantSegs)
			}

			if !imp.HasOne {
				t.Fatal("expected One to be set")
			}

			name, _ := idToString(t, arenas.StringsInterner, imp.One.Name)
			if name != tt.wantName {
				t.Errorf("item name: got %q, want %q", name, tt.wantName)
			}

			alias, _ := idToString(t, arenas.StringsInterner, imp.One.Alias)
			if alias != tt.wantAlias {
				t.Errorf("item alias: got %q, want %q", alias, tt.wantAlias)
			}

			// Проверяем, что нет module alias и Group
			if alias, ok := idToString(t, arenas.StringsInterner, imp.ModuleAlias); ok && alias != "" {
				t.Errorf("expected no module alias, got %q", alias)
			}
			if len(imp.Group) != 0 {
				t.Errorf("expected no Group, got %+v", imp.Group)
			}
		})
	}
}

// TestParseImport_Group тестирует импорт группы элементов
func TestParseImport_Group(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantSegs  []string
		wantPairs []struct {
			Name  string
			Alias string
		}
	}{
		{
			name:     "two items without aliases",
			input:    "import foo::{Bar, Baz};",
			wantSegs: []string{"foo"},
			wantPairs: []struct {
				Name  string
				Alias string
			}{
				{Name: "Bar", Alias: ""},
				{Name: "Baz", Alias: ""},
			},
		},
		{
			name:     "items with aliases",
			input:    "import foo::{Bar as B, Baz as Z};",
			wantSegs: []string{"foo"},
			wantPairs: []struct {
				Name  string
				Alias string
			}{
				{Name: "Bar", Alias: "B"},
				{Name: "Baz", Alias: "Z"},
			},
		},
		{
			name:     "mixed aliases",
			input:    "import foo::{Bar, Baz as Z, Qux};",
			wantSegs: []string{"foo"},
			wantPairs: []struct {
				Name  string
				Alias string
			}{
				{Name: "Bar", Alias: ""},
				{Name: "Baz", Alias: "Z"},
				{Name: "Qux", Alias: ""},
			},
		},
		{
			name:     "nested module group",
			input:    "import std/io::{File, Reader, Writer as W};",
			wantSegs: []string{"std", "io"},
			wantPairs: []struct {
				Name  string
				Alias string
			}{
				{Name: "File", Alias: ""},
				{Name: "Reader", Alias: ""},
				{Name: "Writer", Alias: "W"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp, bag, arenas := parseImportString(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			if imp == nil {
				t.Fatal("import item is nil")
			}

			actualSegs := idsToStrings(t, arenas.StringsInterner, imp.Module)
			if len(actualSegs) != len(tt.wantSegs) {
				t.Errorf("module segments: got %v, want %v", actualSegs, tt.wantSegs)
			}

			if len(imp.Group) != len(tt.wantPairs) {
				t.Fatalf("group count: got %d, want %d", len(imp.Group), len(tt.wantPairs))
			}

			for i, want := range tt.wantPairs {
				got := imp.Group[i]
				name, _ := idToString(t, arenas.StringsInterner, got.Name)
				if name != want.Name {
					t.Errorf("pair[%d] name: got %q, want %q", i, name, want.Name)
				}
				alias, _ := idToString(t, arenas.StringsInterner, got.Alias)
				if alias != want.Alias {
					t.Errorf("pair[%d] alias: got %q, want %q", i, alias, want.Alias)
				}
			}

			// Проверяем, что нет module alias и One
			if alias, ok := idToString(t, arenas.StringsInterner, imp.ModuleAlias); ok && alias != "" {
				t.Errorf("expected no module alias, got %q", alias)
			}
			if imp.HasOne {
				t.Errorf("expected no One, got %+v", imp.One)
			}
		})
	}
}

// TestParseImport_Errors тестирует различные ошибочные случаи
func TestParseImport_Errors(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantErrorCode diag.Code
		description   string
	}{
		{
			name:          "missing module segment",
			input:         "import ::Foo;",
			wantErrorCode: diag.SynExpectModuleSeg,
			description:   "expected SynExpectModuleSeg when module path starts with ::",
		},
		{
			name:          "missing segment after slash",
			input:         "import foo/;",
			wantErrorCode: diag.SynExpectModuleSeg,
			description:   "expected SynExpectModuleSeg when slash is not followed by identifier",
		},
		{
			name:          "double slash",
			input:         "import foo//bar;",
			wantErrorCode: diag.SynExpectSemicolon,
			description:   "expected SynExpectSemicolon when // starts comment",
		},
		// Примечание: double slash "//" воспринимается лексером как начало комментария,
		// поэтому ожидается SynExpectSemicolon
		{
			name:          "missing identifier after ::",
			input:         "import foo::;",
			wantErrorCode: diag.SynExpectItemAfterDbl,
			description:   "expected SynExpectItemAfterDbl when :: is not followed by identifier or {",
		},
		{
			name:          "missing identifier after as (module)",
			input:         "import foo as ;",
			wantErrorCode: diag.SynExpectIdentAfterAs,
			description:   "expected SynExpectIdentAfterAs when 'as' is not followed by identifier",
		},
		{
			name:          "missing identifier after as (item)",
			input:         "import foo::Bar as ;",
			wantErrorCode: diag.SynExpectIdentAfterAs,
			description:   "expected SynExpectIdentAfterAs in single item import",
		},
		{
			name:          "missing identifier after as (group)",
			input:         "import foo::{Bar as };",
			wantErrorCode: diag.SynExpectIdentAfterAs,
			description:   "expected SynExpectIdentAfterAs in group import",
		},
		{
			name:          "missing semicolon",
			input:         "import foo",
			wantErrorCode: diag.SynExpectSemicolon,
			description:   "expected SynExpectSemicolon when EOF reached before semicolon",
		},
		{
			name:          "unclosed group brace",
			input:         "import foo::{Bar, Baz",
			wantErrorCode: diag.SynUnclosedBrace,
			description:   "expected SynUnclosedBrace when } is missing",
		},
		{
			name:          "unexpected token after module path",
			input:         "import foo::{Bar, Baz;",
			wantErrorCode: diag.SynUnclosedBrace,
			description:   "expected SynUnclosedBrace when semicolon token after module path",
		},
		{
			name:          "unexpected token inside group path",
			input:         "import foo::{Bar, Baz +}",
			wantErrorCode: diag.SynUnexpectedToken,
			description:   "expected SynUnexpectedToken when unexpected token inside group path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, bag, _ := parseImportString(t, tt.input)

			if !bag.HasErrors() {
				t.Fatalf("expected error, but got none")
			}

			// Проверяем, что есть ожидаемая ошибка
			found := false
			for _, d := range bag.Items() {
				if d.Code == tt.wantErrorCode {
					found = true
					break
				}
			}

			if !found {
				var codes []string
				for _, d := range bag.Items() {
					codes = append(codes, d.Code.String())
				}
				t.Errorf("%s: expected error code %s, got errors: %s",
					tt.description,
					tt.wantErrorCode.String(),
					strings.Join(codes, ", "))
			}
		})
	}
}

func TestParseImport_UnclosedGroupDiagnostics(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCodes []diag.Code
	}{
		{
			name:      "missing brace before semicolon",
			input:     "import foo::{Bar, Baz;\nimport foo;",
			wantCodes: []diag.Code{diag.SynUnclosedBrace},
		},
		{
			name:      "missing brace and semicolon",
			input:     "import foo::{Bar, Baz\nimport foo;",
			wantCodes: []diag.Code{diag.SynUnclosedBrace, diag.SynExpectSemicolon},
		},
		{
			name:      "missing brace with comment and no semicolon",
			input:     "import foo::{Bar, Baz\n\n// trailing comment\n",
			wantCodes: []diag.Code{diag.SynUnclosedBrace, diag.SynExpectSemicolon},
		},
		{
			name:      "missing brace at end of file with semicolon",
			input:     "import foo::{Bar, Baz;\n\n",
			wantCodes: []diag.Code{diag.SynUnclosedBrace},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, bag, _ := parseImportString(t, tt.input)

			items := bag.Items()
			if len(items) != len(tt.wantCodes) {
				var got []string
				for _, d := range items {
					got = append(got, d.Code.String())
				}
				t.Fatalf("unexpected diagnostics count: got %d (%v), want %d",
					len(items), got, len(tt.wantCodes))
			}

			for i, code := range tt.wantCodes {
				if items[i].Code != code {
					t.Errorf("diag[%d] code mismatch: got %s, want %s", i, items[i].Code, code)
				}
			}

			if len(items) > 0 {
				wantOffset := strings.Index(tt.input, "Baz")
				if wantOffset == -1 {
					t.Fatalf("failed to locate 'Baz' in input")
				}
				wantOffset += len("Baz")
				got := items[0].Primary.Start
				if got != uint32(wantOffset) {
					t.Errorf("unexpected primary span start: got %d, want %d", got, wantOffset)
				}
				if items[0].Primary.End != got {
					t.Errorf("expected zero-length primary span, got end=%d start=%d", items[0].Primary.End, got)
				}
			}
		})
	}
}

// TestParseImport_TrailingComma тестирует поведение с trailing comma в группах
func TestParseImport_TrailingComma(t *testing.T) {
	// В текущей реализации trailing comma должна парситься корректно
	input := "import foo::{Bar, Baz,};"

	imp, bag, _ := parseImportString(t, input)

	// Лексер может выдать ошибку для trailing comma, но парсер должен справиться
	if imp == nil {
		t.Fatal("import item is nil")
	}

	// Проверяем, что группа распарсена
	if len(imp.Group) < 2 {
		t.Errorf("expected at least 2 items in group, got %d", len(imp.Group))
	}

	// Примечание: в зависимости от реализации могут быть warnings
	// Главное — не должно быть фатальных ошибок парсинга
	if bag.Len() > 0 {
		t.Logf("diagnostics (may include warnings): %v", bag.Items())
	}
}

// TestParseImport_Whitespace тестирует различные варианты пробелов
func TestParseImport_Whitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "extra spaces",
			input: "import   foo  ::  Bar  as  B  ;",
		},
		{
			name:  "newlines",
			input: "import foo\n::\nBar\nas\nB\n;",
		},
		{
			name:  "tabs and spaces",
			input: "import\tfoo\t::\t{Bar\t,\tBaz}\t;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp, bag, _ := parseImportString(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			if imp == nil {
				t.Fatal("import item is nil")
			}

			// Просто проверяем, что парсинг прошёл успешно
			if len(imp.Module) == 0 {
				t.Error("expected module segments")
			}
		})
	}
}

// TestParseMultipleImports тестирует парсинг нескольких импортов подряд
func TestParseMultipleImports(t *testing.T) {
	input := `import foo;
import bar::Baz;
import qux as Q;`

	p, _, arenas, bag := makeTestParser(input)
	p.parseItems()

	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %v", bag.Items())
	}

	file := arenas.Files.Get(p.file)
	if len(file.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(file.Items))
	}

	// Проверяем каждый импорт
	for i, itemID := range file.Items {
		item := arenas.Items.Get(itemID)
		if item.Kind != ast.ItemImport {
			t.Errorf("item[%d]: expected import, got %v", i, item.Kind)
		}
	}
}

// TestParseImport_Warnings тестирует случаи, которые должны выдавать предупреждения
func TestParseImport_Warnings(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantWarningCode diag.Code
		description     string
	}{
		{
			name:            "empty group",
			input:           "import foo::{};",
			wantWarningCode: diag.SynEmptyImportGroup,
			description:     "expected SynEmptyImportGroup warning for empty import group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, bag, _ := parseImportString(t, tt.input)

			// Не должно быть ошибок
			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			// Проверяем, что есть ожидаемое предупреждение
			found := false
			for _, d := range bag.Items() {
				if d.Code == tt.wantWarningCode {
					found = true
					break
				}
			}

			if !found {
				var codes []string
				for _, d := range bag.Items() {
					codes = append(codes, d.Code.String())
				}
				t.Errorf("%s: expected warning code %s, got diagnostics: %s",
					tt.description,
					tt.wantWarningCode.String(),
					strings.Join(codes, ", "))
			}
		})
	}
}

// TestParseImport_RelativePaths тестирует импорты с относительными путями
func TestParseImport_RelativePaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantSegs []string
	}{
		{
			name:     "current directory - single module",
			input:    "import ./module;",
			wantSegs: []string{".", "module"},
		},
		{
			name:     "current directory - nested path",
			input:    "import ./foo/bar;",
			wantSegs: []string{".", "foo", "bar"},
		},
		{
			name:     "parent directory - single module",
			input:    "import ../module;",
			wantSegs: []string{"..", "module"},
		},
		{
			name:     "parent directory - nested path",
			input:    "import ../foo/bar;",
			wantSegs: []string{"..", "foo", "bar"},
		},
		{
			name:     "two levels up - single module",
			input:    "import ../../module;",
			wantSegs: []string{"..", "..", "module"},
		},
		{
			name:     "two levels up - nested path",
			input:    "import ../../foo/bar/baz;",
			wantSegs: []string{"..", "..", "foo", "bar", "baz"},
		},
		{
			name:     "three levels up",
			input:    "import ../../../module;",
			wantSegs: []string{"..", "..", "..", "module"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp, bag, arenas := parseImportString(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			if imp == nil {
				t.Fatal("import item is nil")
			}

			actualSegs := idsToStrings(t, arenas.StringsInterner, imp.Module)
			if len(actualSegs) != len(tt.wantSegs) {
				t.Errorf("module segments count: got %d, want %d", len(actualSegs), len(tt.wantSegs))
			}

			for i, seg := range tt.wantSegs {
				if i >= len(actualSegs) {
					break
				}
				if actualSegs[i] != seg {
					t.Errorf("segment[%d]: got %q, want %q", i, actualSegs[i], seg)
				}
			}

			// Проверяем, что нет дополнительных элементов
			if alias, ok := idToString(t, arenas.StringsInterner, imp.ModuleAlias); ok && alias != "" {
				t.Errorf("expected no alias, got %q", alias)
			}
			if imp.HasOne {
				t.Errorf("expected no One, got %+v", imp.One)
			}
			if len(imp.Group) != 0 {
				t.Errorf("expected no Group, got %+v", imp.Group)
			}
		})
	}
}

// TestParseImport_RelativePathsWithAliases тестирует относительные импорты с алиасами
func TestParseImport_RelativePathsWithAliases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantSegs  []string
		wantAlias string
	}{
		{
			name:      "current directory module with alias",
			input:     "import ./module as M;",
			wantSegs:  []string{".", "module"},
			wantAlias: "M",
		},
		{
			name:      "parent directory module with alias",
			input:     "import ../utils as Utils;",
			wantSegs:  []string{"..", "utils"},
			wantAlias: "Utils",
		},
		{
			name:      "two levels up with alias",
			input:     "import ../../lib/core as Core;",
			wantSegs:  []string{"..", "..", "lib", "core"},
			wantAlias: "Core",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp, bag, arenas := parseImportString(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			if imp == nil {
				t.Fatal("import item is nil")
			}

			actualSegs := idsToStrings(t, arenas.StringsInterner, imp.Module)
			if len(actualSegs) != len(tt.wantSegs) {
				t.Errorf("module segments: got %v, want %v", actualSegs, tt.wantSegs)
			}

			alias, ok := idToString(t, arenas.StringsInterner, imp.ModuleAlias)
			if !ok || alias != tt.wantAlias {
				t.Errorf("alias: got %q, want %q", alias, tt.wantAlias)
			}

			// Проверяем, что нет One и Group
			if imp.HasOne {
				t.Errorf("expected no One, got %+v", imp.One)
			}
			if len(imp.Group) != 0 {
				t.Errorf("expected no Group, got %+v", imp.Group)
			}
		})
	}
}

// TestParseImport_RelativePathsWithItems тестирует относительные импорты конкретных элементов
func TestParseImport_RelativePathsWithItems(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantSegs  []string
		wantName  string
		wantAlias string
	}{
		{
			name:      "current directory item",
			input:     "import ./module::Foo;",
			wantSegs:  []string{".", "module"},
			wantName:  "Foo",
			wantAlias: "",
		},
		{
			name:      "current directory item with alias",
			input:     "import ./module::Foo as F;",
			wantSegs:  []string{".", "module"},
			wantName:  "Foo",
			wantAlias: "F",
		},
		{
			name:      "parent directory item",
			input:     "import ../utils::Helper;",
			wantSegs:  []string{"..", "utils"},
			wantName:  "Helper",
			wantAlias: "",
		},
		{
			name:      "parent directory item with alias",
			input:     "import ../utils::Helper as H;",
			wantSegs:  []string{"..", "utils"},
			wantName:  "Helper",
			wantAlias: "H",
		},
		{
			name:      "two levels up item",
			input:     "import ../../lib/core::Engine;",
			wantSegs:  []string{"..", "..", "lib", "core"},
			wantName:  "Engine",
			wantAlias: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp, bag, arenas := parseImportString(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			if imp == nil {
				t.Fatal("import item is nil")
			}

			actualSegs := idsToStrings(t, arenas.StringsInterner, imp.Module)
			if len(actualSegs) != len(tt.wantSegs) {
				t.Errorf("module segments: got %v, want %v", actualSegs, tt.wantSegs)
			}

			if !imp.HasOne {
				t.Fatal("expected One to be set")
			}

			name, _ := idToString(t, arenas.StringsInterner, imp.One.Name)
			if name != tt.wantName {
				t.Errorf("item name: got %q, want %q", name, tt.wantName)
			}

			alias, _ := idToString(t, arenas.StringsInterner, imp.One.Alias)
			if alias != tt.wantAlias {
				t.Errorf("item alias: got %q, want %q", alias, tt.wantAlias)
			}

			// Проверяем, что нет module alias и Group
			if alias, ok := idToString(t, arenas.StringsInterner, imp.ModuleAlias); ok && alias != "" {
				t.Errorf("expected no module alias, got %q", alias)
			}
			if len(imp.Group) != 0 {
				t.Errorf("expected no Group, got %+v", imp.Group)
			}
		})
	}
}

// TestParseImport_RelativePathsWithGroups тестирует относительные импорты групп элементов
func TestParseImport_RelativePathsWithGroups(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantSegs  []string
		wantPairs []struct {
			Name  string
			Alias string
		}
	}{
		{
			name:     "current directory group",
			input:    "import ./module::{Foo, Bar};",
			wantSegs: []string{".", "module"},
			wantPairs: []struct {
				Name  string
				Alias string
			}{
				{Name: "Foo", Alias: ""},
				{Name: "Bar", Alias: ""},
			},
		},
		{
			name:     "parent directory group with aliases",
			input:    "import ../utils::{Helper as H, Logger as L};",
			wantSegs: []string{"..", "utils"},
			wantPairs: []struct {
				Name  string
				Alias string
			}{
				{Name: "Helper", Alias: "H"},
				{Name: "Logger", Alias: "L"},
			},
		},
		{
			name:     "two levels up group mixed",
			input:    "import ../../lib::{Engine, Config as Cfg};",
			wantSegs: []string{"..", "..", "lib"},
			wantPairs: []struct {
				Name  string
				Alias string
			}{
				{Name: "Engine", Alias: ""},
				{Name: "Config", Alias: "Cfg"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp, bag, arenas := parseImportString(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %v", bag.Items())
			}

			if imp == nil {
				t.Fatal("import item is nil")
			}

			actualSegs := idsToStrings(t, arenas.StringsInterner, imp.Module)
			if len(actualSegs) != len(tt.wantSegs) {
				t.Errorf("module segments: got %v, want %v", actualSegs, tt.wantSegs)
			}

			if len(imp.Group) != len(tt.wantPairs) {
				t.Fatalf("group count: got %d, want %d", len(imp.Group), len(tt.wantPairs))
			}

			for i, want := range tt.wantPairs {
				got := imp.Group[i]
				name, _ := idToString(t, arenas.StringsInterner, got.Name)
				if name != want.Name {
					t.Errorf("pair[%d] name: got %q, want %q", i, name, want.Name)
				}
				alias, _ := idToString(t, arenas.StringsInterner, got.Alias)
				if alias != want.Alias {
					t.Errorf("pair[%d] alias: got %q, want %q", i, alias, want.Alias)
				}
			}

			// Проверяем, что нет module alias и One
			if alias, ok := idToString(t, arenas.StringsInterner, imp.ModuleAlias); ok && alias != "" {
				t.Errorf("expected no module alias, got %q", alias)
			}
			if imp.HasOne {
				t.Errorf("expected no One, got %+v", imp.One)
			}
		})
	}
}
