use surge_token::SourceId;

#[derive(Debug)]
pub struct Cursor<'a> {
    src: &'a str,
    /// текущая позиция в БАЙТАХ (всегда на границе UTF-8)
    i: usize,
    file: SourceId,
}

impl<'a> Cursor<'a> {
    /// Создать новый курсор
    pub fn new(src: &'a str, file: SourceId) -> Self {
        Self { src, i: 0, file }
    }

    /// Текущий байтовый оффсет (u32 для Span)
    pub fn pos(&self) -> u32 {
        self.i as u32
    }

    /// Достигнут ли конец файла
    pub fn eof(&self) -> bool {
        self.i >= self.src.len()
    }

    /// Получить идентификатор исходного файла
    pub fn file(&self) -> SourceId {
        self.file
    }

    /// Находится ли курсор в начале строки (или файла)
    pub fn is_line_start(&self) -> bool {
        self.i == 0 || self.src.as_bytes()[self.i - 1] == b'\n'
    }

    /// Посмотреть текущую руну (не двигая курсор)
    pub fn peek(&self) -> Option<char> {
        if self.eof() {
            return None;
        }
        self.src[self.i..].chars().next()
    }

    /// Lookahead на n рун вперёд (n=1 — как peek следующего после текущего)
    pub fn peek_n(&self, n: usize) -> Option<char> {
        self.src[self.i..].chars().nth(n)
    }

    /// Стартуется ли оставшийся текст с данным литералом (байтовое сравнение)
    /// Без сдвига курсора
    pub fn starts_with(&self, s: &str) -> bool {
        self.src[self.i..].starts_with(s)
    }

    /// Прочитать одну руну и сдвинуть курсор. Возвращает эту руну.
    /// На EOF — None.
    pub fn bump(&mut self) -> Option<char> {
        if self.eof() {
            return None;
        }

        let ch = self.src[self.i..].chars().next().unwrap();
        self.i += ch.len_utf8();
        Some(ch)
    }

    /// Поглотить последовательность рун по предикату, вернуть СКОЛЬКО рун поглотили
    /// Предикат получает одну руну. Останавливаемся на первой, для которой предикат = false
    pub fn bump_while<F: Fn(char) -> bool>(&mut self, f: F) -> usize {
        let mut count = 0;
        while let Some(ch) = self.peek() {
            if !f(ch) {
                break;
            }
            self.bump();
            count += 1;
        }
        count
    }

    /// Поглотить конкретную строку (если начинается ею), вернуть true/false
    /// Удобно для многосимвольных операторов
    pub fn bump_str(&mut self, s: &str) -> bool {
        if self.starts_with(s) {
            self.i += s.len();
            true
        } else {
            false
        }
    }

    /// Сохранить текущую позицию для возможного отката
    pub fn save_pos(&self) -> usize {
        self.i
    }

    /// Восстановить позицию из сохраненной
    pub fn restore_pos(&mut self, pos: usize) {
        self.i = pos;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use surge_token::SourceId;

    #[test]
    fn test_utf8_handling() {
        let src = "πx";
        let file = SourceId(0);
        let mut cursor = Cursor::new(src, file);

        // Проверяем peek UTF-8 символа
        assert_eq!(cursor.peek(), Some('π'));
        assert_eq!(cursor.pos(), 0);

        // После bump - позиция изменилась на len_utf8
        assert_eq!(cursor.bump(), Some('π'));
        assert_eq!(cursor.pos(), 2); // π занимает 2 байта
        assert_eq!(cursor.peek(), Some('x'));

        // Bump следующего символа
        assert_eq!(cursor.bump(), Some('x'));
        assert_eq!(cursor.pos(), 3); // π (2) + x (1) = 3 байта
        assert!(cursor.eof());
    }

    #[test]
    fn test_starts_with_and_bump_str() {
        let src = "->>>>";
        let file = SourceId(0);
        let mut cursor = Cursor::new(src, file);

        // starts_with работает
        assert!(cursor.starts_with("->"));
        assert!(cursor.starts_with("->>>"));

        // bump_str захватывает точное совпадение
        assert!(cursor.bump_str("->"));
        assert_eq!(cursor.pos(), 2);
        assert!(cursor.starts_with(">>"));

        // bump_str захватывает совпадение
        assert!(cursor.bump_str(">>>"));
        assert_eq!(cursor.pos(), 5);
    }

    #[test]
    fn test_bump_while() {
        let src = "   \t\nabc";
        let file = SourceId(0);
        let mut cursor = Cursor::new(src, file);

        // bump_while для пробельных символов
        let count = cursor.bump_while(|c| c.is_whitespace());
        assert_eq!(count, 5); // 3 пробела + 1 табуляция + 1 перевод строки
        assert_eq!(cursor.pos(), 5);
        assert_eq!(cursor.peek(), Some('a'));

        // bump_while для букв
        let count = cursor.bump_while(|c| c.is_alphabetic());
        assert_eq!(count, 3); // a, b, c
        assert!(cursor.eof());
    }

    #[test]
    fn test_peek_n() {
        let src = "abcd";
        let file = SourceId(0);
        let cursor = Cursor::new(src, file);

        assert_eq!(cursor.peek_n(0), Some('a'));
        assert_eq!(cursor.peek_n(1), Some('b'));
        assert_eq!(cursor.peek_n(2), Some('c'));
        assert_eq!(cursor.peek_n(3), Some('d'));
        assert_eq!(cursor.peek_n(4), None);
    }

    #[test]
    fn test_empty_string() {
        let src = "";
        let file = SourceId(0);
        let mut cursor = Cursor::new(src, file);

        assert!(cursor.eof());
        assert_eq!(cursor.peek(), None);
        assert_eq!(cursor.bump(), None);
        assert_eq!(cursor.bump_while(|_| true), 0);
        assert!(!cursor.bump_str("anything"));
    }
}
