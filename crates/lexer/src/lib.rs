use surge_token::{lookup_keyword, SourceId, Span, Token, TokenKind};

pub struct LexResult {
    pub tokens: Vec<Token>,
    pub diags: Vec<String>, // на первое время — просто строки
}

pub fn lex(src: &str, file: SourceId) -> LexResult {
    let mut lx = Lexer { src, i: 0, file, out: Vec::new(), diags: Vec::new() };
    while !lx.eof() {
        lx.skip_ws_and_comments();
        if lx.eof() { break; }
        lx.next_token();
    }
    lx.push(TokenKind::Eof, 0);
    LexResult { tokens: lx.out, diags: lx.diags }
}

struct Lexer<'a> {
    src: &'a str,
    i: usize,
    file: SourceId,
    out: Vec<Token>,
    diags: Vec<String>,
}

impl<'a> Lexer<'a> {
    fn eof(&self) -> bool { self.i >= self.src.len() }
    fn b(&self) -> u8 { self.src.as_bytes()[self.i] }
    fn push(&mut self, kind: TokenKind, len: usize) {
        let start = self.i as u32;
        self.i += len;
        let end = self.i as u32;
        self.out.push(Token::new(kind, Span { source: self.file, start, end }));
    }
    fn skip_ws_and_comments(&mut self) {
        loop {
            // whitespace
            while !self.eof() && self.src[self.i..].starts_with(char::is_whitespace) {
                self.i += self.src[self.i..].chars().next().unwrap().len_utf8();
            }
            // // comment
            if self.src[self.i..].starts_with("//") {
                while !self.eof() && self.src.as_bytes()[self.i] != b'\n' { self.i += 1; }
                continue;
            }
            // /* ... */ (без вложенности для простоты)
            if self.src[self.i..].starts_with("/*") {
                self.i += 2;
                while !self.eof() && !self.src[self.i..].starts_with("*/") {
                    self.i += 1;
                }
                if !self.eof() { self.i += 2; }
                continue;
            }
            break;
        }
    }

    fn next_token(&mut self) {
        // punct/sequences first (multi-char)
        if let Some(k) = self.take_multi(&["->", "=>", "::", "...", "&&", "||", "<=", ">=", "==", "!="]) {
            let kind = match k {
                "->" => TokenKind::ThinArrow,
                "=>" => TokenKind::FatArrow,
                "::" => TokenKind::PathSep,
                "..." => TokenKind::Ellipsis,
                "&&" => TokenKind::AndAnd,
                "||" => TokenKind::OrOr,
                "<=" => TokenKind::Le,
                ">=" => TokenKind::Ge,
                "==" => TokenKind::EqEq,
                "!=" => TokenKind::Ne,
                _ => unreachable!(),
            };
            return self.push(kind, k.len());
        }

        // single-char punct
        let b = self.b();
        let (kind, len) = match b {
            b'(' => (TokenKind::LParen, 1),
            b')' => (TokenKind::RParen, 1),
            b'[' => (TokenKind::LBracket, 1),
            b']' => (TokenKind::RBracket, 1),
            b'{' => (TokenKind::LBrace, 1),
            b'}' => (TokenKind::RBrace, 1),
            b',' => (TokenKind::Comma, 1),
            b';' => (TokenKind::Semicolon, 1),
            b':' => (TokenKind::Colon, 1),
            b'.' => (TokenKind::Dot, 1),
            b'&' => (TokenKind::Amp, 1),
            b'*' => (TokenKind::Star, 1),
            b'!' => (TokenKind::Not, 1),
            b'=' => (TokenKind::Eq, 1),
            b'<' => (TokenKind::Lt, 1),
            b'>' => (TokenKind::Gt, 1),
            b'+' => (TokenKind::Plus, 1),
            b'-' => (TokenKind::Minus, 1),
            b'/' => (TokenKind::Slash, 1),
            b'%' => (TokenKind::Percent, 1),
            _ => { self.lex_word_or_number_or_string(); return; }
        };
        self.push(kind, len);
    }

    fn take_multi<'s>(&self, ks: &[&'s str]) -> Option<&'s str> {
        for &k in ks {
            if self.src[self.i..].starts_with(k) { return Some(k); }
        }
        None
    }

    fn lex_word_or_number_or_string(&mut self) {
        let ch = self.src[self.i..].chars().next().unwrap();
        if ch == '"' {
            // "string"
            let start = self.i;
            self.i += 1;
            while !self.eof() {
                let c = self.src[self.i..].chars().next().unwrap();
                self.i += c.len_utf8();
                if c == '"' { break; }
                if c == '\\' && !self.eof() {
                    // skip escaped char (naive)
                    let c2 = self.src[self.i..].chars().next().unwrap();
                    self.i += c2.len_utf8();
                }
            }
            let len = self.i - start;
            return self.push(TokenKind::StringLit, len);
        }

        if ch.is_ascii_digit() {
            // number: simple version (no bases yet)
            let start = self.i;
            self.consume_while(|c| c.is_ascii_digit() || c == '_');
            if self.peek_char() == Some('.') && self.peek_char2().map(|c| c.is_ascii_digit()).unwrap_or(false) {
                self.i += 1; // dot
                self.consume_while(|c| c.is_ascii_digit() || c == '_');
                // optional exponent
                if matches!(self.peek_char(), Some('e' | 'E')) {
                    self.i += 1;
                    if matches!(self.peek_char(), Some('+' | '-')) { self.i += 1; }
                    self.consume_while(|c| c.is_ascii_digit());
                }
                let len = self.i - start;
                return self.push(TokenKind::FloatLit, len);
            }
            let len = self.i - start;
            return self.push(TokenKind::IntLit, len);
        }

        // ident / keyword (включая @атрибуты — они придут как одно слово "@pure")
        if ch == '@' || ch.is_alphabetic() || ch == '_' {
            let start = self.i;
            self.consume_while(|c| c == '@' || c.is_alphanumeric() || c == '_');
            let s = &self.src[start..self.i];
            if let Some(kw) = lookup_keyword(s) {
                let len = self.i - start;
                return self.push(TokenKind::Keyword(kw), len);
            } else {
                let len = self.i - start;
                return self.push(TokenKind::Ident, len);
            }
        }

        // неизвестный символ — пропускаем и фиксируем диагностику
        let bad = self.src[self.i..].chars().next().unwrap();
        self.diags.push(format!("unknown char {:?}", bad));
        self.i += bad.len_utf8();
    }

    fn consume_while<F: Fn(char) -> bool>(&mut self, f: F) {
        while !self.eof() {
            let c = self.src[self.i..].chars().next().unwrap();
            if !f(c) { break; }
            self.i += c.len_utf8();
        }
    }

    fn peek_char(&self) -> Option<char> {
        if self.eof() { None } else { self.src[self.i..].chars().next() }
    }
    fn peek_char2(&self) -> Option<char> {
        let mut it = self.src[self.i..].chars();
        it.next()?;
        it.next()
    }
}
