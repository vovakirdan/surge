# Стандартная библиотека Surge
[English](STDLIB.md) | [Russian](STDLIB.ru.md)

> **Статус:** Реализованная поверхность API на текущем состоянии репозитория
> **Аудитория:** Пользователи Surge, которым нужен практический справочник по shipped stdlib
> **Цель:** Описывает публичные модули `stdlib`, их основные экспорты и типовые сценарии использования.

См. также: [MODULES.ru.md](MODULES.ru.md), [RUNTIME.ru.md](RUNTIME.ru.md), [CONCURRENCY.ru.md](CONCURRENCY.ru.md).

---

## 1. Обзор

Surge поставляет стандартную библиотеку в пространстве импортов `stdlib/...`.

Примеры:

```sg
import stdlib/fs;
import stdlib/json;
import stdlib/random;
import stdlib/uuid;
```

Этот документ описывает именно ту публичную поверхность, которая реально есть в репозитории сейчас. Он намеренно опирается на concrete API, а не на отложенные или запланированные абстракции.

Важно:

- `stdlib` частично написана на Surge, а частично опирается на runtime-backed intrinsics.
- VM и LLVM/native поддерживают описанные здесь модули, если в разделе не сказано иное.
- `stdlib/random.RandomSource<T>` существует, но самый надежный пользовательский путь сегодня — concrete API: `SystemRng`, `Pcg32` и верхнеуровневые helper-функции.
- В `stdlib` также входят directive-oriented helper-модули под `stdlib/directives/...`.

---

## 2. Индекс модулей

| Модуль | Назначение | Типичное применение |
| --- | --- | --- |
| `stdlib/entropy` | безопасные байты энтропии от хоста | secure randomness input |
| `stdlib/random` | host-backed RNG и детерминированный `Pcg32` | токены, тесты, фикстуры |
| `stdlib/uuid` | UUID parse/format/v4 | идентификаторы |
| `stdlib/fs` | файловый ввод-вывод | чтение/запись файлов, обход директорий |
| `stdlib/path` | чистые path helper-функции | join, normalize, basename |
| `stdlib/strings` | небольшие string helper-функции | `ord`, `chr`, `is_int` |
| `stdlib/time` | монотонные duration-значения | измерение elapsed time |
| `stdlib/json` | JSON value model, parse, stringify | конфиги, payload'ы |
| `stdlib/net` | async TCP helpers | сокеты, кастомные протоколы |
| `stdlib/http` и подмодули | HTTP request/response/server helpers | HTTP-сервисы |
| `stdlib/term` и `stdlib/term/ansi` | terminal IO и ANSI output | TUI и управление терминалом |
| `stdlib/directives/test` | directive-only helper'ы для тестов | блоки `/// test:` |
| `stdlib/directives/benchmark` | directive-only helper'ы для benchmark'ов | блоки `/// benchmark:` |
| `stdlib/directives/time` | directive-only helper'ы для профилирования | блоки `/// time:` |
| `stdlib/saturating_cast` | saturating numeric conversion | безопасные приведения чисел |

---

## 3. `stdlib/entropy`

Импорт:

```sg
import stdlib/entropy as entropy;
```

Публичный API:

- `ENTROPY_ERR_UNAVAILABLE`
- `ENTROPY_ERR_BACKEND`
- `bytes(len: uint) -> Erring<byte[], Error>`
- `fill(out: &mut byte[]) -> Erring<nothing, Error>`

Используй `entropy`, когда нужны свежие безопасные байты от host runtime. Этот модуль не предлагает seeded deterministic режим и не делает fallback на слабые источники вроде часов или счетчиков.

Пример:

```sg
import stdlib/entropy as entropy;

fn nonce16() -> Erring<byte[], Error> {
    return entropy.bytes(16:uint);
}

fn refill(buf: &mut byte[]) -> Erring<nothing, Error> {
    return entropy.fill(buf);
}
```

Замечание про replay:

- В VM record/replay режиме точные байты энтропии логируются и затем воспроизводятся детерминированно.

---

## 4. `stdlib/random`

Импорт:

```sg
import stdlib/random as random;
```

Публичный API:

- `contract RandomSource<T>`
- `RANDOM_ERR_ZERO_LIMIT`
- `RANDOM_ERR_EMPTY_RANGE`
- `type SystemRng`
- `type Pcg32`
- `system() -> SystemRng`
- `bytes(n: uint) -> Erring<byte[], Error>`
- `fill(out: &mut byte[]) -> Erring<nothing, Error>`
- `next_bool() -> Erring<bool, Error>`
- `next_u32() -> Erring<uint32, Error>`
- `next_u64() -> Erring<uint64, Error>`
- `below_u32(limit: uint32) -> Erring<uint32, Error>`
- `below_u64(limit: uint64) -> Erring<uint64, Error>`
- `range_u32(start: uint32, end_exclusive: uint32) -> Erring<uint32, Error>`
- `range_u64(start: uint64, end_exclusive: uint64) -> Erring<uint64, Error>`
- `pcg32(seed: uint64) -> Pcg32`
- `pcg32_stream(seed: uint64, stream: uint64) -> Pcg32`
- `SystemRng.fill(...)`, `SystemRng.next_bool()`, `SystemRng.next_u32()`, `SystemRng.next_u64()`
- `SystemRng.below_u32(...)`, `SystemRng.below_u64(...)`, `SystemRng.range_u32(...)`, `SystemRng.range_u64(...)`
- `Pcg32.fill(...)`, `Pcg32.next_bool()`, `Pcg32.next_u32()`, `Pcg32.next_u64()`
- `Pcg32.below_u32(...)`, `Pcg32.below_u64(...)`, `Pcg32.range_u32(...)`, `Pcg32.range_u64(...)`

Разделение ролей:

- `SystemRng` использует `stdlib/entropy`.
- `Pcg32` детерминирован и подходит для тестов и фикстур.
- `Pcg32` не является криптографически стойким.
- `RandomSource<T>` остается минимальным primitive contract: `fill`, `next_u32`, `next_u64`.
- Range helpers используют полуоткрытые диапазоны: `[start, end_exclusive)`.
- `below_*` возвращает `RANDOM_ERR_ZERO_LIMIT` для нулевого limit; `range_*` возвращает `RANDOM_ERR_EMPTY_RANGE`, если `start >= end_exclusive`.
- Ошибки entropy/backend от источника проходят наружу без оборачивания.

Пример: secure random bytes

```sg
import stdlib/random as random;

fn session_key() -> Erring<byte[], Error> {
    return random.bytes(32:uint);
}
```

Пример: deterministic fixture data

```sg
import stdlib/random as random;

fn fixture_word() -> Erring<uint64, Error> {
    let mut rng: random.Pcg32 = random.pcg32_stream(42:uint64, 54:uint64);
    return rng.next_u64();
}
```

Пример: deterministic bounded value

```sg
import stdlib/random as random;

fn fixture_index() -> Erring<uint32, Error> {
    let mut rng: random.Pcg32 = random.pcg32(123:uint64);
    return rng.range_u32(10:uint32, 20:uint32);
}
```

Замечание про реальность:

- generic contract существует, но generic consumer-код вокруг `&mut T` пока заметно более хрупкий, чем concrete path через `SystemRng` и `Pcg32`. В пользовательском коде сейчас лучше опираться на concrete API.

---

## 5. `stdlib/uuid`

Импорт:

```sg
import stdlib/uuid as uuid;
```

Публичный API:

- `UUID_ERR_PARSE`
- `UUID_ERR_RANDOM`
- `type Uuid`
- `nil() -> Uuid`
- `parse(text: &string) -> Erring<Uuid, Error>`
- `v4() -> Erring<Uuid, Error>`
- `v4_from_system(rng: &mut random.SystemRng) -> Erring<Uuid, Error>`
- `v4_from_pcg32(rng: &mut random.Pcg32) -> Erring<Uuid, Error>`
- `Uuid.to_string() -> string`
- `Uuid.is_nil() -> bool`

Поведение:

- `to_string()` выдаёт canonical lowercase строку.
- `parse()` ожидает canonical 36-character layout с дефисами.
- `v4()` использует host-backed randomness.
- `v4_from_pcg32()` полезен для deterministic tests.

Пример:

```sg
import stdlib/random as random;
import stdlib/uuid as uuid;

fn make_user_id() -> Erring<string, Error> {
    compare uuid.v4() {
        Success(value) => {
            return Success(value.to_string());
        }
        err => {
            return err;
        }
    };
}

fn deterministic_id() -> Erring<string, Error> {
    let mut seeded: random.Pcg32 = random.pcg32_stream(42:uint64, 54:uint64);
    compare uuid.v4_from_pcg32(&mut seeded) {
        Success(value) => {
            return Success(value.to_string());
        }
        err => {
            return err;
        }
    };
}
```

Замечание про реальность:

- `Uuid` сейчас хранит байты в `byte[]` с внутренним инвариантом длины `16`.
- shipped API использует `v4_from_system(...)` и `v4_from_pcg32(...)`, а не generic `v4_from<T>(...)`.

---

## 6. `stdlib/fs`

Импорт:

```sg
import stdlib/fs as fs;
```

Основной публичный API:

- `FsResult<T>`
- core filesystem types, с которыми работает модуль:
  - `FsError`
  - `FileType`, `FileTypes`
  - `DirEntry`
  - `File`
  - `FsOpenFlags`, `FS_O`
  - `SeekWhence`, `SeekWhences`
- файловые helper-функции:
  - `read_to_bytes`
  - `write_bytes`
  - `read_to_string`
  - `write_string`
- handle-based IO:
  - `open`
  - `close`
  - `read`
  - `read_all`
  - `write_all`
  - `seek`
  - `flush`
- convenience helpers:
  - `head`
  - `tail`
  - `read_dir`
  - `walkdir`
  - `WalkDir`

Используй `fs` для обычного файлового ввода-вывода и обхода директорий.

Пример:

```sg
import stdlib/fs as fs;

fn load_config(path: string) -> Erring<string, FsError> {
    return fs.read_to_string(path);
}
```

---

## 7. `stdlib/path`

Импорт:

```sg
import stdlib/path as path;
```

Публичный API:

- `join`
- `basename`
- `dirname`
- `extname`
- `normalize`
- `is_abs`

Это чистые string transformation helper'ы с POSIX-style `/` semantics.

---

## 8. `stdlib/strings`

Импорт:

```sg
import stdlib/strings as strings;
```

Публичный API:

- `ASCII`
- `ord(s: &string) -> uint`
- `chr(cp: uint) -> Erring<string, Error>`
- `is_int(s: &string) -> bool`

Используй этот модуль для небольших Unicode и validation helper-функций.

---

## 9. `stdlib/time`

Импорт:

```sg
import stdlib/time as time;
```

Публичный API:

- `type Duration`
- `monotonic_now() -> Duration`
- `Duration.new(nanos) -> Duration`
- `Duration.now() -> Duration`
- `Duration.sub(other) -> Duration`
- `Duration.as_seconds() -> int64`
- `Duration.as_millis() -> int64`
- `Duration.as_micros() -> int64`
- `Duration.as_nanos() -> int64`

`Duration` копируемый и хранит целые наносекунды. `Duration.new` строит duration из целых наносекунд. `Duration.now` возвращает текущую монотонную отметку времени как duration; используйте её с `sub` для измерения elapsed time. Методы конвертации единиц возвращают целые значения `int64`.

`time` сейчас даёт монотонные часы для измерения elapsed time. Это не wall-clock calendar API.

Пример:

```sg
import stdlib/time as time;

fn elapsed_ms(start: time.Duration) -> int64 {
    let now: time.Duration = time.Duration.now();
    return now.sub(start).as_millis();
}
```

---

## 10. `stdlib/json`

Импорт:

```sg
import stdlib/json as json;
```

Публичный API распределён между `json.sg`, `parser.sg` и `stringify.sg`.

Основные типы:

- `JsonError`
- `JsonValue`
- теги:
  - `JsonNull`
  - `JsonBool`
  - `JsonNumber`
  - `JsonString`
  - `JsonArray`
  - `JsonObject`
- `JsonEncodable<T>`

Основные функции:

- `parse(input: &string) -> Erring<JsonValue, JsonError>`
- `parse_bytes(input: byte[]) -> Erring<JsonValue, JsonError>`
- `stringify(value: &JsonValue) -> string`

Также есть `to_json()`-реализации для `string`, `bool`, `int`, `uint` и `JsonValue`.

Пример:

```sg
import stdlib/json as json;

fn parse_payload(raw: &string) -> Erring<json.JsonValue, json.JsonError> {
    return json.parse(raw);
}
```

---

## 11. `stdlib/net`

Импорт:

```sg
import stdlib/net as net;
```

Публичный API:

- core networking types, на которые опирается модуль:
  - `NetError`
  - `NetResult<T>`
  - `TcpListener`
  - `TcpConn`
- lifecycle:
  - `listen`
  - `close_listener`
  - `connect`
  - `close_conn`
- async operations:
  - `accept`
  - `read_some`
  - `write_some`
  - `write_all`

Этот модуль даёт async TCP helper-функции поверх runtime intrinsics.

---

## 12. HTTP Family

### 12.1 `stdlib/http`

Импорт:

```sg
import stdlib/http as http;
```

Основные публичные типы:

- `HttpVersion`
- `Header`, `Headers`
- `QueryParam`, `QueryParams`
- `HttpError`
- `BodyReader`
- `Request`
- `ByteStream`
- `ResponseBody`
- `Response`
- `Handler`
- `ServerConfig`

Основные публичные конструкторы и helper-функции:

- `bytestream`
- `default_server_config`
- `request_header`
- `request_has_header`
- `request_content_length`
- `request_keep_alive`

### 12.2 `stdlib/http/parser`

Публичный API:

- `parse_request`
- `ByteStream.next()`

### 12.3 `stdlib/http/query`

Публичный API:

- `parse_query`
- `request_query_params`
- `query_param`
- `query_has`
- `query_values`

### 12.4 `stdlib/http/headers`

Публичный API:

- `header_value`
- `headers_has`
- `headers_with`
- `headers_set`
- `headers_without`

### 12.5 `stdlib/http/cookie`

Публичный API:

- типы:
  - `Cookie`
  - `Cookies`
  - `SetCookie`
- parse/request helper-функции:
  - `parse_cookie_header`
  - `request_cookies`
  - `cookie_value`
  - `cookie_has`
  - `request_cookie`
- response helper-функции:
  - `default_set_cookie`
  - `expiring_set_cookie`
  - `delete_set_cookie`
  - `delete_set_cookie_at`
  - `response_with_set_cookie`
  - `response_set_cookie`
  - `response_expiring_cookie`
  - `response_delete_cookie`
  - `response_delete_cookie_at`

### 12.6 `stdlib/http/response`

Публичный API:

- `write_response`
- `response_empty`
- `response_bytes`
- `response_text`
- `response_html`
- `response_json`
- `response_stream`
- `response_redirect`
- `response_found`
- `response_see_other`
- `response_temporary_redirect`
- `response_permanent_redirect`
- `response_with_header`
- `response_header`
- `response_has_header`
- `response_set_header`
- `response_remove_header`

### 12.7 `stdlib/http/context`

Публичный API:

- тип:
  - `Context`
- constructors:
  - `context`
  - `context_with_response`
  - `into_response`
  - `context_json`
- request-side methods:
  - `header`
  - `has_header`
  - `content_length`
  - `keep_alive`
  - `query_params`
  - `query`
  - `has_query`
  - `query_all`
  - `cookies`
  - `cookie`
  - `read_body`
  - `discard_body`
  - `read_body_text`
- response-side methods:
  - `set_status`
  - `append_header`
  - `set_header`
  - `remove_header`
  - `set_cookie`
  - `set_cookie_value`
  - `expire_cookie`
  - `delete_cookie`
  - `delete_cookie_at`
  - `empty`
  - `bytes`
  - `text`
  - `html`
  - `stream`
  - `redirect`
  - `found`
  - `see_other`
  - `temporary_redirect`
  - `permanent_redirect`

### 12.8 `stdlib/http/body`

Публичный API:

- `BodyReader.next() -> Task<Erring<byte[], HttpError>>`

### 12.9 `stdlib/http/server`

- Этот файл сейчас содержит implementation support для HTTP stack и не экспортирует собственный публичный API.

---

## 13. Terminal Modules

### 13.1 `stdlib/term`

Публичный API:

- типы:
  - `TermMods`
  - `TermMod`
  - `KeyEvent`
  - `TermKey`
  - `TermEvent`
- теги:
  - `Char`, `Enter`, `Esc`, `Backspace`, `Tab`
  - `Up`, `Down`, `Left`, `Right`
  - `Home`, `End`, `PageUp`, `PageDown`, `Delete`
  - `F`
  - `Key`, `Resize`, `Eof`
- функции:
  - `enter`
  - `leave`
  - `write_str`
  - `read_event_async`

### 13.2 `stdlib/term/ansi`

Публичный API:

- `Ansi`
- builders и writers:
  - `new`
  - `with_capacity`
  - `clear`
  - `reserve`
  - `push_byte`
  - `push_bytes`
  - `push_str`
  - `push_uint`
  - `push_int`
  - `esc`
  - `csi`
  - `sgr_reset`
  - `sgr_bold`
  - `fg_256`
  - `bg_256`
  - `move_to`
  - `clear_screen`
  - `clear_line`
  - `write`
  - `flush`
  - `to_bytes`
  - `take_bytes`

Используй `term` для terminal mode и событий, а `ansi` — чтобы безопасно собирать escape-sequence output.

### 13.3 `stdlib/term/intrinsics`

- Этот файл экспортирует низкоуровневые terminal intrinsics, которые использует `stdlib/term` и `stdlib/term/ansi`.
- Рассматривай его как implementation detail, а не как стабильный user-facing API.

---

## 14. Directive Modules

Эти модули рассчитаны на directive-driven сценарии, а не на обычные runtime-библиотеки.

### 14.1 `stdlib/directives/test`

Directive pragma:

```sg
pragma module::test, directive;
```

Публичный API:

- `eq<T>(actual, expected)`
- `assert(condition)`
- `assert_msg(condition, message)`
- `ne<T>(actual, expected)`
- `fail(message)`
- `skip(reason)`

### 14.2 `stdlib/directives/benchmark`

Directive pragma:

```sg
pragma module::benchmark, directive;
```

Публичный API:

- `BenchmarkResult`
- `throughput(name, iters, f)`
- `single(name, f)`
- `skip(reason)`

Текущее состояние:

- У модуля уже есть оформленная поверхность API, но реализация пока остаётся intentionally simple.

### 14.3 `stdlib/directives/time`

Directive pragma:

```sg
pragma module::time, directive;
```

Публичный API:

- `ProfileResult`
- `profile_fn(name, iters, f)`
- `profile_once(name, f)`
- `skip(reason)`

Текущее состояние:

- Как и `benchmark`, этот модуль уже можно документировать как shipped surface, но его реализация пока намеренно простая.

---

## 15. `stdlib/saturating_cast`

Импорт:

```sg
import stdlib/saturating_cast;
```

Публичный API:

- overload'ы `saturating_cast(value, target_type)` для разных integer и float комбинаций

Используй этот модуль, когда нужен clamped numeric conversion вместо overflow, truncation или backend-dependent behavior.

Пример:

```sg
import stdlib/saturating_cast;

fn to_u8(x: int) -> uint8 {
    return saturating_cast(x, 0:uint8);
}
```

---

## 16. Практические комбинации

### Сгенерировать secure UUID и сериализовать его

```sg
import stdlib/json as json;
import stdlib/uuid as uuid;

fn user_id_json() -> Erring<string, Error> {
    compare uuid.v4() {
        Success(id) => {
            let value: json.JsonValue = id.to_string().to_json();
            return Success(json.stringify(&value));
        }
        err => {
            return err;
        }
    };
}
```

### Прочитать JSON-файл с диска

```sg
import stdlib/fs as fs;
import stdlib/json as json;

fn load_json(path: string) -> Erring<json.JsonValue, Error> {
    compare fs.read_to_string(path) {
        Success(text) => {
            compare json.parse(&text) {
                Success(value) => return Success(value);
                err => return Error { message = err.message, code = err.code };
            };
        }
        err => {
            return Error { message = err.message, code = err.code };
        }
    };
}
```

### Seeded random bytes для deterministic test

```sg
import stdlib/random as random;

fn deterministic_chunk() -> Erring<byte[], Error> {
    let mut rng: random.Pcg32 = random.pcg32(123:uint64);
    let mut out: byte[] = Array::<byte>.with_len(8:uint);
    compare rng.fill(&mut out) {
        Success(_) => return Success(out);
        err => return err;
    };
}
```
