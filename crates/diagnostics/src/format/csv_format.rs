use crate::format::{Formatter, FormatError};
use crate::model::Diagnostic;
use crate::source::{SourceMap, SourceTextProvider, line_col};

pub struct CsvFormatter;

impl Formatter for CsvFormatter {
    fn format(
        &self,
        diagnostics: &[Diagnostic],
        sources: &SourceMap,
        text: &dyn SourceTextProvider,
    ) -> Result<String, FormatError> {
        let mut wtr = csv::Writer::from_writer(vec![]);
        wtr.write_record(&[
            "file","line","col","end_line","end_col","severity","code","message"
        ])?;
        for d in diagnostics {
            let file_label = sources.label(d.span.source).to_string();
            let (mut line, mut col, mut eline, mut ecol) = (0,0,0,0);
            if let Some(src) = text.get(d.span.source) {
                (line, col) = line_col(src, d.span.start as usize);
                (eline, ecol) = line_col(src, d.span.end as usize);
            }
            wtr.write_record(&[
                file_label,
                line.to_string(),
                col.to_string(),
                eline.to_string(),
                ecol.to_string(),
                format!("{:?}", d.severity),
                d.code.0.clone(),
                d.message.clone(),
            ])?;
        }
        let bytes = wtr.into_inner()?;
        Ok(String::from_utf8(bytes).unwrap_or_default())
    }
}
