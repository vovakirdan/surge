//! Pratt parser binding power table for Surge expressions.

use surge_token::{Keyword, TokenKind};

/// Return left/right binding powers for infix operators.
pub fn infix_binding_power(kind: &TokenKind) -> Option<(u8, u8)> {
    const PRODUCT: u8 = 90;
    const SUM: u8 = 80;
    const SHIFT: u8 = 70;
    const BITWISE: u8 = 60;
    const RANGE: u8 = 55;
    const COMPARE: u8 = 50;
    const AND: u8 = 40;
    const OR: u8 = 30;
    const COALESCE: u8 = 20;
    const ASSIGN: u8 = 10;

    match kind {
        TokenKind::Star | TokenKind::Slash | TokenKind::Percent => Some((PRODUCT, PRODUCT + 1)),
        TokenKind::Plus | TokenKind::Minus => Some((SUM, SUM + 1)),
        TokenKind::Shl | TokenKind::Shr => Some((SHIFT, SHIFT + 1)),
        TokenKind::Amp | TokenKind::Caret | TokenKind::Pipe => Some((BITWISE, BITWISE + 1)),
        TokenKind::DotDot | TokenKind::DotDotEq => Some((RANGE, RANGE + 1)),
        TokenKind::LAngle
        | TokenKind::RAngle
        | TokenKind::Le
        | TokenKind::Ge
        | TokenKind::EqEq
        | TokenKind::Ne
        | TokenKind::Keyword(Keyword::Is) => Some((COMPARE, COMPARE + 1)),
        TokenKind::AndAnd => Some((AND, AND + 1)),
        TokenKind::OrOr => Some((OR, OR + 1)),
        TokenKind::QuestionQuestion => Some((COALESCE, COALESCE + 1)),
        TokenKind::Eq
        | TokenKind::PlusEq
        | TokenKind::MinusEq
        | TokenKind::StarEq
        | TokenKind::SlashEq
        | TokenKind::PercentEq
        | TokenKind::AmpEq
        | TokenKind::PipeEq
        | TokenKind::CaretEq
        | TokenKind::ShlEq
        | TokenKind::ShrEq => Some((ASSIGN, ASSIGN)),
        _ => None,
    }
}
