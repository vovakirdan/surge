use crate::{cursor::Cursor, emit::{Emitter, DiagCode}};
use surge_token::{DirectiveKind, Span, TokenKind};

/// Пытается захватить директиву начинающуюся с ///
/// Возвращает Option<u32> с позицией начала директивы, если она найдена
/// Срабатывает только если /// находится в начале строки (после \n или в начале файла)
/// Проверка opt.enable_directives должна выполняться в вызывающем коде
pub fn try_take_directive(cur: &mut Cursor, em: &mut Emitter) -> Option<u32> {
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

    // Собираем содержимое директивы до конца блока
    collect_directive_content(cur);

    let end_pos = cur.pos();

    // Создаем токен директивы
    em.token(start_pos, end_pos, TokenKind::Directive(directive_kind));

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

/// Собирает содержимое директивы до конца блока
fn collect_directive_content(cur: &mut Cursor) {
    while !cur.eof() {
        // Пропускаем до конца текущей строки
        while let Some(ch) = cur.peek() {
            if ch == '\n' {
                cur.bump(); // включаем \n
                break;
            }
            cur.bump();
        }
        
        // Ищем следующую строку с содержимым директивы
        if !find_and_consume_next_directive_line(cur) {
            break;
        }
    }
}

/// Ищет следующую строку с содержимым директивы, пропуская комментарии
/// Возвращает true если нашли строку с ///, false если дошли до конца директивы
fn find_and_consume_next_directive_line(cur: &mut Cursor) -> bool {
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

        let result = try_take_directive(&mut cursor, &mut emitter);
        assert!(result.is_some());
        assert_eq!(emitter.tokens.len(), 1);
        
        let token = &emitter.tokens[0];
        assert_eq!(token.kind, TokenKind::Directive(DirectiveKind::Test));
    }

    #[test]
    fn test_parse_benchmark_directive() {
        let source = "/// benchmark:\n/// a:int = random.int(); b:int = random.int();\n/// repeat(1000, add(a, b));";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter);
        assert!(result.is_some());
        assert_eq!(emitter.tokens.len(), 1);
        
        let token = &emitter.tokens[0];
        assert_eq!(token.kind, TokenKind::Directive(DirectiveKind::Benchmark));
    }

    #[test]
    fn test_parse_time_directive() {
        let source = "/// time:\n/// for i:int in [1, 2, 3] { add(1, 2); }";
        let file = SourceId(0);
        let mut cursor = Cursor::new(source, file);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: true,
        };
        let mut emitter = Emitter::new(file, &opts);

        let result = try_take_directive(&mut cursor, &mut emitter);
        assert!(result.is_some());
        assert_eq!(emitter.tokens.len(), 1);
        
        let token = &emitter.tokens[0];
        assert_eq!(token.kind, TokenKind::Directive(DirectiveKind::Time));
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

        let result = try_take_directive(&mut cursor, &mut emitter);
        assert!(result.is_some());
        assert_eq!(emitter.tokens.len(), 1);
        
        let token = &emitter.tokens[0];
        assert_eq!(token.kind, TokenKind::Directive(DirectiveKind::Test));
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

        let result = try_take_directive(&mut cursor, &mut emitter);
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

        let result = try_take_directive(&mut cursor, &mut emitter);
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

        let result = try_take_directive(&mut cursor, &mut emitter);
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

        let result = try_take_directive(&mut cursor, &mut emitter);
        assert!(result.is_none());
        assert_eq!(emitter.tokens.len(), 0);
        assert_eq!(emitter.diags.len(), 1);
        assert_eq!(emitter.diags[0].code, DiagCode::UnknownDirective);
        assert!(emitter.diags[0].message.contains("unknown_directive"));
    }
}

