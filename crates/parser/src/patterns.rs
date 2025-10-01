//! Shared parsing utilities for pattern syntax used across expressions and statements.

use crate::ast::{Expr, Pattern, PatternKind, SpanExt};
use crate::error::{ParseCode, ParseDiag};
use crate::lexer_api::Stream;
use surge_token::{Keyword, TokenKind};

/// Parse a pattern from the stream, returning `None` when recovery is required.
pub fn parse_pattern(stream: &mut Stream, diags: &mut Vec<ParseDiag>) -> Option<Pattern> {
    let tok = stream.bump();
    match tok.kind {
        TokenKind::Keyword(Keyword::Finally) => Some(Pattern {
            kind: PatternKind::Finally,
            span: tok.span,
        }),
        TokenKind::Keyword(Keyword::Nothing) => Some(Pattern {
            kind: PatternKind::Nothing,
            span: tok.span,
        }),
        TokenKind::Keyword(Keyword::True) | TokenKind::Keyword(Keyword::False) => {
            let default = if tok.kind == TokenKind::Keyword(Keyword::True) {
                "true"
            } else {
                "false"
            };
            let text = stream.slice(tok.span).unwrap_or(default).to_string();
            Some(Pattern {
                kind: PatternKind::Literal(Expr::Ident(text, tok.span)),
                span: tok.span,
            })
        }
        TokenKind::IntLit => {
            let text = stream.slice(tok.span).unwrap_or("0").to_string();
            Some(Pattern {
                kind: PatternKind::Literal(Expr::LitInt(text, tok.span)),
                span: tok.span,
            })
        }
        TokenKind::FloatLit => {
            let text = stream.slice(tok.span).unwrap_or("0.0").to_string();
            Some(Pattern {
                kind: PatternKind::Literal(Expr::LitFloat(text, tok.span)),
                span: tok.span,
            })
        }
        TokenKind::StringLit => {
            let text = stream.slice(tok.span).unwrap_or("\"\"").to_string();
            Some(Pattern {
                kind: PatternKind::Literal(Expr::LitString(text, tok.span)),
                span: tok.span,
            })
        }
        TokenKind::Ident => {
            let name = if let Some(slice) = stream.slice(tok.span) {
                slice.to_string()
            } else {
                format!("ident_{}", tok.span.start)
            };

            if stream.at(TokenKind::LParen) {
                let open = stream.bump();
                let mut args = Vec::new();
                while !stream.is_eof() && !stream.at(TokenKind::RParen) {
                    match parse_pattern(stream, diags) {
                        Some(arg) => args.push(arg),
                        None => {
                            recover_pattern_args(stream);
                            break;
                        }
                    }
                    if stream.eat(TokenKind::Comma).is_some() {
                        continue;
                    }
                    break;
                }
                let close = match stream.eat(TokenKind::RParen) {
                    Some(tok) => tok,
                    None => {
                        emit_error(
                            diags,
                            ParseCode::UnclosedParen,
                            open.span,
                            "Expected ')' after tag pattern",
                        );
                        open
                    }
                };
                let span = tok.span.join(close.span);
                Some(Pattern {
                    kind: PatternKind::Tag { name, args },
                    span,
                })
            } else {
                Some(Pattern {
                    kind: PatternKind::Binding(name),
                    span: tok.span,
                })
            }
        }
        _ => {
            emit_error(
                diags,
                ParseCode::UnexpectedToken,
                tok.span,
                format!("Unexpected token {:?} in pattern", tok.kind),
            );
            None
        }
    }
}

/// Recovery helper used by pattern parsing when nested arguments fail.
pub fn recover_pattern_args(stream: &mut Stream) {
    while !stream.is_eof() {
        match stream.peek().kind {
            TokenKind::Comma => {
                stream.bump();
                break;
            }
            TokenKind::RParen => break,
            _ => {
                stream.bump();
            }
        }
    }
}

fn emit_error(
    diags: &mut Vec<ParseDiag>,
    code: ParseCode,
    span: surge_token::Span,
    message: impl Into<String>,
) {
    diags.push(ParseDiag::new(code, span, message));
}
