pub mod comments;
pub mod directive;
pub mod ident;
pub mod number;
pub mod punct;
pub mod string;

use crate::emit::DiagCode;
use crate::{LexOptions, cursor::Cursor, emit::Emitter};
use surge_token::Span;

/// Основная функция диспетчера лексера
/// Выполняет лексический анализ в строгом порядке приоритетов
/// Возвращает true если токен был успешно обработан, false при достижении EOF
pub fn next_token(cur: &mut Cursor, em: &mut Emitter, opt: &LexOptions) -> bool {
    // Шаг 1: Пропустить всю тривию (пробелы/комментарии)
    // Функция должна пропустить все пробелы и комментарии подряд
    comments::skip_trivia(cur, em, opt);

    // Если достигли EOF - вернуть false
    if cur.eof() {
        return false;
    }

    // Шаг 2: Проверить директивы (если включены)
    // Обрабатываем специальные директивы начинающиеся с ///
    if opt.enable_directives {
        if directive::try_take_directive(cur, em, opt).is_some() {
            return true;
        }
    }

    // Шаг 3: Проверить многосимвольные операторы
    // Обрабатываем операторы по убыванию длины: "...", "::", "->", "=>", "&&", "||", "<=", ">=", "==", "!=", ":=", "+=", "-=", "*=", "/=", "%="
    if punct::try_take_multi(cur, em) {
        return true;
    }

    // Шаг 4: Проверить одиночные операторы
    // Обрабатываем одиночные символы пунктуации: [ ] ( ) { } < > | , ; : . & * ! = + - / % @ ?
    if punct::try_take_single(cur, em) {
        return true;
    }

    // Шаг 5: Проверить строковые литералы
    // Обрабатываем строки в кавычках с поддержкой escape-последовательностей
    if let Some(ch) = cur.peek() {
        if ch == '"' {
            if string::try_take_string(cur, em) {
                return true;
            }
        }
    }

    // Шаг 6: Проверить числовые литералы
    // Обрабатываем целые и вещественные числа в различных форматах
    if let Some(ch) = cur.peek() {
        if ch.is_ascii_digit() {
            if number::try_take_number(cur, em) {
                return true;
            }
        }
    }

    // Шаг 7: Проверить идентификаторы и ключевые слова
    // Обрабатываем идентификаторы начинающиеся с буквы или _
    if let Some(ch) = cur.peek() {
        if ch.is_alphabetic() || ch == '_' {
            if ident::try_take_ident_or_keyword(cur, em) {
                return true;
            }
        }
    }

    // Шаг 8: Обработка неизвестного символа
    // Если символ не распознан ни одним правилом - выдаем диагностику и пропускаем его
    if let Some(ch) = cur.peek() {
        let start_pos = cur.pos();
        let end_pos = start_pos + ch.len_utf8() as u32;
        let span = Span::new(em.file, start_pos, end_pos);
        em.diag(
            span,
            DiagCode::UnknownChar,
            format!("Unknown character: '{}'", ch),
        );
        cur.bump(); // Пропустить символ
        return true;
    }

    // Неожиданная ситуация - вернуть false
    false
}
