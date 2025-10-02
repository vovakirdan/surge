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
                surge_parser::ParseCode::LetMissingEquals => "PARSE_LET_MISSING_EQUALS".into(),
                surge_parser::ParseCode::SignalMissingAssign => {
                    "PARSE_SIGNAL_MISSING_ASSIGN".into()
                }
                surge_parser::ParseCode::AssignmentWithoutLhs => {
                    "PARSE_ASSIGNMENT_WITHOUT_LHS".into()
                }
                surge_parser::ParseCode::UnexpectedPrimary => "PARSE_UNEXPECTED_PRIMARY".into(),
                surge_parser::ParseCode::ForInMissingType => "PARSE_FORIN_MISSING_TYPE".into(),
                surge_parser::ParseCode::ForInMissingIn => "PARSE_FORIN_MISSING_IN".into(),
                surge_parser::ParseCode::ForInMissingExpr => "PARSE_FORIN_MISSING_EXPR".into(),
                surge_parser::ParseCode::CompareMissingArrow => {
                    "PARSE_COMPARE_MISSING_ARROW".into()
                }
                surge_parser::ParseCode::CompareMissingExpr => "PARSE_COMPARE_MISSING_EXPR".into(),
                surge_parser::ParseCode::CompareMissingBrace => {
                    "PARSE_COMPARE_MISSING_BRACE".into()
                }
                surge_parser::ParseCode::ParallelMissingWith => {
                    "PARSE_PARALLEL_MISSING_WITH".into()
                }
                surge_parser::ParseCode::ParallelMissingFatArrow => {
                    "PARSE_PARALLEL_MISSING_FATARROW".into()
                }
                surge_parser::ParseCode::ParallelBadHeader => "PARSE_PARALLEL_BAD_HEADER".into(),
                surge_parser::ParseCode::FatArrowOutsideParallel => {
                    "PARSE_FATARROW_OUTSIDE_PARALLEL".into()
                }
                surge_parser::ParseCode::ParallelFuncNotPure => {
                    "PARSE_PARALLEL_FUNC_NOT_PURE".into()
                }
                surge_parser::ParseCode::UnknownAttribute => "PARSE_UNKNOWN_ATTRIBUTE".into(),
                surge_parser::ParseCode::ExpectedTypeAfterArrow => {
                    "PARSE_EXPECTED_TYPE_AFTER_ARROW".into()
                }
                surge_parser::ParseCode::MissingReturnType => "PARSE_MISSING_RETURN_TYPE".into(),
                surge_parser::ParseCode::ExternGenericBrackets => {
                    "PARSE_EXTERN_GENERIC_BRACKETS".into()
                }
                surge_parser::ParseCode::ExternMissingType => "PARSE_EXTERN_MISSING_TYPE".into(),
                surge_parser::ParseCode::ExternUnclosedBlock => {
                    "PARSE_EXTERN_UNCLOSED_BLOCK".into()
                }
                surge_parser::ParseCode::UnexpectedToken => "PARSE_UNEXPECTED_TOKEN".into(),
                surge_parser::ParseCode::InvalidArraySyntax => "PARSE_INVALID_ARRAY_SYNTAX".into(),
                surge_parser::ParseCode::FieldConflict => "PARSE_FIELD_CONFLICT".into(),
                surge_parser::ParseCode::DuplicateLiteral => "PARSE_DUPLICATE_LITERAL".into(),
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
