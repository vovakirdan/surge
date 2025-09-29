use crate::{
    LexOptions,
    cursor::Cursor,
    emit::{DiagCode, Emitter},
};
use surge_token::{
    DirectiveKind, Span, TokenContext, TokenKind, lookup_directive_keyword, lookup_keyword,
};

/// Пытается захватить директиву начинающуюся с ///
/// Возвращает Option<u32> с позицией начала директивы, если она найдена
/// Срабатывает только если /// находится в начале строки (после \n или в начале файла)
/// Проверка opt.enable_directives должна выполняться в вызывающем коде
pub fn try_take_directive(cur: &mut Cursor, em: &mut Emitter, opt: &LexOptions) -> Option<u32> {
    // Запоминаем позицию начала для создания span
    let start_pos = cur.pos();

    // Проверяем что текущая позиция начинается с ///
    if !cur.starts_with("///") {
        return None;
    }

    // Проверяем что /// находится в начале строки
    if !cur.is_line_start() {
        return None;
    }

    // Захватываем маркер ///
    cur.bump(); // первый /
    cur.bump(); // второй /
    cur.bump(); // третий /

    // Пропускаем пробелы после ///
    skip_whitespace(cur);

    // Пытаемся определить тип директивы по первой строке
    let directive_kind = match parse_directive_kind(cur, em) {
        Some(kind) => kind,
        None => {
            // Если не удалось распознать директиву - это обычный комментарий
            // Возвращаем курсор в исходное положение
            cur.restore_pos(start_pos as usize);
            return None;
        }
    };

    // Создаем токен начала директивы
    em.token(start_pos, cur.pos(), TokenKind::Directive(directive_kind));

    // Лексируем содержимое директивы как отдельные токены с контекстом
    tokenize_directive_content(cur, em, opt, directive_kind);

    Some(start_pos)
}

/// Пропускает пробельные символы
fn skip_whitespace(cur: &mut Cursor) {
    while let Some(ch) = cur.peek() {
        if !ch.is_whitespace() || ch == '\n' {
            break;
        }
        cur.bump();
    }
}

/// Пытается распознать тип директивы по ключевому слову
fn parse_directive_kind(cur: &mut Cursor, em: &mut Emitter) -> Option<DirectiveKind> {
    let keyword_start = cur.pos();

    // Читаем ключевое слово до двоеточия или пробела
    let mut keyword = String::new();
    while let Some(ch) = cur.peek() {
        if ch == ':' || ch.is_whitespace() || ch == '\n' {
            break;
        }
        keyword.push(ch);
        cur.bump();
    }

    let keyword_end = cur.pos();

    // Проверяем что после ключевого слова идет двоеточие
    skip_whitespace(cur);
    if cur.peek() != Some(':') {
        // Нет двоеточия - выдаем диагностику если есть ключевое слово
        if !keyword.is_empty() {
            let span = Span::new(em.file, keyword_start, keyword_end);
            em.diag(
                span,
                DiagCode::InvalidDirectiveFormat,
                format!("Expected ':' after directive keyword '{}'", keyword),
            );
        }
        return None;
    }
    cur.bump(); // пропускаем ':'

    // Сопоставляем ключевое слово с типом директивы
    match keyword.as_str() {
        "test" => Some(DirectiveKind::Test),
        "benchmark" => Some(DirectiveKind::Benchmark),
        "time" => Some(DirectiveKind::Time),
        "target" => Some(DirectiveKind::Target),
        _ => {
            // Неизвестная директива - выдаем диагностику
            if !keyword.is_empty() {
                let span = Span::new(em.file, keyword_start, keyword_end);
                em.diag(
                    span,
                    DiagCode::UnknownDirective,
                    format!("Unknown directive type '{}'", keyword),
                );
            }
            None
        }
    }
}

/// Лексирует содержимое директивы как отдельные токены
fn tokenize_directive_content(
    cur: &mut Cursor,
    em: &mut Emitter,
    opt: &LexOptions,
    directive_kind: DirectiveKind,
) {
    let context = TokenContext::Directive(directive_kind);

    while !cur.eof() {
        // Пропускаем до конца текущей строки, лексируя содержимое
        tokenize_directive_line(cur, em, opt, context);

        // Ищем следующую строку с содержимым директивы
        if !find_and_consume_next_directive_line(cur, em, opt) {
            break;
        }
    }
}

/// Лексирует одну строку содержимого директивы
fn tokenize_directive_line(
    cur: &mut Cursor,
    em: &mut Emitter,
    _opt: &LexOptions,
    context: TokenContext,
) {
    use crate::rules;

    while !cur.eof() {
        let ch = cur.peek();

        match ch {
            Some('\n') => {
                cur.bump(); // поглощаем \n и заканчиваем строку
                break;
            }
            Some(ch) if ch.is_whitespace() => {
                // Пропускаем пробелы (но не сохраняем их в контексте директивы для простоты)
                cur.bump_while(|c| c.is_whitespace() && c != '\n');
            }
            Some(ch) if ch.is_alphabetic() || ch == '_' => {
                // Идентификатор или ключевое слово директивы
                tokenize_directive_ident(cur, em, context);
            }
            Some(ch) if ch.is_ascii_digit() => {
                // Числовой литерал
                if rules::number::try_take_number(cur, em) {
                    // Обновляем контекст последнего токена
                    if let Some(last_token) = em.tokens.last_mut() {
                        last_token.context = context;
                    }
                }
            }
            Some('"') => {
                // Строковый литерал
                if rules::string::try_take_string(cur, em) {
                    // Обновляем контекст последнего токена
                    if let Some(last_token) = em.tokens.last_mut() {
                        last_token.context = context;
                    }
                }
            }
            Some(_) => {
                // Пытаемся обработать как пунктуацию
                if rules::punct::try_take_multi(cur, em) || rules::punct::try_take_single(cur, em) {
                    // Обновляем контекст последнего токена
                    if let Some(last_token) = em.tokens.last_mut() {
                        last_token.context = context;
                    }
                } else {
                    // Неизвестный символ - пропускаем
                    cur.bump();
                }
            }
            None => {
                // EOF - выходим из цикла
                break;
            }
        }
    }
}

/// Лексирует идентификатор в контексте директивы
fn tokenize_directive_ident(cur: &mut Cursor, em: &mut Emitter, context: TokenContext) {
    let start = cur.pos();

    // Читаем идентификатор
    let mut ident = String::new();
    while let Some(ch) = cur.peek() {
        if ch.is_alphanumeric() || ch == '_' || ch == '.' {
            ident.push(ch);
            cur.bump();
        } else {
            break;
        }
    }

    let end = cur.pos();

    // Проверяем, является ли это ключевым словом директивы или обычным ключевым словом
    let token_kind = if let Some(keyword) = lookup_directive_keyword(&ident) {
        TokenKind::Keyword(keyword)
    } else if let Some(keyword) = lookup_keyword(&ident) {
        TokenKind::Keyword(keyword)
    } else {
        TokenKind::Ident
    };

    em.token_with_context(start, end, token_kind, context);
}

/// Ищет следующую строку с содержимым директивы, пропуская комментарии
/// Возвращает true если нашли строку с ///, false если дошли до конца директивы
fn find_and_consume_next_directive_line(
    cur: &mut Cursor,
    _em: &mut Emitter,
    _opt: &LexOptions,
) -> bool {
    while !cur.eof() {
        // Пропускаем пробелы в начале строки
        skip_line_whitespace(cur);

        if cur.starts_with("///") {
            // Нашли продолжение директивы
            cur.bump(); // /
            cur.bump(); // /
            cur.bump(); // /
            return true;
        } else if cur.starts_with("//") || cur.starts_with("/*") {
            // Это комментарий - пропускаем его полностью
            skip_comment_line(cur);
            continue;
        } else {
            // Это не директива и не комментарий - конец блока директивы
            return false;
        }
    }
    false
}

/// Пропускает пробелы в начале строки (но не переводы строк)
fn skip_line_whitespace(cur: &mut Cursor) {
    while let Some(ch) = cur.peek() {
        if ch == '\n' {
            cur.bump();
            // После \n продолжаем пропускать пробелы новой строки
            continue;
        } else if ch.is_whitespace() {
            cur.bump();
        } else {
            break;
        }
    }
}

/// Пропускает строку с комментарием
fn skip_comment_line(cur: &mut Cursor) {
    if cur.starts_with("//") {
        // Однострочный комментарий - пропускаем до конца строки
        while let Some(ch) = cur.peek() {
            cur.bump();
            if ch == '\n' {
                break;
            }
        }
    } else if cur.starts_with("/*") {
        // Многострочный комментарий - пропускаем до */
        cur.bump(); // /
        cur.bump(); // *
        let mut depth = 1;

        while !cur.eof() && depth > 0 {
            if cur.starts_with("/*") {
                depth += 1;
                cur.bump();
                cur.bump();
            } else if cur.starts_with("*/") {
                depth -= 1;
                cur.bump();
                cur.bump();
            } else {
                cur.bump();
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{LexOptions, emit::DiagCode};
    use surge_token::SourceId;

    #[test]
    fn test_parse_test_directive() {
        let source = "/// test:\n/// Test1:\n/// test.equal(add(2, 3), 5);";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter, &opts);
        assert!(result.is_some());

        // Должен быть токен директивы + токены содержимого
        assert!(emitter.tokens.len() > 1);

        // Первый токен - это токен директивы
        let directive_token = &emitter.tokens[0];
        assert_eq!(
            directive_token.kind,
            TokenKind::Directive(DirectiveKind::Test)
        );
        assert_eq!(directive_token.context, TokenContext::Normal);

        // Проверяем, что есть токены с контекстом директивы
        let directive_content_tokens: Vec<_> = emitter
            .tokens
            .iter()
            .filter(|t| matches!(t.context, TokenContext::Directive(DirectiveKind::Test)))
            .collect();
        assert!(!directive_content_tokens.is_empty());

        // Проверяем наличие ключевого слова test.equal
        let test_equal_tokens: Vec<_> = emitter
            .tokens
            .iter()
            .filter(|t| matches!(t.kind, TokenKind::Keyword(surge_token::Keyword::TestEqual)))
            .collect();
        assert_eq!(test_equal_tokens.len(), 1);
    }

    #[test]
    fn test_parse_benchmark_directive() {
        let source = "/// benchmark:\n/// Benchmark1:\n///   benchmark.measure(add(2, 3), 5);";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter, &opts);
        assert!(result.is_some());
        assert!(emitter.tokens.len() > 1);

        let directive_token = &emitter.tokens[0];
        assert_eq!(
            directive_token.kind,
            TokenKind::Directive(DirectiveKind::Benchmark)
        );

        // Проверяем ключевое слово benchmark.measure
        let benchmark_measure_tokens: Vec<_> = emitter
            .tokens
            .iter()
            .filter(|t| {
                matches!(
                    t.kind,
                    TokenKind::Keyword(surge_token::Keyword::BenchmarkMeasure)
                )
            })
            .collect();
        assert_eq!(benchmark_measure_tokens.len(), 1);
    }

    #[test]
    fn test_parse_time_directive() {
        let source = "/// time:\n/// Time1:\n///   time.measure(add(2, 3), 5);";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter, &opts);
        assert!(result.is_some());
        assert!(emitter.tokens.len() > 1);

        let directive_token = &emitter.tokens[0];
        assert_eq!(
            directive_token.kind,
            TokenKind::Directive(DirectiveKind::Time)
        );

        // Проверяем ключевое слово time.measure
        let time_measure_tokens: Vec<_> = emitter
            .tokens
            .iter()
            .filter(|t| {
                matches!(
                    t.kind,
                    TokenKind::Keyword(surge_token::Keyword::TimeMeasure)
                )
            })
            .collect();
        assert_eq!(time_measure_tokens.len(), 1);
    }

    #[test]
    fn test_directive_with_comments() {
        let source = "/// test:\n// this comment should be skipped\n/// let a = 5;\n/* this comment should be skipped too /// <- and this too */\n/// test.equal(a, 5);";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter, &opts);
        assert!(result.is_some());
        assert!(emitter.tokens.len() > 1);

        let directive_token = &emitter.tokens[0];
        assert_eq!(
            directive_token.kind,
            TokenKind::Directive(DirectiveKind::Test)
        );
    }

    #[test]
    fn test_invalid_directive_not_parsed() {
        let source = "/// invalid_directive:\n/// some content";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter, &opts);
        assert!(result.is_none());
        assert_eq!(emitter.tokens.len(), 0);
    }

    #[test]
    fn test_triple_slash_not_at_line_start() {
        let source = "  /// test:\n/// some content";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter, &opts);
        assert!(result.is_none());
        assert_eq!(emitter.tokens.len(), 0);
    }

    #[test]
    fn test_directive_without_colon() {
        let source = "/// test\n/// some content";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter, &opts);
        assert!(result.is_none());
        assert_eq!(emitter.tokens.len(), 0);
        assert_eq!(emitter.diags.len(), 1);
        assert_eq!(emitter.diags[0].code, DiagCode::InvalidDirectiveFormat);
    }

    #[test]
    fn test_unknown_directive_with_diagnostic() {
        let source = "/// unknown_directive:\n/// some content";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter, &opts);
        assert!(result.is_none());
        assert_eq!(emitter.tokens.len(), 0);
        assert_eq!(emitter.diags.len(), 1);
        assert_eq!(emitter.diags[0].code, DiagCode::UnknownDirective);
        assert!(emitter.diags[0].message.contains("unknown_directive"));
    }

    #[test]
    fn test_complex_directive_tokenization() {
        let source =
            "/// test:\n/// let result = test.equal(add(2, 3), 5);\n/// test.assert(result);";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter, &opts);
        assert!(result.is_some());

        // Проверяем что у нас есть различные типы токенов
        let directive_tokens: Vec<_> = emitter
            .tokens
            .iter()
            .filter(|t| matches!(t.context, TokenContext::Directive(DirectiveKind::Test)))
            .collect();

        // Должны быть токены: let, result, =, test.equal, (, add, (, 2, ,, 3, ), ,, 5, ), ;, test.assert, (, result, ), ;
        assert!(directive_tokens.len() > 10);

        // Проверяем наличие ключевых слов директивы
        let test_equal_count = emitter
            .tokens
            .iter()
            .filter(|t| matches!(t.kind, TokenKind::Keyword(surge_token::Keyword::TestEqual)))
            .count();
        assert_eq!(test_equal_count, 1);

        let test_assert_count = emitter
            .tokens
            .iter()
            .filter(|t| matches!(t.kind, TokenKind::Keyword(surge_token::Keyword::TestAssert)))
            .count();
        assert_eq!(test_assert_count, 1);

        // Проверяем наличие обычных ключевых слов (let)
        let let_count = emitter
            .tokens
            .iter()
            .filter(|t| matches!(t.kind, TokenKind::Keyword(surge_token::Keyword::Let)))
            .count();
        assert_eq!(let_count, 1);

        // Проверяем наличие литералов
        let number_count = emitter
            .tokens
            .iter()
            .filter(|t| matches!(t.kind, TokenKind::IntLit))
            .count();
        assert_eq!(number_count, 3); // 2, 3, и 5
    }
}
