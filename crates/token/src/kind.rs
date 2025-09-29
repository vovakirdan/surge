use crate::keyword::Keyword;

/// Типы директив для компилятора
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum DirectiveKind {
    Test,      // /// test:
    Benchmark, // /// benchmark:
    Time,      // /// time:
    Target,    // /// target:
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
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

    Directive(DirectiveKind), // ///
    Eof,
}
