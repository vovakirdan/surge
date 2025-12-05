package parser

import (
	"context"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
)

func parseSource(t *testing.T, input string) (*ast.Builder, ast.FileID, *diag.Bag) {
	return parseSourceWithOptions(t, input, Options{})
}

func parseSourceWithOptions(t *testing.T, input string, opts Options) (*ast.Builder, ast.FileID, *diag.Bag) {
	t.Helper()

	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(input))
	file := fs.Get(fileID)

	bag := diag.NewBag(100)
	reporter := &diag.BagReporter{Bag: bag}

	lx := lexer.New(file, lexer.Options{Reporter: reporter})
	builder := ast.NewBuilder(ast.Hints{}, nil)

	if opts.MaxErrors == 0 {
		opts.MaxErrors = 100
	}
	opts.Reporter = reporter

	result := ParseFile(context.Background(), fs, lx, builder, opts)
	if result.Bag == nil {
		result.Bag = bag
	}

	return builder, result.File, result.Bag
}

func lookupNameOr(builder *ast.Builder, id source.StringID, fallback string) string {
	if builder == nil || builder.StringsInterner == nil || id == source.NoStringID {
		return fallback
	}
	return builder.StringsInterner.MustLookup(id)
}

func TestParsePragmaDirective(t *testing.T) {
	input := `pragma directive, no_std

fn foo() {}
`
	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatal("file not found")
	}
	if file.Pragma.IsEmpty() {
		t.Fatal("expected pragma metadata")
	}
	if file.Pragma.Flags&ast.PragmaFlagDirective == 0 {
		t.Fatalf("expected directive flag, got %v", file.Pragma.Flags)
	}
	if len(file.Pragma.Entries) != 2 {
		t.Fatalf("expected 2 pragma entries, got %d", len(file.Pragma.Entries))
	}
	names := []string{
		builder.StringsInterner.MustLookup(file.Pragma.Entries[0].Name),
		builder.StringsInterner.MustLookup(file.Pragma.Entries[1].Name),
	}
	if names[0] != "directive" || names[1] != "no_std" {
		t.Fatalf("unexpected pragma entry names: %v", names)
	}
}

func TestPragmaPositionError(t *testing.T) {
	input := `
fn foo() {}

pragma directive
`
	_, _, bag := parseSource(t, input)
	if !bag.HasErrors() {
		t.Fatal("expected pragma position error")
	}
	found := false
	for _, d := range bag.Items() {
		if d.Code == diag.SynPragmaPosition {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected SynPragmaPosition diagnostic, got %s", diagnosticsSummary(bag))
	}
}

func TestDirectiveBlocksCollectedWhenEnabled(t *testing.T) {
	input := `
import stdlib/directives::test;

/// test:
/// test.eq(foo, 42)
fn foo() -> int { return 42; }
`
	opts := Options{
		MaxErrors:     100,
		DirectiveMode: DirectiveModeCollect,
	}
	builder, fileID, bag := parseSourceWithOptions(t, input, opts)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatal("file not found")
	}
	if len(file.Directives) != 1 {
		t.Fatalf("expected 1 directive block, got %d", len(file.Directives))
	}
	block := file.Directives[0]
	if lookupNameOr(builder, block.Namespace, "") != "test" {
		t.Fatalf("expected namespace 'test', got %q", lookupNameOr(builder, block.Namespace, ""))
	}
	if block.Owner == ast.NoItemID {
		t.Fatal("expected directive block to be attached to an item")
	}
	if len(block.Lines) != 1 {
		t.Fatalf("expected 1 directive line, got %d", len(block.Lines))
	}
	lineText := lookupNameOr(builder, block.Lines[0].Text, "")
	if lineText != "test.eq(foo, 42)" {
		t.Fatalf("unexpected directive line %q", lineText)
	}
}

func TestDirectiveBlocksIgnoredWhenOff(t *testing.T) {
	input := `
/// test:
/// test.eq(foo, 42)
fn foo() -> int { return 42; }
`
	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatal("file not found")
	}
	if len(file.Directives) != 0 {
		t.Fatalf("expected no directive blocks when mode=off, got %d", len(file.Directives))
	}
}

func TestParseDropStatement(t *testing.T) {
	input := `
fn foo() {
    let x = 1;
    @drop x;
}
`
	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if file == nil || len(file.Items) == 0 {
		t.Fatalf("expected function item in file")
	}
	item := builder.Items.Get(file.Items[0])
	if item == nil || item.Kind != ast.ItemFn {
		t.Fatalf("expected function item")
	}
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok || fnItem == nil {
		t.Fatalf("expected fn payload")
	}
	block := builder.Stmts.Block(fnItem.Body)
	if block == nil || len(block.Stmts) != 2 {
		t.Fatalf("expected two statements in block, got %d", len(block.Stmts))
	}
	dropStmt := builder.Stmts.Get(block.Stmts[1])
	if dropStmt == nil || dropStmt.Kind != ast.StmtDrop {
		t.Fatalf("expected drop statement, got %v", dropStmt)
	}
	if drop := builder.Stmts.Drop(block.Stmts[1]); drop == nil || !drop.Expr.IsValid() {
		t.Fatalf("expected drop payload")
	}
}

func TestDirectiveIgnoresNonDirectiveDocComment(t *testing.T) {
	input := `
/// Note: returns 42
fn foo() -> int { return 42; }
`
	opts := Options{
		MaxErrors:     100,
		DirectiveMode: DirectiveModeCollect,
	}
	builder, fileID, bag := parseSourceWithOptions(t, input, opts)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatal("file not found")
	}
	if len(file.Directives) != 0 {
		t.Fatalf("expected no directive blocks for regular doc comments, got %d", len(file.Directives))
	}
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
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
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

func TestSignalStatement(t *testing.T) {
	input := `
		fn main() {
			signal total := parallel reduce xs with init, (acc, price) => combine(acc, price);
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	if file == nil || len(file.Items) != 1 {
		t.Fatalf("expected single item file, got %+v", file)
	}

	itemID := file.Items[0]
	fnItem, ok := builder.Items.Fn(itemID)
	if !ok {
		t.Fatalf("expected fn item, got %v", builder.Items.Get(itemID).Kind)
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil || len(block.Stmts) != 1 {
		t.Fatalf("expected single statement in body, got %+v", block)
	}

	stmtID := block.Stmts[0]
	stmt := builder.Stmts.Get(stmtID)
	if stmt == nil {
		t.Fatal("statement missing")
	}
	if stmt.Kind != ast.StmtSignal {
		t.Fatalf("expected signal statement, got %v", stmt.Kind)
	}

	signalStmt := builder.Stmts.Signal(stmtID)
	if signalStmt == nil {
		t.Fatal("signal payload missing")
	}
	if lookupNameOr(builder, signalStmt.Name, "") != "total" {
		t.Fatalf("expected signal target 'total', got %q", lookupNameOr(builder, signalStmt.Name, ""))
	}
	valueExpr := builder.Exprs.Get(signalStmt.Value)
	if valueExpr == nil || valueExpr.Kind != ast.ExprParallel {
		t.Fatalf("expected parallel expression, got %v", valueExpr)
	}
	reduceData, ok := builder.Exprs.Parallel(signalStmt.Value)
	if !ok {
		t.Fatal("parallel payload missing")
	}
	if reduceData.Kind != ast.ExprParallelReduce {
		t.Fatalf("expected reduce kind, got %v", reduceData.Kind)
	}
	if !reduceData.Init.IsValid() {
		t.Fatal("reduce initializer missing")
	}
	if initExpr := builder.Exprs.Get(reduceData.Init); initExpr == nil || initExpr.Kind != ast.ExprIdent {
		t.Fatalf("expected identifier init, got %v", initExpr)
	}
	if len(reduceData.Args) != 2 {
		t.Fatalf("expected two args, got %d", len(reduceData.Args))
	}
}

func TestSignalStatementDiagnostics(t *testing.T) {
	input := `
		fn main() {
			signal total = 1;
		}
	`

	_, _, bag := parseSource(t, input)
	if !bag.HasErrors() {
		t.Fatal("expected diagnostics for malformed signal statement")
	}

	found := false
	for _, d := range bag.Items() {
		if d.Code == diag.SynUnexpectedToken && d.Message == "expected ':=' after signal target" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing signal diagnostic, got %s", diagnosticsSummary(bag))
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

// Additional comprehensive tests for statement parsing

func TestParseReturnStatement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		hasValue bool
	}{
		{
			name:     "return with value",
			input:    "fn foo() { return 42; }",
			hasValue: true,
		},
		{
			name:     "return without value",
			input:    "fn foo() { return; }",
			hasValue: false,
		},
		{
			name:     "return expression",
			input:    "fn foo() { return a + b; }",
			hasValue: true,
		},
		{
			name:     "return function call",
			input:    "fn foo() { return bar(); }",
			hasValue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)
			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			block := builder.Stmts.Block(fnItem.Body)
			if block == nil || len(block.Stmts) == 0 {
				t.Fatal("expected block with statements")
			}

			stmt := builder.Stmts.Get(block.Stmts[0])
			if stmt.Kind != ast.StmtReturn {
				t.Fatalf("expected return statement, got %v", stmt.Kind)
			}
		})
	}
}

func TestParseExpressionStatement(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"function_call", "fn foo() { bar(); }"},
		{"method_call", "fn foo() { obj.method(); }"},
		{"assignment", "fn foo() { x = 10; }"},
		{"compound_assignment", "fn foo() { x += 5; }"},
		{"field_access", "fn foo() { obj.member; }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)
			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			block := builder.Stmts.Block(fnItem.Body)
			if block == nil || len(block.Stmts) == 0 {
				t.Fatal("expected block with statements")
			}

			stmt := builder.Stmts.Get(block.Stmts[0])
			if stmt.Kind != ast.StmtExpr {
				t.Fatalf("expected expression statement, got %v", stmt.Kind)
			}
		})
	}
}

// can't parse nested block yet
// func TestParseNestedBlocks(t *testing.T) {
// 	input := `
// 		fn foo() {
// 			{
// 				let x = 1;
// 				{
// 					let y = 2;
// 				}
// 			}
// 		}
// 	`

// 	builder, fileID, bag := parseSource(t, input)
// 	if bag.HasErrors() {
// 		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
// 	}

// 	file := builder.Files.Get(fileID)
// 	fnItem, ok := builder.Items.Fn(file.Items[0])
// 	if !ok {
// 		t.Fatal("expected fn item")
// 	}

// 	if !fnItem.Body.IsValid() {
// 		t.Fatal("expected function body")
// 	}

// 	outerBlock := builder.Stmts.Block(fnItem.Body)
// 	if outerBlock == nil {
// 		t.Fatal("expected outer block")
// 	}
// }

func TestParseLetStatementInFunction(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "let with type and value",
			input: "fn foo() { let x: int = 42; }",
		},
		{
			name:  "let with value only",
			input: "fn foo() { let x = 42; }",
		},
		{
			name:  "let with type only",
			input: "fn foo() { let x: int; }",
		},
		{
			name:  "mutable let",
			input: "fn foo() { let mut x = 42; }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)
			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			block := builder.Stmts.Block(fnItem.Body)
			if block == nil || len(block.Stmts) == 0 {
				t.Fatal("expected block with statements")
			}

			stmt := builder.Stmts.Get(block.Stmts[0])
			if stmt.Kind != ast.StmtLet {
				t.Fatalf("expected let statement, got %v", stmt.Kind)
			}
		})
	}
}

func TestParseMultipleStatementsInBlock(t *testing.T) {
	input := `
		fn foo() {
			let x = 1;
			let y = 2;
			bar();
			return x + y;
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil {
		t.Fatal("expected block")
	}

	expectedCount := 4 // 2 let statements, 1 expression statement, 1 return
	if len(block.Stmts) != expectedCount {
		t.Errorf("expected %d statements, got %d", expectedCount, len(block.Stmts))
	}
}

func TestParseEmptyBlock(t *testing.T) {
	input := "fn foo() {}"

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil {
		t.Fatal("expected block")
	}

	if len(block.Stmts) != 0 {
		t.Errorf("expected empty block, got %d statements", len(block.Stmts))
	}
}

func TestParseStatementErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "missing semicolon after let",
			input: "fn foo() { let x = 1 }",
		},
		{
			name:  "missing semicolon after expression",
			input: "fn foo() { bar() }",
		},
		{
			name:  "missing semicolon after return",
			input: "fn foo() { return 1 }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, bag := parseSource(t, tt.input)

			if !bag.HasErrors() {
				t.Error("expected errors, but got none")
			}
		})
	}
}

func TestParseBlockWithWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "extra newlines",
			input: `fn foo() {

				let x = 1;

				return x;

			}`,
		},
		{
			name:  "tabs and spaces",
			input: "fn foo() {\n\t\tlet x = 1;\n\t\treturn x;\n\t}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)
			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			block := builder.Stmts.Block(fnItem.Body)
			if block == nil {
				t.Fatal("expected block")
			}
		})
	}
}

func TestParseComplexStatements(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "let with complex expression",
			input: `fn foo() {
				let x = (a + b) * c - d / e;
			}`,
		},
		{
			name: "return with complex expression",
			input: `fn foo() {
				return (a && b) || (c && d);
			}`,
		},
		{
			name: "chained method calls",
			input: `fn foo() {
				obj.method1().method2().method3();
			}`,
		},
		{
			name: "nested field access",
			input: `fn foo() {
				let x = obj.field1.field2.field3;
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)
			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
			}

			file := builder.Files.Get(fileID)
			if len(file.Items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(file.Items))
			}
		})
	}
}

func TestParseIfStatement(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "with_parentheses",
			input: `fn foo() { if (a > 0) { return; } else { return; } }`,
		},
		{
			name:  "without_parentheses",
			input: `fn foo() { if a > 0 { return; } else { return; } }`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)
			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			block := builder.Stmts.Block(fnItem.Body)
			if block == nil || len(block.Stmts) != 1 {
				t.Fatalf("expected single statement block, got %d", len(block.Stmts))
			}

			stmt := builder.Stmts.Get(block.Stmts[0])
			if stmt.Kind != ast.StmtIf {
				t.Fatalf("expected StmtIf, got %v", stmt.Kind)
			}

			ifStmt := builder.Stmts.If(block.Stmts[0])
			if ifStmt == nil {
				t.Fatal("if payload missing")
			}
			condExpr := builder.Exprs.Get(ifStmt.Cond)
			if condExpr == nil || condExpr.Kind != ast.ExprBinary {
				t.Fatalf("expected binary condition, got %v", condExpr)
			}
			thenBlock := builder.Stmts.Block(ifStmt.Then)
			if thenBlock == nil || len(thenBlock.Stmts) != 1 {
				t.Fatalf("expected then-block with single stmt, got %+v", thenBlock)
			}
			elseBlock := builder.Stmts.Block(ifStmt.Else)
			if elseBlock == nil || len(elseBlock.Stmts) != 1 {
				t.Fatalf("expected else-block with single stmt, got %+v", elseBlock)
			}
		})
	}
}

func TestParseWhileStatement(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "with_parentheses",
			input: `fn foo() { while (ready) { return; } }`,
		},
		{
			name:  "without_parentheses",
			input: `fn foo() { while ready { return; } }`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, fileID, bag := parseSource(t, tt.input)
			if bag.HasErrors() {
				t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
			}

			file := builder.Files.Get(fileID)
			fnItem, ok := builder.Items.Fn(file.Items[0])
			if !ok {
				t.Fatal("expected fn item")
			}

			block := builder.Stmts.Block(fnItem.Body)
			if block == nil || len(block.Stmts) != 1 {
				t.Fatalf("expected single statement block, got %d", len(block.Stmts))
			}

			stmt := builder.Stmts.Get(block.Stmts[0])
			if stmt.Kind != ast.StmtWhile {
				t.Fatalf("expected StmtWhile, got %v", stmt.Kind)
			}

			whileStmt := builder.Stmts.While(block.Stmts[0])
			if whileStmt == nil {
				t.Fatal("while payload missing")
			}
			if builder.Exprs.Get(whileStmt.Cond) == nil {
				t.Fatal("while condition missing")
			}
			body := builder.Stmts.Block(whileStmt.Body)
			if body == nil || len(body.Stmts) != 1 {
				t.Fatalf("expected while-body with single stmt, got %+v", body)
			}
		})
	}
}

func TestParseForClassicStatement(t *testing.T) {
	input := `
		fn foo() {
			for (let i: int = 0; i < 10; i = i + 1) {
				return;
			}
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil || len(block.Stmts) != 1 {
		t.Fatalf("expected single statement block, got %d", len(block.Stmts))
	}

	stmt := builder.Stmts.Get(block.Stmts[0])
	if stmt.Kind != ast.StmtForClassic {
		t.Fatalf("expected StmtForClassic, got %v", stmt.Kind)
	}

	forStmt := builder.Stmts.ForClassic(block.Stmts[0])
	if forStmt == nil {
		t.Fatal("for-classic payload missing")
	}
	init := builder.Stmts.Get(forStmt.Init)
	if init == nil || init.Kind != ast.StmtLet {
		t.Fatalf("expected let initializer, got %v", init)
	}
	if builder.Exprs.Get(forStmt.Cond) == nil {
		t.Fatal("for condition missing")
	}
	if builder.Exprs.Get(forStmt.Post) == nil {
		t.Fatal("for post expression missing")
	}
	body := builder.Stmts.Block(forStmt.Body)
	if body == nil || len(body.Stmts) != 1 {
		t.Fatalf("expected for-body with single stmt, got %+v", body)
	}
}

func TestParseForInStatement(t *testing.T) {
	input := `
		fn foo() {
			for item: int in items {
				return;
			}
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil || len(block.Stmts) != 1 {
		t.Fatalf("expected single statement block, got %d", len(block.Stmts))
	}

	stmt := builder.Stmts.Get(block.Stmts[0])
	if stmt.Kind != ast.StmtForIn {
		t.Fatalf("expected StmtForIn, got %v", stmt.Kind)
	}

	forIn := builder.Stmts.ForIn(block.Stmts[0])
	if forIn == nil {
		t.Fatal("for-in payload missing")
	}
	name := lookupNameOr(builder, forIn.Pattern, "")
	if name != "item" {
		t.Fatalf("expected pattern name 'item', got %q", name)
	}
	if !forIn.Type.IsValid() {
		t.Fatal("expected explicit type annotation")
	}
	if builder.Exprs.Get(forIn.Iterable) == nil {
		t.Fatal("iterable expression missing")
	}
	body := builder.Stmts.Block(forIn.Body)
	if body == nil || len(body.Stmts) != 1 {
		t.Fatalf("expected for-in body with single stmt, got %+v", body)
	}
}

func TestParseBreakContinueStatements(t *testing.T) {
	input := `
		fn foo() {
			break;
			continue;
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil || len(block.Stmts) != 2 {
		t.Fatalf("expected two statements, got %d", len(block.Stmts))
	}

	first := builder.Stmts.Get(block.Stmts[0])
	second := builder.Stmts.Get(block.Stmts[1])
	if first.Kind != ast.StmtBreak {
		t.Fatalf("expected first statement Break, got %v", first.Kind)
	}
	if second.Kind != ast.StmtContinue {
		t.Fatalf("expected second statement Continue, got %v", second.Kind)
	}
}

func TestParseCompareExpressionStatement(t *testing.T) {
	input := `
		fn foo() {
			compare value {
				target if ready => 1;
				finally => 2;
			};
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil || len(block.Stmts) != 1 {
		t.Fatalf("expected single statement, got %d", len(block.Stmts))
	}

	stmt := builder.Stmts.Get(block.Stmts[0])
	if stmt.Kind != ast.StmtExpr {
		t.Fatalf("expected expression statement, got %v", stmt.Kind)
	}

	exprStmt := builder.Stmts.Expr(block.Stmts[0])
	if exprStmt == nil {
		t.Fatal("expression payload missing")
	}
	expr := builder.Exprs.Get(exprStmt.Expr)
	if expr == nil || expr.Kind != ast.ExprCompare {
		t.Fatalf("expected compare expression, got %v", expr)
	}
	data, ok := builder.Exprs.Compare(exprStmt.Expr)
	if !ok {
		t.Fatal("compare payload missing")
	}
	if len(data.Arms) != 2 {
		t.Fatalf("expected 2 compare arms, got %d", len(data.Arms))
	}
	firstArm := data.Arms[0]
	if firstArm.IsFinally {
		t.Fatal("first arm should not be finally")
	}
	if firstArm.Pattern.IsValid() {
		if ident, ok := builder.Exprs.Ident(firstArm.Pattern); ok {
			name := lookupNameOr(builder, ident.Name, "")
			if name != "target" {
				t.Fatalf("expected pattern 'target', got %q", name)
			}
		} else {
			t.Fatalf("expected pattern ident, got %+v", builder.Exprs.Get(firstArm.Pattern))
		}
	}
	if !firstArm.Guard.IsValid() {
		t.Fatal("expected guard expression on first arm")
	}
	lastArm := data.Arms[1]
	if !lastArm.IsFinally {
		t.Fatal("expected second arm to be finally")
	}
	if !lastArm.Result.IsValid() {
		t.Fatal("expected result expression in finally arm")
	}
}

func TestParseLetWithCompareExpression(t *testing.T) {
	input := `
		fn foo() {
			let a = compare something {
				target if ready => 1;
				finally => 2;
			};
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil || len(block.Stmts) != 1 {
		t.Fatalf("expected single statement block, got %d", len(block.Stmts))
	}

	stmt := builder.Stmts.Get(block.Stmts[0])
	if stmt.Kind != ast.StmtLet {
		t.Fatalf("expected let statement, got %v", stmt.Kind)
	}

	letData := builder.Stmts.Let(block.Stmts[0])
	if letData == nil {
		t.Fatal("missing let payload")
	}
	expr := builder.Exprs.Get(letData.Value)
	if expr == nil || expr.Kind != ast.ExprCompare {
		t.Fatalf("expected compare expression, got %v", expr)
	}
}

func TestParseIfElseChain(t *testing.T) {
	input := `
		fn classify(x: int) -> int {
			if (x < 0) {
				return -1;
			} else if (x == 0) {
				return 0;
			} else {
				return 1;
			}
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil || len(block.Stmts) != 1 {
		t.Fatalf("expected single statement block, got %d", len(block.Stmts))
	}

	rootIf := builder.Stmts.If(block.Stmts[0])
	if rootIf == nil {
		t.Fatal("expected if payload")
	}

	elseStmt := rootIf.Else
	if !elseStmt.IsValid() {
		t.Fatal("expected else branch")
	}

	next := builder.Stmts.Get(elseStmt)
	if next.Kind != ast.StmtIf {
		t.Fatalf("expected chained if, got %v", next.Kind)
	}
}

func TestParseForInWithoutTypeAnnotation(t *testing.T) {
	input := `
		fn iterate(items: int[]) {
			for value in items {
				return;
			}
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected errors: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatal("expected fn item")
	}

	block := builder.Stmts.Block(fnItem.Body)
	if block == nil || len(block.Stmts) != 1 {
		t.Fatalf("expected single statement block, got %d", len(block.Stmts))
	}

	stmt := builder.Stmts.Get(block.Stmts[0])
	if stmt.Kind != ast.StmtForIn {
		t.Fatalf("expected for-in statement, got %v", stmt.Kind)
	}

	forIn := builder.Stmts.ForIn(block.Stmts[0])
	if forIn == nil {
		t.Fatal("for-in payload missing")
	}
	if forIn.Type.IsValid() {
		t.Fatal("expected no explicit type annotation")
	}
}

func TestBlockDisallowsPubAsyncAndAttributes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantDiag diag.Code
	}{
		{
			name: "pub modifier inside block",
			input: `
				fn foo() {
					pub let x = 1;
				}
			`,
			wantDiag: diag.SynModifierNotAllowed,
		},
		{
			name: "attribute inside block",
			input: `
				fn foo() {
					@pure let x = 1;
				}
			`,
			wantDiag: diag.SynAttributeNotAllowed,
		},
		{
			name: "type declaration inside block",
			input: `
				fn foo() {
					type A = int;
				}
			`,
			wantDiag: diag.SynTypeNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, bag := parseSource(t, tt.input)
			if !bag.HasErrors() {
				t.Fatalf("expected diagnostics, got none")
			}
			found := false
			for _, item := range bag.Items() {
				if item.Code == tt.wantDiag {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected diagnostic %s, got %+v", tt.wantDiag, bag.Items())
			}
		})
	}
}

func TestBlockTypeDeclResyncKeepsFollowingStatements(t *testing.T) {
	input := `
		fn foo() {
			type A = {
				value: int,
			}
			let x = 1;
			return;
		}
	`

	builder, fileID, bag := parseSource(t, input)
	if !bag.HasErrors() {
		t.Fatalf("expected diagnostics, got none")
	}

	found := false
	for _, item := range bag.Items() {
		if item.Code == diag.SynTypeNotAllowed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected SynTypeNotAllowed diagnostic, got %+v", bag.Items())
	}

	file := builder.Files.Get(fileID)
	if file == nil || len(file.Items) != 1 {
		t.Fatalf("expected single function item, got %+v", file)
	}

	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok {
		t.Fatalf("expected fn item, got %v", builder.Items.Get(file.Items[0]).Kind)
	}
	block := builder.Stmts.Block(fnItem.Body)
	if block == nil {
		t.Fatal("function body missing")
	}
	if len(block.Stmts) != 2 {
		t.Fatalf("expected two statements in block, got %d", len(block.Stmts))
	}

	first := builder.Stmts.Get(block.Stmts[0])
	if first.Kind != ast.StmtLet {
		t.Fatalf("expected first statement after type to be let, got %v", first.Kind)
	}
	second := builder.Stmts.Get(block.Stmts[1])
	if second.Kind != ast.StmtReturn {
		t.Fatalf("expected second statement to be return, got %v", second.Kind)
	}
}
