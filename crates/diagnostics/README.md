# Surge Diagnostics Crate

**Surge Diagnostics** — это подсистема языка Surge, отвечающая за сбор, обработку и форматирование диагностических сообщений (ошибок, предупреждений, подсказок) из различных этапов компиляции.

Модуль обеспечивает единообразную работу с диагностикой во всех подсистемах компилятора: лексер, парсер, семантический анализ и кодогенерация.

---

## Цели и задачи

* **Унифицированная модель диагностики**: единая структура `Diagnostic` для всех типов сообщений с поддержкой связанных сообщений.
* **Гибкое форматирование**: поддержка различных форматов вывода (Pretty, JSON, CSV) для интеграции с IDE и CI/CD.
* **Работа с исходным кодом**: привязка диагностики к конкретным позициям в исходном коде с поддержкой UTF-8.
* **Расширяемость**: простая интеграция новых источников диагностики и форматов вывода.
* **Минимальные зависимости**: использует только необходимые внешние библиотеки (`serde`, `csv`, `thiserror`).

---

## Структура

```
diagnostics/
├── Cargo.toml
└── src/
    ├── lib.rs          # основной API (Diagnostic, Reporter, Formatter)
    ├── model.rs        # базовые типы (Diagnostic, Severity, Code, SpanRef)
    ├── source.rs       # работа с исходным кодом (SourceMap, SourceTextProvider)
    ├── collect.rs      # сбор диагностики из других подсистем
    ├── report.rs       # Reporter и ReportOptions
    └── format/         # форматтеры вывода
        ├── mod.rs      # трейт Formatter и FormatError
        ├── pretty.rs   # PrettyFormatter (человекочитаемый формат)
        ├── json.rs     # JsonFormatter (машиночитаемый JSON)
        └── csv_format.rs # CsvFormatter (табличный формат)
```

---

## Основные компоненты

### 1. Модель диагностики (`model.rs`)

Базовые типы для представления диагностических сообщений:

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Severity { Error, Warning, Info, Hint }

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Code(pub String);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SpanRef {
    pub source: SourceId,
    pub start: u32,  // байтовый offset (включительно)
    pub end: u32,    // байтовый offset (исключительно)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Diagnostic {
    pub severity: Severity,
    pub code: Code,
    pub message: String,
    pub span: SpanRef,
    pub related: Vec<Related>,  // связанные сообщения
}
```

Пример создания диагностики:

```rust
use surge_diagnostics::{Diagnostic, Severity, Code, SpanRef, SourceId};

let span = SpanRef { source: SourceId(1), start: 10, end: 15 };
let diag = Diagnostic::error("SYNTAX_ERROR", "Expected ';', got identifier", span);
```

### 2. Работа с исходным кодом (`source.rs`)

Компоненты для привязки диагностики к исходному коду:

* `SourceMap` — маппинг `SourceId` на человекочитаемые имена файлов.
* `SourceTextProvider` — трейт для получения текста исходного кода.
* `InMemorySourceText` — реализация провайдера для работы с текстом в памяти.
* `line_col()` — утилита для вычисления номера строки и колонки из байтового offset.

Пример:

```rust
use surge_diagnostics::{SourceMap, InMemorySourceText, SourceId};

let mut sources = SourceMap::new();
sources.insert(SourceId(1), "main.sg");

let mut text = InMemorySourceText::new();
text.insert(SourceId(1), "let x = 42".to_string());

let (line, col) = line_col(&text.get(SourceId(1)).unwrap(), 5);
assert_eq!((line, col), (1, 6));
```

### 3. Сбор диагностики (`collect.rs`)

Функции для преобразования диагностики из других подсистем в единый формат:

```rust
use surge_diagnostics::from_lexer_diags;

let lexer_diags = vec![/* LexDiag из лексера */];
let diagnostics = from_lexer_diags(SourceId(1), &lexer_diags);
```

Поддерживаемые источники:
* `from_lexer_diags()` — диагностика из лексера (`LexDiag` → `Diagnostic`)

### 4. Форматирование (`format/`)

Система форматирования с поддержкой различных форматов вывода:

#### PrettyFormatter (человекочитаемый формат)

```
main.sg:2:5: Error [SYNTAX_ERROR]: Expected ';', got identifier
    2 | let x = 42
      |     ~~~~^
```

#### JsonFormatter (машиночитаемый JSON)

```json
[
  {
    "severity": "Error",
    "code": "SYNTAX_ERROR",
    "message": "Expected ';', got identifier",
    "span": {
      "source": {"0": 1},
      "start": 10,
      "end": 15
    },
    "related": []
  }
]
```

#### CsvFormatter (табличный формат)

```csv
file,line,col,end_line,end_col,severity,code,message
main.sg,2,5,2,10,Error,SYNTAX_ERROR,Expected ';', got identifier
```

### 5. Reporter (`report.rs`)

Основной компонент для рендеринга диагностики:

```rust
use surge_diagnostics::{Reporter, ReportOptions, Format, SourceMap, InMemorySourceText};

let sources = SourceMap::new();
let text = Box::new(InMemorySourceText::new());
let opts = ReportOptions { format: Format::Pretty };
let reporter = Reporter::new(sources, text, opts);

let diagnostics = vec![/* Diagnostic */];
let output = reporter.render(&diagnostics)?;
println!("{}", output);
```

---

## API

### Основные типы

```rust
// Создание диагностики
pub fn Diagnostic::error(code: impl Into<String>, message: impl Into<String>, span: SpanRef) -> Self

// Форматы вывода
pub enum Format { Pretty, Json, Csv }

// Опции репортера
pub struct ReportOptions {
    pub format: Format,
}

// Основной репортер
pub struct Reporter {
    pub sources: SourceMap,
    pub text: Box<dyn SourceTextProvider + Send + Sync>,
    pub opts: ReportOptions,
}
```

### Основные функции

```rust
// Сбор диагностики из лексера
pub fn from_lexer_diags(file: SourceId, diags: &[LexDiag]) -> Vec<Diagnostic>

// Вычисление позиции в исходном коде
pub fn line_col(text: &str, byte_off: usize) -> (usize, usize)
```

---

## Примеры использования

### Базовое использование

```rust
use surge_diagnostics::{Diagnostic, Severity, Code, SpanRef, SourceId, Reporter, ReportOptions, Format, SourceMap, InMemorySourceText};

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Создание диагностики
    let span = SpanRef { source: SourceId(1), start: 10, end: 15 };
    let diag = Diagnostic::error("SYNTAX_ERROR", "Expected ';', got identifier", span);
    
    // Настройка репортера
    let mut sources = SourceMap::new();
    sources.insert(SourceId(1), "main.sg");
    
    let mut text = InMemorySourceText::new();
    text.insert(SourceId(1), "let x = 42".to_string());
    
    let opts = ReportOptions { format: Format::Pretty };
    let reporter = Reporter::new(sources, Box::new(text), opts);
    
    // Рендеринг
    let output = reporter.render(&[diag])?;
    println!("{}", output);
    
    Ok(())
}
```

### Интеграция с лексером

```rust
use surge_lexer::{lex, LexOptions, SourceId};
use surge_diagnostics::{from_lexer_diags, Reporter, ReportOptions, Format, SourceMap, InMemorySourceText};

let src = r#"let x = "hello"#; // незакрытая строка
let opts = LexOptions::default();
let result = lex(src, SourceId(1), &opts);

// Преобразование диагностики лексера
let diagnostics = from_lexer_diags(SourceId(1), &result.diags);

// Настройка репортера
let mut sources = SourceMap::new();
sources.insert(SourceId(1), "test.sg");

let mut text = InMemorySourceText::new();
text.insert(SourceId(1), src.to_string());

let reporter = Reporter::new(sources, Box::new(text), ReportOptions { format: Format::Pretty });

// Вывод диагностики
if !diagnostics.is_empty() {
    println!("{}", reporter.render(&diagnostics)?);
}
```

### Работа с различными форматами

```rust
use surge_diagnostics::{Format, Reporter, ReportOptions, SourceMap, InMemorySourceText};

let diagnostics = vec![/* Diagnostic */];
let sources = SourceMap::new();
let text = Box::new(InMemorySourceText::new());

// Pretty формат (для пользователей)
let pretty_reporter = Reporter::new(sources.clone(), text.clone(), ReportOptions { format: Format::Pretty });
println!("{}", pretty_reporter.render(&diagnostics)?);

// JSON формат (для IDE/инструментов)
let json_reporter = Reporter::new(sources.clone(), text.clone(), ReportOptions { format: Format::Json });
let json_output = json_reporter.render(&diagnostics)?;

// CSV формат (для анализа/статистики)
let csv_reporter = Reporter::new(sources, text, ReportOptions { format: Format::Csv });
let csv_output = csv_reporter.render(&diagnostics)?;
```

---

## Интеграция с другими подсистемами

* **Lexer** → `from_lexer_diags()` преобразует `LexDiag` в `Diagnostic`
* **Parser** → планируется `from_parser_diags()` для синтаксических ошибок
* **Semantic** → планируется `from_semantic_diags()` для семантических ошибок
* **CLI** → использует `Reporter` для вывода диагностики пользователю
* **IDE** → использует `JsonFormatter` для интеграции с редакторами

---

## Расширение функциональности

### Добавление нового формата

```rust
use surge_diagnostics::format::{Formatter, FormatError};

pub struct XmlFormatter;

impl Formatter for XmlFormatter {
    fn format(
        &self,
        diagnostics: &[Diagnostic],
        sources: &SourceMap,
        text: &dyn SourceTextProvider,
    ) -> Result<String, FormatError> {
        // Реализация XML форматирования
        Ok("<!-- XML output -->".to_string())
    }
}
```

### Добавление нового источника диагностики

```rust
use surge_diagnostics::{Diagnostic, Severity, Code, SpanRef};

pub fn from_parser_diags(file: SourceId, diags: &[ParserDiag]) -> Vec<Diagnostic> {
    diags.iter().map(|d| {
        Diagnostic {
            severity: Severity::Error,
            code: Code(format!("PARSE_{:?}", d.kind)),
            message: d.message.clone(),
            span: SpanRef { source: file, start: d.start, end: d.end },
            related: vec![],
        }
    }).collect()
}
```

---

## Тесты

В каждом модуле реализованы unit-тесты:

* корректность создания и сериализации `Diagnostic`
* работа с UTF-8 в `line_col()`
* форматирование в различных форматах
* интеграция с лексером через `from_lexer_diags()`

Прогон всех тестов:

```bash
cargo test -p surge-diagnostics
```
