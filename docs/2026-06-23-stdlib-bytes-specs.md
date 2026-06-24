# `stdlib/bytes` Design Spec

> Status: roadmap design; range/compact, LF/CRLF line scanning, ASCII token,
> and byte-range literal compare slices are shipped.
> Date: 2026-06-23.
> Scope: standard-library byte helpers for protocol and binary hot paths.

## Summary

Surge needs a standard byte toolkit for code that parses protocols, frames
network input, and builds byte responses. `string` remains the right type for
Unicode text. `byte[]`, `BytesView`, and small range structs should be the fast
path for ASCII and binary protocols.

This design keeps the fix in Surge, not in a downstream project. External
projects such as surgekv can use the module as a proving ground, but they
should not have to build their own byte-string abstraction.

The first implementation should ship a broad but small `stdlib/bytes` module:
ASCII predicates, range search and trimming, streaming input buffers, and
byte-output builders. If current runtime primitives cannot make these helpers
fast, the implementation must stop and add the missing runtime intrinsics
instead of hiding slow loops behind a nice API.

## Problem

Today, user code often falls back to `string` for line-oriented protocols:
`string.from_bytes`, concatenation, `find`, slicing, `trim`, and tokenization.
That path pays for text semantics even when the protocol is byte-oriented. It
also tends to copy buffers when consuming input.

Current primitives are useful but incomplete:

- `byte[]` has `reserve`, `push`, `pop`, indexing, and `__len`.
- `Array<byte>` can append a `string` or `BytesView` through
  `rt_array_append_raw_bytes`.
- `string.bytes()` exposes `BytesView`.
- `stdlib/json` and `stdlib/http` already contain local byte scanners.

The missing piece is a common, ergonomic byte layer that avoids per-project
helpers and can be benchmarked directly.

## Goals

- Make byte-oriented protocol parsing possible without converting input chunks
  to `string`.
- Keep `string` semantics intact: UTF-8 text, code point length, and text
  slicing stay separate from protocol bytes.
- Give users a complete enough API for line parsing, token parsing, numeric
  parsing, response building, and streaming input.
- Reuse existing runtime support where it is fast enough.
- Identify the exact runtime intrinsics needed before implementation.
- Prove the result with microbenchmarks and a small protocol benchmark before
  recommending adoption to external projects.

## Non-Goals

- Do not implement a RESP, HTTP, or JSON framework in `stdlib/bytes`.
- Do not redesign `string`.
- Do not expose raw pointers to ordinary Surge code.
- Do not optimize surgekv directly as part of this work.
- Do not add speculative generic parser combinators.

## Public Import

Users should import the module with an explicit alias:

```sg
import stdlib/bytes as by;
```

The bare module name `bytes` currently collides with the existing string
`.bytes()` symbol in the global namespace.

The implementation may be split internally:

- `stdlib/bytes/bytes.sg`: public facade.
- `stdlib/bytes/ascii.sg`: ASCII predicates and case helpers.
- `stdlib/bytes/range.sg`: range search, trim, compare, split, and parse.
- `stdlib/bytes/buffer.sg`: streaming input buffer.
- `stdlib/bytes/builder.sg`: byte output helpers.

The public surface should remain available through `stdlib/bytes`.

## Types

```sg
pub type ByteRange = {
    start: uint,
    end: uint,
};

pub type ByteSplit = {
    head: ByteRange,
    tail: ByteRange,
};

pub type ByteLine = {
    body: ByteRange,
    next: uint,
};

pub type ByteBuffer = {
    data: byte[],
    start: uint,
};
```

`ByteRange` is half-open: `[start, end)`. It does not own bytes and does not
keep a buffer alive. Every function that reads a range takes the original
`&byte[]` or `&BytesView` explicitly.

`ByteBuffer.start` marks consumed bytes. The module should compact lazily, not
shift the backing array on every `consume`.

## Range Semantics

All range functions must handle invalid ranges predictably:

- `start > end` is invalid.
- `end > data.__len()` is invalid.
- Functions that can fail return `Erring<T, Error>` or `Option<T>`.
- Predicates return `false` for invalid ranges.
- Constructors and mutating buffer methods should keep `ByteBuffer` internally
  valid.

No function should panic for ordinary malformed input. Bounds bugs inside the
module are still bugs and should be covered by tests.

## ASCII Helpers

```sg
pub fn is_ascii_ws(b: byte) -> bool;
pub fn is_ascii_digit(b: byte) -> bool;
pub fn is_ascii_alpha(b: byte) -> bool;
pub fn is_ascii_alnum(b: byte) -> bool;
pub fn is_ascii_hex_digit(b: byte) -> bool;
pub fn ascii_to_lower(b: byte) -> byte;
pub fn ascii_to_upper(b: byte) -> byte;
pub fn ascii_hex_value(b: byte) -> int;
```

These helpers operate on single bytes. They do not validate Unicode.

## Search Helpers

```sg
pub fn find_byte(data: &byte[], range: ByteRange, needle: byte) -> Option<uint>;
pub fn find_lf(data: &byte[], range: ByteRange) -> Option<uint>;
pub fn find_crlf(data: &byte[], range: ByteRange) -> Option<uint>;
pub fn find_bytes(data: &byte[], range: ByteRange, needle: &byte[]) -> Option<uint>;
```

The return value is an absolute byte offset in `data`. `find_crlf` returns the
offset of `\r`.

`find_byte` is the first search helper that should get runtime support if pure
Surge loops are too slow. The native benchmark does not justify that intrinsic
yet: `ByteBuffer.peek_line_lf` is about `7.6x` faster than the string conversion
path without `rt_byte_find`.

## Range Helpers

```sg
pub fn all(data: &byte[]) -> ByteRange;
pub fn range_len(range: ByteRange) -> uint;
pub fn is_valid_range(data: &byte[], range: ByteRange) -> bool;
pub fn trim_ascii(data: &byte[], range: ByteRange) -> ByteRange;
pub fn trim_ascii_start(data: &byte[], range: ByteRange) -> ByteRange;
pub fn trim_ascii_end(data: &byte[], range: ByteRange) -> ByteRange;
pub fn split_once_byte(data: &byte[], range: ByteRange, sep: byte) -> Option<ByteSplit>;
pub fn next_ascii_token(data: &byte[], range: ByteRange) -> Option<ByteSplit>;
```

`next_ascii_token` trims leading ASCII whitespace, returns the next token as
`head`, and returns the remaining range as `tail`. It should not allocate.

## Compare Helpers

```sg
pub fn range_eq(data: &byte[], range: ByteRange, expected: &byte[]) -> bool;
pub fn range_eq_ascii(data: &byte[], range: ByteRange, expected: &string) -> bool;
pub fn range_eq_ascii_ci(data: &byte[], range: ByteRange, expected: &string) -> bool;
pub fn starts_with_ascii(data: &byte[], range: ByteRange, expected: &string) -> bool;
```

`*_ascii` helpers compare bytes from `expected.bytes()`. They do not allocate.
They are intended for small protocol literals such as `GET`, `PING`, and
`Content-Length`.

## Numeric Parse Helpers

```sg
pub fn parse_uint64_ascii(data: &byte[], range: ByteRange) -> Erring<uint64, Error>;
pub fn parse_int64_ascii(data: &byte[], range: ByteRange) -> Erring<int64, Error>;
pub fn parse_hex_uint64_ascii(data: &byte[], range: ByteRange) -> Erring<uint64, Error>;
```

These functions parse ASCII digits only. They should reject empty input,
whitespace, signs in unsigned values, and overflow.

Numeric parsing is not shipped yet. A pure Surge `parse_uint64_ascii` prototype
was correct but did not beat the current `string.from_bytes + split +
uint.from_str` benchmark after safety checks were added (`~0.90x` in a
single-run probe). The next numeric slice should either fuse token scanning with
numeric parsing or add a narrow runtime byte-range parse intrinsic; do not ship
a slower range helper just to complete the surface.

## Conversion Helpers

```sg
pub fn copy_range(data: &byte[], range: ByteRange) -> Erring<byte[], Error>;
pub fn range_to_string(data: &byte[], range: ByteRange) -> Erring<string, Error>;
pub fn view_to_string(view: &BytesView) -> Erring<string, Error>;
```

Conversions are explicit allocation points. Protocol parsers should keep data as
bytes until they must produce a `string` key, value, or user-visible message.

## `ByteBuffer` API

```sg
pub fn buffer() -> ByteBuffer;
pub fn buffer_with_capacity(cap: uint) -> ByteBuffer;
```

```sg
extern<ByteBuffer> {
    pub fn len(self: &ByteBuffer) -> uint;
    pub fn is_empty(self: &ByteBuffer) -> bool;
    pub fn range(self: &ByteBuffer) -> ByteRange;
    pub fn clear(self: &mut ByteBuffer) -> nothing;
    pub fn clear_keep_capacity(self: &mut ByteBuffer) -> nothing;
    pub fn reserve(self: &mut ByteBuffer, additional: uint) -> nothing;
    pub fn append(self: &mut ByteBuffer, chunk: &byte[]) -> nothing;
    pub fn append_view(self: &mut ByteBuffer, view: &BytesView) -> nothing;
    pub fn append_string(self: &mut ByteBuffer, text: &string) -> nothing;
    pub fn peek_line_lf(self: &ByteBuffer) -> Option<ByteLine>;
    pub fn peek_line_crlf(self: &ByteBuffer) -> Option<ByteLine>;
    pub fn consume(self: &mut ByteBuffer, count: uint) -> nothing;
    pub fn compact(self: &mut ByteBuffer) -> nothing;
}
```

`peek_line_lf` returns a line without the trailing `\n`. If the line ends with
`\r\n`, the returned body should exclude `\r`. `ByteLine.next` is the absolute
offset after the terminator.

`consume` advances `start`. It should not move bytes immediately. `compact`
moves live bytes back to offset `0` and resets `start`.

This API intentionally avoids default parameters on methods. Current method
resolution does not apply default arguments when matching method-call arity, so
`buf.clear()` would not resolve to `clear(keep_capacity: bool = true)`.

## Byte Builder API

The builder can be plain `byte[]` methods. A separate `ByteBuilder` type adds
little value until the language has better capacity controls.

```sg
extern<Array<byte>> {
    pub fn append_byte(self: &mut Array<byte>, b: byte) -> nothing;
    pub fn append_bytes(self: &mut Array<byte>, data: &byte[]) -> nothing;
    pub fn append_bytes_range(self: &mut Array<byte>, data: &byte[], range: ByteRange) -> Erring<nothing, Error>;
    pub fn append_view(self: &mut Array<byte>, view: &BytesView) -> nothing;
    pub fn append_ascii(self: &mut Array<byte>, text: &string) -> nothing;
    pub fn append_uint_ascii(self: &mut Array<byte>, value: uint) -> nothing;
    pub fn append_int_ascii(self: &mut Array<byte>, value: int) -> nothing;
    pub fn clear_keep_capacity(self: &mut Array<byte>) -> nothing;
}
```

This surface covers response construction without a second owned wrapper around
`byte[]`.

## Runtime Support Check

Current runtime support:

- `rt_array_append_raw_bytes(a, ptr, length)` supports bulk append from
  `string` and `BytesView`.
- `rt_string_bytes_view` gives byte access to `string`.
- `rt_memcpy` and `rt_memmove` exist but are not ordinary user APIs.
- `rt_net_write_bytes` already writes a byte-array range by offset and length.

Shipped in the first runtime-backed slice:

- `rt_byte_array_append_range(dst: &mut byte[], src: &byte[], start: uint, len: uint)`.
- `rt_byte_array_drop_prefix(buf: &mut byte[], count: uint)`.

Shipped in the line-scanning slice:

- `ByteLine`.
- `find_byte`, `find_lf`, and `find_crlf`.
- `ByteBuffer.peek_line_lf` and `ByteBuffer.peek_line_crlf`.
- `benchmarks/native/byte_lines` plus `scripts/bench_native_byte_lines.sh`.

Shipped in the ASCII token slice:

- `ByteSplit`.
- ASCII predicates and case/hex helpers.
- `trim_ascii`, `trim_ascii_start`, and `trim_ascii_end`.
- `split_once_byte` and `next_ascii_token`.
- `scripts/bench_native_byte_lines.sh` also reports token extraction timings.

Shipped in the literal compare slice:

- `range_eq`, `range_eq_ascii`, `range_eq_ascii_ci`, and `starts_with_ascii`.
- `scripts/bench_native_byte_lines.sh` also reports command-dispatch timings.

Still optional:

- `rt_byte_find(buf: &byte[], start: uint, end: uint, needle: byte)`.

`clear_keep_capacity` can drop the full length with `rt_byte_array_drop_prefix`.
`ByteBuffer.compact` can drop only the consumed prefix. `copy_range` can build
an empty destination and use `rt_byte_array_append_range`, so a separate
truncate/clear intrinsic should not be introduced unless a benchmark proves it
is needed.

The implementation should benchmark pure Surge loops first. If loops dominate,
add the smallest intrinsic that removes the measured cost. Do not add a broad
memory API just to make this module look complete.

Preflight notes:

- The proposed `ByteRange`, `ByteLine`, `ByteBuffer`, and `extern<Array<byte>>`
  method shapes pass current sema, LLVM build, and VM run when they avoid method
  default arguments.
- Bulk append from `string` and `BytesView` is already implemented for native,
  LLVM lowering, and VM.
- There is no public fast path for appending a range from one `byte[]` into
  another. Without a new intrinsic, `append_bytes_range` must copy byte by byte.
- There is no public fast path for truncating or clearing a `byte[]` while
  keeping capacity. Without a new intrinsic, `clear_keep_capacity` must pop in a
  loop.
- Array range indexing was checked after issue #159 landed in `origin/main`:
  `data[[1..3]]` now passes sema, VM, LLVM build, and native run. This restores
  VM/native parity, but it still is not a bulk-copy primitive, so
  `append_bytes_range` still needs either a loop or a narrow runtime intrinsic.

## Expected Performance

The target is a protocol fast path, not general string speed.

Initial gates:

- Line scanning and token extraction should allocate zero objects per command
  before explicit conversion.
- A `PING_PIPE`-style parser should move from about `14us/op` toward low
  single-digit microseconds.
- A `GET` parser without manager or TCP should improve by at least 2x from the
  current string path.
- Response byte construction should stay near the existing bulk-copy cost.

If the first implementation improves parser benchmarks by only 10-20%, the
stdlib API is not enough. Stop and inspect runtime/compiler costs before
expanding the public surface.

## Benchmark Plan

Add a standalone benchmark under this repo, not under an external project:

- Current string line path vs `ByteBuffer.peek_line_lf`.
- Current string token path vs `next_ascii_token`.
- Current string command dispatch vs `next_ascii_token + range_eq_ascii`.
- Fixed-width or fused numeric parsing for small and large integers.
- `byte[]` response builder for simple text and value responses.
- Buffer append, consume, and compact with realistic network chunk sizes.

Run each benchmark with:

- `SURGE_THREADS=1` and `SURGE_THREADS=8`.
- Small requests, pipelined batches, and mixed chunk boundaries.
- Allocation tracing when available.
- Fresh compiler and local stdlib paths.

External surgekv checks may come after the standalone proof, but they are not
the acceptance gate for the stdlib module.

## Correctness Tests

The first shipped slice covers:

- `copy_range`
- `Array<byte>.append_bytes_range`
- `Array<byte>.clear_keep_capacity`
- `ByteBuffer.consume`
- `ByteBuffer.compact`
- `ByteBuffer.clear_keep_capacity`
- VM and LLVM/native parity for valid ranges, invalid ranges, source array
  views, and buffer compaction.
- `find_byte`, `find_lf`, `find_crlf`
- `ByteBuffer.peek_line_lf`, `ByteBuffer.peek_line_crlf`
- LF and CRLF line endings
- Lines split across chunks
- ASCII predicates and case/hex helpers
- ASCII whitespace trimming
- `split_once_byte`
- `next_ascii_token`
- `range_eq`
- `range_eq_ascii`
- `range_eq_ascii_ci`
- `starts_with_ascii`

The implementation PR should add focused tests for:

- Empty ranges and invalid ranges.
- Integer parsing, including overflow.
- Buffer consume and compact behavior.
- Explicit string conversion failure on invalid UTF-8.

Keep tests small. Add one runnable benchmark/probe that fails clearly if the
fast path regresses.

## Documentation Plan

When the API ships:

- Add `stdlib/bytes` to `docs/STDLIB.md` and `docs/STDLIB.ru.md`.
- Update `docs/LANGUAGE.md` only if new array or byte semantics become public.
- Update `docs/ABI_LAYOUT.md` only if a new ABI-visible type or intrinsic
  contract becomes public.
- Mention benchmark results in the PR description.
- Keep generated `STATS.md` changes if the repo hook updates it.

## Adoption Guidance

After the module is implemented and measured, tell downstream teams:

- Keep `string` for user text.
- Use `stdlib/bytes` for protocol input buffers and response bytes.
- Convert byte ranges to `string` only at API boundaries or when storing textual
  keys and values.
- Batch application-level actor calls separately; `stdlib/bytes` removes parser
  cost but does not remove per-command channel hops.
