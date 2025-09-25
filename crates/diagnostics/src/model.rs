use serde::{Deserialize, Serialize};
use surge_token::SourceId;

#[derive(Serialize, Deserialize, Debug, Clone, Copy, PartialEq, Eq)]
pub enum Severity {
    Error,
    Warning,
    Info,
    Hint,
}

#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
pub struct Code(pub String);

#[derive(Serialize, Deserialize, Debug, Clone, Copy, PartialEq, Eq)]
pub struct SpanRef {
    pub source: SourceId,
    pub start: u32,
    pub end: u32,
}

#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
pub struct Related {
    pub span: SpanRef,
    pub message: String,
}

#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
pub struct Diagnostic {
    pub severity: Severity,
    pub code: Code,
    pub message: String,
    pub span: SpanRef,
    pub related: Vec<Related>,
}

impl Diagnostic {
    pub fn error(code: impl Into<String>, message: impl Into<String>, span: SpanRef) -> Self {
        Self {
            severity: Severity::Error,
            code: Code(code.into()),
            message: message.into(),
            span,
            related: vec![],
        }
    }
}
