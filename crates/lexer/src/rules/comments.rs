use crate::{cursor::Cursor, emit::{Emitter, DiagCode, TriviaKind}, LexOptions};
use surge_token::Span;

/// Пропускает всю подряд идущую тривию (пробелы и комментарии)
/// Если opt.keep_trivia == true, публикует каждый элемент тривии через em.trivia()
/// Возвращает () - функция не возвращает значения, только модифицирует курсор и эмиттер
pub fn skip_trivia(
    cur: &mut Cursor,
    em: &mut Emitter,
    opt: &LexOptions
) {
    // Цикл продолжается пока есть тривия
    while !cur.eof() {
        let ch = cur.peek();

        match ch {
            Some(ch) if ch.is_whitespace() => {
                // Обработка пробельных символов
                skip_whitespace(cur, em, opt);
            }
            Some('/') if cur.starts_with("//") => {
                // Однострочный комментарий
                skip_line_comment(cur, em, opt);
            }
            Some('/') if cur.starts_with("/*") => {
                // Многострочный комментарий
                skip_block_comment(cur, em, opt);
            }
            _ => {
                // Не тривия - выходим из цикла
                break;
            }
        }
    }
}

/// Пропускает последовательность пробельных символов
fn skip_whitespace(
    cur: &mut Cursor,
    em: &mut Emitter,
    opt: &LexOptions
) {
    let start = cur.pos();

    // Пропускаем все пробельные символы подряд
    while let Some(ch) = cur.peek() {
        if !ch.is_whitespace() {
            break;
        }
        cur.bump();
    }

    let end = cur.pos();

    // Если нужно сохранять тривию - публикуем
    if opt.keep_trivia {
        em.trivia(start, end, TriviaKind::Whitespace);
    }
}

/// Пропускает однострочный комментарий от // до конца строки или EOF
fn skip_line_comment(
    cur: &mut Cursor,
    em: &mut Emitter,
    opt: &LexOptions
) {
    let start = cur.pos();

    // Пропускаем маркер начала комментария //
    cur.bump(); // первый /
    cur.bump(); // второй /

    // Пропускаем все символы до конца строки или EOF
    while let Some(ch) = cur.peek() {
        if ch == '\n' {
            cur.bump(); // включаем \n в комментарий
            break;
        }
        cur.bump();
    }

    let end = cur.pos();

    // Если нужно сохранять тривию - публикуем
    if opt.keep_trivia {
        em.trivia(start, end, TriviaKind::LineComment);
    }
}

/// Пропускает многострочный комментарий /* ... */ с поддержкой вложенности
fn skip_block_comment(
    cur: &mut Cursor,
    em: &mut Emitter,
    opt: &LexOptions
) {
    let start = cur.pos();

    // Пропускаем маркер начала комментария /*
    cur.bump(); // первый /
    cur.bump(); // второй /

    let mut depth = 1; // Уровень вложенности комментариев

    // Обрабатываем содержимое комментария
    while !cur.eof() && depth > 0 {
        if cur.starts_with("/*") {
            // Начало вложенного комментария
            depth += 1;
            cur.bump(); // первый /
            cur.bump(); // второй /
        } else if cur.starts_with("*/") {
            // Конец комментария
            depth -= 1;
            cur.bump(); // первый /
            cur.bump(); // второй /
        } else {
            // Обычный символ внутри комментария
            cur.bump();
        }
    }

    let end = cur.pos();

    // Проверяем, закрыт ли комментарий
    if depth > 0 {
        // Незакрытый комментарий до EOF - выдаем диагностику
        let span = Span::new(em.file, start, end);
        em.diag(span, DiagCode::UnclosedBlockComment, "Unclosed block comment");
    }

    // Если нужно сохранять тривию - публикуем
    if opt.keep_trivia {
        em.trivia(start, end, TriviaKind::BlockComment);
    }
}
