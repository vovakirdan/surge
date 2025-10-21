package parser

// Тесты для парсинга типов.
//
// Покрытие:
//   - Базовые типы: int, float, string, bool, nothing
//   - Базовые типы с фиксированной шириной: int8-int64, uint8-uint64, float16-float64
//   - Квалифицированные пути к типам: foo.bar.Baz
//   - Ownership/borrowing: own T, &T, &mut T, *T
//   - Массивы: T[], T[n]
//   - Кортежи: (T, T, ...)
// Все комбинации let:
// let a [:Type]?;
// let a: own/&/&mut/*Type?;
// И тоже самое для массивов
//   - Функции: fn(T) -> U
//   todo: Обработка ошибок: некорректные типы, некорректные пути к типам
//   - Различные варианты пробелов и переносов строк

import (
	"strconv"
	"strings"
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
	"testing"
)

func TestBasicTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"int", "let x: int;", "int"},
		{"float", "let x: float;", "float"},
		{"string", "let x: string;", "string"},
		{"bool", "let x: bool;", "bool"},
		{"nothing", "let x: nothing;", "nothing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseTestInput(t, tt.input)
			assertSingleLetItem(t, letItem, arenas, tt.expected)
		})
	}
}

func TestFixedWidthTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Signed integers
		{"int8", "let x: int8;", "int8"},
		{"int16", "let x: int16;", "int16"},
		{"int32", "let x: int32;", "int32"},
		{"int64", "let x: int64;", "int64"},
		// Unsigned integers
		{"uint8", "let x: uint8;", "uint8"},
		{"uint16", "let x: uint16;", "uint16"},
		{"uint32", "let x: uint32;", "uint32"},
		{"uint64", "let x: uint64;", "uint64"},
		// Floating point
		{"float16", "let x: float16;", "float16"},
		{"float32", "let x: float32;", "float32"},
		{"float64", "let x: float64;", "float64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseTestInput(t, tt.input)
			assertSingleLetItem(t, letItem, arenas, tt.expected)
		})
	}
}

func TestQualifiedTypePaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple_path", "let x: foo;", "foo"},
		{"two_segments", "let x: foo.bar;", "foo.bar"},
		{"three_segments", "let x: foo.bar.Baz;", "foo.bar.Baz"},
		{"long_path", "let x: std.collections.vector.Vector;", "std.collections.vector.Vector"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseTestInput(t, tt.input)
			assertSingleLetItem(t, letItem, arenas, tt.expected)
		})
	}
}

func TestOwnershipBorrowingTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"owned_int", "let x: own int;", "own int"},
		{"ref_int", "let x: &int;", "&int"},
		{"ref_mut_int", "let x: &mut int;", "&mut int"},
		{"pointer_int", "let x: *int;", "*int"},
		{"double_ref", "let x: &&int;", "&&int"},
		{"ref_ref_mut", "let x: &&mut int;", "&&mut int"},
		{"double_pointer", "let x: **int;", "**int"},
		{"complex_ownership", "let x: own &mut *int;", "own &mut *int"},
		{"owned_qualified", "let x: own foo.bar.Baz;", "own foo.bar.Baz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseTestInput(t, tt.input)
			assertSingleLetItem(t, letItem, arenas, tt.expected)
		})
	}
}

func TestArrayTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"slice_int", "let x: int[];", "int[]"},
		{"sized_array_10", "let x: int[10];", "int[10]"},
		{"sized_array_100", "let x: string[100];", "string[100]"},
		{"nested_slice", "let x: int[][];", "int[][]"},
		{"nested_mixed", "let x: int[5][];", "int[5][]"},
		{"slice_of_sized", "let x: int[][10];", "int[][10]"},
		{"owned_slice", "let x: own int[];", "own int[]"},
		{"ref_to_array", "let x: &int[5];", "&int[5]"},
		{"qualified_array", "let x: foo.bar.Baz[];", "foo.bar.Baz[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseTestInput(t, tt.input)
			assertSingleLetItem(t, letItem, arenas, tt.expected)
		})
	}
}

func TestTupleTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty_tuple", "let x: ();", "()"},
		{"single_element_parens", "let x: (int);", "int"},
		{"single_element_tuple", "let x: (int,);", "(int,)"},
		{"two_elements", "let x: (int, string);", "(int, string)"},
		{"three_elements", "let x: (int, string, bool);", "(int, string, bool)"},
		{"nested_tuples", "let x: ((int, string), bool);", "((int, string), bool)"},
		{"tuple_with_ownership", "let x: (own int, &string);", "(own int, &string)"},
		{"tuple_with_arrays", "let x: (int[], string[10]);", "(int[], string[10])"},
		{"qualified_in_tuple", "let x: (foo.bar.Baz, int);", "(foo.bar.Baz, int)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseTestInput(t, tt.input)
			assertSingleLetItem(t, letItem, arenas, tt.expected)
		})
	}
}

func TestLetStatementsWithTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		mut      bool
	}{
		{"immutable_let", "let x: int;", "int", false},
		{"mutable_let", "let mut x: int;", "int", true},
		{"immutable_complex", "let data: own &mut foo.bar.Baz[];", "own &mut foo.bar.Baz[]", false},
		{"mutable_complex", "let mut data: own &mut foo.bar.Baz[];", "own &mut foo.bar.Baz[]", true},
		{"tuple_let", "let coords: (int, int);", "(int, int)", false},
		{"mutable_tuple", "let mut coords: (int, int);", "(int, int)", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseTestInput(t, tt.input)
			actualLetItem := assertSingleLetItem(t, letItem, arenas, tt.expected)
			if actualLetItem.IsMut != tt.mut {
				t.Errorf("Expected mut=%v, got mut=%v", tt.mut, actualLetItem.IsMut)
			}
		})
	}
}

func TestFunctionTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no_params_no_return", "let f: fn();", "fn() -> nothing"},
		{"no_params_with_return", "let f: fn() -> int;", "fn() -> int"},
		{"single_param", "let f: fn(int);", "fn(int) -> nothing"},
		{"single_param_with_return", "let f: fn(int) -> string;", "fn(int) -> string"},
		{"multiple_params", "let f: fn(int, string, bool);", "fn(int, string, bool) -> nothing"},
		{"multiple_params_with_return", "let f: fn(int, string) -> bool;", "fn(int, string) -> bool"},
		{"complex_param_types", "let f: fn(own int, &mut string, foo.bar.Baz[]);", "fn(own int, &mut string, foo.bar.Baz[]) -> nothing"},
		{"complex_return_type", "let f: fn(int) -> own &mut foo.bar.Baz[];", "fn(int) -> own &mut foo.bar.Baz[]"},
		{"tuple_params", "let f: fn((int, string), bool);", "fn((int, string), bool) -> nothing"},
		{"tuple_return", "let f: fn(int) -> (string, bool);", "fn(int) -> (string, bool)"},
		{"variadic_param", "let f: fn(...int);", "fn(...int) -> nothing"},
		{"variadic_with_other_params", "let f: fn(string, ...int);", "fn(string, ...int) -> nothing"},
		{"variadic_with_return", "let f: fn(...int) -> string;", "fn(...int) -> string"},
		{"nested_function_type", "let f: fn(fn(int) -> string) -> bool;", "fn(fn(int) -> string) -> bool"},
		{"trailing_comma", "let f: fn(int, string,);", "fn(int, string) -> nothing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseTestInput(t, tt.input)
			assertSingleLetItem(t, letItem, arenas, tt.expected)
		})
	}
}

func TestWhitespaceVariations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"extra_spaces", "let   x  :   int  ;", "int"},
		{"tabs", "let\tx\t:\tint\t;", "int"},
		{"mixed_whitespace", "let \t x \t : \t int \t ;", "int"},
		{"newlines", "let\nx\n:\nint\n;", "int"},
		{"complex_with_spaces", "let   data  :  own  &mut  foo . bar . Baz [ ] ;", "own &mut foo.bar.Baz[]"},
		{"tuple_with_spaces", "let   x  :  (  int  ,  string  ) ;", "(int, string)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseTestInput(t, tt.input)
			assertSingleLetItem(t, letItem, arenas, tt.expected)
		})
	}
}

// Helper functions

func parseTestInput(t *testing.T, input string) (*ast.LetItem, *ast.Builder) {
	// Create a test file and parser using the same pattern as import tests
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

	// Parse a single let item
	itemID, ok := p.parseLetItem()
	if !ok {
		if bag.Len() > 0 {
			t.Fatalf("Parsing failed with errors (count: %d)", bag.Len())
		}
		t.Fatal("Failed to parse let item")
	}

	// Get the let item
	item := arenas.Items.Get(itemID)
	if item.Kind != ast.ItemLet {
		t.Fatalf("expected let item, got %v", item.Kind)
	}

	letItem, ok := arenas.Items.Let(itemID)
	if !ok {
		t.Fatal("failed to get let item")
	}

	// Check for parsing errors
	if bag.Len() > 0 {
		t.Fatalf("Parsing failed with errors (count: %d)", bag.Len())
	}

	return letItem, arenas
}

func assertSingleLetItem(t *testing.T, letItem *ast.LetItem, arenas *ast.Builder, expectedType string) *ast.LetItem {
	// Check that type is present
	if letItem.Type == ast.NoTypeID {
		t.Fatal("LetItem has no type")
	}

	// Format the type and compare
	actualType := formatType(arenas, letItem.Type)
	if actualType != expectedType {
		t.Errorf("Expected type %q, got %q", expectedType, actualType)
	}

	return letItem
}

func formatType(arenas *ast.Builder, typeID ast.TypeID) string {
	typeExpr := arenas.Types.Get(typeID)
	if typeExpr == nil {
		return "<invalid>"
	}

	switch typeExpr.Kind {
	case ast.TypeExprPath:
		path, ok := arenas.Types.Path(typeID)
		if !ok {
			return "<invalid path>"
		}
		var parts []string
		for _, segment := range path.Segments {
			name, ok := arenas.StringsInterner.Lookup(segment.Name)
			if !ok {
				return "<invalid string>"
			}
			parts = append(parts, name)
		}
		return strings.Join(parts, ".")

	case ast.TypeExprUnary:
		unary, ok := arenas.Types.UnaryType(typeID)
		if !ok {
			return "<invalid unary>"
		}
		inner := formatType(arenas, unary.Inner)
		switch unary.Op {
		case ast.TypeUnaryOwn:
			return "own " + inner
		case ast.TypeUnaryRef:
			return "&" + inner
		case ast.TypeUnaryRefMut:
			return "&mut " + inner
		case ast.TypeUnaryPointer:
			return "*" + inner
		default:
			return "<unknown unary op>" + inner
		}

	case ast.TypeExprArray:
		array, ok := arenas.Types.Array(typeID)
		if !ok {
			return "<invalid array>"
		}
		elem := formatType(arenas, array.Elem)
		switch array.Kind {
		case ast.ArraySlice:
			return elem + "[]"
		case ast.ArraySized:
			if array.HasConstLen {
				return elem + "[" + formatUint64(array.ConstLength) + "]"
			}
			return elem + "[?]"
		default:
			return elem + "[<unknown>]"
		}

	case ast.TypeExprTuple:
		tuple, ok := arenas.Types.Tuple(typeID)
		if !ok {
			return "<invalid tuple>"
		}
		if len(tuple.Elems) == 0 {
			return "()"
		}
		var parts []string
		for _, elem := range tuple.Elems {
			parts = append(parts, formatType(arenas, elem))
		}
		result := "(" + strings.Join(parts, ", ")
		if len(tuple.Elems) == 1 {
			result += ","
		}
		result += ")"
		return result

	case ast.TypeExprFn:
		fn, ok := arenas.Types.Fn(typeID)
		if !ok {
			return "<invalid fn>"
		}
		var params []string
		for _, param := range fn.Params {
			paramType := formatType(arenas, param.Type)
			if param.Variadic {
				paramType = "..." + paramType
			}
			params = append(params, paramType)
		}
		retType := formatType(arenas, fn.Return)
		return "fn(" + strings.Join(params, ", ") + ") -> " + retType

	default:
		return "<unknown type>"
	}
}

func formatUint64(value uint64) string {
	return strconv.FormatUint(value, 10)
}
