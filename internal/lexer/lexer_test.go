package lexer_test

import (
	"fmt"
	"strings"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
	"surge/internal/token"
	"testing"
)

// testReporter собирает все диагностики, полученные от лексера
type testReporter struct {
	diagnostics []diag.Diagnostic
}

// Report реализует интерфейс diag.Reporter
func (r *testReporter) Report(code diag.Code, sev diag.Severity, primary source.Span, msg string, notes []diag.Note, fixes []*diag.Fix) {
	r.diagnostics = append(r.diagnostics, diag.Diagnostic{
		Severity: sev,
		Code:     code,
		Message:  msg,
		Primary:  primary,
		Notes:    notes,
		Fixes:    fixes,
	})
}

// HasErrors возвращает true, если были зарегистрированы ошибки
func (r *testReporter) HasErrors() bool {
	for _, d := range r.diagnostics {
		if d.Severity == diag.SevError {
			return true
		}
	}
	return false
}

// ErrorCount возвращает количество ошибок
func (r *testReporter) ErrorCount() int {
	count := 0
	for _, d := range r.diagnostics {
		if d.Severity == diag.SevError {
			count++
		}
	}
	return count
}

// ErrorMessages возвращает список сообщений об ошибках (для обратной совместимости с тестами)
func (r *testReporter) ErrorMessages() []string {
	messages := make([]string, 0, len(r.diagnostics))
	for _, d := range r.diagnostics {
		messages = append(messages, fmt.Sprintf("[%s] %s: %s", d.Code.ID(), d.Severity, d.Message))
	}
	return messages
}

// makeTestLexer создаёт лексер для тестовой строки
func makeTestLexer(input string) (*lexer.Lexer, *testReporter) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(input))
	file := fs.Get(fileID)

	reporter := &testReporter{diagnostics: make([]diag.Diagnostic, 0)}
	opts := lexer.Options{Reporter: reporter}
	lx := lexer.New(file, opts)

	return lx, reporter
}

// collectAllTokens собирает все токены до EOF
func collectAllTokens(lx *lexer.Lexer) []token.Token {
	tokens := make([]token.Token, 0)
	for {
		tok := lx.Next()
		tokens = append(tokens, tok)
		if tok.Kind == token.EOF {
			break
		}
	}
	return tokens
}

// expectTokens проверяет последовательность токенов
func expectTokens(t *testing.T, input string, expected []token.Kind) {
	t.Helper()
	lx, reporter := makeTestLexer(input)
	tokens := collectAllTokens(lx)

	// убираем EOF из сравнения
	if len(tokens) > 0 && tokens[len(tokens)-1].Kind == token.EOF {
		tokens = tokens[:len(tokens)-1]
	}

	if len(tokens) != len(expected) {
		t.Fatalf("Expected %d tokens, got %d\nInput: %q\nTokens: %v\nErrors: %v",
			len(expected), len(tokens), input, tokensToString(tokens), reporter.ErrorMessages())
	}

	for i, tok := range tokens {
		if tok.Kind != expected[i] {
			t.Errorf("Token %d: expected %v, got %v (text: %q)",
				i, expected[i], tok.Kind, tok.Text)
		}
	}
}

// expectSingleToken проверяет, что вход создаёт ровно один токен
func expectSingleToken(t *testing.T, input string, expectedKind token.Kind, expectedText string) {
	t.Helper()
	lx, _ := makeTestLexer(input)
	tok := lx.Next()

	if tok.Kind != expectedKind {
		t.Errorf("Expected kind %v, got %v", expectedKind, tok.Kind)
	}
	if tok.Text != expectedText {
		t.Errorf("Expected text %q, got %q", expectedText, tok.Text)
	}
}

func tokensToString(tokens []token.Token) string {
	parts := make([]string, len(tokens))
	for i, tok := range tokens {
		parts[i] = fmt.Sprintf("%v(%q)", tok.Kind, tok.Text)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// ====== Тесты для scan_ident.go ======

func TestIdentifiers_ASCII(t *testing.T) {
	tests := []struct {
		input string
		kind  token.Kind
		text  string
	}{
		{"foo", token.Ident, "foo"},
		{"_bar", token.Ident, "_bar"},
		{"__test", token.Ident, "__test"},
		{"x123", token.Ident, "x123"},
		{"camelCase", token.Ident, "camelCase"},
		{"UPPER", token.Ident, "UPPER"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expectSingleToken(t, tt.input, tt.kind, tt.text)
		})
	}
}

func TestUnderscore_Single(t *testing.T) {
	// одиночный underscore — токен Underscore
	expectSingleToken(t, "_", token.Underscore, "_")
}

func TestKeywords_Lowercase(t *testing.T) {
	// Ключевые слова регистрозависимые — только строчные распознаются как ключевые слова
	tests := []struct {
		input string
		kind  token.Kind
	}{
		{"fn", token.KwFn},
		{"let", token.KwLet},
		{"const", token.KwConst},
		{"mut", token.KwMut},
		{"own", token.KwOwn},
		{"if", token.KwIf},
		{"else", token.KwElse},
		{"while", token.KwWhile},
		{"for", token.KwFor},
		{"in", token.KwIn},
		{"break", token.KwBreak},
		{"continue", token.KwContinue},
		{"return", token.KwReturn},
		{"import", token.KwImport},
		{"as", token.KwAs},
		{"type", token.KwType},
		{"tag", token.KwTag},
		{"extern", token.KwExtern},
		{"pub", token.KwPub},
		{"async", token.KwAsync},
		{"true", token.KwTrue},
		{"false", token.KwFalse},
		{"compare", token.KwCompare},
		{"finally", token.KwFinally},
		{"channel", token.KwChannel},
		{"spawn", token.KwSpawn},
		{"signal", token.KwSignal},
		{"parallel", token.KwParallel},
		{"macro", token.KwMacro},
		{"pragma", token.KwPragma},
		{"to", token.KwTo},
		{"heir", token.KwHeir},
		{"is", token.KwIs},
		{"nothing", token.NothingLit},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lx, _ := makeTestLexer(tt.input)
			tok := lx.Next()
			if tok.Kind != tt.kind {
				t.Errorf("Expected %v, got %v", tt.kind, tok.Kind)
			}
		})
	}
}

func TestTaskIsIdentifier(t *testing.T) {
	lx, _ := makeTestLexer("task")
	tok := lx.Next()
	if tok.Kind != token.Ident {
		t.Fatalf("expected Ident, got %v", tok.Kind)
	}
}

func TestKeywords_CapitalizedAreIdents(t *testing.T) {
	// Капитализированные версии ключевых слов — это обычные идентификаторы
	tests := []string{
		"Fn", "FN",
		"Let", "LET",
		"Const", "CONST",
		"Mut", "MUT",
		"Own", "OWN",
		"If", "IF",
		"Else", "ELSE",
		"While", "WHILE",
		"For", "FOR",
		"In", "IN",
		"Break", "BREAK",
		"Continue", "CONTINUE",
		"Return", "RETURN",
		"Import", "IMPORT",
		"As", "AS",
		"Type", "TYPE",
		"Newtype", "NEWTYPE",
		"Alias", "ALIAS",
		"Literal", "LITERAL",
		"Tag", "TAG",
		"Extern", "EXTERN",
		"Pub", "PUB",
		"Async", "ASYNC",
		"Await", "AWAIT",
		"True", "TRUE",
		"False", "FALSE",
		"Compare", "COMPARE",
		"Finally", "FINALLY",
		"Channel", "CHANNEL",
		"Task", "TASK",
		"Spawn", "SPAWN",
		"Signal", "SIGNAL",
		"Parallel", "PARALLEL",
		"Macro", "MACRO",
		"Pragma", "PRAGMA",
		"To", "TO",
		"Heir", "HEIR",
		"Is", "IS",
		"Nothing", "NOTHING",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			lx, _ := makeTestLexer(input)
			tok := lx.Next()
			if tok.Kind != token.Ident {
				t.Errorf("Expected Ident for %q, got %v", input, tok.Kind)
			}
			if tok.Text != input {
				t.Errorf("Expected text %q, got %q", input, tok.Text)
			}
		})
	}
}

func TestIdentifiers_Unicode(t *testing.T) {
	tests := []string{
		"идентификатор",
		"переменная",
		"δ",
		"λx",
		"函数",
		"変数",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			lx, _ := makeTestLexer(input)
			tok := lx.Next()
			if tok.Kind != token.Ident {
				t.Errorf("Expected Ident, got %v for %q", tok.Kind, input)
			}
			if tok.Text != input {
				t.Errorf("Expected text %q, got %q", input, tok.Text)
			}
		})
	}
}

// ====== Тесты для scan_number.go ======

func TestNumbers_Decimal(t *testing.T) {
	tests := []struct {
		input string
		kind  token.Kind
	}{
		{"0", token.IntLit},
		{"123", token.IntLit},
		{"456789", token.IntLit},
		{"1_000", token.IntLit},
		{"1_000_000", token.IntLit},
		{"999_999_999", token.IntLit},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expectSingleToken(t, tt.input, tt.kind, tt.input)
		})
	}
}

func TestNumbers_Binary(t *testing.T) {
	tests := []string{
		"0b0",
		"0b1",
		"0b1010",
		"0b1111_0000",
		"0B1010",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			expectSingleToken(t, input, token.IntLit, input)
		})
	}
}

func TestNumbers_Octal(t *testing.T) {
	tests := []string{
		"0o0",
		"0o7",
		"0o777",
		"0o12_34",
		"0O777",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			expectSingleToken(t, input, token.IntLit, input)
		})
	}
}

func TestNumbers_Hexadecimal(t *testing.T) {
	tests := []string{
		"0x0",
		"0xF",
		"0xDEADBEEF",
		"0xff",
		"0xAB_CD",
		"0X123",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			expectSingleToken(t, input, token.IntLit, input)
		})
	}
}

func TestNumbers_Float(t *testing.T) {
	tests := []string{
		"1.0",
		"3.14",
		"0.5",
		"123.456",
		"1_000.5",
		"0.123_456",
		"1.", // допустимо
		".5", // начинается с точки
		".123",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			expectSingleToken(t, input, token.FloatLit, input)
		})
	}
}

func TestNumbers_FloatWithExponent(t *testing.T) {
	tests := []string{
		"1e10",
		"1E10",
		"1e+10",
		"1e-10",
		"1.5e10",
		"3.14e-2",
		"123.456e+789",
		"1_000e3",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			expectSingleToken(t, input, token.FloatLit, input)
		})
	}
}

func TestNumbers_InvalidExponent(t *testing.T) {
	tests := []string{
		"1e",
		"1e+",
		"1e-",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			lx, reporter := makeTestLexer(input)
			tok := lx.Next()

			// должны получить Invalid или ошибку
			if tok.Kind != token.Invalid && !reporter.HasErrors() {
				t.Errorf("Expected Invalid token or error for %q, got %v", input, tok.Kind)
			}
		})
	}
}

func TestNumbers_DotFollowedByLetter(t *testing.T) {
	// ".e10" — это Dot + Ident, а не число
	expectTokens(t, ".e10", []token.Kind{
		token.Dot,
		token.Ident,
	})
}

func TestNumbers_DotDotNotPartOfNumber(t *testing.T) {
	// Проверяем, что ".." и "..=" не съедаются как часть числа
	expectTokens(t, "1..10", []token.Kind{
		token.IntLit,
		token.DotDot,
		token.IntLit,
	})

	expectTokens(t, "0..=5", []token.Kind{
		token.IntLit,
		token.DotDotEq,
		token.IntLit,
	})
}

// ====== Тесты для scan_string.go ======

func TestString_Simple(t *testing.T) {
	tests := []struct {
		input string
		text  string
	}{
		{`""`, `""`},
		{`"hello"`, `"hello"`},
		{`"hello world"`, `"hello world"`},
		{`"123"`, `"123"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expectSingleToken(t, tt.input, token.StringLit, tt.text)
		})
	}
}

func TestString_Escapes(t *testing.T) {
	tests := []struct {
		input string
		text  string
	}{
		{`"hello\nworld"`, `"hello\nworld"`},
		{`"tab\there"`, `"tab\there"`},
		{`"quote\"inside"`, `"quote\"inside"`},
		{`"backslash\\"`, `"backslash\\"`},
		{`"single\'quote"`, `"single\'quote"`},
		{`"\r\n"`, `"\r\n"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expectSingleToken(t, tt.input, token.StringLit, tt.text)
		})
	}
}

func TestString_Unterminated(t *testing.T) {
	tests := []string{
		`"hello`,
		`"world`,
		`"unclosed string`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			lx, reporter := makeTestLexer(input)
			tok := lx.Next()

			if tok.Kind != token.Invalid {
				t.Errorf("Expected Invalid for unterminated string, got %v", tok.Kind)
			}
			if !reporter.HasErrors() {
				t.Error("Expected error report for unterminated string")
			}
		})
	}
}

func TestString_NewlineInString(t *testing.T) {
	input := "\"hello\nworld\""
	lx, reporter := makeTestLexer(input)
	tok := lx.Next()

	if tok.Kind != token.Invalid {
		t.Errorf("Expected Invalid for newline in string, got %v", tok.Kind)
	}
	if !reporter.HasErrors() {
		t.Error("Expected error report for newline in string")
	}
}

func TestFString_Simple(t *testing.T) {
	tests := []struct {
		input string
		text  string
	}{
		{`f""`, `f""`},
		{`f"hello"`, `f"hello"`},
		{`f"{name}"`, `f"{name}"`},
		{`f"{{}}"`, `f"{{}}"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expectSingleToken(t, tt.input, token.FStringLit, tt.text)
		})
	}
}

func TestFString_SeparatedPrefix(t *testing.T) {
	expectTokens(t, `f "hello"`, []token.Kind{
		token.Ident,
		token.StringLit,
	})
}

// ====== Тесты для scan_ops.go ======

func TestOperators_Single(t *testing.T) {
	tests := []struct {
		input string
		kind  token.Kind
	}{
		{"+", token.Plus},
		{"-", token.Minus},
		{"*", token.Star},
		{"/", token.Slash},
		{"%", token.Percent},
		{"=", token.Assign},
		{"!", token.Bang},
		{"<", token.Lt},
		{">", token.Gt},
		{"&", token.Amp},
		{"|", token.Pipe},
		{"^", token.Caret},
		{"?", token.Question},
		{":", token.Colon},
		{";", token.Semicolon},
		{",", token.Comma},
		{".", token.Dot},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expectSingleToken(t, tt.input, tt.kind, tt.input)
		})
	}
}

func TestOperators_Double(t *testing.T) {
	tests := []struct {
		input string
		kind  token.Kind
	}{
		{"==", token.EqEq},
		{"!=", token.BangEq},
		{"<=", token.LtEq},
		{">=", token.GtEq},
		{"<<", token.Shl},
		{">>", token.Shr},
		{"&&", token.AndAnd},
		{"||", token.OrOr},
		{"::", token.ColonColon},
		{"->", token.Arrow},
		{"=>", token.FatArrow},
		{"..", token.DotDot},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expectSingleToken(t, tt.input, tt.kind, tt.input)
		})
	}
}

func TestOperators_Triple(t *testing.T) {
	tests := []struct {
		input string
		kind  token.Kind
	}{
		{"..=", token.DotDotEq},
		{"...", token.DotDotDot},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expectSingleToken(t, tt.input, tt.kind, tt.input)
		})
	}
}

func TestPunctuation(t *testing.T) {
	tests := []struct {
		input string
		kind  token.Kind
	}{
		{"(", token.LParen},
		{")", token.RParen},
		{"{", token.LBrace},
		{"}", token.RBrace},
		{"[", token.LBracket},
		{"]", token.RBracket},
		{"@", token.At},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expectSingleToken(t, tt.input, tt.kind, tt.input)
		})
	}
}

func TestOperators_Greedy(t *testing.T) {
	// Проверяем жадность: "..." не должен быть разбит на ".." + "."
	expectTokens(t, "...", []token.Kind{token.DotDotDot})
	expectTokens(t, "..=", []token.Kind{token.DotDotEq})

	// А вот "..+.." должен быть ".." "+" ".."
	expectTokens(t, "..+..", []token.Kind{
		token.DotDot,
		token.Plus,
		token.DotDot,
	})
}

// ====== Тесты для trivia.go ======

func TestTrivia_Spaces(t *testing.T) {
	lx, _ := makeTestLexer("  \t  foo")
	tok := lx.Next()

	if tok.Kind != token.Ident {
		t.Fatalf("Expected Ident, got %v", tok.Kind)
	}
	if len(tok.Leading) != 1 {
		t.Fatalf("Expected 1 leading trivia, got %d", len(tok.Leading))
	}
	if tok.Leading[0].Kind != token.TriviaSpace {
		t.Errorf("Expected TriviaSpace, got %v", tok.Leading[0].Kind)
	}
}

func TestTrivia_Newlines(t *testing.T) {
	lx, _ := makeTestLexer("\n\n\nfoo")
	tok := lx.Next()

	if tok.Kind != token.Ident {
		t.Fatalf("Expected Ident, got %v", tok.Kind)
	}
	if len(tok.Leading) != 1 {
		t.Fatalf("Expected 1 leading trivia (coalesced newlines), got %d", len(tok.Leading))
	}
	if tok.Leading[0].Kind != token.TriviaNewline {
		t.Errorf("Expected TriviaNewline, got %v", tok.Leading[0].Kind)
	}
}

func TestTrivia_LineComment(t *testing.T) {
	lx, _ := makeTestLexer("// this is a comment\nfoo")
	tok := lx.Next()

	if tok.Kind != token.Ident {
		t.Fatalf("Expected Ident, got %v", tok.Kind)
	}
	// Должно быть 2 trivia: comment + newline
	if len(tok.Leading) != 2 {
		t.Fatalf("Expected 2 leading trivia, got %d", len(tok.Leading))
	}
	if tok.Leading[0].Kind != token.TriviaLineComment {
		t.Errorf("Expected TriviaLineComment, got %v", tok.Leading[0].Kind)
	}
	if tok.Leading[1].Kind != token.TriviaNewline {
		t.Errorf("Expected TriviaNewline, got %v", tok.Leading[1].Kind)
	}
}

func TestTrivia_DocComment(t *testing.T) {
	lx, _ := makeTestLexer("/// doc comment\nfoo")
	tok := lx.Next()

	if tok.Kind != token.Ident {
		t.Fatalf("Expected Ident, got %v", tok.Kind)
	}
	if len(tok.Leading) != 2 {
		t.Fatalf("Expected 2 leading trivia, got %d", len(tok.Leading))
	}
	if tok.Leading[0].Kind != token.TriviaDocLine {
		t.Errorf("Expected TriviaDocLine, got %v", tok.Leading[0].Kind)
	}
}

func TestTrivia_BlockComment(t *testing.T) {
	lx, _ := makeTestLexer("/* block comment */foo")
	tok := lx.Next()

	if tok.Kind != token.Ident {
		t.Fatalf("Expected Ident, got %v", tok.Kind)
	}
	if len(tok.Leading) != 1 {
		t.Fatalf("Expected 1 leading trivia, got %d", len(tok.Leading))
	}
	if tok.Leading[0].Kind != token.TriviaBlockComment {
		t.Errorf("Expected TriviaBlockComment, got %v", tok.Leading[0].Kind)
	}
}

func TestTrivia_UnterminatedBlockComment(t *testing.T) {
	// Незакрытый комментарий съедает всё до конца файла
	lx, reporter := makeTestLexer("/* unterminated\nfoo")
	tok := lx.Next()

	// После незакрытого комментария, который съел весь текст, следует EOF
	if tok.Kind != token.EOF {
		t.Errorf("Expected EOF after unterminated block comment consuming all input, got %v", tok.Kind)
	}
	// должен быть репорт об ошибке
	if !reporter.HasErrors() {
		t.Error("Expected error report for unterminated block comment")
	}

	// Тест с незакрытым комментарием, после которого есть токены
	lx2, reporter2 := makeTestLexer("/* unterminated */ foo")
	tok2 := lx2.Next()
	if tok2.Kind != token.Ident {
		t.Errorf("Expected Ident after terminated block comment, got %v", tok2.Kind)
	}
	if len(tok2.Leading) == 0 {
		t.Error("Expected at least one leading trivia (the block comment)")
	}
	if reporter2.HasErrors() {
		t.Errorf("Expected no errors for properly terminated block comment, got %v", reporter2.ErrorMessages())
	}
}

func TestTrivia_Mixed(t *testing.T) {
	input := `
	// comment 1
	/* block */
	/// doc
	foo`

	lx, _ := makeTestLexer(input)
	tok := lx.Next()

	if tok.Kind != token.Ident {
		t.Fatalf("Expected Ident, got %v", tok.Kind)
	}

	// Должно быть несколько trivia
	if len(tok.Leading) < 3 {
		t.Errorf("Expected at least 3 trivia, got %d", len(tok.Leading))
	}
}

// ====== Интеграционные тесты ======

func TestLexer_SimpleExpression(t *testing.T) {
	input := "let x = 123 + 456"
	expectTokens(t, input, []token.Kind{
		token.KwLet,
		token.Ident,
		token.Assign,
		token.IntLit,
		token.Plus,
		token.IntLit,
	})
}

func TestLexer_FunctionDefinition(t *testing.T) {
	input := "fn add(a, b) -> int { return a + b }"
	expectTokens(t, input, []token.Kind{
		token.KwFn,
		token.Ident,
		token.LParen,
		token.Ident,
		token.Comma,
		token.Ident,
		token.RParen,
		token.Arrow,
		token.Ident,
		token.LBrace,
		token.KwReturn,
		token.Ident,
		token.Plus,
		token.Ident,
		token.RBrace,
	})
}

func TestLexer_ComplexExpression(t *testing.T) {
	input := "arr[0..10] && flag || !condition"
	expectTokens(t, input, []token.Kind{
		token.Ident,
		token.LBracket,
		token.IntLit,
		token.DotDot,
		token.IntLit,
		token.RBracket,
		token.AndAnd,
		token.Ident,
		token.OrOr,
		token.Bang,
		token.Ident,
	})
}

func TestLexer_WithComments(t *testing.T) {
	input := `
// leading comment
let x = 42 // inline comment
`
	expectTokens(t, input, []token.Kind{
		token.KwLet,
		token.Ident,
		token.Assign,
		token.IntLit,
	})
}

func TestLexer_PeekBehavior(t *testing.T) {
	lx, _ := makeTestLexer("a b c")

	// Peek не должен потреблять токен
	peek1 := lx.Peek()
	if peek1.Kind != token.Ident || peek1.Text != "a" {
		t.Errorf("First peek: expected Ident 'a', got %v '%s'", peek1.Kind, peek1.Text)
	}

	peek2 := lx.Peek()
	if peek2.Kind != peek1.Kind || peek2.Text != peek1.Text {
		t.Error("Second peek should return the same token")
	}

	// Next должен вернуть тот же токен и продвинуться
	next1 := lx.Next()
	if next1.Kind != peek1.Kind || next1.Text != peek1.Text {
		t.Error("Next should return the peeked token")
	}

	// Следующий токен должен быть другим
	next2 := lx.Next()
	if next2.Text != "b" {
		t.Errorf("Expected 'b', got '%s'", next2.Text)
	}
}

func TestLexer_EOF(t *testing.T) {
	lx, _ := makeTestLexer("x")

	tok1 := lx.Next()
	if tok1.Kind != token.Ident {
		t.Fatalf("Expected Ident, got %v", tok1.Kind)
	}

	tok2 := lx.Next()
	if tok2.Kind != token.EOF {
		t.Fatalf("Expected EOF, got %v", tok2.Kind)
	}

	// Повторные вызовы Next после EOF должны продолжать возвращать EOF
	tok3 := lx.Next()
	if tok3.Kind != token.EOF {
		t.Errorf("Expected EOF again, got %v", tok3.Kind)
	}
}

func TestLexer_EmptyInput(t *testing.T) {
	lx, _ := makeTestLexer("")
	tok := lx.Next()

	if tok.Kind != token.EOF {
		t.Errorf("Expected EOF for empty input, got %v", tok.Kind)
	}
}

func TestLexer_OnlyWhitespace(t *testing.T) {
	lx, _ := makeTestLexer("   \t\n  ")
	tok := lx.Next()

	if tok.Kind != token.EOF {
		t.Errorf("Expected EOF for whitespace-only input, got %v", tok.Kind)
	}
}

func TestLexer_UnknownCharacter(t *testing.T) {
	// Тестируем символы, которые не являются частью языка
	tests := []string{
		"#",
		"$",
		"§",
		"€",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			lx, reporter := makeTestLexer(input)
			tok := lx.Next()

			if tok.Kind != token.Invalid {
				t.Errorf("Expected Invalid for unknown char %q, got %v", input, tok.Kind)
			}
			if !reporter.HasErrors() {
				t.Error("Expected error report for unknown character")
			}
		})
	}
}

// Бенчмарки

func BenchmarkLexer_SimpleExpression(b *testing.B) {
	input := "let x = 123 + 456 * 789"
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("bench.sg", []byte(input))
	file := fs.Get(fileID)

	b.ResetTimer()
	for b.Loop() {
		lx := lexer.New(file, lexer.Options{})
		for {
			tok := lx.Next()
			if tok.Kind == token.EOF {
				break
			}
		}
	}
}

func BenchmarkLexer_LargeFile(b *testing.B) {
	// Имитируем большой файл с кодом
	var sb strings.Builder
	for i := range 100 {
		sb.WriteString("fn function")
		sb.WriteString(fmt.Sprintf("%d", i))
		sb.WriteString("(arg1, arg2) -> int { return arg1 + arg2 }\n")
	}
	input := sb.String()

	fs := source.NewFileSet()
	fileID := fs.AddVirtual("bench.sg", []byte(input))
	file := fs.Get(fileID)

	b.ResetTimer()
	for b.Loop() {
		lx := lexer.New(file, lexer.Options{})
		for {
			tok := lx.Next()
			if tok.Kind == token.EOF {
				break
			}
		}
	}
}
