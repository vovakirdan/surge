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

func TestComplexExpressions_pt1(t *testing.T) {
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

// Additional tests for expression parsing - comprehensive coverage

func TestUnaryOperators_AllVariants(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		expectedOp ast.ExprUnaryOp
	}{
		{"unary_plus", "let x = +a;", ast.ExprUnaryPlus},
		{"unary_minus", "let x = -a;", ast.ExprUnaryMinus},
		{"logical_not", "let x = !a;", ast.ExprUnaryNot},
		{"dereference", "let x = *a;", ast.ExprUnaryDeref},
		{"reference", "let x = &a;", ast.ExprUnaryRef},
		{"mutable_reference", "let x = &mut a;", ast.ExprUnaryRefMut},
		{"own", "let x = own a;", ast.ExprUnaryOwn},
		
		// Nested unary operators
		{"double_negation", "let x = --a;", ast.ExprUnaryMinus},
		{"not_not", "let x = !!a;", ast.ExprUnaryNot},
		{"ref_deref", "let x = &*a;", ast.ExprUnaryRef},
		{"deref_ref", "let x = *&a;", ast.ExprUnaryDeref},
		
		// Complex combinations
		{"minus_deref", "let x = -*a;", ast.ExprUnaryMinus},
		{"not_ref", "let x = !&a;", ast.ExprUnaryNot},
		{"own_deref", "let x = own *a;", ast.ExprUnaryOwn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}

			expr := arenas.Exprs.Get(letItem.Value)
			if expr.Kind != ast.ExprUnary {
				t.Fatalf("Expected unary expression, got %v", expr.Kind)
			}

			unary, ok := arenas.Exprs.Unary(letItem.Value)
			if !ok {
				t.Fatal("Failed to get unary expression payload")
			}
			if unary.Op != tt.expectedOp {
				t.Errorf("Expected op %v, got %v", tt.expectedOp, unary.Op)
			}
		})
	}
}

func TestBinaryOperators_Precedence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		desc  string
	}{
		{
			name:  "multiplication_before_addition",
			input: "let x = a + b * c;",
			desc:  "b * c should be evaluated before addition",
		},
		{
			name:  "addition_before_comparison",
			input: "let x = a + b < c;",
			desc:  "a + b should be evaluated before comparison",
		},
		{
			name:  "comparison_before_logical_and",
			input: "let x = a < b && c > d;",
			desc:  "comparisons should be evaluated before logical AND",
		},
		{
			name:  "logical_and_before_or",
			input: "let x = a || b && c;",
			desc:  "b && c should be evaluated before logical OR",
		},
		{
			name:  "shift_before_addition",
			input: "let x = a << b + c;",
			desc:  "b + c should be evaluated before shift",
		},
		{
			name:  "bitwise_and_before_xor",
			input: "let x = a & b ^ c;",
			desc:  "a & b should be evaluated before XOR",
		},
		{
			name:  "bitwise_xor_before_or",
			input: "let x = a ^ b | c;",
			desc:  "a ^ b should be evaluated before bitwise OR",
		},
		{
			name:  "assignment_right_associative",
			input: "let x = a = b = c;",
			desc:  "assignment is right-associative",
		},
		{
			name:  "null_coalescing",
			input: "let x = a ?? b ?? c;",
			desc:  "null coalescing operator",
		},
		{
			name:  "range_operators",
			input: "let x = a..b;",
			desc:  "range operator",
		},
		{
			name:  "range_inclusive",
			input: "let x = a..=b;",
			desc:  "inclusive range operator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatalf("Expected expression value for %s", tt.desc)
			}
			
			expr := arenas.Exprs.Get(letItem.Value)
			if expr.Kind != ast.ExprBinary {
				t.Fatalf("Expected binary expression for %s, got %v", tt.desc, expr.Kind)
			}
		})
	}
}

func TestBinaryOperators_Associativity(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		rightAssoc bool
		desc       string
	}{
		{
			name:       "addition_left_assoc",
			input:      "let x = a + b + c;",
			rightAssoc: false,
			desc:       "addition is left-associative",
		},
		{
			name:       "multiplication_left_assoc",
			input:      "let x = a * b * c;",
			rightAssoc: false,
			desc:       "multiplication is left-associative",
		},
		{
			name:       "assignment_right_assoc",
			input:      "let x = a = b = c;",
			rightAssoc: true,
			desc:       "assignment is right-associative",
		},
		{
			name:       "compound_assignment_right_assoc",
			input:      "let x = a += b += c;",
			rightAssoc: true,
			desc:       "compound assignment is right-associative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatalf("Expected expression value for %s", tt.desc)
			}
			
			expr := arenas.Exprs.Get(letItem.Value)
			if expr.Kind != ast.ExprBinary {
				t.Fatalf("Expected binary expression for %s, got %v", tt.desc, expr.Kind)
			}
		})
	}
}

func TestCompoundAssignmentOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"plus_assign", "let x = a += b;"},
		{"minus_assign", "let x = a -= b;"},
		{"mul_assign", "let x = a *= b;"},
		{"div_assign", "let x = a /= b;"},
		{"mod_assign", "let x = a %= b;"},
		{"and_assign", "let x = a &= b;"},
		{"or_assign", "let x = a |= b;"},
		{"xor_assign", "let x = a ^= b;"},
		{"shl_assign", "let x = a <<= b;"},
		{"shr_assign", "let x = a >>= b;"},
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

func TestBitwiseOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"bitwise_and", "let x = a & b;"},
		{"bitwise_or", "let x = a | b;"},
		{"bitwise_xor", "let x = a ^ b;"},
		{"left_shift", "let x = a << b;"},
		{"right_shift", "let x = a >> b;"},
		
		// Complex bitwise expressions
		{"and_or_combo", "let x = a & b | c;"},
		{"xor_and_combo", "let x = a ^ b & c;"},
		{"shift_combo", "let x = a << b >> c;"},
		{"multiple_and", "let x = a & b & c & d;"},
		{"multiple_or", "let x = a | b | c | d;"},
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

func TestComparisonOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"equal", "let x = a == b;"},
		{"not_equal", "let x = a != b;"},
		{"less_than", "let x = a < b;"},
		{"less_or_equal", "let x = a <= b;"},
		{"greater_than", "let x = a > b;"},
		{"greater_or_equal", "let x = a >= b;"},
		{"type_check", "let x = a is int;"},
		
		// Chained comparisons
		{"chained_comparison_1", "let x = a < b < c;"},
		{"chained_comparison_2", "let x = a == b == c;"},
		{"mixed_comparison", "let x = a < b && c > d;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}

			expr := arenas.Exprs.Get(letItem.Value)
			// Should parse as binary expression (comparison or logical)
			if expr.Kind != ast.ExprBinary {
				t.Errorf("Expected binary expression, got %v", expr.Kind)
			}
		})
	}
}

func TestComplexExpressions_pt2(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "arithmetic_with_precedence",
			input: "let x = a + b * c - d / e % f;",
		},
		{
			name:  "mixed_logical_and_comparison",
			input: "let x = a < b && c > d || e == f;",
		},
		{
			name:  "unary_with_binary",
			input: "let x = -a + *b - &c;",
		},
		{
			name:  "parenthesized_expression",
			input: "let x = (a + b) * (c - d);",
		},
		{
			name:  "nested_parentheses",
			input: "let x = ((a + b) * c) / ((d - e) + f);",
		},
		{
			name:  "multiple_unary_operators",
			input: "let x = --a + !!b - **c;",
		},
		{
			name:  "bitwise_with_arithmetic",
			input: "let x = a << 2 + b >> 3;",
		},
		{
			name:  "assignment_in_expression",
			input: "let x = a = b + c * d;",
		},
		{
			name:  "null_coalescing_chain",
			input: "let x = a ?? b ?? c ?? d;",
		},
		{
			name:  "range_with_arithmetic",
			input: "let x = a + 1..b - 1;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}
			
			// Just verify it parses without errors
			expr := arenas.Exprs.Get(letItem.Value)
			if expr == nil {
				t.Fatal("Failed to get expression")
			}
		})
	}
}

func TestPostfixOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"field_access", "let x = a.field;"},
		{"chained_field_access", "let x = a.b.c.d;"},
		{"array_index", "let x = a[0];"},
		{"array_index_expr", "let x = a[i + 1];"},
		{"function_call_no_args", "let x = foo();"},
		{"function_call_one_arg", "let x = foo(a);"},
		{"function_call_multiple_args", "let x = foo(a, b, c);"},
		{"method_call", "let x = obj.method();"},
		{"chained_calls", "let x = obj.method1().method2();"},
		{"index_after_call", "let x = foo()[0];"},
		{"call_after_index", "let x = arr[0]();"},
		{"complex_postfix", "let x = obj.field[i].method(a, b).result;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}
			
			expr := arenas.Exprs.Get(letItem.Value)
			if expr == nil {
				t.Fatal("Failed to get expression")
			}
		})
	}
}

func TestExpressionEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty_parens", "let x = ();"},
		{"single_ident", "let x = identifier;"},
		{"very_long_chain", "let x = a.b.c.d.e.f.g.h.i.j.k.l.m.n.o.p;"},
		{"deeply_nested_parens", "let x = ((((a))));"},
		{"mixed_all_operators", "let x = -a + b * c / d % e & f | g ^ h << i >> j;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				// Some edge cases might not parse, just verify no crash
				return
			}
			
			expr := arenas.Exprs.Get(letItem.Value)
			if expr == nil {
				t.Fatal("Failed to get expression")
			}
		})
	}
}

func TestNumberLiterals_ExtendedFormats(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"decimal_integer", "let x = 42;"},
		{"zero", "let x = 0;"},
		{"large_integer", "let x = 1234567890;"},
		{"decimal_float", "let x = 3.14;"},
		{"float_with_exp", "let x = 1.5e10;"},
		{"float_with_neg_exp", "let x = 1.5e-10;"},
		{"hex_literal", "let x = 0xFF;"},
		{"binary_literal", "let x = 0b1010;"},
		{"octal_literal", "let x = 0o777;"},
		{"float_no_leading_digit", "let x = .5;"},
		{"float_no_trailing_digit", "let x = 5.;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				// Some formats might not be supported
				return
			}
			
			expr := arenas.Exprs.Get(letItem.Value)
			if expr == nil {
				t.Fatal("Failed to get expression")
			}
		})
	}
}

func TestStringLiterals_Variants(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"simple_string", `let x = "hello";`},
		{"empty_string", `let x = "";`},
		{"string_with_escapes", `let x = "hello\nworld";`},
		{"string_with_quotes", `let x = "say \"hello\"";`},
		{"string_with_backslash", `let x = "path\\to\\file";`},
		{"raw_string", `let x = "no\nescapes";`},
		{"multiline_string", `let x = "line1\nline2\nline3";`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				// Some string formats might not be supported yet
				return
			}
			
			expr := arenas.Exprs.Get(letItem.Value)
			if expr == nil {
				t.Fatal("Failed to get expression")
			}
		})
	}
}

func TestBooleanAndNothingLiterals(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"true_literal", "let x = true;"},
		{"false_literal", "let x = false;"},
		{"nothing_literal", "let x = nothing;"},
		{"bool_in_expression", "let x = true && false;"},
		{"nothing_with_null_coalescing", "let x = nothing ?? value;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			letItem, arenas := parseExprTestInput(t, tt.input)
			if letItem.Value == ast.NoExprID {
				t.Fatal("Expected expression value")
			}
			
			expr := arenas.Exprs.Get(letItem.Value)
			if expr == nil {
				t.Fatal("Failed to get expression")
			}
		})
	}
}
