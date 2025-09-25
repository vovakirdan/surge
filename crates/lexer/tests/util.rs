use surge_token::Token;

pub struct LineMap<'a> {
    src: &'a str,
    starts: Vec<usize>,
}
impl<'a> LineMap<'a> {
    pub fn new(src: &'a str) -> Self {
        let mut starts = vec![0];
        for (i, ch) in src.char_indices() {
            if ch == '\n' { starts.push(i + 1); }
        }
        Self { src, starts }
    }
    pub fn line_col(&self, byte_off: usize) -> (usize, usize) {
        let (line_idx, line_start) = match self.starts.binary_search(&byte_off) {
            Ok(idx) => (idx, self.starts[idx]),
            Err(ins) => {
                let idx = ins.saturating_sub(1);
                (idx, self.starts[idx])
            }
        };
        let line = &self.src[line_start..byte_off];
        let col = line.chars().count() + 1; // 1-based
        (line_idx + 1, col)
    }
}

fn escape_lexeme(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for ch in s.chars() {
        match ch {
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            _ => out.push(ch),
        }
    }
    out
}

/// Стабильный дамп для снепшотов: `IDX  LINE:COL  KIND  "LEXEME"`
pub fn dump_tokens(tokens: &[Token], src: &str) -> String {
    let lm = LineMap::new(src);
    let mut out = String::new();
    out.push_str("IDX  LINE:COL  KIND                 LEXEME\n");
    for (i, t) in tokens.iter().enumerate() {
        let (l, c) = lm.line_col(t.span.start as usize);
        let lexeme = &src[t.span.start as usize .. t.span.end as usize];
        let esc = escape_lexeme(lexeme);
        use std::fmt::Write;
        let _ = writeln!(
            out,
            "{:>3}  {:>4}:{:<-3}  {:<20}  \"{}\"",
            i, l, c, format!("{:?}", t.kind), esc
        );
    }
    out
}
