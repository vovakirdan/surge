package parser

// An expression that cannot begin (`f(+)`, `f(])`, the prefix `...arr`) used
// to fail without a diagnostic wherever the enclosing construct recovered on
// its own: a statement loop, a call argument list, a struct or map literal, a
// field default. The statement was then missing from the AST of a file that
// `surge diag` accepted. Every row below must report exactly one diagnostic,
// at the token that could not start an expression.

import (
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
)

func TestMissingExpressionIsReportedOnce(t *testing.T) {
	cases := []struct {
		name string
		src  string
		at   string // the diagnostic starts at the first byte of this unique substring
	}{
		{"stmt_call_unary_plus", "fn main() { f(+); }", ");"},
		{"stmt_call_close_bracket", "fn main() { f(]); }", "]);"},
		{"stmt_call_prefix_spread", "fn main() { print(sumi(...arr) to string); }", "...arr"},
		{"nested_call", "fn main() { g(f(+)); }", "));"},
		{"empty_arg_before_comma", "fn main() { f(,); }", ",);"},
		{"named_arg_value", "fn main() { f(x: +); }", ");"},
		{"paren_in_arg", "fn main() { f((+)); }", "));"},
		{"method_call_arg", "fn main() { a.m(-); }", ");"},
		{"bare_stmt", "fn main() { f(1); +; f(2); }", "; f(2)"},
		{"drop_stmt", "fn main() { @drop +; }", "; }"},
		{"return_stmt", "fn h() -> int { return +; }", "; }"},
		{"stmt_in_block_expr", "fn main() { let r = { let z = 1; f(+); return z; }; }", "); return"},
		{"struct_literal_field", "fn main() { let p: P = P { x = +, y = 2 }; }", ", y"},
		{"map_literal_key", "fn main() { let m: Map<int, int> = { 1 => 2, + => 3 }; }", "=> 3"},
		{"type_field_default", "type Q = { x: int = +, y: int };", ", y"},
		{"top_level_const", "const X: int = +;", ";"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Count(tc.src, tc.at) != 1 {
				t.Fatalf("marker %q must occur once in %q", tc.at, tc.src)
			}
			_, bag, _ := parseProgram(t, tc.src)
			items := bag.Items()
			if len(items) != 1 {
				t.Fatalf("want exactly one diagnostic, got %d: %s", len(items), diagnosticsSummary(bag))
			}
			d := items[0]
			if d.Code != diag.SynExpectExpression || d.Message != "expected expression" {
				t.Fatalf("want SYN2203 'expected expression', got %s", diagnosticsSummary(bag))
			}
			if want := uint32(strings.Index(tc.src, tc.at)); d.Primary.Start != want {
				t.Fatalf("diagnostic at byte %d, want %d (%q)", d.Primary.Start, want, tc.at)
			}
		})
	}
}

// Valid calls keep every statement and report nothing; a stray ';' holds no
// code and stays accepted; an Invalid token is reported by the lexer alone.
func TestMissingExpressionControls(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantStmts int
	}{
		{"plain_calls", "fn main() { f(1); g(f(2), 3); }", 2},
		{"postfix_spread", "fn main() { let arr: int[] = [1]; print(sumi(arr...) to string); }", 2},
		{"trailing_comma", "fn main() { f(1,); f(1, 2,); }", 2},
		{"named_arg", "fn main() { f(x: 1); }", 1},
		{"stray_semicolon", "fn main() { f(1);; ; }", 1},
		{"stray_semicolon_in_block_expr", "fn main() { let r = { let z = 1;; return z; }; }", 1},
		// The end bound of a range literal is optional: no expression is missing.
		{"open_range_literal", "fn main() { let c = [1..]; let s = \"abc\"; print(s[[-3..]]); }", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder, bag, fileID := parseProgram(t, tc.src)
			if bag.Len() != 0 {
				t.Fatalf("want no diagnostics, got %s", diagnosticsSummary(bag))
			}
			if got := mainStmtCount(t, builder, fileID); got != tc.wantStmts {
				t.Fatalf("main has %d statements, want %d", got, tc.wantStmts)
			}
		})
	}

	t.Run("invalid_token_reported_by_lexer_only", func(t *testing.T) {
		_, bag, _ := parseProgram(t, "fn main() { f($); }")
		if bag.Len() != 1 || bagHasCode(bag, diag.SynExpectExpression) {
			t.Fatalf("want the lexer's diagnostic alone, got %s", diagnosticsSummary(bag))
		}
	})
}

func mainStmtCount(t *testing.T, builder *ast.Builder, fileID ast.FileID) int {
	t.Helper()
	file := builder.Files.Get(fileID)
	if file == nil || len(file.Items) != 1 {
		t.Fatalf("want one item")
	}
	fnItem, ok := builder.Items.Fn(file.Items[0])
	if !ok || fnItem == nil {
		t.Fatalf("want a fn item")
	}
	block := builder.Stmts.Block(fnItem.Body)
	if block == nil {
		t.Fatalf("want a body block")
	}
	return len(block.Stmts)
}

// Recovery must not turn one failure into two: the rest of a broken statement
// and a token that already carries an error stay quiet, while the next broken
// statement is still reported.
func TestMissingExpressionAfterRecovery(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // SYN code and the unique substring each diagnostic starts at
	}{
		{"array_len_then_rest_of_let", "fn main() { let x: int[+] = 1; }", []string{"SYN2203@] ="}},
		{"stray_paren_then_next_stmt", "fn main() { f(+)); f(]); }", []string{"SYN2203@)); f", "SYN2203@]);"}},
		{"missing_semicolon_at_stray_paren", "fn main() { f(1)); f(]); }", []string{"SYN2012@); f(]", "SYN2203@]);"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, bag, _ := parseProgram(t, tc.src)
			items := bag.Items()
			if len(items) != len(tc.want) {
				t.Fatalf("want %d diagnostics, got %s", len(tc.want), diagnosticsSummary(bag))
			}
			for i, w := range tc.want {
				code, at, _ := strings.Cut(w, "@")
				if strings.Count(tc.src, at) != 1 {
					t.Fatalf("marker %q must occur once", at)
				}
				if items[i].Code.ID() != code || items[i].Primary.Start != uint32(strings.Index(tc.src, at)) {
					t.Fatalf("diagnostic %d: want %s, got %s at byte %d", i, w, items[i].Code.ID(), items[i].Primary.Start)
				}
			}
		})
	}
}
