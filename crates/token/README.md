# Surge Token Crate

`surge-token` — это подсистема лексера языка **Surge**, отвечающая за базовые строительные блоки анализа исходного кода: **ключевые слова, токены, позиции в исходнике и директивы**.

Crate изолирован, чтобы его можно было переиспользовать в разных подсистемах (`lexer`, `parser`, `diagnostics`, `doctest runner` и т.д.) без лишних зависимостей.

---

## Возможности

* Определение **ключевых слов** языка (например, `fn`, `let`, `signal`, `parallel`, `map/reduce`).
* Типизация токенов через `TokenKind` (идентификаторы, литералы, операторы, скобки и т.п.).
* Поддержка **директив** в комментариях (`/// test:`, `/// benchmark:`, `/// time:`).
* Единая модель **позиции в исходнике** (`Span`, `SourceId`) для диагностики.
* Универсальная структура `Token` с привязкой к исходному коду.

---

## Структура

```
token/
├─ lib.rs        # точка входа: pub use всех модулей
├─ keyword.rs    # перечисление Keyword и lookup_keyword()
├─ kind.rs       # TokenKind и DirectiveKind
├─ span.rs       # Span и SourceId
└─ token.rs      # структура Token
```

---

## Основные элементы

### 1. Ключевые слова (`keyword.rs`)

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Keyword {
    Fn, Let, Mut, If, Else, While, For, In,
    Break, Continue, As, Import, Using,
    Type, Literal, Alias, Extern, Return,
    Signal, Parallel, Map, Reduce,
    // атрибуты
    AtPure, AtOverload, AtOverride, AtBackend,
    True, False, Own,
}
```

* Включают **основные конструкции языка** (`fn`, `let`, `signal`, `parallel`) и спец-атрибуты (`@pure`, `@override`).
* Функция `lookup_keyword(&str) -> Option<Keyword>` позволяет отличить идентификаторы от ключевых слов.

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
    At,
    Directive(DirectiveKind),
    Eof,
}
```

* Перечислены **все базовые токены** языка Surge на Phase A.
* Директивы (`DirectiveKind`) используются для doctest/benchmark аннотаций в комментариях.

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

### 4. Токены (`token.rs`)

```rust
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub struct Token {
    pub kind: TokenKind,
    pub span: Span,
}
```

* Минимальная единица лексического анализа.
* `kind` указывает тип, `span` связывает токен с исходником.

---

## Пример использования

```rust
use surge_token::{Token, TokenKind, Span, SourceId, lookup_keyword};

fn main() {
    let sid = SourceId(1);
    let span = Span::new(sid, 0, 2);

    // Идентификатор "fn"
    let ident = "fn";
    let kind = match lookup_keyword(ident) {
        Some(kw) => TokenKind::Keyword(kw),
        None => TokenKind::Ident,
    };

    let token = Token::new(kind, span);

    println!("{:?}", token);
    // Token { kind: Keyword(Fn), span: Span { source: SourceId(1), start: 0, end: 2 } }
}
```

---

## Связь с другими подсистемами

* **Lexer** использует `TokenKind` и `lookup_keyword` для генерации токенов.
* **Parser** строит AST, различая идентификаторы, ключевые слова и операторы.
* **Diagnostics** опирается на `Span`, чтобы печатать ошибки с позициями.
* **Doctest runner** обрабатывает `DirectiveKind` (`/// test:` и т.п.).
