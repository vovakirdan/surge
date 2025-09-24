use crate::model::{Diagnostic, Severity, Code, SpanRef};
use surge_token::SourceId;

pub fn from_lexer_diags(file: SourceId, diags: &[surge_lexer::LexDiag]) -> Vec<Diagnostic> {
    diags.iter().map(|d| {
        let code = Code(match d.code {
            surge_lexer::DiagCode::UnclosedString => "LEX_UNCLOSED_STRING".into(),
            surge_lexer::DiagCode::BadEscape => "LEX_BAD_ESCAPE".into(),
            surge_lexer::DiagCode::UnclosedBlockComment => "LEX_UNCLOSED_BLOCK_COMMENT".into(),
            surge_lexer::DiagCode::InvalidDigitForBase => "LEX_INVALID_DIGIT_FOR_BASE".into(),
            surge_lexer::DiagCode::UnknownChar => "LEX_UNKNOWN_CHAR".into(),
            surge_lexer::DiagCode::UnknownDirective => "LEX_UNKNOWN_DIRECTIVE".into(),
            surge_lexer::DiagCode::InvalidDirectiveFormat => "LEX_INVALID_DIRECTIVE_FORMAT".into(),
        });
        let span = SpanRef { source: file, start: d.span.start, end: d.span.end };
        Diagnostic {
            severity: Severity::Error,
            code,
            message: d.message.clone(),
            span,
            related: vec![],
        }
    }).collect()
}
