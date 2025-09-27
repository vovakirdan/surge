//! Synchronisation helpers for parser error recovery.

use surge_token::{Keyword, TokenKind};

/// Statement-level synchronisation tokens (`;`, `}`, EOF).
pub fn is_stmt_sync(tok: &TokenKind) -> bool {
    matches!(
        tok,
        TokenKind::Semicolon | TokenKind::RBrace | TokenKind::Eof
    )
}

/// Top-level synchronisation tokens include statement sync plus item-introducing keywords.
pub fn is_top_level_sync(tok: &TokenKind) -> bool {
    if is_stmt_sync(tok) {
        return true;
    }
    matches!(
        tok,
        TokenKind::Keyword(Keyword::Fn)
            | TokenKind::Keyword(Keyword::Type)
            | TokenKind::Keyword(Keyword::Extern)
            | TokenKind::Keyword(Keyword::Literal)
            | TokenKind::Keyword(Keyword::Alias)
            | TokenKind::Keyword(Keyword::Import)
    )
}
