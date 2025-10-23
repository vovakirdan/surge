package parser

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
)

func parseSource(t *testing.T, input string) (*ast.Builder, ast.FileID, *diag.Bag) {
	t.Helper()

	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(input))
	file := fs.Get(fileID)

	bag := diag.NewBag(100)
	reporter := &diag.BagReporter{Bag: bag}

	lx := lexer.New(file, lexer.Options{Reporter: reporter})
	builder := ast.NewBuilder(ast.Hints{}, nil)

	opts := Options{
		MaxErrors: 100,
		Reporter:  reporter,
	}

	result := ParseFile(fs, lx, builder, opts)
	if result.Bag == nil {
		result.Bag = bag
	}

	return builder, result.File, result.Bag
}

func TestParseBlockStatements_Positive(t *testing.T) {
	input := `
		fn foo() {
			let x = 1;
			foo();
			return;
			return foo();
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatal("file not found in builder")
	}
	if len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(file.Items))
	}

	itemID := file.Items[0]
	fnItem, ok := builder.Items.Fn(itemID)
	if !ok {
		t.Fatalf("expected fn item, got %v", builder.Items.Get(itemID).Kind)
	}
	if !fnItem.Body.IsValid() {
		t.Fatal("function body not recorded")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil {
		t.Fatal("block payload missing")
	}
	if len(block.Stmts) != 4 {
		t.Fatalf("expected 4 statements, got %d", len(block.Stmts))
	}

	letStmt := builder.Stmts.Get(block.Stmts[0])
	if letStmt.Kind != ast.StmtLet {
		t.Fatalf("expected first stmt to be let, got %v", letStmt.Kind)
	}
	letPayload := builder.Stmts.Let(block.Stmts[0])
	if letPayload == nil {
		t.Fatal("let payload is nil")
	}
	if lookup := builder.StringsInterner.MustLookup(letPayload.Name); lookup != "x" {
		t.Fatalf("expected let name 'x', got %q", lookup)
	}
	if letPayload.Value == ast.NoExprID {
		t.Fatal("let value missing")
	}

	exprStmt := builder.Stmts.Get(block.Stmts[1])
	if exprStmt.Kind != ast.StmtExpr {
		t.Fatalf("expected second stmt to be expression, got %v", exprStmt.Kind)
	}
	exprPayload := builder.Stmts.Expr(block.Stmts[1])
	if exprPayload == nil || !exprPayload.Expr.IsValid() {
		t.Fatal("expression statement payload missing")
	}
	if expr := builder.Exprs.Get(exprPayload.Expr); expr == nil || expr.Kind != ast.ExprCall {
		t.Fatalf("expected call expression, got %v", expr)
	}

	retNoExpr := builder.Stmts.Get(block.Stmts[2])
	if retNoExpr.Kind != ast.StmtReturn {
		t.Fatalf("expected third stmt to be return, got %v", retNoExpr.Kind)
	}
	retNoExprPayload := builder.Stmts.Return(block.Stmts[2])
	if retNoExprPayload == nil {
		t.Fatal("return payload missing")
	}
	if retNoExprPayload.Expr.IsValid() {
		t.Fatal("expected bare return without expression")
	}

	retWithExpr := builder.Stmts.Get(block.Stmts[3])
	if retWithExpr.Kind != ast.StmtReturn {
		t.Fatalf("expected fourth stmt to be return, got %v", retWithExpr.Kind)
	}
	retWithExprPayload := builder.Stmts.Return(block.Stmts[3])
	if retWithExprPayload == nil || !retWithExprPayload.Expr.IsValid() {
		t.Fatal("expected return with expression payload")
	}
	if expr := builder.Exprs.Get(retWithExprPayload.Expr); expr == nil || expr.Kind != ast.ExprCall {
		t.Fatalf("expected return expression to be call, got %v", expr)
	}
}

func TestParseBlockStatements_Diagnostics(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCodes []diag.Code
	}{
		{
			name:      "LetMissingSemicolon",
			input:     "fn foo() { let x = 1 }",
			wantCodes: []diag.Code{diag.SynExpectSemicolon},
		},
		{
			name:      "ReturnMissingSemicolon",
			input:     "fn foo() { return 1 }",
			wantCodes: []diag.Code{diag.SynExpectSemicolon},
		},
		{
			name:      "MissingClosingBrace",
			input:     "fn foo() { return; ",
			wantCodes: []diag.Code{diag.SynUnclosedBrace},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, bag := parseSource(t, tt.input)
			if !bag.HasErrors() {
				t.Fatalf("expected diagnostics, got none")
			}
			got := make(map[diag.Code]bool)
			for _, d := range bag.Items() {
				got[d.Code] = true
			}
			for _, want := range tt.wantCodes {
				if !got[want] {
					t.Fatalf("expected diagnostic %s, got %+v", want.String(), bag.Items())
				}
			}
		})
	}
}
