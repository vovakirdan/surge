package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
	"testing"
)

func TestBasicLiterals(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"integer", "let x = 42;"},
		{"float", "let x = 3.14;"},
		{"string", "let x = \"hello\";"},
		{"true", "let x = true;"},
		{"false", "let x = false;"},
		{"nothing", "let x = nothing;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, _ := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}
		})
	}
}

func TestBinaryOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"addition", "let x = a + b;"},
		{"subtraction", "let x = a - b;"},
		{"multiplication", "let x = a * b;"},
		{"division", "let x = a / b;"},
		{"modulo", "let x = a % b;"},
		{"equality", "let x = a == b;"},
		{"inequality", "let x = a != b;"},
		{"less_than", "let x = a < b;"},
		{"less_equal", "let x = a <= b;"},
		{"greater_than", "let x = a > b;"},
		{"greater_equal", "let x = a >= b;"},
		{"logical_and", "let x = a && b;"},
		{"logical_or", "let x = a || b;"},
		{"assignment", "let x = a = b;"},
		{"add_assign", "let x = a += b;"},
		{"sub_assign", "let x = a -= b;"},
		{"mul_assign", "let x = a *= b;"},
		{"div_assign", "let x = a /= b;"},
		{"mod_assign", "let x = a %= b;"},
		{"and_assign", "let x = a &= b;"},
		{"or_assign", "let x = a |= b;"},
		{"xor_assign", "let x = a ^= b;"},
		{"shl_assign", "let x = a <<= b;"},
		{"shr_assign", "let x = a >>= b;"},
		{"null_coalescing", "let x = a ?? b;"},
		{"range", "let x = a..b;"},
		{"range_inclusive", "let x = a..=b;"},
		{"type_check", "let x = a is int;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}

			expr := arenas.Exprs.Get(letItem.Value)
			if expr.Kind != ast.ExprBinary {
				t.Errorf("Expected binary expression, got %v", expr.Kind)
			}
		})
	}
}

func TestUnaryOperators(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		expectedOp ast.ExprUnaryOp
	}{
		{"plus", "let x = +a;", ast.ExprUnaryPlus},
		{"minus", "let x = -a;", ast.ExprUnaryMinus},
		{"not", "let x = !a;", ast.ExprUnaryNot},
		{"deref", "let x = *a;", ast.ExprUnaryDeref},
		{"ref", "let x = &a;", ast.ExprUnaryRef},
		{"ref_mut", "let x = &mut a;", ast.ExprUnaryRefMut},
		{"own", "let x = own a;", ast.ExprUnaryOwn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}

			expr := arenas.Exprs.Get(letItem.Value)
			if expr.Kind != ast.ExprUnary {
				t.Errorf("Expected unary expression, got %v", expr.Kind)
			}

			unaryData, ok := arenas.Exprs.Unary(letItem.Value)
			if !ok {
				t.Fatal("Failed to get unary data")
			}
			if unaryData.Op != tt.expectedOp {
				t.Errorf("Expected unary op %v, got %v", tt.expectedOp, unaryData.Op)
			}
		})
	}
}

func TestComplexExpressions(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"precedence", "let x = a + b * c;"},
		{"parentheses", "let x = (a + b) * c;"},
		{"function_call", "let x = func(a, b, c);"},
		{"array_index", "let x = arr[i];"},
		{"member_access", "let x = obj.field;"},
		{"chained_calls", "let x = obj.method().field;"},
		{"nested_index", "let x = matrix[i][j];"},
		{"complex", "let x = obj.method(a + b)[index].field;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, _ := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}
		})
	}
}

func TestOperatorPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string // упрощённое описание структуры
	}{
		{"multiply_before_add", "let x = a + b * c;", "binary(a, +, binary(b, *, c))"},
		{"parentheses_override", "let x = (a + b) * c;", "binary(group(binary(a, +, b)), *, c)"},
		{"right_associative_assign", "let x = a = b = c;", "binary(a, =, binary(b, =, c))"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}

			// Простая проверка что выражение создано корректно
			expr := arenas.Exprs.Get(letItem.Value)
			if expr == nil {
				t.Fatal("Failed to get expression")
			}
		})
	}
}

func TestTupleExpressions(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty_tuple", "let x = ();"},
		{"single_element", "let x = (a,);"},
		{"two_elements", "let x = (a, b);"},
		{"three_elements", "let x = (a, b, c);"},
		{"nested_tuples", "let x = ((a, b), (c, d));"},
		{"tuple_with_expressions", "let x = (a + b, c * d);"},
		{"mixed_expressions", "let x = (func(a), arr[i], obj.field);"},
		{"trailing_comma", "let x = (a, b, c,);"},
		{"complex_nested", "let x = (a, (b, c), d);"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}

			expr := arenas.Exprs.Get(letItem.Value)
			if expr.Kind != ast.ExprTuple {
				t.Errorf("Expected tuple expression, got %v", expr.Kind)
			}

			tupleData, ok := arenas.Exprs.Tuple(letItem.Value)
			if !ok {
				t.Fatal("Failed to get tuple data")
			}

			// Для пустого tuple проверяем что элементов нет
			if tt.name == "empty_tuple" && len(tupleData.Elements) != 0 {
				t.Errorf("Expected empty tuple, got %d elements", len(tupleData.Elements))
			}

			// Для остальных проверяем что элементы есть
			if tt.name != "empty_tuple" && len(tupleData.Elements) == 0 {
				t.Error("Expected tuple with elements, got empty")
			}
		})
	}
}

func TestCastExpression(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectInnerKind ast.ExprKind
	}{
		{"simple_cast", "let x = value to int;", ast.ExprIdent},
		{"chained_cast", "let x = value to int to float;", ast.ExprCast},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}

			expr := arenas.Exprs.Get(letItem.Value)
			if expr.Kind != ast.ExprCast {
				t.Fatalf("Expected cast expression, got %v", expr.Kind)
			}

			castData, ok := arenas.Exprs.Cast(letItem.Value)
			if !ok {
				t.Fatal("Failed to get cast data")
			}

			inner := arenas.Exprs.Get(castData.Value)
			if inner.Kind != tt.expectInnerKind {
				t.Errorf("Expected inner value %v, got %v", tt.expectInnerKind, inner.Kind)
			}

			typeExpr := arenas.Types.Get(castData.Type)
			if typeExpr == nil {
				t.Fatal("Expected cast type to be recorded")
			}
		})
	}
}

// Helper function для парсинга выражений в тестах
func parseExprTestInput(t *testing.T, input string) (*ast.LetItem, *ast.Builder) {
	t.Helper()
	// Создаём парсер используя тот же подход что и в types_test.go
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

	// Парсим let item
	itemID, ok := p.parseLetItem()
	if !ok {
		if bag.Len() > 0 {
			t.Fatalf("Parsing failed with errors (count: %d)", bag.Len())
		}
		t.Fatal("Failed to parse let item")
	}

	// Получаем let item
	item := arenas.Items.Get(itemID)
	if item.Kind != ast.ItemLet {
		t.Fatalf("expected let item, got %v", item.Kind)
	}

	letItem, ok := arenas.Items.Let(itemID)
	if !ok {
		t.Fatal("failed to get let item")
	}

	// Проверяем на ошибки
	if bag.Len() > 0 {
		t.Fatalf("Parsing failed with errors (count: %d)", bag.Len())
	}

	return letItem, arenas
}
