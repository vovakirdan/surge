use crate::format::{FormatError, Formatter};
use crate::model::Diagnostic;
use crate::source::{SourceMap, SourceTextProvider, line_col};

#[derive(Default)]
pub struct PrettyFormatter;

impl Formatter for PrettyFormatter {
    fn format(
        &self,
        diagnostics: &[Diagnostic],
        sources: &SourceMap,
        text: &dyn SourceTextProvider,
    ) -> Result<String, FormatError> {
        let mut out = String::new();
        for d in diagnostics {
            let file_label = sources.label(d.span.source);
            if let Some(src) = text.get(d.span.source) {
                let (line, col) = line_col(src, d.span.start as usize);
                use std::fmt::Write as _;
                let _ = write!(
                    out,
                    "{}:{}:{}: {:?} [{}]: {}\n",
                    file_label, line, col, d.severity, d.code.0, d.message
                );
                // контекст строки
                let (ctx_line_start, ctx_line_text) = current_line(src, d.span.start as usize);
                let _ = writeln!(out, "  {:>4} | {}", line, ctx_line_text);
                // каретка
                let caret_pos = display_col(&src[ctx_line_start..d.span.start as usize]);
                let underline = if d.span.end > d.span.start {
                    let len = display_col(&src[d.span.start as usize..d.span.end as usize]).max(1);
                    "~".repeat(len)
                } else {
                    "^".into()
                };
                let _ = writeln!(out, "       | {}{}", " ".repeat(caret_pos), underline);
            } else {
                use std::fmt::Write as _;
                let _ = write!(
                    out,
                    "{}:?:?: {:?} [{}]: {}\n",
                    file_label, d.severity, d.code.0, d.message
                );
            }
            out.push('\n');
        }
        Ok(out)
    }
}

/// Возвращает стартовый байтовый индекс текущей строки и её текст без завершающего '\n'
fn current_line(src: &str, byte_off: usize) -> (usize, String) {
    let bytes = src.as_bytes();
    let mut start = 0usize;
    let mut i = byte_off.min(bytes.len());
    while i > 0 {
        if bytes[i - 1] == b'\n' {
            start = i;
            break;
        }
        i -= 1;
    }
    let mut end = byte_off;
    while end < bytes.len() && bytes[end] != b'\n' {
        end += 1;
    }
    (start, src[start..end].to_string())
}

/// Длина визуальной колонки (кол-во рун), табы считаем как 4 пробела
fn display_col(s: &str) -> usize {
    let mut col = 0usize;
    for ch in s.chars() {
        match ch {
            '\t' => col += 4,
            _ => col += 1,
        }
    }
    col
}
