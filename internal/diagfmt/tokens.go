package diagfmt

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"surge/internal/source"
	"surge/internal/token"
)

// TokenOutput represents a token in the JSON output.
type TokenOutput struct {
	Kind    string      `json:"kind"`
	Text    string      `json:"text,omitempty"`
	Span    source.Span `json:"span"`
	Leading []string    `json:"leading,omitempty"`
}

// FormatTokensPretty выводит токены в человекочитаемом формате
func FormatTokensPretty(w io.Writer, tokens []token.Token, fs *source.FileSet) error {
	for i, tok := range tokens {
		// Получаем позицию токена
		startPos, endPos := fs.Resolve(tok.Span)

		// Форматируем leading trivia
		var leading []string
		for _, trivia := range tok.Leading {
			leading = append(leading, trivia.Kind.String())
		}

		// Выводим информацию о токене
		if _, err := fmt.Fprintf(w, "%3d: %-15s", i+1, tok.Kind.String()); err != nil {
			return err
		}

		if tok.Text != "" {
			if _, err := fmt.Fprintf(w, " %q", tok.Text); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintf(w, " at %d:%d-%d:%d",
			startPos.Line, startPos.Col,
			endPos.Line, endPos.Col); err != nil {
			return err
		}

		if len(leading) > 0 {
			if _, err := fmt.Fprintf(w, " (leading: %s)", strings.Join(leading, ", ")); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}

		if tok.Kind == token.EOF {
			break
		}
	}
	return nil
}

// TokenOutputsJSON готовит токены к сериализации в JSON формате.
func TokenOutputsJSON(tokens []token.Token) []TokenOutput {
	output := make([]TokenOutput, 0, len(tokens))
	for _, tok := range tokens {
		var leading []string
		for _, trivia := range tok.Leading {
			leading = append(leading, trivia.Kind.String())
		}

		tokenOut := TokenOutput{
			Kind:    tok.Kind.String(),
			Text:    tok.Text,
			Span:    tok.Span,
			Leading: leading,
		}

		if len(leading) == 0 {
			tokenOut.Leading = nil // Убираем пустые массивы из JSON
		}

		if tok.Text == "" {
			tokenOut.Text = "" // Для consistency
		}

		output = append(output, tokenOut)

		if tok.Kind == token.EOF {
			break
		}
	}

	return output
}

// FormatTokensJSON выводит токены в JSON формате
func FormatTokensJSON(w io.Writer, tokens []token.Token) error {
	output := TokenOutputsJSON(tokens)

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
