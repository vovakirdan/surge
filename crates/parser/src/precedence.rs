//! Pratt parser binding power table for Surge expressions.

use surge_token::TokenKind;

/// Return left/right binding powers for infix operators.
pub fn infix_binding_power(kind: &TokenKind) -> Option<(u8, u8)> {
    const PRODUCT: u8 = 70;
    const SUM: u8 = 60;
    const COMPARE: u8 = 50;
    const AND: u8 = 40;
    const OR: u8 = 30;
    const ASSIGN: u8 = 10;

    match kind {
        TokenKind::Star | TokenKind::Slash | TokenKind::Percent => Some((PRODUCT, PRODUCT + 1)),
        TokenKind::Plus | TokenKind::Minus => Some((SUM, SUM + 1)),
        TokenKind::LAngle => Some((COMPARE, COMPARE + 1)),
        TokenKind::RAngle => Some((COMPARE, COMPARE + 1)),
        TokenKind::Le | TokenKind::Ge | TokenKind::EqEq | TokenKind::Ne => {
            Some((COMPARE, COMPARE + 1))
        }
        TokenKind::AndAnd => Some((AND, AND + 1)),
        TokenKind::OrOr => Some((OR, OR + 1)),
        TokenKind::Eq => Some((ASSIGN, ASSIGN)),
        _ => None,
    }
}
