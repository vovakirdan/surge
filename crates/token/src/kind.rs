use std::sync::Arc;

use crate::keyword::Keyword;

/// Типы директив для компилятора. `Custom` отмечает пользовательские пространства имён.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum DirectiveKind {
    Test,      // /// test:
    Benchmark, // /// benchmark:
    Time,      // /// time:
    Target,    // /// target:
    Custom,    // /// <ns>[:<subns>]:
}

/// Имя директивы в формате `<namespace>` или `<namespace>:<sub_namespace>`.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct DirectiveName {
    pub namespace: String,
    pub sub_namespace: Option<String>,
}

impl DirectiveName {
    /// Возвращает «сырой» идентификатор пространства имён (например, `lint:deadcode`).
    pub fn raw(&self) -> String {
        match &self.sub_namespace {
            Some(sub) => format!("{}:{}", self.namespace, sub),
            None => self.namespace.clone(),
        }
    }
}

/// Метаданные директивы, которые лексер прикрепляет к токену `TokenKind::Directive`.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct DirectiveSpec {
    pub kind: DirectiveKind,
    pub name: DirectiveName,
    /// True если лексер обнаружил завершающее `:` после имени директивы.
    pub has_trailing_colon: bool,
}

impl DirectiveSpec {
    #[inline]
    pub fn namespace(&self) -> &str {
        &self.name.namespace
    }

    #[inline]
    pub fn sub_namespace(&self) -> Option<&str> {
        self.name.sub_namespace.as_deref()
    }

    #[inline]
    pub fn raw_name(&self) -> String {
        self.name.raw()
    }

    #[inline]
    pub fn is_builtin(&self) -> bool {
        !matches!(self.kind, DirectiveKind::Custom)
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum TokenKind {
    Ident,
    Keyword(Keyword),

    // literals
    IntLit,    // 0, 123, 0xFF, 0b1010, 1_000
    FloatLit,  // 1.0, 0.5, 1e-9, 2.5e+10
    StringLit, // "..."
    // bool is a keyword

    // markers
    Amp,   // &
    Star,  // *
    Pipe,  // |
    Caret, // ^

    LBracket,
    RBracket, // [ ]
    LParen,
    RParen, // ( )
    LBrace,
    RBrace, // { }
    LAngle,
    RAngle, // < >

    Comma,
    Semicolon,
    Colon,
    ColonEq, // :=

    Dot,      // .
    DotDot,   // ..
    DotDotEq, // ..=
    Ellipsis, // ...

    PathSep,   // ::
    ThinArrow, // ->
    FatArrow,  // =>

    AndAnd,
    OrOr,
    Not, // && || !

    Eq,
    EqEq,
    Ne,
    Le,
    Ge,

    Plus,
    Minus,
    Slash,
    Percent,

    PlusEq,
    MinusEq,
    StarEq,
    SlashEq,
    PercentEq,
    AmpEq,
    PipeEq,
    CaretEq,
    Shl,
    Shr,
    ShlEq,
    ShrEq,

    Question,
    QuestionQuestion,
    At,

    Directive(Arc<DirectiveSpec>), // ///
    Eof,
}
