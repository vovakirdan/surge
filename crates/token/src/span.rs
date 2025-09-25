use serde::{Deserialize, Serialize};
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize, Serialize)]
pub struct SourceId(pub u32);

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Deserialize, Serialize)]
pub struct Span {
    pub source: SourceId,
    pub start: u32, // byte offset, inclusive
    pub end: u32,   // byte offset, exclusive
}
impl Span {
    pub fn new(source: SourceId, start: u32, end: u32) -> Self {
        Self { source, start, end }
    }
    pub fn len(&self) -> u32 {
        self.end.saturating_sub(self.start)
    }
}
