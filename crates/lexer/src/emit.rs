use crate::token::{TokenKind, Span};
use crate::rules::DiagCode;
use crate::TriviaKind;

pub struct Emitter<'a> { /* owns &mut Cursor, collects output */ }
impl<'a> Emitter<'a> {
    pub fn token(&mut self, start: u32, end: u32, kind: TokenKind);
    pub fn diag(&mut self, span: Span, code: DiagCode, msg: impl Into<String>);
    pub fn trivia(&mut self, start: u32, end: u32, kind: TriviaKind); // если нужно
}
