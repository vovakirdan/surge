// Импортируем реальный Token из крейта token
pub use surge_token::{Token, SourceId, TokenKind, Span};

// Временные типы для диагностик и тривии - позже будут заменены на реальные
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LexDiag {
    pub span: Span,
    pub message: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TriviaKind {
    Whitespace,
    LineComment,
    BlockComment,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Trivia {
    pub span: Span,
    pub kind: TriviaKind,
}

/// Опции лексического анализа
pub struct LexOptions {
    pub keep_trivia: bool,      // вернуть пробелы/комменты отдельным каналом
    pub enable_directives: bool // активировать /// test:
}

impl Default for LexOptions {
    fn default() -> Self {
        Self {
            keep_trivia: false,
            enable_directives: false,
        }
    }
}

/// Результат лексического анализа
pub struct LexResult {
    pub tokens: Vec<Token>,     // основные токены (без trivia)
    pub trivia: Vec<Trivia>,    // если keep_trivia = true
    pub diags: Vec<LexDiag>,    // диагностические сообщения
}

/// Основная функция лексического анализа
/// Заглушка - реализация будет позже
pub fn lex(_source: &str, _file: SourceId, _opts: &LexOptions) -> LexResult {
    todo!("Lexer implementation is not yet complete")
}
