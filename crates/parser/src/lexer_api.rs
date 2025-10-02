//! Thin wrapper around the token stream produced by the lexer.

use surge_token::{Keyword, Span, Token, TokenKind};

/// Provides cursor-like access to a slice of tokens.
pub struct Stream<'src> {
    tokens: &'src [Token],
    src: Option<&'src str>,
    idx: usize,
}

/// Snapshot of the current cursor position in the token stream.
#[allow(dead_code)] // Will be used by speculative parses (for/extern) in upcoming iterations.
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub struct Checkpoint {
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

    /// Peek at the keyword of the current token, if any.
    #[inline]
    pub fn peek_keyword(&self) -> Option<Keyword> {
        self.peek().keyword()
    }

    /// Peek at the nth token ahead (0-based).
    #[inline]
    pub fn nth(&self, n: usize) -> Token {
        self.tokens.get(self.idx + n).cloned().unwrap_or_else(|| {
            self.tokens
                .last()
                .cloned()
                .expect("token stream must end with EOF")
        })
    }

    /// Return the previously consumed token if any.
    #[inline]
    pub fn previous(&self) -> Option<Token> {
        if self.idx == 0 {
            None
        } else {
            Some(self.tokens[self.idx - 1].clone())
        }
    }

    /// Return the token `n` steps before the most recently consumed one.
    #[inline]
    pub fn previous_n(&self, n: usize) -> Option<Token> {
        if self.idx <= n + 1 {
            None
        } else {
            Some(self.tokens[self.idx - 1 - n].clone())
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

    /// Consume the current token when it is the requested keyword.
    pub fn eat_keyword(&mut self, kw: Keyword) -> Option<Token> {
        if self.peek().is_keyword(kw) {
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

    /// Check whether the current token is the requested keyword.
    #[inline]
    pub fn at_keyword(&self, kw: Keyword) -> bool {
        self.peek().is_keyword(kw)
    }

    /// Marker indicating the cursor has reached EOF.
    #[inline]
    pub fn is_eof(&self) -> bool {
        matches!(self.peek().kind, TokenKind::Eof)
    }

    /// Create a checkpoint for speculative parsing.
    #[inline]
    #[allow(dead_code)] // Speculative constructs rely on this; parser support is being staged.
    pub fn checkpoint(&self) -> Checkpoint {
        Checkpoint { idx: self.idx }
    }

    /// Rewind the stream back to a previously created checkpoint.
    #[allow(dead_code)] // Speculative constructs rely on this; parser support is being staged.
    #[inline]
    pub fn rewind(&mut self, checkpoint: Checkpoint) {
        self.idx = checkpoint.idx.min(self.tokens.len().saturating_sub(1));
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
    #[allow(dead_code)]
    pub fn text(&self, span: Span) -> String {
        self.slice(span).unwrap_or("").to_string()
    }
}
