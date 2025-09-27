# Surge Token Crate

`surge-token` — это подсистема лексера языка **Surge**, отвечающая за базовые строительные блоки анализа исходного кода: **ключевые слова, токены, позиции в исходнике, директивы и контекст токенизации**.

Crate изолирован, чтобы его можно было переиспользовать в разных подсистемах (`lexer`, `parser`, `diagnostics`, `doctest runner` и т.д.) без лишних зависимостей.

---

## Возможности

* Определение **ключевых слов** языка (например, `fn`, `let`, `signal`, `parallel`).
* **Специальные ключевые слова для директив** (`test.equal`, `benchmark.measure`, `time.measure`).
* Типизация токенов через `TokenKind` (идентификаторы, литералы, операторы, скобки и т.п.).
* Поддержка **директив** в комментариях (`/// test:`, `/// benchmark:`, `/// time:`).
* **Контекст токенизации** для различения обычных токенов и токенов внутри директив.
* Единая модель **позиции в исходнике** (`Span`, `SourceId`) для диагностики.
* Универсальная структура `Token` с привязкой к исходному коду и контекстом.

---

## Структура

```
token/
├─ lib.rs        # точка входа: pub use всех модулей
├─ keyword.rs    # перечисление Keyword, lookup_keyword() и lookup_directive_keyword()
├─ kind.rs       # TokenKind и DirectiveKind
├─ span.rs       # Span и SourceId
└─ token.rs      # структура Token с TokenContext
```

---

## Основные элементы

### 1. Ключевые слова (`keyword.rs`)

```rust
// См. актуальный список в `crates/token/src/keyword.rs`.
// Ключевые слова включают: fn, let, mut, if, else, while, for, in,
// break, continue, as, import, pub, newtype, type, literal, alias, extern,
// return, signal, parallel, compare, spawn, async, await, is, finally,
// true, false, nothing, own.
// Для директив распознаются test.equal/test.ne/…/test.assert, а также
// benchmark.measure и time.measure.
```

* Включают **основные конструкции языка** (`fn`, `let`, `signal`, `parallel`) и спец-атрибуты (`@pure`, `@override`).
* **Специальные ключевые слова для директив** (`test.equal`, `repeat`, `random.int`) используются только в контексте директив.
* Функция `lookup_keyword(&str) -> Option<Keyword>` позволяет отличить идентификаторы от ключевых слов.
* Функция `lookup_directive_keyword(&str) -> Option<Keyword>` распознает ключевые слова специфичные для директив.

### 2. Виды токенов (`kind.rs`)

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum TokenKind {
    Ident,
    Keyword(Keyword),

    // Литералы
    IntLit, FloatLit, StringLit,

    // Разделители и операторы
    Amp, Star, Pipe, LBracket, RBracket, LParen, RParen,
    LBrace, RBrace, LAngle, RAngle,
    Comma, Semicolon, Colon, Dot,
    ThinArrow, FatArrow, Ellipsis, PathSep,
    AndAnd, OrOr, Not,
    Eq, EqEq, Ne, Le, Ge,
    Plus, Minus, Slash, Percent,
    PlusEq, MinusEq, StarEq, SlashEq, PercentEq,
    ColonEq,
    Question,
    At,
    Directive(DirectiveKind),
    Eof,
}
```

* Перечислены **все базовые токены** языка Surge на Phase A.
* Директивы (`DirectiveKind`) используются для doctest/benchmark аннотаций в комментариях.
* Типы директив: `Test`, `Benchmark`, `Time`.

### 3. Позиции (`span.rs`)

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct SourceId(pub u32);

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct Span {
    pub source: SourceId,
    pub start: u32, // байтовый offset (включительно)
    pub end: u32,   // байтовый offset (исключительно)
}
```

* `SourceId` — уникальный идентификатор файла/буфера.
* `Span` указывает диапазон байт внутри исходника, используется в диагностике:

  ```
  file.sg:2:5: error: Expected ';', got KW_LET
      let y:int = 1;
      ^~~
  ```

### 4. Контекст токенизации (`token.rs`)

```rust
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub enum TokenContext {
    Normal,                        // обычный токен
    Directive(DirectiveKind),      // токен внутри директивы определенного типа
}
```

* Определяет **контекст токенизации** - где был создан токен.
* `Normal` - обычные токены в коде.
* `Directive(DirectiveKind)` - токены внутри блоков директив.

### 5. Токены (`token.rs`)

```rust
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub struct Token {
    pub kind: TokenKind,
    pub span: Span,
    pub context: TokenContext,
}
```

* Минимальная единица лексического анализа.
* `kind` указывает тип, `span` связывает токен с исходником.
* `context` определяет контекст токенизации (обычный код или директива).

---

## Пример использования

```rust
use surge_token::{Token, TokenKind, TokenContext, DirectiveKind, Span, SourceId, lookup_keyword, lookup_directive_keyword};

fn main() {
    let sid = SourceId(1);
    let span = Span::new(sid, 0, 2);

    // Обычный токен - ключевое слово "fn"
    let ident = "fn";
    let kind = match lookup_keyword(ident) {
        Some(kw) => TokenKind::Keyword(kw),
        None => TokenKind::Ident,
    };
    let token = Token::new(kind, span);

    // Токен внутри директивы - специальное ключевое слово
    let directive_span = Span::new(sid, 10, 20);
    let directive_kind = match lookup_directive_keyword("test.equal") {
        Some(kw) => TokenKind::Keyword(kw),
        None => TokenKind::Ident,
    };
    let directive_token = Token::new_with_context(
        directive_kind, 
        directive_span, 
        TokenContext::Directive(DirectiveKind::Test)
    );

    println!("{:?}", token);
    // Token { kind: Keyword(Fn), span: Span { source: SourceId(1), start: 0, end: 2 }, context: Normal }

    println!("{:?}", directive_token);
    // Token { kind: Keyword(TestEqual), span: Span { source: SourceId(1), start: 10, end: 20 }, context: Directive(Test) }
}
```

---

## Связь с другими подсистемами

* **Lexer** использует `TokenKind`, `lookup_keyword`, `lookup_directive_keyword` и `TokenContext` для генерации токенов с правильным контекстом.
* **Parser** строит AST, различая идентификаторы, ключевые слова и операторы, учитывая контекст токенизации.
* **Diagnostics** опирается на `Span`, чтобы печатать ошибки с позициями.
* **Doctest runner** обрабатывает `DirectiveKind` (`/// test:` и т.п.) и использует `TokenContext` для различения токенов директив от обычных.

---

## Примеры директив

### Тестовые директивы

```sg
/// test:
/// Test1: // заголовок теста
/// test.equal(add(2, 3), 5);
/// let result = add(4, 0);
/// test.le(result, 4);
```

### Бенчмарк директивы

```sg
/// benchmark:
/// Benchmark1:
///   benchmark.measure(add(2, 3), 5);
```

### Директивы измерения времени

```sg
/// time:
/// Time1:
///   time.measure(add(2, 3), 5);
```

Все содержимое директив токенизируется как обычный код, но с контекстом `TokenContext::Directive(DirectiveKind)`, что позволяет в будущем легко генерировать соответствующий тестовый/бенчмарковый код.
