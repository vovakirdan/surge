use crate::{cursor::Cursor, emit::Emitter};
use crate::token::{TokenKind, Span, SourceId};
use super::DiagCode;

/// Пытается захватить числовой литерал
/// Стартует только если первая руна - is_ascii_digit()
/// Поддерживает hex (0x), binary (0b), decimal, float и экспоненциальную запись
/// Возвращает true если число было найдено и захвачено
pub fn try_take_number(cur: &mut Cursor, em: &mut Emitter) -> bool {
    // Проверяем что начинается с цифры
    if let Some(ch) = cur.peek() {
        if !ch.is_ascii_digit() {
            return false;
        }
    } else {
        return false;
    }

    let start_pos = cur.pos();
    let mut is_float = false;
    let mut base = 10; // по умолчанию десятичная система
    let mut current_end = start_pos;

    // Проверяем префикс
    if cur.starts_with("0x") || cur.starts_with("0X") {
        base = 16;
        cur.bump(); cur.bump(); // захватываем 0x/0X
    } else if cur.starts_with("0b") || cur.starts_with("0B") {
        base = 2;
        cur.bump(); cur.bump(); // захватываем 0b/0B
    }

    // Собираем число
    current_end = collect_number_digits(cur, em, base);

    // Проверяем float часть
    if cur.peek() == Some('.') {
        // Смотрим следующий символ после точки
        let next_ch = cur.peek_n(1);
        if next_ch.map_or(false, |ch| ch.is_ascii_digit()) {
            is_float = true;
            cur.bump(); // захватываем .
            current_end = collect_number_digits(cur, em, 10);
        }
    }

    // Проверяем экспоненциальную часть
    if cur.peek() == Some('e') || cur.peek() == Some('E') {
        // Смотрим следующий символ после e/E
        let after_e = cur.peek_n(1);
        if after_e.is_some() && (after_e.unwrap().is_ascii_digit() ||
                                 after_e.unwrap() == '+' || after_e.unwrap() == '-') {
            cur.bump(); // захватываем e/E
            current_end = cur.pos();

            // Необязательный + или -
            if cur.peek() == Some('+') || cur.peek() == Some('-') {
                cur.bump();
                current_end = cur.pos();
            }

            // Должна быть хотя бы одна цифра
            if let Some(ch) = cur.peek() {
                if ch.is_ascii_digit() {
                    is_float = true;
                    current_end = collect_number_digits(cur, em, 10);
                }
            }
        }
    }

    // Создаем токен
    let token_kind = if is_float { TokenKind::FloatLit } else { TokenKind::IntLit };
    em.token(start_pos, current_end, token_kind);

    true
}

/// Собирает цифры числа с учетом подчеркиваний
/// Возвращает позицию после последней корректной цифры
fn collect_number_digits(cur: &mut Cursor, em: &mut Emitter, base: u32) -> u32 {
    let mut prev_underscore = false;

    while let Some(ch) = cur.peek() {
        if ch == '_' {
            if prev_underscore {
                // Два подчеркивания подряд - завершаем
                break;
            }
            prev_underscore = true;
            cur.bump();
        } else if is_valid_digit(ch, base) {
            prev_underscore = false;
            cur.bump();
        } else {
            // Недопустимая цифра для базы
            let span = Span::new(SourceId(0), cur.pos(), cur.pos() + 1);
            em.diag(span, DiagCode::InvalidDigitForBase, format!("Invalid digit '{}' for base {}", ch, base));
            break;
        }
    }

    // Проверяем что не заканчивается на _
    // Если заканчивается на _, то последний символ не захватываем
    if prev_underscore {
        // Возвращаем позицию без последнего _
        return cur.pos() - 1;
    }

    cur.pos()
}

/// Проверяет является ли символ допустимой цифрой для заданной базы
fn is_valid_digit(ch: char, base: u32) -> bool {
    match base {
        2 => ch == '0' || ch == '1',
        10 => ch.is_ascii_digit(),
        16 => ch.is_ascii_hexdigit(),
        _ => false,
    }
}
