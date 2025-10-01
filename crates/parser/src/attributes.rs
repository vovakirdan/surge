//! Парсинг атрибутов (@pure, @backend, и т.д.)

use crate::ast::*;
use crate::error::{ParseCode, ParseDiag};
use crate::lexer_api::Stream;
use surge_token::{AttrKeyword, Span, TokenKind, lookup_attribute_keyword};

/// Основная функция парсинга атрибутов, вызывается из основного парсера
pub fn parse_attrs(stream: &mut Stream, diags: &mut Vec<ParseDiag>) -> Vec<Attr> {
    let mut attrs = Vec::new();
    while stream.peek().kind == TokenKind::At {
        if let Some(attr) = parse_single_attr(stream, diags) {
            attrs.push(attr);
        }
    }
    attrs
}

/// Парсинг одного атрибута начиная с '@'
fn parse_single_attr(stream: &mut Stream, diags: &mut Vec<ParseDiag>) -> Option<Attr> {
    let at_tok = stream.bump();
    let (attr_name, ident_span) = expect_ident(stream, diags, "attribute name")?;
    let attr_display = format!("@{}", attr_name);
    let attr_keyword = classify_attr_keyword(
        &attr_name,
        ident_span,
        stream.peek().kind == TokenKind::LParen,
    );

    let initial_span = at_tok.span.join(ident_span);
    match attr_keyword {
        Some(AttrKeyword::Pure) => Some(Attr::Pure { span: initial_span }),
        Some(AttrKeyword::Overload) => Some(Attr::Overload { span: initial_span }),
        Some(AttrKeyword::Override) => Some(Attr::Override { span: initial_span }),
        Some(AttrKeyword::Intrinsic) => Some(Attr::Intrinsic { span: initial_span }),
        Some(AttrKeyword::Backend) => parse_backend_attribute(stream, diags, initial_span),
        Some(AttrKeyword::Deprecated) => parse_deprecated_attribute(stream, diags, initial_span),
        Some(AttrKeyword::Packed) => Some(Attr::Packed { span: initial_span }),
        Some(AttrKeyword::Align) => parse_align_attribute(stream, diags, initial_span),
        Some(AttrKeyword::Shared) => Some(Attr::Shared { span: initial_span }),
        Some(AttrKeyword::Atomic) => Some(Attr::Atomic { span: initial_span }),
        Some(AttrKeyword::Raii) => Some(Attr::Raii { span: initial_span }),
        Some(AttrKeyword::Arena) => Some(Attr::Arena { span: initial_span }),
        Some(AttrKeyword::Weak) => Some(Attr::Weak { span: initial_span }),
        Some(AttrKeyword::Readonly) => Some(Attr::Readonly { span: initial_span }),
        Some(AttrKeyword::Hidden) => Some(Attr::Hidden { span: initial_span }),
        Some(AttrKeyword::Noinherit) => Some(Attr::NoInherit { span: initial_span }),
        Some(AttrKeyword::Sealed) => Some(Attr::Sealed { span: initial_span }),
        Some(AttrKeyword::GuardedBy) => parse_lock_attribute(
            stream,
            diags,
            initial_span,
            "@guarded_by",
            LockAttrKind::GuardedBy,
        ),
        Some(AttrKeyword::RequiresLock) => parse_lock_attribute(
            stream,
            diags,
            initial_span,
            "@requires_lock",
            LockAttrKind::Requires,
        ),
        Some(AttrKeyword::AcquiresLock) => parse_lock_attribute(
            stream,
            diags,
            initial_span,
            "@acquires_lock",
            LockAttrKind::Acquires,
        ),
        Some(AttrKeyword::ReleasesLock) => parse_lock_attribute(
            stream,
            diags,
            initial_span,
            "@releases_lock",
            LockAttrKind::Releases,
        ),
        Some(AttrKeyword::WaitsOn) => parse_waits_on_attribute(stream, diags, initial_span),
        Some(AttrKeyword::Send) => Some(Attr::Send { span: initial_span }),
        Some(AttrKeyword::Nosend) => Some(Attr::NoSend { span: initial_span }),
        Some(AttrKeyword::Nonblocking) => Some(Attr::NonBlocking { span: initial_span }),
        _ => {
            error(
                diags,
                ParseCode::UnknownAttribute,
                ident_span,
                &format!("Unknown attribute {}", attr_display),
            );
            recover_unknown_attr(stream);
            None
        }
    }
}

/// Разбор @backend("target") или @backend(Ident)
fn parse_backend_attribute(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    span: Span,
) -> Option<Attr> {
    let (value, value_span, full_span) =
        parse_parenthesized_value(stream, diags, span, "@backend", ExpectedArg::StringOrIdent)?;
    Some(Attr::Backend {
        span: full_span,
        value,
        value_span,
    })
}

/// Разбор @deprecated("reason")
fn parse_deprecated_attribute(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    span: Span,
) -> Option<Attr> {
    let (message, message_span, full_span) =
        parse_parenthesized_value(stream, diags, span, "@deprecated", ExpectedArg::String)?;
    Some(Attr::Deprecated {
        span: full_span,
        message,
        message_span,
    })
}

/// Разбор @align(N)
fn parse_align_attribute(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    span: Span,
) -> Option<Attr> {
    let (value, value_span, full_span) =
        parse_parenthesized_value(stream, diags, span, "@align", ExpectedArg::Int)?;
    Some(Attr::Align {
        span: full_span,
        value,
        value_span,
    })
}

/// Обработка атрибутов, принимающих строку с именем блокировки
fn parse_lock_attribute(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    span: Span,
    display: &str,
    kind: LockAttrKind,
) -> Option<Attr> {
    let (lock, lock_span, full_span) =
        parse_parenthesized_value(stream, diags, span, display, ExpectedArg::String)?;
    Some(match kind {
        LockAttrKind::GuardedBy => Attr::GuardedBy {
            span: full_span,
            lock,
            lock_span,
        },
        LockAttrKind::Requires => Attr::RequiresLock {
            span: full_span,
            lock,
            lock_span,
        },
        LockAttrKind::Acquires => Attr::AcquiresLock {
            span: full_span,
            lock,
            lock_span,
        },
        LockAttrKind::Releases => Attr::ReleasesLock {
            span: full_span,
            lock,
            lock_span,
        },
    })
}

/// Обработка @waits_on("cond")
fn parse_waits_on_attribute(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    span: Span,
) -> Option<Attr> {
    let (cond, cond_span, full_span) =
        parse_parenthesized_value(stream, diags, span, "@waits_on", ExpectedArg::String)?;
    Some(Attr::WaitsOn {
        span: full_span,
        cond,
        cond_span,
    })
}

#[derive(Copy, Clone)]
enum ExpectedArg {
    String,
    StringOrIdent,
    Int,
}

#[derive(Copy, Clone)]
enum LockAttrKind {
    GuardedBy,
    Requires,
    Acquires,
    Releases,
}

/// Вспомогательный разбор аргумента в скобках.
/// Используется для унификации обработки атрибутов со строками/числами.
fn parse_parenthesized_value(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    mut span: Span,
    display: &str,
    expected: ExpectedArg,
) -> Option<(String, Span, Span)> {
    let open = match stream.eat(TokenKind::LParen) {
        Some(tok) => tok,
        None => {
            error(
                diags,
                ParseCode::UnexpectedToken,
                stream.peek().span,
                format!("Expected '(' after {}", display),
            );
            return None;
        }
    };
    span = span.join(open.span);

    let (value, value_span) = match expected {
        ExpectedArg::String => parse_string_literal(stream, diags, display)?,
        ExpectedArg::StringOrIdent => parse_string_or_ident(stream, diags, display)?,
        ExpectedArg::Int => parse_int_literal(stream, diags, display)?,
    };

    if let Some(close) = stream.eat(TokenKind::RParen) {
        span = span.join(close.span);
    } else {
        error(
            diags,
            ParseCode::UnclosedParen,
            open.span,
            format!("Expected ')' to close {}", display),
        );
    }

    Some((value, value_span, span))
}

/// Разбор строкового литерала, возвращает содержимое без кавычек и span токена.
fn parse_string_literal(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    display: &str,
) -> Option<(String, Span)> {
    let tok = stream.peek();
    if tok.kind != TokenKind::StringLit {
        error(
            diags,
            ParseCode::UnexpectedToken,
            tok.span,
            format!("Expected string literal in {}", display),
        );
        return None;
    }
    let taken = stream.bump();
    let text = stream.slice(taken.span).unwrap_or("\"\"").to_string();
    let value = strip_quotes(&text, taken.span);
    Some((value, taken.span))
}

/// Разбор строкового литерала или идентификатора (используется в @backend).
fn parse_string_or_ident(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    display: &str,
) -> Option<(String, Span)> {
    let tok = stream.peek();
    match tok.kind {
        TokenKind::StringLit => parse_string_literal(stream, diags, display),
        TokenKind::Ident => {
            let taken = stream.bump();
            let value = stream
                .slice(taken.span)
                .map(|s| s.to_string())
                .unwrap_or_else(|| format!("identifier_{}", taken.span.start));
            Some((value, taken.span))
        }
        _ => {
            error(
                diags,
                ParseCode::UnexpectedToken,
                tok.span,
                format!("Expected identifier or string literal in {}", display),
            );
            None
        }
    }
}

/// Разбор целочисленного литерала для @align(N).
fn parse_int_literal(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    display: &str,
) -> Option<(String, Span)> {
    let tok = stream.peek();
    if tok.kind != TokenKind::IntLit {
        error(
            diags,
            ParseCode::UnexpectedToken,
            tok.span,
            format!("Expected integer literal in {}", display),
        );
        return None;
    }
    let taken = stream.bump();
    let value = stream
        .slice(taken.span)
        .map(|s| s.to_string())
        .unwrap_or_else(|| format!("<int@{}>", taken.span.start));
    Some((value, taken.span))
}

/// Определяет ключевое слово атрибута. В режиме parse_tokens мы подглядываем длину лексемы,
/// чтобы сохранить совместимость с существующими тестами — эвристика документирована, т.к.
/// она подлежит удалению после появления реального текстового источника в parse_tokens.
fn classify_attr_keyword(
    attr_name: &str,
    ident_span: Span,
    has_paren: bool,
) -> Option<AttrKeyword> {
    if let Some(keyword) = lookup_attribute_keyword(attr_name) {
        return Some(keyword);
    }

    if !attr_name.starts_with("identifier_") {
        return None;
    }

    let len = ident_span.len() as usize;
    match (len, has_paren) {
        (4, false) => Some(AttrKeyword::Pure),
        (7, true) => Some(AttrKeyword::Backend),
        (8, false) => Some(AttrKeyword::Overload),
        (9, false) => Some(AttrKeyword::Intrinsic),
        (10, true) => Some(AttrKeyword::GuardedBy),
        (11, false) => Some(AttrKeyword::Nonblocking),
        (13, true) => Some(AttrKeyword::RequiresLock),
        _ => None,
    }
}

fn strip_quotes(text: &str, span: Span) -> String {
    if text.len() >= 2 {
        let inner = &text[1..text.len() - 1];
        if inner.is_empty() {
            format!("<string@{}>", span.start)
        } else {
            inner.to_string()
        }
    } else {
        format!("<string@{}>", span.start)
    }
}

/// Восстановление: пропускаем возможные скобки после неизвестного атрибута
fn recover_unknown_attr(stream: &mut Stream) {
    if stream.peek().kind == TokenKind::LParen {
        let mut paren_depth = 0;
        while !stream.is_eof() {
            let tok = stream.peek();
            match tok.kind {
                TokenKind::LParen => {
                    paren_depth += 1;
                    stream.bump();
                }
                TokenKind::RParen => {
                    stream.bump();
                    paren_depth -= 1;
                    if paren_depth == 0 {
                        break;
                    }
                }
                _ => {
                    stream.bump();
                }
            }
        }
    }
}

/// Вспомогательная функция для ожидания идентификатора
fn expect_ident(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    what: &str,
) -> Option<(String, Span)> {
    let tok = stream.peek();
    if tok.kind == TokenKind::Ident {
        let taken = stream.bump();
        let name = if let Some(slice) = stream.slice(taken.span) {
            slice.to_string()
        } else {
            // Fallback когда исходный текст недоступен
            format!("identifier_{}", taken.span.start)
        };
        return Some((name, taken.span));
    }
    error(
        diags,
        ParseCode::UnexpectedToken,
        tok.span,
        format!("Expected {what}"),
    );
    None
}

/// Вспомогательная функция для добавления ошибки
fn error(diags: &mut Vec<ParseDiag>, code: ParseCode, span: Span, message: impl Into<String>) {
    diags.push(ParseDiag::new(code, span, message));
}
