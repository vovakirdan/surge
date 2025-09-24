use crate::format::{Format, Formatter, PrettyFormatter, JsonFormatter, CsvFormatter, FormatError};
use crate::source::{SourceMap, SourceTextProvider};
use crate::model::Diagnostic;

pub struct ReportOptions {
    pub format: Format,
}

pub struct Reporter {
    pub sources: SourceMap,
    pub text: Box<dyn SourceTextProvider + Send + Sync>,
    pub opts: ReportOptions,
}

impl Reporter {
    pub fn new(
        sources: SourceMap,
        text: Box<dyn SourceTextProvider + Send + Sync>,
        opts: ReportOptions,
    ) -> Self {
        Self { sources, text, opts }
    }

    pub fn render(
        &self,
        diagnostics: &[Diagnostic],
    ) -> Result<String, FormatError> {
        let fmt: Box<dyn Formatter> = match self.opts.format {
            Format::Pretty => Box::new(PrettyFormatter::default()),
            Format::Json => Box::new(JsonFormatter),
            Format::Csv => Box::new(CsvFormatter),
        };
        fmt.format(diagnostics, &self.sources, &*self.text)
    }
}
