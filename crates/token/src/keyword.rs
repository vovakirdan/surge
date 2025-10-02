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
    // Using, - removed
    Pub,
    Newtype,
    Type,
    Literal,
    Alias,
    Extern,
    Return,
    Signal,
    Parallel,
    Compare,
    Spawn,
    Async,
    Await,
    Macro,
    Tag,
    Heir,
    With,
    Pragma,
    Is,
    Finally,
    True,
    False,
    Nothing,
    Own,
    // directive-specific keywords
    TestEqual,        // test.equal
    TestNotEqual,     // test.ne
    TestLess,         // test.lt
    TestLessEq,       // test.le
    TestGreater,      // test.gt
    TestGreaterEq,    // test.ge
    TestAssert,       // test.assert
    BenchmarkMeasure, // benchmark.measure
    TimeMeasure,      // time.measure
    TargetAll,        // all(...)
    TargetAny,        // any(...)
    TargetNot,        // not(...)
                      // Repeat, RandomInt, RandomFloat - removed
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
        "pub" => Pub,
        "newtype" => Newtype,
        "type" => Type,
        "literal" => Literal,
        "alias" => Alias,
        "extern" => Extern,
        "return" => Return,
        "signal" => Signal,
        "parallel" => Parallel,
        "compare" => Compare,
        "spawn" => Spawn,
        "async" => Async,
        "await" => Await,
        "macro" => Macro,
        "tag" => Tag,
        "heir" => Heir,
        "with" => With,
        "pragma" => Pragma,
        "is" => Is,
        "finally" => Finally,
        "true" => True,
        "false" => False,
        "nothing" => Nothing,
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
        "benchmark.measure" => BenchmarkMeasure,
        "time.measure" => TimeMeasure,
        "all" => TargetAll,
        "any" => TargetAny,
        "not" => TargetNot,
        _ => return None,
    })
}

/// Ключевые слова для атрибутов (@attribute)
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum AttrKeyword {
    Pure,
    Overload,
    Override,
    Intrinsic,
    Backend,
    Test,
    Benchmark,
    Time,
    Deprecated,
    Packed,
    Align,
    Shared,
    Atomic,
    Raii,
    Arena,
    Weak,
    Readonly,
    Hidden,
    Noinherit,
    Sealed,
    GuardedBy,
    RequiresLock,
    AcquiresLock,
    ReleasesLock,
    WaitsOn,
    Send,
    Nosend,
    Nonblocking,
}

/// Поиск ключевых слов атрибутов
/// Возвращает Some(AttrKeyword) если найдено ключевое слово атрибута
pub fn lookup_attribute_keyword(ident: &str) -> Option<AttrKeyword> {
    use AttrKeyword::*;
    Some(match ident {
        "pure" => Pure,
        "overload" => Overload,
        "override" => Override,
        "intrinsic" => Intrinsic,
        "backend" => Backend,
        "test" => Test,
        "benchmark" => Benchmark,
        "time" => Time,
        "deprecated" => Deprecated,
        "packed" => Packed,
        "align" => Align,
        "shared" => Shared,
        "atomic" => Atomic,
        "raii" => Raii,
        "arena" => Arena,
        "weak" => Weak,
        "readonly" => Readonly,
        "hidden" => Hidden,
        "noinherit" => Noinherit,
        "sealed" => Sealed,
        "guarded_by" => GuardedBy,
        "requires_lock" => RequiresLock,
        "acquires_lock" => AcquiresLock,
        "releases_lock" => ReleasesLock,
        "waits_on" => WaitsOn,
        "send" => Send,
        "nosend" => Nosend,
        "nonblocking" => Nonblocking,
        _ => return None,
    })
}
