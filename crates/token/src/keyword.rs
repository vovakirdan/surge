#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Keyword {
    Fn,
    Let,
    Mut,
    If,
    Else,
    While,
    For,
    In,
    Break,
    Continue,
    As,
    Import,
    Using,
    Type,
    Literal,
    Alias,
    Extern,
    Return,
    Signal,
    Parallel,
    Map,
    Reduce,
    True,
    False,
    Own,
    // directive-specific keywords
    TestEqual,     // test.equal
    TestNotEqual,  // test.ne
    TestLess,      // test.lt
    TestLessEq,    // test.le
    TestGreater,   // test.gt
    TestGreaterEq, // test.ge
    TestAssert,    // test.assert
    Repeat,        // repeat
    RandomInt,     // random.int
    RandomFloat,   // random.float
}

pub fn lookup_keyword(ident: &str) -> Option<Keyword> {
    use Keyword::*;
    Some(match ident {
        "fn" => Fn,
        "let" => Let,
        "mut" => Mut,
        "if" => If,
        "else" => Else,
        "while" => While,
        "for" => For,
        "in" => In,
        "break" => Break,
        "continue" => Continue,
        "import" => Import,
        "as" => As,
        "using" => Using,
        "type" => Type,
        "literal" => Literal,
        "alias" => Alias,
        "extern" => Extern,
        "return" => Return,
        "signal" => Signal,
        "parallel" => Parallel,
        "map" => Map,
        "reduce" => Reduce,
        "true" => True,
        "false" => False,
        "own" => Own,
        // directive keywords не обрабатываются здесь - они требуют специального контекста
        _ => return None,
    })
}

/// Поиск ключевых слов специфичных для директив
/// Возвращает Some(Keyword) если найдено ключевое слово директивы
pub fn lookup_directive_keyword(ident: &str) -> Option<Keyword> {
    use Keyword::*;
    Some(match ident {
        "test.equal" => TestEqual,
        "test.ne" => TestNotEqual,
        "test.lt" => TestLess,
        "test.le" => TestLessEq,
        "test.gt" => TestGreater,
        "test.ge" => TestGreaterEq,
        "test.assert" => TestAssert,
        "repeat" => Repeat,
        "random.int" => RandomInt,
        "random.float" => RandomFloat,
        _ => return None,
    })
}
