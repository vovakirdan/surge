use crate::LexOptions;
use surge_token::{SourceId, Span, Token, TokenContext, TokenKind};

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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LexDiag {
    pub span: Span,
    pub code: DiagCode,
    pub message: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DiagCode {
    UnclosedString,
    BadEscape,
    UnclosedBlockComment,
    InvalidDigitForBase,
    UnknownChar,
    UnknownDirective,
    InvalidDirectiveFormat,
}

pub struct Emitter {
    pub file: SourceId,
    pub tokens: Vec<Token>,
    pub trivia: Vec<Trivia>,
    pub diags: Vec<LexDiag>,
    keep_trivia: bool,
}

impl Emitter {
    /// Создать emitter поверх курсора
    pub fn new(file: SourceId, opts: &LexOptions) -> Self {
        Self {
            file,
            tokens: Vec::new(),
            trivia: Vec::new(),
            diags: Vec::new(),
            keep_trivia: opts.keep_trivia,
        }
    }

    /// Эмит токена [start..end). Предусловие: start/end — корректные байтовые оффсеты
    pub fn token(&mut self, start: u32, end: u32, kind: TokenKind) {
        let span = Span::new(self.file, start, end);
        let token = Token::new(kind, span);
        self.tokens.push(token);
    }

    /// Эмит токена с контекстом [start..end). Предусловие: start/end — корректные байтовые оффсеты
    pub fn token_with_context(
        &mut self,
        start: u32,
        end: u32,
        kind: TokenKind,
        context: TokenContext,
    ) {
        let span = Span::new(self.file, start, end);
        let token = Token::new_with_context(kind, span, context);
        self.tokens.push(token);
    }

    /// Эмит тривии
    pub fn trivia(&mut self, start: u32, end: u32, kind: TriviaKind) {
        // Только если включена опция keep_trivia
        if !self.keep_trivia {
            return;
        }

        // Не эмитим пустые спаны
        if start == end {
            return;
        }

        let span = Span::new(self.file, start, end);
        let trivia = Trivia { span, kind };
        self.trivia.push(trivia);
    }

    /// Эмит диагностики
    pub fn diag(&mut self, span: Span, code: DiagCode, msg: impl Into<String>) {
        let message = msg.into();
        let diag = LexDiag {
            span,
            code,
            message,
        };
        self.diags.push(diag);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::LexOptions;

    #[test]
    fn test_emitter_token_creation() {
        let file = SourceId(0);
        let opts = LexOptions::default();
        let mut emitter = Emitter::new(file, &opts);

        // Создаем токен
        emitter.token(0, 4, TokenKind::Ident);

        assert_eq!(emitter.tokens.len(), 1);
        let token = &emitter.tokens[0];
        assert_eq!(token.kind, TokenKind::Ident);
        assert_eq!(token.span.start, 0);
        assert_eq!(token.span.end, 4);
    }

    #[test]
    fn test_emitter_trivia_with_keep_trivia() {
        let file = SourceId(0);
        let opts = LexOptions {
            keep_trivia: true,
            enable_directives: false,
        };
        let mut emitter = Emitter::new(file, &opts);

        // Эмитим тривию
        emitter.trivia(0, 3, TriviaKind::Whitespace);

        assert_eq!(emitter.trivia.len(), 1);
        let trivia = &emitter.trivia[0];
        assert_eq!(trivia.kind, TriviaKind::Whitespace);
        assert_eq!(trivia.span.start, 0);
        assert_eq!(trivia.span.end, 3);
    }

    #[test]
    fn test_emitter_trivia_without_keep_trivia() {
        let file = SourceId(0);
        let opts = LexOptions {
            keep_trivia: false,
            enable_directives: false,
        };
        let mut emitter = Emitter::new(file, &opts);

        // Эмитим тривию
        emitter.trivia(0, 3, TriviaKind::Whitespace);

        // Тривия не должна сохраниться
        assert_eq!(emitter.trivia.len(), 0);
    }

    #[test]
    fn test_emitter_trivia_empty_span() {
        let file = SourceId(0);
        let opts = LexOptions {
            keep_trivia: true,
            enable_directives: false,
        };
        let mut emitter = Emitter::new(file, &opts);

        // Эмитим пустую тривию
        emitter.trivia(0, 0, TriviaKind::Whitespace);

        // Пустая тривия не должна сохраниться
        assert_eq!(emitter.trivia.len(), 0);
    }

    #[test]
    fn test_emitter_diag() {
        let file = SourceId(0);
        let opts = LexOptions::default();
        let mut emitter = Emitter::new(file, &opts);

        let span = Span::new(SourceId(0), 0, 4);

        // Эмитим диагностику
        emitter.diag(span, DiagCode::UnknownChar, "Unknown character");

        assert_eq!(emitter.diags.len(), 1);
        let diag = &emitter.diags[0];
        assert_eq!(diag.code, DiagCode::UnknownChar);
        assert_eq!(diag.message, "Unknown character");
        assert_eq!(diag.span.start, 0);
        assert_eq!(diag.span.end, 4);
    }
}
