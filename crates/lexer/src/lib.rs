// Модули
pub mod cursor;
pub mod emit;
pub mod rules;

// Импортируем реальный Token из крейта token
pub use surge_token::{SourceId, Span, Token, TokenKind};

// Реэкспортируем типы из emit для удобства
pub use emit::{DiagCode, LexDiag, Trivia, TriviaKind};

/// Опции лексического анализа
pub struct LexOptions {
    pub keep_trivia: bool,       // вернуть пробелы/комменты отдельным каналом
    pub enable_directives: bool, // активировать /// test:
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
    pub tokens: Vec<Token>,  // основные токены (без trivia)
    pub trivia: Vec<Trivia>, // если keep_trivia = true
    pub diags: Vec<LexDiag>, // диагностические сообщения
}

/// Основная функция лексического анализа
pub fn lex(source: &str, file: SourceId, opts: &LexOptions) -> LexResult {
    use crate::cursor::Cursor;
    use crate::emit::Emitter;
    use crate::rules;
    use surge_token::TokenKind;

    let mut cur = Cursor::new(source, file);
    let mut em = Emitter::new(file, opts);

    while rules::next_token(&mut cur, &mut em, opts) {}

    em.token(cur.pos(), cur.pos(), TokenKind::Eof);

    LexResult {
        tokens: em.tokens,
        trivia: em.trivia,
        diags: em.diags,
    }
}
