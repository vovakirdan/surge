pub struct Cursor<'a> { /* src, i, file */ }
impl<'a> Cursor<'a> {
    pub fn pos(&self) -> u32;                  // текущий byte offset
    pub fn eof(&self) -> bool;
    pub fn peek(&self) -> Option<char>;        // текущий char
    pub fn peek_n(&self, n: usize) -> Option<char>; // lookahead по runes
    pub fn starts_with(&self, s: &str) -> bool;// быстрые проверки префиксов
    pub fn bump(&mut self) -> Option<char>;    // читает 1 char, двигает i
    pub fn bump_while(&mut self, f: impl Fn(char)->bool) -> usize;
}
