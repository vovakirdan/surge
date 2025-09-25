use crate::format::{FormatError, Formatter};
use crate::model::Diagnostic;
use crate::source::{SourceMap, SourceTextProvider};

pub struct JsonFormatter;

impl Formatter for JsonFormatter {
    fn format(
        &self,
        diagnostics: &[Diagnostic],
        _sources: &SourceMap,
        _text: &dyn SourceTextProvider,
    ) -> Result<String, FormatError> {
        Ok(serde_json::to_string_pretty(diagnostics)?)
    }
}
