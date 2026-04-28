# Surge Standard Library
[English](STDLIB.md) | [Russian](STDLIB.ru.md)

> **Status:** Implemented surface as of the current repo state
> **Audience:** Surge users who want a practical reference for the shipped standard library
> **Purpose:** Describes the current public `stdlib` modules, their main exports, and common usage patterns.

See also: [MODULES.md](MODULES.md), [RUNTIME.md](RUNTIME.md), [CONCURRENCY.md](CONCURRENCY.md).

---

## 1. Overview

Surge ships a source-level standard library under the `stdlib/...` import namespace.

Examples:

```sg
import stdlib/fs;
import stdlib/json;
import stdlib/random;
import stdlib/uuid;
```

This document describes the public surface that is actually shipped in this repo. It prefers concrete, stable APIs over planned abstractions.

Important notes:

- `stdlib` is implemented partly in Surge and partly via runtime-backed intrinsics.
- VM and LLVM/native both support the modules described here unless a section says otherwise.
- `stdlib/random.RandomSource<T>` exists, but the most reliable public path today is the concrete API: `SystemRng`, `Pcg32`, and the top-level helper functions.
- `stdlib` also includes directive-oriented helper modules under `stdlib/directives/...`.

---

## 2. Module Index

| Module | Purpose | Typical use |
| --- | --- | --- |
| `stdlib/entropy` | Secure host entropy bytes | secure randomness input |
| `stdlib/random` | Host-backed RNG plus deterministic `Pcg32` | tokens, tests, fixtures |
| `stdlib/uuid` | UUID parse/format/v4 | identifiers |
| `stdlib/hash` | Stable non-cryptographic hashing | sharding, cache keys, snapshots |
| `stdlib/fs` | Filesystem IO | read/write files, walk directories |
| `stdlib/path` | Pure path helpers | join, normalize, basename |
| `stdlib/strings` | Small string helpers | `ord`, `chr`, `is_int` |
| `stdlib/time` | Monotonic durations | elapsed time measurement |
| `stdlib/json` | JSON value model, parse, stringify | config, payloads |
| `stdlib/net` | Async TCP helpers | sockets, custom protocols |
| `stdlib/http` and submodules | HTTP request/response/server helpers | HTTP services |
| `stdlib/term` and `stdlib/term/ansi` | terminal IO and ANSI output | TUIs, terminal control |
| `stdlib/directives/test` | directive-only testing helpers | `/// test:` blocks |
| `stdlib/directives/benchmark` | directive-only benchmarking helpers | `/// benchmark:` blocks |
| `stdlib/directives/time` | directive-only profiling helpers | `/// time:` blocks |
| `stdlib/saturating_cast` | saturating numeric conversion | boundary-safe casts |

---

## 3. `stdlib/entropy`

Import:

```sg
import stdlib/entropy as entropy;
```

Public API:

- `ENTROPY_ERR_UNAVAILABLE`
- `ENTROPY_ERR_BACKEND`
- `bytes(len: uint) -> Erring<byte[], Error>`
- `fill(out: &mut byte[]) -> Erring<nothing, Error>`

Use `entropy` when you need fresh secure bytes from the host runtime. This module does not offer seeded determinism and does not fall back to weak sources such as clocks or counters.

Example:

```sg
import stdlib/entropy as entropy;

fn nonce16() -> Erring<byte[], Error> {
    return entropy.bytes(16:uint);
}

fn refill(buf: &mut byte[]) -> Erring<nothing, Error> {
    return entropy.fill(buf);
}
```

Replay note:

- In VM record/replay mode, exact entropy bytes are logged and replayed deterministically.

---

## 4. `stdlib/random`

Import:

```sg
import stdlib/random as random;
```

Public API:

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

Design split:

- `SystemRng` is host-backed and uses `stdlib/entropy`.
- `Pcg32` is deterministic and suitable for tests and fixtures.
- `Pcg32` is not cryptographically secure.
- `RandomSource<T>` remains the minimal primitive contract: `fill`, `next_u32`, `next_u64`.
- Range helpers use half-open ranges: `[start, end_exclusive)`.
- `below_*` returns `RANDOM_ERR_ZERO_LIMIT` for a zero limit; `range_*` returns `RANDOM_ERR_EMPTY_RANGE` when `start >= end_exclusive`.
- Entropy/backend errors from the source are passed through unchanged.

Example: secure random bytes

```sg
import stdlib/random as random;

fn session_key() -> Erring<byte[], Error> {
    return random.bytes(32:uint);
}
```

Example: deterministic fixture data

```sg
import stdlib/random as random;

fn fixture_word() -> Erring<uint64, Error> {
    let mut rng: random.Pcg32 = random.pcg32_stream(42:uint64, 54:uint64);
    return rng.next_u64();
}
```

Example: deterministic bounded value

```sg
import stdlib/random as random;

fn fixture_index() -> Erring<uint32, Error> {
    let mut rng: random.Pcg32 = random.pcg32(123:uint64);
    return rng.range_u32(10:uint32, 20:uint32);
}
```

Reality note:

- The generic contract is present, but generic consumer code around `&mut T` is still more fragile than the concrete `SystemRng` and `Pcg32` paths. Prefer the concrete API in user-facing code today.

---

## 5. `stdlib/uuid`

Import:

```sg
import stdlib/uuid as uuid;
```

Public API:

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

Behavior:

- `to_string()` emits canonical lowercase text.
- `parse()` expects the canonical 36-character layout with hyphens.
- `v4()` uses host-backed randomness.
- `v4_from_pcg32()` is useful for deterministic tests.

Example:

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

Reality note:

- `Uuid` currently stores its bytes in `byte[]` with an internal fixed-length invariant of 16 bytes.
- The shipped API uses `v4_from_system(...)` and `v4_from_pcg32(...)`, not a generic `v4_from<T>(...)`.

---

## 6. `stdlib/hash`

Import:

```sg
import stdlib/hash as hash;
```

Public API:

- `STABLE64_VERSION`
- `STABLE64_SEED`
- `type Hash64`
- `type Xxh64`
- `type Stable64`
- `xxh64_bytes(bytes: &byte[], seed: uint64) -> Hash64`
- `xxh64_string(text: &string, seed: uint64) -> Hash64`
- `stable64_bytes(bytes: &byte[]) -> Hash64`
- `stable64_string(text: &string) -> Hash64`
- `stable64_with_seed(seed: uint64) -> Stable64`
- `Hash64.as_u64() -> uint64`
- `Hash64.bucket(bucket_count: uint) -> Option<uint>`
- `Hash64.to_hex() -> string`
- `Xxh64::new(seed: uint64) -> Xxh64`
- `Xxh64.update_byte(value: byte)`
- `Xxh64.update_bytes(...)`
- `Xxh64.update_string_bytes(...)`
- `Xxh64.finish() -> Hash64`
- `Stable64::new() -> Stable64`
- `Stable64::with_seed(seed: uint64) -> Stable64`
- `Stable64.write_bool(...)`, `write_byte(...)`
- `Stable64.write_u8(...)`, `write_u16(...)`, `write_u32(...)`, `write_u64(...)`
- `Stable64.write_i8(...)`, `write_i16(...)`, `write_i32(...)`, `write_i64(...)`
- `Stable64.write_bytes(...)`, `write_string(...)`
- `Stable64.begin_list(...)`, `begin_record(...)`, `write_field(...)`, `begin_variant(...)`
- `Stable64.finish() -> Hash64`

Design split:

- `Xxh64` is the raw byte-stream xxHash64 algorithm. It adds no tags, length prefixes, or schema information.
- `Stable64` is the structured hasher. It writes type tags, fixed-width little-endian payloads, lengths, and structural frames before hashing.
- `stable64_*` helpers use the v1 stable encoding. The unversioned names are the permanent v1 contract.
- This module is not cryptographic. Do not use it for passwords, signatures, MACs, authentication tokens, or hostile-input integrity checks.
- Dynamic `int` and `uint` writers are intentionally omitted. Pick an explicit fixed-width writer such as `write_i64()` or `write_u64()`.

Example: shard a key

```sg
import stdlib/hash as hash;

fn shard_for(key: &string, shard_count: uint) -> Option<uint> {
    let digest: hash.Hash64 = hash.stable64_string(key);
    return digest.bucket(shard_count);
}
```

Example: structured cache key

```sg
import stdlib/hash as hash;

fn cache_key(tenant: &string, key: &string, generation: uint64) -> hash.Hash64 {
    let record_name: string = "CacheKey";
    let tenant_field: string = "tenant";
    let key_field: string = "key";
    let generation_field: string = "generation";

    let mut h: hash.Stable64 = hash.Stable64::new();
    h.begin_record(&record_name, 3:uint);
    h.write_field(&tenant_field);
    h.write_string(tenant);
    h.write_field(&key_field);
    h.write_string(key);
    h.write_field(&generation_field);
    h.write_u64(generation);
    return h.finish();
}
```

Example: raw xxHash64 for byte compatibility

```sg
import stdlib/hash as hash;

fn raw_digest(bytes: &byte[]) -> string {
    let digest: hash.Hash64 = hash.xxh64_bytes(bytes, 0:uint64);
    return digest.to_hex();
}
```

Reality note:

- Generic `StableHash<T>` is planned separately after the concrete `Stable64` API has settled.
- FNV-1a is deferred. If added later, it should be documented as a compatibility or teaching algorithm, not the recommended default.

---

## 7. `stdlib/fs`

Import:

```sg
import stdlib/fs as fs;
```

Main public API:

- `FsResult<T>`
- core filesystem types used by this module:
  - `FsError`
  - `FileType`, `FileTypes`
  - `DirEntry`
  - `File`
  - `FsOpenFlags`, `FS_O`
  - `SeekWhence`, `SeekWhences`
- file-level helpers:
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

Use `fs` for regular file IO and directory traversal.

Example:

```sg
import stdlib/fs as fs;

fn load_config(path: string) -> Erring<string, FsError> {
    return fs.read_to_string(path);
}
```

---

## 8. `stdlib/path`

Import:

```sg
import stdlib/path as path;
```

Public API:

- `join`
- `basename`
- `dirname`
- `extname`
- `normalize`
- `is_abs`

These helpers use POSIX-style `/` semantics and are pure string transformations.

---

## 9. `stdlib/strings`

Import:

```sg
import stdlib/strings as strings;
```

Public API:

- `ASCII`
- `ord(s: &string) -> uint`
- `chr(cp: uint) -> Erring<string, Error>`
- `is_int(s: &string) -> bool`

Use this module for small Unicode and validation helpers.

---

## 10. `stdlib/time`

Import:

```sg
import stdlib/time as time;
```

Public API:

- `type Duration`
- `monotonic_now() -> Duration`
- `Duration.new(nanos) -> Duration`
- `Duration.now() -> Duration`
- `Duration.sub(other) -> Duration`
- `Duration.as_seconds() -> int64`
- `Duration.as_millis() -> int64`
- `Duration.as_micros() -> int64`
- `Duration.as_nanos() -> int64`

`Duration` is copyable and stores whole nanoseconds. `Duration.new` builds a duration from whole nanoseconds. `Duration.now` returns the current monotonic timestamp as a duration; use it with `sub` to measure elapsed time. Unit conversion methods return whole units as `int64`.

`time` currently exposes a monotonic clock for measuring elapsed time. It is not a wall-clock calendar API.

Example:

```sg
import stdlib/time as time;

fn elapsed_ms(start: time.Duration) -> int64 {
    let now: time.Duration = time.Duration.now();
    return now.sub(start).as_millis();
}
```

---

## 11. `stdlib/json`

Imports:

```sg
import stdlib/json as json;
```

Public API is split across `json.sg`, `parser.sg`, and `stringify.sg`.

Main types:

- `JsonError`
- `JsonValue`
- tags:
  - `JsonNull`
  - `JsonBool`
  - `JsonNumber`
  - `JsonString`
  - `JsonArray`
  - `JsonObject`
- `JsonEncodable<T>`

Main functions:

- `parse(input: &string) -> Erring<JsonValue, JsonError>`
- `parse_bytes(input: byte[]) -> Erring<JsonValue, JsonError>`
- `stringify(value: &JsonValue) -> string`

There are also `to_json()` implementations for `string`, `bool`, `int`, `uint`, and `JsonValue`.

Example:

```sg
import stdlib/json as json;

fn parse_payload(raw: &string) -> Erring<json.JsonValue, json.JsonError> {
    return json.parse(raw);
}
```

---

## 12. `stdlib/net`

Import:

```sg
import stdlib/net as net;
```

Public API:

- core networking types used by this module:
  - `NetError`
  - `NetResult<T>`
  - `TcpListener`
  - `TcpConn`
- connection lifecycle:
  - `listen`
  - `close_listener`
  - `connect`
  - `close_conn`
- async operations:
  - `accept`
  - `read_some`
  - `write_some`
  - `write_all`

This module provides async TCP helpers backed by runtime intrinsics.

---

## 13. HTTP Family

### 13.1 `stdlib/http`

Import:

```sg
import stdlib/http as http;
```

Core public types:

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

Core public constructors and helpers:

- `bytestream`
- `default_server_config`
- `request_header`
- `request_has_header`
- `request_content_length`
- `request_keep_alive`

### 13.2 `stdlib/http/parser`

Public API:

- `parse_request`
- `ByteStream.next()`

### 13.3 `stdlib/http/query`

Public API:

- `parse_query`
- `request_query_params`
- `query_param`
- `query_has`
- `query_values`

### 13.4 `stdlib/http/headers`

Public API:

- `header_value`
- `headers_has`
- `headers_with`
- `headers_set`
- `headers_without`

### 13.5 `stdlib/http/cookie`

Public API:

- types:
  - `Cookie`
  - `Cookies`
  - `SetCookie`
- parse/request helpers:
  - `parse_cookie_header`
  - `request_cookies`
  - `cookie_value`
  - `cookie_has`
  - `request_cookie`
- response helpers:
  - `default_set_cookie`
  - `expiring_set_cookie`
  - `delete_set_cookie`
  - `delete_set_cookie_at`
  - `response_with_set_cookie`
  - `response_set_cookie`
  - `response_expiring_cookie`
  - `response_delete_cookie`
  - `response_delete_cookie_at`

### 13.6 `stdlib/http/response`

Public API:

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

### 13.7 `stdlib/http/context`

Public API:

- type:
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

### 13.8 `stdlib/http/body`

Public API:

- `BodyReader.next() -> Task<Erring<byte[], HttpError>>`

### 13.9 `stdlib/http/server`

- This file currently contains implementation support for the HTTP stack and does not expose its own public API.

---

## 14. Terminal Modules

### 14.1 `stdlib/term`

Public API:

- types:
  - `TermMods`
  - `TermMod`
  - `KeyEvent`
  - `TermKey`
  - `TermEvent`
- tags:
  - `Char`, `Enter`, `Esc`, `Backspace`, `Tab`
  - `Up`, `Down`, `Left`, `Right`
  - `Home`, `End`, `PageUp`, `PageDown`, `Delete`
  - `F`
  - `Key`, `Resize`, `Eof`
- functions:
  - `enter`
  - `leave`
  - `write_str`
  - `read_event_async`

### 14.2 `stdlib/term/ansi`

Public API:

- `Ansi`
- builders and writers:
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

Use `term` for terminal mode and events, and `ansi` to build escape-sequence output safely.

### 14.3 `stdlib/term/intrinsics`

- This file exposes low-level terminal intrinsics used by `stdlib/term` and `stdlib/term/ansi`.
- Treat it as implementation detail, not as stable user-facing API.

---

## 15. Directive Modules

These modules are intended for directive-driven workflows rather than regular runtime libraries.

### 15.1 `stdlib/directives/test`

Directive pragma:

```sg
pragma module::test, directive;
```

Public API:

- `eq<T>(actual, expected)`
- `assert(condition)`
- `assert_msg(condition, message)`
- `ne<T>(actual, expected)`
- `fail(message)`
- `skip(reason)`

### 15.2 `stdlib/directives/benchmark`

Directive pragma:

```sg
pragma module::benchmark, directive;
```

Public API:

- `BenchmarkResult`
- `throughput(name, iters, f)`
- `single(name, f)`
- `skip(reason)`

Current reality:

- This module is still mostly a stage-level helper. Its API is present, but the implementation is still lightweight.

### 15.3 `stdlib/directives/time`

Directive pragma:

```sg
pragma module::time, directive;
```

Public API:

- `ProfileResult`
- `profile_fn(name, iters, f)`
- `profile_once(name, f)`
- `skip(reason)`

Current reality:

- Like `benchmark`, this module currently exposes a usable surface with intentionally simple implementation.

---

## 16. `stdlib/saturating_cast`

Import:

```sg
import stdlib/saturating_cast;
```

Public API:

- `saturating_cast(value, target_type)` overloads for integer and float combinations

Use this module when you want clamped numeric conversion instead of overflow, truncation, or backend-dependent behavior.

Example:

```sg
import stdlib/saturating_cast;

fn to_u8(x: int) -> uint8 {
    return saturating_cast(x, 0:uint8);
}
```

---

## 17. Practical Combinations

### Generate a secure UUID and serialize it

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

### Read a JSON file from disk

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

### Seeded random bytes for a deterministic test

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
