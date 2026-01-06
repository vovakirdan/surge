package dialect

import (
	"fmt"

	"surge/internal/token"
)

// ObserveTokenPair records token-pattern evidence, if any, using a sliding 2-token
// window. The caller is responsible for feeding tokens in source order.
func ObserveTokenPair(e *Evidence, prev, tok token.Token) {
	if e == nil {
		return
	}

	adjacent := prev.Span.File == tok.Span.File && prev.Span.End == tok.Span.Start

	// Rust macro call syntax: ident!(...)
	if prev.Kind == token.Ident && tok.Kind == token.Bang && adjacent {
		reason := "rust macro call syntax `ident!`"
		score := 4
		if prev.Text == "println" {
			reason = "rust macro call `println!`"
			score = 6
		}
		e.Add(Hint{
			Dialect: Rust,
			Score:   score,
			Reason:  reason,
			Span:    prev.Span.Cover(tok.Span),
		})
	}

	// Rust attribute syntax start: #[...]
	if prev.Kind == token.Invalid && prev.Text == "#" && tok.Kind == token.LBracket && adjacent {
		e.Add(Hint{
			Dialect: Rust,
			Score:   6,
			Reason:  "rust attribute syntax `#[...]`",
			Span:    prev.Span.Cover(tok.Span),
		})
	}

	// Go short variable declaration: :=
	if tok.Kind == token.ColonAssign {
		e.Add(Hint{
			Dialect: Go,
			Score:   5,
			Reason:  "go short variable declaration `:=`",
			Span:    tok.Span,
		})
	}

	// Rust-ish path roots: crate:: / self:: / super::
	if prev.Kind == token.Ident && tok.Kind == token.ColonColon && adjacent {
		switch prev.Text {
		case "crate", "self", "super":
			e.Add(Hint{
				Dialect: Rust,
				Score:   5,
				Reason:  fmt.Sprintf("rust path syntax `%s::`", prev.Text),
				Span:    prev.Span.Cover(tok.Span),
			})
		}
	}
}
