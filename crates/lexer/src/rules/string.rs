use crate::{cursor::Cursor, emit::Emitter};
use crate::token::{TokenKind, Span, SourceId};
use super::DiagCode;

/// Пытается захватить строковый литерал
/// Стартует только если текущий символ == '"'
/// Поддерживает escape-последовательности и unicode литералы
/// Возвращает true если строка была найдена и захвачена
pub fn try_take_string(cur: &mut Cursor, em: &mut Emitter) -> bool {
    // Проверяем что начинается с кавычки
    if cur.peek() != Some('"') {
        return false;
    }

    let start_pos = cur.pos();
    cur.bump(); // захватываем открывающую кавычку

    let mut has_errors = false;

    // Обрабатываем содержимое строки до закрывающей кавычки или EOF
    while let Some(ch) = cur.peek() {
        if ch == '"' {
            // Закрывающая кавычка - завершаем строку
            cur.bump(); // захватываем закрывающую кавычку
            break;
        }

        if ch == '\\' {
            // Escape-последовательность
            cur.bump(); // захватываем \

            if let Some(escape_ch) = cur.peek() {
                match escape_ch {
                    '"' | '\\' | 'n' | 't' | 'r' => {
                        // Стандартные escape-последовательности
                        cur.bump();
                    }
                    'u' => {
                        // Unicode escape \u{HEX}
                        cur.bump(); // захватываем u

                        if cur.peek() != Some('{') {
                            // Ожидалась {, но её нет
                            has_errors = true;
                            let span = Span::new(SourceId(0), cur.pos(), cur.pos() + 1);
                            em.diag(span, DiagCode::BadEscape, "Expected '{' after '\\u'".to_string());
                            continue;
                        }

                        cur.bump(); // захватываем {

                        // Считываем hex цифры (1-6)
                        let mut hex_count = 0;
                        let mut valid_hex = true;

                        while let Some(hex_ch) = cur.peek() {
                            if hex_ch == '}' {
                                cur.bump(); // захватываем }
                                break;
                            }

                            if !hex_ch.is_ascii_hexdigit() {
                                valid_hex = false;
                            }

                            hex_count += 1;
                            cur.bump();

                            // Если больше 6 hex цифр - считаем ошибкой
                            if hex_count > 6 {
                                valid_hex = false;
                            }
                        }

                        // Проверяем что была закрывающая }
                        if cur.peek() != Some('}') {
                            has_errors = true;
                            let span = Span::new(SourceId(0), cur.pos(), cur.pos() + 1);
                            em.diag(span, DiagCode::BadEscape, "Missing '}' in unicode escape".to_string());
                        } else {
                            cur.bump(); // захватываем }

                            if !valid_hex || hex_count == 0 {
                                has_errors = true;
                                let span = Span::new(SourceId(0), cur.pos() - hex_count - 2, cur.pos());
                                em.diag(span, DiagCode::BadEscape, "Invalid unicode escape sequence".to_string());
                            }
                        }
                    }
                    _ => {
                        // Неподдерживаемая escape-последовательность
                        has_errors = true;
                        let span = Span::new(SourceId(0), cur.pos(), cur.pos() + 1);
                        em.diag(span, DiagCode::BadEscape, format!("Unknown escape sequence '\\{}'", escape_ch));
                        cur.bump(); // захватываем символ
                    }
                }
            }
        } else {
            // Обычный символ
            cur.bump();
        }
    }

    let end_pos = cur.pos();

    // Проверяем что строка была закрыта
    if cur.eof() || cur.peek() != Some('"') {
        // Незакрытая строка
        has_errors = true;
        let span = Span::new(SourceId(0), start_pos, end_pos);
        em.diag(span, DiagCode::UnclosedString, "Unclosed string literal".to_string());
    }

    // Создаем токен строки
    em.token(start_pos, end_pos, TokenKind::StringLit);

    true
}
