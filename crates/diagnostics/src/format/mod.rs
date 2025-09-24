mod pretty;
mod json;
mod csv_format;

pub use pretty::PrettyFormatter;
pub use json::JsonFormatter;
pub use csv_format::CsvFormatter;

use thiserror::Error;

#[derive(Debug, Clone, Copy)]
pub enum Format { Pretty, Json, Csv }

#[derive(Error, Debug)]
pub enum FormatError {
    #[error("json error: {0}")]
    Json(#[from] serde_json::Error),
    #[error("csv error: {0}")]
    Csv(#[from] csv::Error),
    #[error("csv into inner error: {0}")]
    CsvIntoInner(#[from] csv::IntoInnerError<csv::Writer<Vec<u8>>>),
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),
}

pub trait Formatter {
    fn format(
        &self,
        diagnostics: &[crate::model::Diagnostic],
        sources: &crate::source::SourceMap,
        text: &dyn crate::source::SourceTextProvider,
    ) -> Result<String, FormatError>;
}
