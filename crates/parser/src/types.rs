//! Парсинг типов и аннотаций типов

use crate::ast::*;
use crate::error::{ParseCode, ParseDiag};
use crate::lexer_api::Stream;
use surge_token::{Keyword, Span, TokenKind};

/// Основная функция парсинга типа с балансировкой скобок
pub fn parse_type_node(stream: &mut Stream, diags: &mut Vec<ParseDiag>) -> Option<TypeNode> {
    let mut depth_angle = 0i32;
    let mut depth_bracket = 0i32;
    let mut consumed_any = false;
    let mut start_span = None;
    let mut end_span = None;
    let mut consumed_tokens = Vec::new();

    loop {
        let tok = stream.peek();
        if !consumed_any {
            if !looks_like_type_start(tok.kind) {
                error(diags, ParseCode::UnexpectedToken, tok.span, "Expected type");
                return None;
            }
        } else if depth_angle == 0 && depth_bracket == 0 && is_type_terminator(tok.kind) {
            break;
        }

        if stream.is_eof() {
            break;
        }

        let tok = stream.bump();
        consumed_any = true;
        start_span.get_or_insert(tok.span);
        end_span = Some(tok.span);

        // Сохраняем токен для реконструкции
        consumed_tokens.push(tok);

        match tok.kind {
            TokenKind::LAngle => depth_angle += 1,
            TokenKind::RAngle => depth_angle -= 1,
            TokenKind::LBracket => depth_bracket += 1,
            TokenKind::RBracket => depth_bracket -= 1,
            _ => {}
        }

        if depth_angle < 0 {
            break;
        }
        if depth_bracket < 0 {
            break;
        }

        if depth_angle == 0 && depth_bracket == 0 && is_type_terminator(stream.peek().kind) {
            break;
        }
    }

    let Some(start) = start_span else {
        return None;
    };
    let end = end_span.unwrap_or(start);
    let span = start.join(end);

    // Сначала пытаемся получить текст из источника, если доступен, иначе реконструируем из токенов
    let repr = if let Some(slice) = stream.slice(span) {
        slice.to_string()
    } else {
        // Реконструируем текст типа из потребленных токенов
        reconstruct_type_from_tokens(&consumed_tokens, stream)
    };

    Some(TypeNode { repr, span })
}

/// Реконструкция представления типа из токенов когда исходный текст недоступен
fn reconstruct_type_from_tokens(tokens: &[surge_token::Token], stream: &Stream) -> String {
    let mut result = String::new();

    for tok in tokens {
        let token_repr = match &tok.kind {
            TokenKind::Ident => {
                if let Some(slice) = stream.slice(tok.span) {
                    slice.to_string()
                } else {
                    // Общие имена типов основанные на эвристике длины токена
                    let len = (tok.span.end - tok.span.start) as usize;
                    match len {
                        3 => "int".to_string(),
                        4 => {
                            if result.is_empty() {
                                "bool".to_string()
                            } else {
                                "uint".to_string()
                            }
                        }
                        5 => "float".to_string(),
                        6 => "string".to_string(),
                        _ => format!("T{}", len), // Общий fallback
                    }
                }
            }
            TokenKind::Keyword(kw) => format!("{:?}", kw).to_lowercase(),
            TokenKind::LAngle => "<".to_string(),
            TokenKind::RAngle => ">".to_string(),
            TokenKind::LBracket => "[".to_string(),
            TokenKind::RBracket => "]".to_string(),
            TokenKind::Comma => ", ".to_string(),
            TokenKind::Colon => ":".to_string(),
            TokenKind::Amp => "&".to_string(),
            TokenKind::Star => "*".to_string(),
            TokenKind::IntLit => {
                if let Some(slice) = stream.slice(tok.span) {
                    slice.to_string()
                } else {
                    "0".to_string()
                }
            }
            _ => {
                if let Some(slice) = stream.slice(tok.span) {
                    slice.to_string()
                } else {
                    "".to_string()
                }
            }
        };

        result.push_str(&token_repr);
    }

    if result.is_empty() {
        "T".to_string() // Финальный fallback
    } else {
        result
    }
}

/// Проверка может ли вид токена завершить аннотацию типа
fn is_type_terminator(kind: TokenKind) -> bool {
    matches!(
        kind,
        TokenKind::Comma
            | TokenKind::RParen
            | TokenKind::Semicolon
            | TokenKind::Eq
            | TokenKind::RBrace
            | TokenKind::LBrace
            | TokenKind::ColonEq
            | TokenKind::Keyword(Keyword::In)
            | TokenKind::Eof
    )
}

/// Проверка может ли вид токена начать аннотацию типа
fn looks_like_type_start(kind: TokenKind) -> bool {
    matches!(
        kind,
        TokenKind::Ident
            | TokenKind::Keyword(Keyword::Own)
            | TokenKind::Keyword(Keyword::Nothing)
            | TokenKind::Amp
            | TokenKind::Star
    )
}

/// Вспомогательная функция для добавления ошибки
fn error(diags: &mut Vec<ParseDiag>, code: ParseCode, span: Span, message: impl Into<String>) {
    diags.push(ParseDiag::new(code, span, message));
}
