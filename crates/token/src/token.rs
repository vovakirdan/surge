use crate::{DirectiveKind, Keyword, Span, TokenKind};

/// Контекст токена - указывает, находится ли токен внутри директивы
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub enum TokenContext {
    Normal,                   // обычный токен
    Directive(DirectiveKind), // токен внутри директивы определенного типа
}

#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub struct Token {
    pub kind: TokenKind,
    pub span: Span,
    pub context: TokenContext,
}

impl Token {
    pub fn new(kind: TokenKind, span: Span) -> Self {
        Self {
            kind,
            span,
            context: TokenContext::Normal,
        }
    }

    pub fn new_with_context(kind: TokenKind, span: Span, context: TokenContext) -> Self {
        Self {
            kind,
            span,
            context,
        }
    }

    #[inline]
    pub fn is(&self, kind: TokenKind) -> bool {
        self.kind == kind
    }

    #[inline]
    pub fn keyword(&self) -> Option<Keyword> {
        match self.kind {
            TokenKind::Keyword(kw) => Some(kw),
            _ => None,
        }
    }

    #[inline]
    pub fn is_keyword(&self, kw: Keyword) -> bool {
        matches!(self.kind, TokenKind::Keyword(actual) if actual == kw)
    }
}
