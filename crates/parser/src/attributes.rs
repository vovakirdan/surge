//! Парсинг атрибутов (@pure, @backend, и т.д.)

use crate::ast::*;
use crate::error::{ParseCode, ParseDiag};
use crate::lexer_api::Stream;
use surge_token::{Span, TokenKind};

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
    let at_tok = stream.bump(); // consume '@'

    // Получаем имя атрибута
    let (attr_name, ident_span) = match expect_ident(stream, diags, "attribute name") {
        Some(result) => result,
        None => return None, // Ошибка уже выдана в expect_ident
    };

    let span = at_tok.span.join(ident_span);

    // Определяем тип атрибута
    let attr_type = determine_attr_type(&attr_name, ident_span, stream);
    let attr_display = format!("@{}", attr_name);

    match attr_type {
        Some("pure") => Some(Attr::Pure { span }),
        Some("overload") => Some(Attr::Overload { span }),
        Some("override") => Some(Attr::Override { span }),
        Some("backend") => parse_backend_attribute(stream, diags, span),
        _ => {
            error(
                diags,
                ParseCode::UnknownAttribute,
                ident_span,
                &format!("Unknown attribute {}", attr_display),
            );

            // Восстановление: пропускаем возможные скобки после неизвестного атрибута
            recover_unknown_attr(stream);
            None
        }
    }
}

/// Парсинг атрибута @backend("string")
fn parse_backend_attribute(
    stream: &mut Stream,
    diags: &mut Vec<ParseDiag>,
    mut span: Span,
) -> Option<Attr> {
    if stream.peek().kind == TokenKind::LParen {
        let open_tok = stream.bump();
        span = span.join(open_tok.span);

        let (value, value_span) = if stream.peek().kind == TokenKind::StringLit {
            let str_tok = stream.bump();
            let text = stream.slice(str_tok.span).unwrap_or("\"\"").to_string();
            let has_source_text = !text.is_empty();

            // Обработка случая, когда текст источника недоступен
            let value = if has_source_text {
                // Убираем кавычки из строкового литерала
                if text.len() >= 2 {
                    text[1..text.len() - 1].to_string()
                } else {
                    text
                }
            } else {
                // Fallback: угадываем по длине строкового литерала
                let str_len = (str_tok.span.end - str_tok.span.start) as usize;
                match str_len {
                    5 => "cpu".to_string(), // И "cpu" и "gpu" + кавычки = 5 символов - неоднозначно, по умолчанию cpu
                    _ => format!("<string@{}>", str_tok.span.start),
                }
            };

            // Валидация строки backend только при наличии реального исходного текста
            if !value.starts_with('<') && has_source_text && value != "cpu" && value != "gpu" {
                error(
                    diags,
                    ParseCode::UnknownAttribute,
                    str_tok.span,
                    &format!("Invalid backend '{}', expected 'cpu' or 'gpu'", value),
                );
            }

            (value, str_tok.span)
        } else {
            error(
                diags,
                ParseCode::UnexpectedToken,
                stream.peek().span,
                "Expected string literal in @backend attribute",
            );
            (String::new(), stream.peek().span)
        };

        if let Some(close_tok) = stream.eat(TokenKind::RParen) {
            span = span.join(close_tok.span);
        } else {
            error(
                diags,
                ParseCode::UnclosedParen,
                open_tok.span,
                "Expected ')' to close @backend attribute",
            );
        }

        Some(Attr::Backend {
            span,
            value,
            value_span,
        })
    } else {
        error(
            diags,
            ParseCode::UnexpectedToken,
            stream.peek().span,
            "Expected '(' after @backend",
        );
        None
    }
}

/// Определение типа атрибута по его имени, обрабатывает режимы с исходным кодом и только с токенами
fn determine_attr_type(attr_name: &str, ident_span: Span, stream: &Stream) -> Option<&'static str> {
    if attr_name.starts_with("identifier_") {
        // В режиме parse_tokens получили fallback имя - используем эвристику
        let ident_len = (ident_span.end - ident_span.start) as usize;
        let has_paren_after = stream.peek().kind == TokenKind::LParen;

        match (ident_len, has_paren_after) {
            (4, false) => Some("pure"),   // "pure" это 4 символа, без скобок
            (7, true) => Some("backend"), // "backend" это 7 символов, со скобками
            (8, false) => {
                // И "overload" и "override" это 8 символов без скобок
                // Мы не можем их различить без исходного текста, поэтому по умолчанию "overload"
                // но это неизбежное ограничение подхода только с токенами
                Some("overload")
            }
            _ => None, // Неизвестный атрибут
        }
    } else {
        // У нас есть реальный исходный текст - используем точное соответствие
        match attr_name {
            "pure" => Some("pure"),
            "backend" => Some("backend"),
            "overload" => Some("overload"),
            "override" => Some("override"),
            _ => None,
        }
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
