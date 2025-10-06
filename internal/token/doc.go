// Package token defines lexical token kinds and trivia for the Surge compiler.
// Invariants:
//   - Token.Text is a slice of the original source (no copies).
//   - Token.Span matches Text exactly (Begin..End).
//   - Attributes are lexed as '@' (Kind: At) + Ident; no per-attribute token kinds.
//   - Directives (/// ...) are represented as leading Trivia (TriviaDirective) and
//     never appear in the main token stream.
//   - Built-in type names (int, int8, uint32, float64, ...) are identifiers.
//     They are recognized by the semantic layer, not the lexer.
package token
