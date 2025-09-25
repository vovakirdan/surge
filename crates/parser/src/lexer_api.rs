//! Thin wrapper around the token stream produced by the lexer.

use surge_token::{Span, Token, TokenKind};

/// Provides cursor-like access to a slice of tokens.
pub struct Stream<'src> {
    tokens: &'src [Token],
    src: Option<&'src str>,
    idx: usize,
}

impl<'src> Stream<'src> {
    /// Create a new stream over the given token slice.
    pub fn new(tokens: &'src [Token], src: Option<&'src str>) -> Self {
        Self {
            tokens,
            src,
            idx: 0,
        }
    }

    /// Peek at the current token without consuming it.
    #[inline]
    pub fn peek(&self) -> Token {
        self.nth(0)
    }

    /// Peek at the nth token ahead (0-based).
    #[inline]
    pub fn nth(&self, n: usize) -> Token {
        self.tokens
            .get(self.idx + n)
            .copied()
            .unwrap_or_else(|| *self.tokens.last().expect("token stream must end with EOF"))
    }

    /// Return the previously consumed token if any.
    #[inline]
    pub fn previous(&self) -> Option<Token> {
        if self.idx == 0 {
            None
        } else {
            Some(self.tokens[self.idx - 1])
        }
    }

    /// Return the token `n` steps before the most recently consumed one.
    #[inline]
    pub fn previous_n(&self, n: usize) -> Option<Token> {
        if self.idx <= n + 1 {
            None
        } else {
            Some(self.tokens[self.idx - 1 - n])
        }
    }

    /// Consume and return the current token.
    #[inline]
    pub fn bump(&mut self) -> Token {
        let tok = self.peek();
        if !matches!(tok.kind, TokenKind::Eof) {
            self.idx = (self.idx + 1).min(self.tokens.len());
        }
        tok
    }

    /// Consume the current token if it matches `kind`.
    pub fn eat(&mut self, kind: TokenKind) -> Option<Token> {
        if self.peek().kind == kind {
            Some(self.bump())
        } else {
            None
        }
    }

    /// Check whether the current token is of the specified kind.
    #[inline]
    pub fn at(&self, kind: TokenKind) -> bool {
        self.peek().kind == kind
    }

    /// Marker indicating the cursor has reached EOF.
    #[inline]
    pub fn is_eof(&self) -> bool {
        matches!(self.peek().kind, TokenKind::Eof)
    }

    /// Extract the source slice for the provided span if the underlying source is available.
    pub fn slice(&self, span: Span) -> Option<&'src str> {
        let src = self.src?;
        let start = span.start as usize;
        let end = span.end as usize;
        if start <= end && end <= src.len() {
            Some(&src[start..end])
        } else {
            None
        }
    }

    /// Extract text for a span; returns empty string when the original source is not available.
    pub fn text(&self, span: Span) -> String {
        self.slice(span).unwrap_or("").to_string()
    }
}
