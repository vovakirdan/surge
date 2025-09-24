use crate::{cursor::Cursor, emit::Emitter};
use surge_token::TokenKind;

/// Пытается захватить многосимвольный оператор
/// Проверяет операторы в строгом порядке по убыванию длины:
/// "..." "::" "->" "=>" "&&" "||" "<=" ">=" "==" "!=" ":="
/// Возвращает true если оператор был найден и захвачен
pub fn try_take_multi(
    cur: &mut Cursor,
    em: &mut Emitter
) -> bool {
    let start_pos = cur.pos();

    // Проверяем операторы в порядке приоритета (по убыванию длины)
    if cur.starts_with("...") {
        // Ellipsis
        cur.bump(); cur.bump(); cur.bump(); // захватываем ...
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::Ellipsis);
        return true;
    }

    if cur.starts_with("::") {
        // PathSep
        cur.bump(); cur.bump(); // захватываем ::
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::PathSep);
        return true;
    }

    if cur.starts_with("->") {
        // ThinArrow
        cur.bump(); cur.bump(); // захватываем ->
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::ThinArrow);
        return true;
    }

    if cur.starts_with("=>") {
        // FatArrow
        cur.bump(); cur.bump(); // захватываем =>
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::FatArrow);
        return true;
    }

    if cur.starts_with("&&") {
        // AndAnd
        cur.bump(); cur.bump(); // захватываем &&
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::AndAnd);
        return true;
    }

    if cur.starts_with("||") {
        // OrOr
        cur.bump(); cur.bump(); // захватываем ||
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::OrOr);
        return true;
    }

    if cur.starts_with("<=") {
        // Le
        cur.bump(); cur.bump(); // захватываем <=
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::Le);
        return true;
    }

    if cur.starts_with(">=") {
        // Ge
        cur.bump(); cur.bump(); // захватываем >=
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::Ge);
        return true;
    }

    if cur.starts_with("==") {
        // EqEq
        cur.bump(); cur.bump(); // захватываем ==
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::EqEq);
        return true;
    }

    if cur.starts_with("!=") {
        // Ne
        cur.bump(); cur.bump(); // захватываем !=
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::Ne);
        return true;
    }

    if cur.starts_with(":=") {
        // ColonEq
        cur.bump(); cur.bump(); // захватываем :=
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, TokenKind::ColonEq);
        return true;
    }

    false
}

/// Пытается захватить одиночный символ пунктуации
/// Обрабатывает: [ ] ( ) { } < > | , ; : . & * ! = + - / %
/// Возвращает true если символ был найден и захвачен
pub fn try_take_single(
    cur: &mut Cursor,
    em: &mut Emitter
) -> bool {
    let start_pos = cur.pos();

    if let Some(ch) = cur.peek() {
        let token_kind = match ch {
            '[' => TokenKind::LBracket,
            ']' => TokenKind::RBracket,
            '(' => TokenKind::LParen,
            ')' => TokenKind::RParen,
            '{' => TokenKind::LBrace,
            '}' => TokenKind::RBrace,
            '<' => TokenKind::LAngle,
            '>' => TokenKind::RAngle,
            '|' => TokenKind::Pipe,
            ',' => TokenKind::Comma,
            ';' => TokenKind::Semicolon,
            ':' => TokenKind::Colon,
            '.' => TokenKind::Dot,
            '&' => TokenKind::Amp,
            '*' => TokenKind::Star,
            '!' => TokenKind::Not,
            '=' => TokenKind::Eq,
            '+' => TokenKind::Plus,
            '-' => TokenKind::Minus,
            '/' => TokenKind::Slash,
            '%' => TokenKind::Percent,
            _ => return false, // не одиночный оператор
        };

        cur.bump(); // захватываем символ
        let end_pos = cur.pos();
        em.token(start_pos, end_pos, token_kind);
        return true;
    }

    false
}
