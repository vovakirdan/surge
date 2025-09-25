use crate::{cursor::Cursor, emit::Emitter};
use surge_token::TokenKind;

/// Пытается захватить идентификатор или ключевое слово
/// Условия входа: текущая руна is_alphabetic() или '_'
/// Собирает [A-Za-z_][A-Za-z0-9_]*
/// Использует lookup_keyword для определения типа токена
/// Возвращает true если идентификатор/ключевое слово было найдено и захвачено
pub fn try_take_ident_or_keyword(cur: &mut Cursor, em: &mut Emitter) -> bool {
    // Проверяем условие входа
    if let Some(ch) = cur.peek() {
        if ch != '_' && !ch.is_alphabetic() {
            return false;
        }
    } else {
        return false;
    }

    let start_pos = cur.pos();

    // Собираем идентификатор
    let mut ident = String::new();

    // Захватываем первый символ
    if let Some(ch) = cur.peek() {
        ident.push(ch);
        cur.bump();
    }

    // Собираем остальные символы [A-Za-z0-9_]*
    while let Some(ch) = cur.peek() {
        if ch.is_alphanumeric() || ch == '_' {
            ident.push(ch);
            cur.bump();
        } else {
            break;
        }
    }

    let end_pos = cur.pos();

    // Используем lookup_keyword для определения типа токена
    if let Some(keyword) = surge_token::keyword::lookup_keyword(&ident) {
        // Это ключевое слово или атрибут
        em.token(start_pos, end_pos, TokenKind::Keyword(keyword));
    } else {
        // Это обычный идентификатор
        em.token(start_pos, end_pos, TokenKind::Ident);
    }

    true
}
