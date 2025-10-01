//! Parser diagnostics emitted when syntax errors are encountered.

use surge_token::Span;

/// Parser diagnostic codes covering syntax and recovery failures.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ParseCode {
    UnclosedParen,
    UnclosedBrace,
    UnclosedBracket,
    MissingSemicolon,
    MissingColonInType,
    LetMissingEquals,
    SignalMissingAssign,
    AssignmentWithoutLhs,
    UnexpectedPrimary,
    ForInMissingType,
    ForInMissingIn,
    ForInMissingExpr,
    CompareMissingArrow,
    CompareMissingExpr,
    CompareMissingBrace,
    ParallelMissingWith,
    ParallelMissingFatArrow,
    ParallelBadHeader,
    FatArrowOutsideParallel,
    ParallelFuncNotPure,
    UnknownAttribute,
    ExpectedTypeAfterArrow,
    MissingReturnType,
    ExternGenericBrackets,
    ExternMissingType,
    ExternUnclosedBlock,
    UnexpectedToken,
    InvalidArraySyntax,
    IncompleteFunction,
    Recoverable,
}

/// Parser diagnostic with message and related spans.
#[derive(Debug, Clone)]
pub struct ParseDiag {
    pub code: ParseCode,
    pub span: Span,
    pub message: String,
    pub related: Vec<(Span, String)>,
}

impl ParseDiag {
    /// Construct a new parser diagnostic entry.
    pub fn new(code: ParseCode, span: Span, message: impl Into<String>) -> Self {
        Self {
            code,
            span,
            message: message.into(),
            related: Vec::new(),
        }
    }

    /// Attach a related span/message pair to the diagnostic.
    pub fn with_related(mut self, span: Span, message: impl Into<String>) -> Self {
        self.related.push((span, message.into()));
        self
    }
}
