# Surge Lexer Crate

**Surge Lexer** — это подсистема языка Surge, отвечающая за разбор исходного текста в поток токенов.
Она используется как первый этап компиляции: токенизация → парсинг → семантика → кодогенерация.

---

## Цели и задачи

* **Корректная работа с UTF-8**: курсор (`Cursor`) всегда работает на границах UTF-8, корректно обрабатывая многобайтовые символы.
* **Простая диагностика**: сбор ошибок в виде списка (`LexDiag`), без немедленного выхода.
* **Поддержка “живых” комментариев / директив** (`/// test:`, `/// benchmark:` и др.) для doctest-раннера.
* **Тривия (whitespace, комментарии)** может сохраняться или игнорироваться по опции.
* **Минимальная зависимость**: crate зависит только от `surge_token` (базовые типы токенов, `Span`, `SourceId`).

---

## Структура

```
lexer/
├── Cargo.toml
└── src/
    ├── cursor.rs       # итератор по UTF-8 входу
    ├── emit.rs         # эмиттер токенов, тривии, диагностики
    ├── lib.rs          # основной API (lex, LexOptions, LexResult)
    └── rules/          # правила лексера
        ├── comments.rs # пробелы, //, /* ... */
        ├── directive.rs# /// test:/benchmark:/time:
        ├── ident.rs    # идентификаторы и ключевые слова
        ├── number.rs   # числа (int/float, 0x, 0b, экспоненты)
        ├── punct.rs    # операторы и пунктуация
        └── string.rs   # строковые литералы + escapes
```

---

## Основные компоненты

### 1. Cursor (`cursor.rs`)

Минималистичный курсор для работы с UTF-8 строкой:

* `peek`, `peek_n` — просмотр текущего и следующих символов.
* `bump` — сдвиг вперёд на одну руну.
* `bump_while` — поглощение последовательности по предикату.
* `starts_with`, `bump_str` — работа с подстроками (для операторов).
* `save_pos` / `restore_pos` — контрольные точки для откатов (нужно для директив).

Пример:

```rust
let mut cur = Cursor::new("πx", file);
assert_eq!(cur.peek(), Some('π'));
assert_eq!(cur.bump(), Some('π')); // сдвинулся на 2 байта
assert_eq!(cur.peek(), Some('x'));
```

### 2. Emitter (`emit.rs`)

Накопитель результатов:

* `tokens: Vec<Token>` — основные токены.
* `trivia: Vec<Trivia>` — пробелы и комментарии (если `keep_trivia = true`).
* `diags: Vec<LexDiag>` — диагностические сообщения.

Методы:

* `token(start, end, kind)` — добавить токен.
* `trivia(start, end, kind)` — добавить тривию.
* `diag(span, code, msg)` — добавить диагностику.

Коды ошибок (`DiagCode`):

* `UnclosedString`, `BadEscape`
* `UnclosedBlockComment`
* `InvalidDigitForBase`
* `UnknownChar`
* `UnknownDirective`, `InvalidDirectiveFormat`

### 3. Правила (`rules/`)

Каждый файл отвечает за свою группу токенов:

* `comments.rs` — whitespace, `//`, `/* ... */` (с поддержкой вложенности).
* `directive.rs` — `/// test:` и подобные (токенизируются отдельно с `TokenContext::Directive`).
* `ident.rs` — идентификаторы и ключевые слова (`lookup_keyword` из `surge_token`).
* `number.rs` — int/float литералы (`0x`, `0b`, десятичные, экспоненты, `_` для разделения).
* `punct.rs` — многосимвольные (`::`, `->`, `&&` и т.д.) и одиночные (`;`, `+`, `-`, `*`).
* `string.rs` — строки, поддержка `\"`, `\n`, `\u{hex}` и диагностика ошибок.

### 4. API (`lib.rs`)

* `LexOptions` — опции работы:

  ```rust
  pub struct LexOptions {
      pub keep_trivia: bool,       // сохранять пробелы и комментарии
      pub enable_directives: bool, // включить поддержку /// test:
  }
  ```

* `LexResult` — результат:

  ```rust
  pub struct LexResult {
      pub tokens: Vec<Token>,
      pub trivia: Vec<Trivia>,
      pub diags: Vec<LexDiag>,
  }
  ```

* `lex(source, file, opts)` — основная функция:

  ```rust
  use surge_lexer::{lex, LexOptions, SourceId};

  let opts = LexOptions { keep_trivia: true, enable_directives: true };
  let result = lex("let x = 42;", SourceId(0), &opts);

  for tok in result.tokens {
      println!("{:?}", tok);
  }
  ```

---

## Поддерживаемый синтаксис

Кратко:

* Идентификаторы и ключевые слова (`fn`, `let`, `mut`, …).
* Числа: `123`, `0xFF`, `0b1010`, `1.23`, `2e-3`.
* Строки: `"..."` с escapes.
* Булевы литералы: `true`, `false`.
* Операторы: `+ - * / == != <= >= && || :=` и др.
* Комментарии: `//`, `/* ... */`, `/// test:`.

---

## Примеры использования

### Лексинг простого выражения

```rust
use surge_lexer::{lex, LexOptions, SourceId, TokenKind};

let src = r#"let s:string = "hello";"#;
let opts = LexOptions::default();
let result = lex(src, SourceId(1), &opts);

assert!(result.diags.is_empty());
assert_eq!(result.tokens[0].kind, TokenKind::Keyword(surge_token::Keyword::Let));
assert_eq!(result.tokens[1].kind, TokenKind::Ident);
assert_eq!(result.tokens[2].kind, TokenKind::Colon);
assert_eq!(result.tokens[3].kind, TokenKind::Ident); // string
assert_eq!(result.tokens[4].kind, TokenKind::Eq);
assert_eq!(result.tokens[5].kind, TokenKind::StringLit);
```

### Обработка ошибки

```rust
let src = r#"let x = "hello"#; // строка без закрывающей кавычки
let result = lex(src, SourceId(2), &LexOptions::default());

assert_eq!(result.diags.len(), 1);
println!("diag: {:?}", result.diags[0]);
```

Вывод:

```
diag: LexDiag { span: [file=2, start=8, end=14], code: UnclosedString, message: "Unclosed string literal" }
```

---

## Тесты

В `cursor.rs`, `emit.rs`, `rules/*` реализованы unit-тесты:

* корректная работа с UTF-8
* тривия (с сохранением и без)
* вложенные комментарии
* строки и escape-последовательности
* директивы (`/// test:`) и диагностика ошибок

Прогон всех тестов:

```bash
cargo test -p surge_lexer
```
