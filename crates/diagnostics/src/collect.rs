use crate::model::{Code, Diagnostic, Related, Severity, SpanRef};
use surge_token::SourceId;

pub fn from_lexer_diags(file: SourceId, diags: &[surge_lexer::LexDiag]) -> Vec<Diagnostic> {
    diags
        .iter()
        .map(|d| {
            let code = Code(match d.code {
                surge_lexer::DiagCode::UnclosedString => "LEX_UNCLOSED_STRING".into(),
                surge_lexer::DiagCode::BadEscape => "LEX_BAD_ESCAPE".into(),
                surge_lexer::DiagCode::UnclosedBlockComment => "LEX_UNCLOSED_BLOCK_COMMENT".into(),
                surge_lexer::DiagCode::InvalidDigitForBase => "LEX_INVALID_DIGIT_FOR_BASE".into(),
                surge_lexer::DiagCode::UnknownChar => "LEX_UNKNOWN_CHAR".into(),
                surge_lexer::DiagCode::UnknownDirective => "LEX_UNKNOWN_DIRECTIVE".into(),
                surge_lexer::DiagCode::InvalidDirectiveFormat => {
                    "LEX_INVALID_DIRECTIVE_FORMAT".into()
                }
            });
            let span = SpanRef {
                source: file,
                start: d.span.start,
                end: d.span.end,
            };
            Diagnostic {
                severity: Severity::Error,
                code,
                message: d.message.clone(),
                span,
                related: vec![],
            }
        })
        .collect()
}

pub fn from_parser_diags(file: SourceId, diags: &[surge_parser::ParseDiag]) -> Vec<Diagnostic> {
    diags
        .iter()
        .map(|d| {
            let code = Code(match d.code {
                surge_parser::ParseCode::UnclosedParen => "PARSE_UNCLOSED_PAREN".into(),
                surge_parser::ParseCode::UnclosedBrace => "PARSE_UNCLOSED_BRACE".into(),
                surge_parser::ParseCode::UnclosedBracket => "PARSE_UNCLOSED_BRACKET".into(),
                surge_parser::ParseCode::MissingSemicolon => "PARSE_MISSING_SEMICOLON".into(),
                surge_parser::ParseCode::MissingColonInType => "PARSE_MISSING_COLON_TYPE".into(),
                surge_parser::ParseCode::MissingReturnType => "PARSE_MISSING_RETURN_TYPE".into(),
                surge_parser::ParseCode::UnexpectedToken => "PARSE_UNEXPECTED_TOKEN".into(),
                surge_parser::ParseCode::InvalidArraySyntax => "PARSE_INVALID_ARRAY_SYNTAX".into(),
                surge_parser::ParseCode::IncompleteFunction => "PARSE_INCOMPLETE_FUNCTION".into(),
                surge_parser::ParseCode::Recoverable => "PARSE_RECOVERABLE".into(),
            });
            let span = SpanRef {
                source: file,
                start: d.span.start,
                end: d.span.end,
            };
            let related = d
                .related
                .iter()
                .map(|(sp, msg)| Related {
                    span: SpanRef {
                        source: file,
                        start: sp.start,
                        end: sp.end,
                    },
                    message: msg.clone(),
                })
                .collect();
            Diagnostic {
                severity: Severity::Error,
                code,
                message: d.message.clone(),
                span,
                related,
            }
        })
        .collect()
}
