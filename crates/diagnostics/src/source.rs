use std::collections::HashMap;
use surge_token::SourceId;

pub type SourceLabel = String;

pub struct SourceMap {
    map: HashMap<SourceId, SourceLabel>,
}

impl SourceMap {
    pub fn new() -> Self { Self { map: HashMap::new() } }
    pub fn insert(&mut self, id: SourceId, label: impl Into<String>) {
        self.map.insert(id, label.into());
    }
    pub fn label(&self, id: SourceId) -> &str {
        self.map.get(&id).map(|s| s.as_str()).unwrap_or("<unknown>")
    }
}

pub trait SourceTextProvider {
    fn get(&self, id: SourceId) -> Option<&str>;
}

pub struct InMemorySourceText {
    map: HashMap<SourceId, String>,
}
impl InMemorySourceText {
    pub fn new() -> Self { Self { map: HashMap::new() } }
    pub fn insert(&mut self, id: SourceId, text: String) { self.map.insert(id, text); }
}
impl SourceTextProvider for InMemorySourceText {
    fn get(&self, id: SourceId) -> Option<&str> { self.map.get(&id).map(|s| s.as_str()) }
}

/// Утилита: вычисление (line, col) 1-based из byte offset. Юникод-колонка по runes.
pub fn line_col(text: &str, byte_off: usize) -> (usize, usize) {
    // line starts
    let mut line = 1usize;
    let mut col = 1usize;
    for (idx, ch) in text.char_indices() {
        if idx == byte_off { return (line, col); }
        if ch == '\n' {
            line += 1;
            col = 1;
        } else {
            col += 1;
        }
    }
    // если off == len
    (line, col)
}
