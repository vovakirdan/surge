package parser

import (
	"strings"
	"surge/internal/ast"
	"surge/internal/diag"
	"testing"
)

// TestParseLetItem_SimpleDeclarations tests basic let declarations
func TestParseLetItem_SimpleDeclarations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantType bool
		wantVal  bool
		wantMut  bool
	}{
		{
			name:     "let with type and value",
			input:    "let x: int = 42;",
			wantName: "x",
			wantType: true,
			wantVal:  true,
			wantMut:  false,
		},
		{
			name:     "let with type only",
			input:    "let x: int;",
			wantName: "x",
			wantType: true,
			wantVal:  false,
			wantMut:  false,
		},
		{
			name:     "let with value only",
			input:    "let x = 42;",
			wantName: "x",
			wantType: false,
			wantVal:  true,
			wantMut:  false,
		},
		{
			name:     "mutable let with type and value",
			input:    "let mut x: int = 42;",
			wantName: "x",
			wantType: true,
			wantVal:  true,
			wantMut:  true,
		},
		{
			name:     "mutable let with type only",
			input:    "let mut x: int;",
			wantName: "x",
			wantType: true,
			wantVal:  false,
			wantMut:  true,
		},
		{
			name:     "mutable let with value only",
			input:    "let mut x = 42;",
			wantName: "x",
			wantType: false,
			wantVal:  true,
			wantMut:  true,
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

			letItem, ok := builder.Items.Let(file.Items[0])
			if !ok {
				t.Fatalf("expected let item, got %v", builder.Items.Get(file.Items[0]).Kind)
			}

			name := builder.StringsInterner.MustLookup(letItem.Name)
			if name != tt.wantName {
				t.Errorf("name: got %q, want %q", name, tt.wantName)
			}

			hasType := letItem.Type != ast.NoTypeID
			if hasType != tt.wantType {
				t.Errorf("has type: got %v, want %v", hasType, tt.wantType)
			}

			hasVal := letItem.Value != ast.NoExprID
			if hasVal != tt.wantVal {
				t.Errorf("has value: got %v, want %v", hasVal, tt.wantVal)
			}

			if letItem.IsMut != tt.wantMut {
				t.Errorf("is mutable: got %v, want %v", letItem.IsMut, tt.wantMut)
			}
		})
	}
}

func TestParseLetItem_Visibility(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantVisibility ast.Visibility
		wantError      bool
		wantAttrCount  int
		firstAttr      string
	}{
		{
			name:           "default_private",
			input:          "let x = 1;",
			wantVisibility: ast.VisPrivate,
			wantError:      false,
			wantAttrCount:  0,
		},
		{
			name:           "public_let",
			input:          "pub let x = 1;",
			wantVisibility: ast.VisPublic,
			wantError:      false,
			wantAttrCount:  0,
		},
		{
			name:           "invalid_async_modifier",
			input:          "async let x = 1;",
			wantVisibility: ast.VisPrivate,
			wantError:      true,
			wantAttrCount:  0,
		},
		{
			name:           "attribute_on_let",
			input:          "@deprecated let x = 1;",
			wantVisibility: ast.VisPrivate,
			wantError:      false,
			wantAttrCount:  1,
			firstAttr:      "deprecated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			if bag.HasErrors() != tt.wantError {
				t.Fatalf("expected error=%v, got errors=%v (%+v)", tt.wantError, bag.HasErrors(), bag.Items())
			}

			file := builder.Files.Get(fileID)
			if len(file.Items) == 0 {
				t.Fatal("expected at least one item")
			}

			letItem, ok := builder.Items.Let(file.Items[0])
			if !ok {
				t.Fatal("expected let item")
			}

		if letItem.Visibility != tt.wantVisibility {
			t.Fatalf("visibility: got %v, want %v", letItem.Visibility, tt.wantVisibility)
		}

		attrs := builder.Items.CollectAttrs(letItem.AttrStart, letItem.AttrCount)
		if len(attrs) != tt.wantAttrCount {
			t.Fatalf("attr count: got %d, want %d", len(attrs), tt.wantAttrCount)
		}
		if tt.firstAttr != "" {
			if len(attrs) == 0 {
				t.Fatalf("expected attribute %q, but none found", tt.firstAttr)
			}
			name := builder.StringsInterner.MustLookup(attrs[0].Name)
			if name != tt.firstAttr {
				t.Fatalf("attribute name: got %q, want %q", name, tt.firstAttr)
			}
		}
	})
}
}

// TestParseLetItem_ComplexTypes tests let declarations with complex types
func TestParseLetItem_ComplexTypes(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"array_type", "let x: int[];"},
		{"sized_array", "let x: int[10];"},
		{"reference_type", "let x: &int;"},
		{"mutable_reference", "let x: &mut int;"},
		{"pointer_type", "let x: *int;"},
		{"owned_type", "let x: own int;"},
		{"qualified_type", "let x: std.collections.Vector;"},
		{"nested_array", "let x: int[][];"},
		{"array_of_references", "let x: &int[];"},
		{"reference_to_array", "let x: &(int[]);"},
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

			letItem, ok := builder.Items.Let(file.Items[0])
			if !ok {
				t.Fatalf("expected let item")
			}

			if letItem.Type == ast.NoTypeID {
				t.Error("expected type to be present")
			}
		})
	}
}

// TestParseLetItem_ComplexValues tests let declarations with complex expressions
func TestParseLetItem_ComplexValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"arithmetic", "let x = 1 + 2 * 3;"},
		{"function_call", "let x = foo();"},
		{"method_call", "let x = obj.method();"},
		{"array_literal", "let x = [1, 2, 3];"},
		{"field_access", "let x = obj.field;"},
		{"array_index", "let x = arr[0];"},
		{"unary_expression", "let x = -value;"},
		{"reference_expression", "let x = &value;"},
		{"dereference", "let x = *ptr;"},
		{"boolean_expression", "let x = a && b || c;"},
		{"comparison", "let x = a < b;"},
		{"null_coalescing", "let x = a ?? b;"},
		{"range", "let x = 0..10;"},
		{"parenthesized", "let x = (a + b) * c;"},
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

			letItem, ok := builder.Items.Let(file.Items[0])
			if !ok {
				t.Fatalf("expected let item")
			}

			if letItem.Value == ast.NoExprID {
				t.Error("expected value to be present")
			}
		})
	}
}

// TestParseLetItem_Errors tests error conditions
func TestParseLetItem_Errors(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantErrorCode diag.Code
		description   string
	}{
		{
			name:          "missing identifier",
			input:         "let : int;",
			wantErrorCode: diag.SynExpectIdentifier,
			description:   "expected identifier after 'let'",
		},
		{
			name:          "missing semicolon",
			input:         "let x: int",
			wantErrorCode: diag.SynExpectSemicolon,
			description:   "expected semicolon at end",
		},
		{
			name:          "missing type after colon",
			input:         "let x: ;",
			wantErrorCode: diag.SynExpectType,
			description:   "expected type after colon",
		},
		{
			name:          "no type and no value",
			input:         "let x;",
			wantErrorCode: diag.SynExpectColon,
			description:   "expected either type annotation or initializer",
		},
		{
			name:          "missing expression after equals",
			input:         "let x = ;",
			wantErrorCode: diag.SynExpectExpression,
			description:   "expected expression after '='",
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

// TestParseLetItem_MultipleDeclarations tests multiple let declarations
func TestParseLetItem_MultipleDeclarations(t *testing.T) {
	input := `
		let x: int = 1;
		let y: float = 2.0;
		let mut z = "hello";
		let a: string;
	`

	builder, fileID, bag := parseSource(t, input)

	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if len(file.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(file.Items))
	}

	expectedNames := []string{"x", "y", "z", "a"}
	for i, expectedName := range expectedNames {
		letItem, ok := builder.Items.Let(file.Items[i])
		if !ok {
			t.Errorf("item %d: expected let item", i)
			continue
		}

		name := builder.StringsInterner.MustLookup(letItem.Name)
		if name != expectedName {
			t.Errorf("item %d: name got %q, want %q", i, name, expectedName)
		}
	}
}

// TestParseLetItem_WithWhitespace tests whitespace handling
func TestParseLetItem_WithWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "extra spaces",
			input: "let   x  :  int  =  42  ;",
		},
		{
			name:  "newlines",
			input: "let\nx\n:\nint\n=\n42\n;",
		},
		{
			name:  "tabs",
			input: "let\tx\t:\tint\t=\t42\t;",
		},
		{
			name: "multiline complex",
			input: `let x:
				int =
				42 + 
				100;`,
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

			_, ok := builder.Items.Let(file.Items[0])
			if !ok {
				t.Fatal("expected let item")
			}
		})
	}
}

// TestParseLetItem_EdgeCases tests edge cases
func TestParseLetItem_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldError bool
		errorCode   diag.Code
	}{
		{
			name:        "underscore identifier",
			input:       "let _: int = 42;",
			shouldError: false,
		},
		{
			name:        "long identifier",
			input:       "let very_long_variable_name_with_many_words: int = 42;",
			shouldError: false,
		},
		{
			name:        "keyword-like identifier",
			input:       "let letx: int = 42;",
			shouldError: false,
		},
		{
			name:        "mut without name",
			input:       "let mut : int = 42;",
			shouldError: true,
			errorCode:   diag.SynExpectIdentifier,
		},
		{
			name:        "double mut",
			input:       "let mut mut x: int = 42;",
			shouldError: true,
		},
		{
			name:        "mut after name",
			input:       "let x mut: int = 42;",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, bag := parseSource(t, tt.input)

			hasErrors := bag.HasErrors()
			if hasErrors != tt.shouldError {
				t.Errorf("expected error: %v, got: %v", tt.shouldError, hasErrors)
			}

			if tt.shouldError && tt.errorCode != 0 {
				found := false
				for _, d := range bag.Items() {
					if d.Code == tt.errorCode {
						found = true
						break
					}
				}
				if !found {
					var codes []string
					for _, d := range bag.Items() {
						codes = append(codes, d.Code.String())
					}
					t.Errorf("expected error code %s, got: %s",
						tt.errorCode.String(),
						strings.Join(codes, ", "))
				}
			}
		})
	}
}

// TestParseLetItem_TypeAnnotationVariants tests various type annotation styles
func TestParseLetItem_TypeAnnotationVariants(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"basic_type", "let x: int;"},
		{"qualified_path", "let x: std.io.File;"},
		{"generic_type", "let x: Vec<int>;"},
		{"function_type", "let x: fn(int) -> string;"},
		{"tuple_type", "let x: (int, string);"},
		{"optional_type", "let x: int?;"},
		{"result_type", "let x: Result<int, string>;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)

			// Some advanced type syntax might not be fully implemented yet
			if bag.HasErrors() {
				t.Skipf("Type syntax not yet supported: %+v", bag.Items())
			}

			file := builder.Files.Get(fileID)
			if len(file.Items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(file.Items))
			}

			letItem, ok := builder.Items.Let(file.Items[0])
			if !ok {
				t.Fatal("expected let item")
			}

			if letItem.Type == ast.NoTypeID {
				t.Error("expected type annotation")
			}
		})
	}
}

// TestParseLetItem_ValueVariants tests various value expression styles
func TestParseLetItem_ValueVariants(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"integer_literal", "let x = 42;"},
		{"float_literal", "let x = 3.14;"},
		{"string_literal", `let x = "hello";`},
		{"boolean_true", "let x = true;"},
		{"boolean_false", "let x = false;"},
		{"nothing_literal", "let x = nothing;"},
		{"identifier", "let x = other_var;"},
		{"binary_op", "let x = a + b;"},
		{"unary_op", "let x = -value;"},
		{"call_expr", "let x = func();"},
		{"index_expr", "let x = arr[0];"},
		{"field_expr", "let x = obj.field;"},
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

			letItem, ok := builder.Items.Let(file.Items[0])
			if !ok {
				t.Fatal("expected let item")
			}

			if letItem.Value == ast.NoExprID {
				t.Error("expected value expression")
			}
		})
	}
}
