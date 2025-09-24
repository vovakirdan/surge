
use crate::{cursor::Cursor, emit::Emitter, LexOptions};
use token::{TokenKind, Span, SourceId};
use std::char;

// Диагностические коды для ошибок лексического анализа
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DiagCode {
    UnclosedString,      // Незакрытая строковая константа
    BadEscape,           // Неправильная escape-последовательность
    UnclosedBlockComment,// Незакрытый блочный комментарий
    InvalidDigitForBase, // Неправильная цифра для системы счисления
    UnknownChar,         // Неизвестный символ
}

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
        if let Some(start_pos) = directive::try_take_directive(cur, em) {
            return true;
        }
    }

    // Шаг 3: Проверить многосимвольные операторы
    // Обрабатываем операторы по убыванию длины: "...", "::", "->", "=>", "&&", "||", "<=", ">=", "==", "!=", ":="
    if punct::try_take_multi(cur, em) {
        return true;
    }

    // Шаг 4: Проверить одиночные операторы
    // Обрабатываем одиночные символы пунктуации: [ ] ( ) { } < > | , ; : . & * ! = + - / %
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
    // Обрабатываем идентификаторы начинающиеся с буквы, _ или @
    if let Some(ch) = cur.peek() {
        if ch == '@' || ch.is_alphabetic() || ch == '_' {
            if ident::try_take_ident_or_keyword(cur, em) {
                return true;
            }
        }
    }

    // Шаг 8: Обработка неизвестного символа
    // Если символ не распознан ни одним правилом - выдаем диагностику и пропускаем его
    if let Some(ch) = cur.peek() {
        let start_pos = cur.pos();
        let span = Span::new(SourceId(0), start_pos, start_pos + 1);
        em.diag(span, DiagCode::UnknownChar, format!("Unknown character: '{}'", ch));
        cur.bump(); // Пропустить символ
        return true;
    }

    // Неожиданная ситуация - вернуть false
    false
}
