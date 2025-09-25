use crate::emit::DiagCode;
use crate::{cursor::Cursor, emit::Emitter};
use surge_token::TokenKind;

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

    // Проверяем префикс
    if cur.starts_with("0x") || cur.starts_with("0X") {
        base = 16;
        cur.bump();
        cur.bump(); // захватываем 0x/0X
    } else if cur.starts_with("0b") || cur.starts_with("0B") {
        base = 2;
        cur.bump();
        cur.bump(); // захватываем 0b/0B
    }

    // Собираем число
    let (mut current_end, had_digits) = collect_number_digits(cur, em, base, /*allow_exp=*/ base == 10);

    // Если был указан не-десятичный префикс и ни одной цифры не встретили — это ошибка
    if (base == 16 || base == 2) && !had_digits {
        let span = surge_token::Span::new(em.file, cur.pos(), cur.pos());
        em.diag(
            span,
            DiagCode::InvalidDigitForBase,
            format!("expected at least one digit for base {}", base),
        );
    }

    // Проверяем float часть
    if cur.peek() == Some('.') {
        // Смотрим следующий символ после точки
        let next_ch = cur.peek_n(1);
        if next_ch.map_or(false, |ch| ch.is_ascii_digit()) {
            is_float = true;
            cur.bump(); // захватываем .
            let (end_after_frac, _) = collect_number_digits(cur, em, 10, /*allow_exp=*/ true);
            current_end = end_after_frac;
        }
    }

    // Проверяем экспоненциальную часть
    if cur.peek() == Some('e') || cur.peek() == Some('E') {
        // Смотрим следующий символ после e/E
        let after_e = cur.peek_n(1);
        if after_e.is_some()
            && (after_e.unwrap().is_ascii_digit()
                || after_e.unwrap() == '+'
                || after_e.unwrap() == '-')
        {
            cur.bump(); // захватываем e/E
            current_end = cur.pos();

            // Необязательный + или -
            if cur.peek() == Some('+') || cur.peek() == Some('-') {
                cur.bump();
                current_end = cur.pos();
            }

            // Должна быть хотя бы одна цифра
            if let Some(ch_after) = cur.peek() {
                if ch_after.is_ascii_digit() {
                    is_float = true;
                    let (end_after_exp, _) = collect_number_digits(cur, em, 10, /*allow_exp=*/ false);
                    current_end = end_after_exp;
                }
            }
        }
    }

    // Создаем токен
    let token_kind = if is_float {
        TokenKind::FloatLit
    } else {
        TokenKind::IntLit
    };
    em.token(start_pos, current_end, token_kind);

    true
}

/// Собирает цифры числа с учетом подчеркиваний.
/// Если `allow_exp == true`, то для base=10 `e|E` считается **терминатором**, а не ошибкой.
/// Возвращает (позиция после последней цифры, были_ли_цифры).
fn collect_number_digits(cur: &mut Cursor, em: &mut Emitter, base: u32, allow_exp: bool) -> (u32, bool) {
    let mut had_digits = false;

    while let Some(ch) = cur.peek() {
        if ch == '_' {
            // '_' используем только как разделитель, если дальше действительно идёт допустимая цифра.
            match cur.peek_n(1) {
                Some(next) if is_valid_digit(next, base) => {
                    cur.bump(); // съели '_', следующая итерация проверит цифру
                }
                _ => break, // висячий '_' — заканчиваем число, '_' останется для следующего токена
            }
        } else if is_valid_digit(ch, base) {
            had_digits = true;
            cur.bump();
        } else {
            // Не цифра для этой базы: решаем, это терминатор или реальная ошибка продолжения.
            // Терминаторы: пробелы, пунктуация и (если allow_exp && base==10) 'e'|'E'.
            if allow_exp && base == 10 && (ch == 'e' || ch == 'E') {
                break;
            }
            if ch.is_alphanumeric() {
                let span = surge_token::Span::new(em.file, cur.pos(), cur.pos() + ch.len_utf8() as u32);
                em.diag(
                    span,
                    DiagCode::InvalidDigitForBase,
                    format!("Invalid digit '{}' for base {}", ch, base),
                );
            }
            break;
        }
    }

    (cur.pos(), had_digits)
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
