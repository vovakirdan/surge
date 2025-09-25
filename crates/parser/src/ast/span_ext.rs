//! Convenience helpers for manipulating [`Span`] ranges.

use surge_token::Span;

/// Extension trait with span utilities used across the parser.
pub trait SpanExt {
    /// Combine two spans that belong to the same [`SourceId`].
    fn join(&self, other: Span) -> Span;
}

impl SpanExt for Span {
    fn join(&self, other: Span) -> Span {
        debug_assert_eq!(
            self.source, other.source,
            "attempt to merge spans from different sources"
        );
        let start = self.start.min(other.start);
        let end = self.end.max(other.end);
        Span::new(self.source, start, end)
    }
}
