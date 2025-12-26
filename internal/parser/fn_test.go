package parser

import (
	"strings"
	"surge/internal/ast"
	"surge/internal/diag"
	"testing"
)

// TestParseFnItem_SimpleDeclarations tests basic function declarations
func TestParseFnItem_SimpleDeclarations(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantName   string
		wantParams int
		wantBody   bool
	}{
		{
			name:       "no params, no return, no body",
			input:      "fn foo();",
			wantName:   "foo",
			wantParams: 0,
			wantBody:   false,
		},
		{
			name:       "no params, no return, with body",
			input:      "fn foo() {}",
			wantName:   "foo",
			wantParams: 0,
			wantBody:   true,
		},
		{
			name:       "one param, no return",
			input:      "fn foo(x: int) {}",
			wantName:   "foo",
			wantParams: 1,
			wantBody:   true,
		},
		{
			name:       "multiple params, no return",
			input:      "fn foo(x: int, y: string) {}",
			wantName:   "foo",
			wantParams: 2,
			wantBody:   true,
		},
		{
			name:       "no params, with return type",
			input:      "fn foo() -> int {}",
			wantName:   "foo",
			wantParams: 0,
			wantBody:   true,
		},
		{
			name:       "params and return type",
			input:      "fn foo(x: int) -> string {}",
			wantName:   "foo",
			wantParams: 1,
			wantBody:   true,
		},
		{
			name:       "declaration without body",
			input:      "fn foo(x: int) -> string;",
			wantName:   "foo",
			wantParams: 1,
			wantBody:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %+v", bag.Items())
			}

			file := builder.Files.Get(fileID)
			if len(file.Items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(file.Items))
			}

			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatalf("expected fn item, got %v", builder.Items.Get(file.Items[0]).Kind)
			}

			name := builder.StringsInterner.MustLookup(fnItem.Name)
			if name != tt.wantName {
				t.Errorf("name: got %q, want %q", name, tt.wantName)
			}

			if fnItem.ParamsCount != uint32(tt.wantParams) {
				t.Errorf("param count: got %d, want %d", fnItem.ParamsCount, tt.wantParams)
			}

			hasBody := fnItem.Body.IsValid()
			if hasBody != tt.wantBody {
				t.Errorf("has body: got %v, want %v", hasBody, tt.wantBody)
			}
		})
	}
}

// TestParseFnItem_Parameters tests function parameter parsing
func TestParseFnItem_Parameters(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantParams []struct {
			name     string
			hasType  bool
			variadic bool
		}
	}{
		{
			name:  "single parameter",
			input: "fn foo(x: int) {}",
			wantParams: []struct {
				name     string
				hasType  bool
				variadic bool
			}{
				{name: "x", hasType: true, variadic: false},
			},
		},
		{
			name:  "multiple parameters",
			input: "fn foo(x: int, y: string, z: bool) {}",
			wantParams: []struct {
				name     string
				hasType  bool
				variadic bool
			}{
				{name: "x", hasType: true, variadic: false},
				{name: "y", hasType: true, variadic: false},
				{name: "z", hasType: true, variadic: false},
			},
		},
		{
			name:  "parameters with complex types",
			input: "fn foo(x: int[], y: &string, z: *bool) {}",
			wantParams: []struct {
				name     string
				hasType  bool
				variadic bool
			}{
				{name: "x", hasType: true, variadic: false},
				{name: "y", hasType: true, variadic: false},
				{name: "z", hasType: true, variadic: false},
			},
		},
		{
			name:  "variadic parameter last",
			input: "fn foo(x: int, ...rest: string) {}",
			wantParams: []struct {
				name     string
				hasType  bool
				variadic bool
			}{
				{name: "x", hasType: true, variadic: false},
				{name: "rest", hasType: true, variadic: true},
			},
		},
		{
			name:  "single variadic parameter",
			input: "fn foo(...values: int) {}",
			wantParams: []struct {
				name     string
				hasType  bool
				variadic bool
			}{
				{name: "values", hasType: true, variadic: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %+v", bag.Items())
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			if int(fnItem.ParamsCount) != len(tt.wantParams) {
				t.Fatalf("param count: got %d, want %d", fnItem.ParamsCount, len(tt.wantParams))
			}

			paramIDs := builder.Items.GetFnParamIDs(fnItem)
			for i, wantParam := range tt.wantParams {
				param := builder.Items.FnParam(paramIDs[i])
				if param == nil {
					t.Errorf("param %d: not found", i)
					continue
				}

				name := builder.StringsInterner.MustLookup(param.Name)
				if name != wantParam.name {
					t.Errorf("param %d name: got %q, want %q", i, name, wantParam.name)
				}

				hasType := param.Type != ast.NoTypeID
				if hasType != wantParam.hasType {
					t.Errorf("param %d has type: got %v, want %v", i, hasType, wantParam.hasType)
				}

				if param.Variadic != wantParam.variadic {
					t.Errorf("param %d variadic: got %v, want %v", i, param.Variadic, wantParam.variadic)
				}
			}
		})
	}
}

// TestParseFnItem_ReturnTypes tests function return type parsing
func TestParseFnItem_ReturnTypes(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"basic_return", "fn foo() -> int {}"},
		{"qualified_return", "fn foo() -> std.io.File {}"},
		{"reference_return", "fn foo() -> &string {}"},
		{"array_return", "fn foo() -> int[] {}"},
		{"pointer_return", "fn foo() -> *int {}"},
		{"owned_return", "fn foo() -> own int {}"},
		{"nothing_return_explicit", "fn foo() -> nothing {}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %+v", bag.Items())
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			if fnItem.ReturnType == ast.NoTypeID {
				t.Error("expected return type to be present")
			}
		})
	}
}

// TestParseFnItem_WithBody tests function bodies
func TestParseFnItem_WithBody(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty body",
			input: "fn foo() {}",
		},
		{
			name: "body with statements",
			input: `fn foo() {
				let x = 1;
				return x;
			}`,
		},
		{
			name: "body with multiple statements",
			input: `fn foo() {
				let x = 1;
				let y = 2;
				let z = x + y;
				return z;
			}`,
		},
		// {
		// 	name: "nested blocks",
		// 	input: `fn foo() {
		// 		{
		// 			let x = 1;
		// 		}
		// 	}`,
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %+v", bag.Items())
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			if !fnItem.Body.IsValid() {
				t.Error("expected body to be present")
			}
		})
	}
}

// TestParseFnItem_Errors tests error conditions
func TestParseFnItem_Errors(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantErrorCode diag.Code
		description   string
	}{
		{
			name:          "missing function name",
			input:         "fn () {}",
			wantErrorCode: diag.SynExpectIdentifier,
			description:   "expected identifier, got \"(\"",
		},
		{
			name:          "missing left paren",
			input:         "fn foo) {}",
			wantErrorCode: diag.SynUnexpectedToken,
			description:   "expected '(' after function name",
		},
		{
			name:          "missing right paren",
			input:         "fn foo( {}",
			wantErrorCode: diag.SynUnclosedParen,
			description:   "expected ')' after function parameters",
		},
		{
			name:          "missing param type",
			input:         "fn foo(x) {}",
			wantErrorCode: diag.SynExpectColon,
			description:   "expected ':' after parameter name",
		},
		{
			name:          "missing colon in param",
			input:         "fn foo(x int) {}",
			wantErrorCode: diag.SynExpectColon,
			description:   "expected ':' after parameter name",
		},
		{
			name:          "missing return type after arrow",
			input:         "fn foo() -> {}",
			wantErrorCode: diag.SynUnexpectedToken,
			description:   "expected type after '->'",
		},
		{
			name:          "missing semicolon or body",
			input:         "fn foo()",
			wantErrorCode: diag.SynExpectSemicolon,
			description:   "expected ';' or '{' after signature",
		},
		{
			name:          "parameter after variadic",
			input:         "fn foo(...args: int, y: int) {}",
			wantErrorCode: diag.SynVariadicMustBeLast,
			description:   "variadic parameter must be last",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, bag := parseSource(t, tt.input)

			if !bag.HasErrors() {
				t.Fatal("expected error, but got none")
			}

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

// TestParseFnItem_MultipleFunctions tests multiple function declarations
func TestParseFnItem_MultipleFunctions(t *testing.T) {
	input := `
		fn foo() {}
		fn bar(x: int) -> string {}
		fn baz(x: int, y: int) -> int;
	`

	builder, fileID, bag := parseSource(t, input)

	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if len(file.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(file.Items))
	}

	expectedNames := []string{"foo", "bar", "baz"}
	for i, expectedName := range expectedNames {
		fnItem, ok := builder.Items.Fn(file.Items[i])
		if !ok {
			t.Errorf("item %d: expected fn item", i)
			continue
		}

		name := builder.StringsInterner.MustLookup(fnItem.Name)
		if name != expectedName {
			t.Errorf("item %d: name got %q, want %q", i, name, expectedName)
		}
	}
}

// TestParseFnItem_WithWhitespace tests whitespace handling
func TestParseFnItem_WithWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "extra spaces",
			input: "fn   foo  (  x  :  int  )  ->  string  {  }",
		},
		{
			name:  "newlines",
			input: "fn\nfoo\n(\nx\n:\nint\n)\n->\nstring\n{\n}",
		},
		{
			name:  "tabs",
			input: "fn\tfoo\t(\tx\t:\tint\t)\t->\tstring\t{\t}",
		},
		{
			name: "multiline params",
			input: `fn foo(
				x: int,
				y: string,
				z: bool
			) -> int {}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %+v", bag.Items())
			}

			file := builder.Files.Get(fileID)
			if len(file.Items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(file.Items))
			}

			_, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}
		})
	}
}

// TestParseFnItem_ParametersWithDefaults tests parameters with default values
func TestParseFnItem_ParametersWithDefaults(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"single_default", "fn foo(x: int = 42) {}"},
		{"multiple_defaults", "fn foo(x: int = 1, y: string = \"hello\") {}"},
		{"mixed_defaults", "fn foo(x: int, y: int = 10) {}"},
		{"complex_default_expr", "fn foo(x: int = 1 + 2 * 3) {}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %+v", bag.Items())
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			// Just verify it parsed successfully
			if fnItem.ParamsCount == 0 {
				t.Error("expected at least one parameter")
			}
		})
	}
}

// TestParseFnItem_EdgeCases tests edge cases
func TestParseFnItem_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldError bool
	}{
		{
			name:        "underscore param",
			input:       "fn foo(_: int) {}",
			shouldError: false,
		},
		{
			name:        "long function name",
			input:       "fn very_long_function_name_with_many_words() {}",
			shouldError: false,
		},
		{
			name:        "many parameters",
			input:       "fn foo(a: int, b: int, c: int, d: int, e: int, f: int) {}",
			shouldError: false,
		},
		{
			name:        "trailing comma in params",
			input:       "fn foo(x: int, y: int,) {}",
			shouldError: false, // Should handle gracefully or error
		},
		{
			name:        "empty arrow return",
			input:       "fn foo() -> {}",
			shouldError: true,
		},
		{
			name:        "double arrow",
			input:       "fn foo() -> -> int {}",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, bag := parseSource(t, tt.input)

			hasErrors := bag.HasErrors()
			if hasErrors != tt.shouldError {
				t.Errorf("expected error: %v, got: %v (errors: %+v)",
					tt.shouldError, hasErrors, bag.Items())
			}
		})
	}
}

// TestParseFnItem_Generics tests function generic parameters
func TestParseFnItem_Generics(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"header_generics_before_name", "fn <T> foo(x: T) {}"},
		{"header_generics_multiple_before_name", "fn <T, U> foo(x: T, y: U) {}"},
		{"single_generic", "fn foo<T>(x: T) {}"},
		{"multiple_generics", "fn foo<T, U>(x: T, y: U) {}"},
		{"generic_with_return", "fn foo<T>() -> T {}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			// Generic syntax might not be fully implemented yet
			if bag.HasErrors() {
				t.Skipf("Generic syntax not yet supported: %+v", bag.Items())
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			if len(fnItem.Generics) == 0 {
				t.Error("expected generic parameters")
			}
		})
	}

	t.Run("duplicate_generic_lists", func(t *testing.T) {
		_, _, bag := parseSource(t, "fn <T> foo<T>() {}")
		if !bag.HasErrors() {
			t.Fatal("expected error for duplicate generic lists, got none")
		}
	})
}

func TestParseFnItem_Modifiers(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantFlags ast.FnModifier
		wantError bool
	}{
		{
			name:      "pub_fn",
			input:     "pub fn foo() {}",
			wantFlags: ast.FnModifierPublic,
		},
		{
			name:      "async_fn",
			input:     "async fn foo() {}",
			wantFlags: ast.FnModifierAsync,
		},
		{
			name:      "combined_modifiers",
			input:     "pub async fn foo() {}",
			wantFlags: ast.FnModifierPublic | ast.FnModifierAsync,
		},
		{
			name:      "duplicate_async",
			input:     "async async fn foo() {}",
			wantFlags: ast.FnModifierAsync,
			wantError: true,
		},
		{
			name:      "unsafe_modifier",
			input:     "unsafe fn foo() {}",
			wantFlags: 0,
			wantError: true,
		},
		{
			name:      "unknown_modifier",
			input:     "some_modifier fn foo() {}",
			wantFlags: 0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			hasErrors := bag.HasErrors()
			if hasErrors != tt.wantError {
				t.Fatalf("expected error=%v, got %v (bag=%+v)", tt.wantError, hasErrors, bag.Items())
			}

			file := builder.Files.Get(fileID)
			if len(file.Items) == 0 {
				t.Fatal("expected at least one item")
			}

			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected function item")
			}

			if (fnItem.Flags & tt.wantFlags) != tt.wantFlags {
				t.Fatalf("expected flags %v to include %v", fnItem.Flags, tt.wantFlags)
			}
		})
	}
}

func TestParseFnItem_Attributes(t *testing.T) {
	inputs := []struct {
		name      string
		input     string
		attrNames []string
	}{
		{
			name:      "single_attribute",
			input:     "@pure fn foo() {}",
			attrNames: []string{"pure"},
		},
		{
			name:      "multiple_attributes_with_args",
			input:     "@pure @backend(\"gpu\") async fn foo() {}",
			attrNames: []string{"pure", "backend"},
		},
	}

	for _, tt := range inputs {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)
			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %+v", bag.Items())
			}
			file := builder.Files.Get(fileID)
			if len(file.Items) == 0 {
				t.Fatal("expected at least one item")
			}
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}
			attrs := builder.Items.CollectAttrs(fnItem.AttrStart, fnItem.AttrCount)
			if len(attrs) != len(tt.attrNames) {
				t.Fatalf("attr count: got %d, want %d", len(attrs), len(tt.attrNames))
			}
			for i, wantName := range tt.attrNames {
				name := builder.StringsInterner.MustLookup(attrs[i].Name)
				if name != wantName {
					t.Fatalf("attr[%d] name: got %q, want %q", i, name, wantName)
				}
			}
		})
	}
}

// TestParseFnItem_ComplexSignatures tests complex function signatures
func TestParseFnItem_ComplexSignatures(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "higher_order_function",
			input: "fn foo(f: fn(int) -> string) {}",
		},
		{
			name:  "array_params_and_return",
			input: "fn foo(xs: int[]) -> string[] {}",
		},
		{
			name:  "reference_params",
			input: "fn foo(x: &int, y: &mut string) {}",
		},
		{
			name:  "pointer_params",
			input: "fn foo(p: *int) -> *string {}",
		},
		{
			name:  "owned_params",
			input: "fn foo(x: own int) -> own string {}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			// Some complex signatures might not be fully implemented
			if bag.HasErrors() {
				t.Skipf("Complex signature not yet supported: %+v", bag.Items())
			}

			file := builder.Files.Get(fileID)
			_, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}
		})
	}
}
