use crate::keyword::Keyword;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum TokenKind {
    Ident,
    Keyword(Keyword),

    // literals
    IntLit, // 0, 123, 0xFF, 0b1010, 1_000
    FloatLit, // 1.0, 0.5, 1e-9, 2.5e+10
    StringLit, // "..."
    // bool is a keyword

    // markers
    Amp, // &
    Star, // *
    LBracket, RBracket, // [ ]
    LParen, RParen, // ( )
    LBrace, RBrace, // { }
    Comma, Semicolon, Colon, Dot, // , : .
    ThinArrow, // ->
    FatArrow, // =>
    Ellipsis, // ...
    PathSep, // ::
    AndAnd, OrOr, Not, // && || !
    Eq, EqEq, Ne, // = == !=
    Lt, Le, Gt, Ge, // < <= > >=
    Plus, Minus, Slash, Percent, // + - / %
    PlusEq, MinusEq, StarEq, SlashEq, PercentEq, // += -= *= /= %=
    At, // @
    Eof,
}