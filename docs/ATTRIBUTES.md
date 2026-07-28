# Surge Language Attributes
[English](ATTRIBUTES.md) | [Russian](ATTRIBUTES.ru.md)

Attributes are declarative annotations that attach extra constraints or metadata to
functions, types, fields, parameters, blocks, and statements. The compiler
validates targets, conflicts, and arguments. Some attributes are **parsed** but
do not affect semantics yet; those are called out explicitly.

---

## Syntax

```sg
@attribute
fn example() { return nothing; }

@attribute("arg")
type Data = { value: int };

@a
@b
fn multiple() { return nothing; }
```

Notes:
- Attributes appear immediately before the declaration they modify.
- Unknown attributes are errors.
- Statement attributes: only `@drop expr;` is allowed (no arguments).
- Async block attributes: only `@failfast` is accepted.
- Spawn expression attributes: only `@local` is accepted.
- Attribute arguments must be literals (string or integer as required).

---

## Attribute Index (current behavior)

Status legend:
- **Enforced**: affects semantics or emits diagnostics.
- **Validated**: argument/target checks only; no semantic effect yet.
- **Parsed**: accepted but no checks or behavior beyond target validation.

| Attribute | Targets | Args | Status | Notes |
| --- | --- | --- | --- | --- |
| `@allow_to` | fn, param | none | Enforced | Enables implicit `__to` conversion. |
| `@backend` | fn | string | Validated | Warns on unknown targets; no codegen effect. |
| `@copy` | type | none | Enforced | All fields/members must be Copy. |
| `@deprecated` | fn, type, field, let, const | optional string | Enforced | Emits warnings on use. |
| `@drop` | stmt | none | Enforced | Explicit drop/borrow end point; frees non-copy owned values (drop emission epic). |
| `@entrypoint` | fn | optional string | Enforced | Program entrypoint. |
| `@failfast` | async fn, async block | none | Enforced | Structured concurrency cancellation. |
| `@local` | spawn expr | none | Enforced | Allows @nosend captures; local task handle is not sendable. |
| `@guarded_by` | field | string | Enforced | Requires holding a lock to access. |
| `@hidden` | fn, type, field, let, const | none | Enforced (top-level) | Field-level is parsed only. |
| `@intrinsic` | fn, type | none | Enforced | Decl-only; type body restrictions. |
| `@noinherit` | type, field | none | Enforced | Prevents inheritance. |
| `@nosend` | type | none | Enforced | Disallows crossing task boundaries. |
| `@nonblocking` | fn | none | Enforced | Forbids may-wait (park/unpark) and OS-blocking calls. |
| `@overload` | fn | none | Enforced | Adds a new signature. |
| `@override` | fn | none | Enforced | Replaces an existing signature. |
| `@packed` | type, field | none | Enforced (type) | Field-level has no layout effect. |
| `@align` | type, field | int pow2 | Enforced | Layout alignment override. |
| `@raii` | type | none | Parsed | Reserved for a future user-defined scope-exit destructor hook; drops are universal and NOT @raii-gated. |
| `@arena` | type, field, param | string | Parsed | Reserved. |
| `@shared` | type, field | none | Parsed | Reserved. |
| `@weak` | field | none | Parsed | Reserved. |
| `@atomic` | field | none | Enforced | Type restrictions + access rules. |
| `@readonly` | field | none | Enforced | Forbids writes after init. |
| `@requires_lock` | fn | string | Enforced | Caller must hold lock. |
| `@acquires_lock` | fn | string | Enforced | Callee acquires lock. |
| `@releases_lock` | fn | string | Enforced | Callee releases lock. |
| `@waits_on` | fn | string | Enforced | Marks potential blocking. |
| `@send` | type | none | Enforced | Field composition must be sendable. |
| `@shard_movable` | type | none | Enforced | Owned values may cross shard boundaries if fields/members are recursively movable. |
| `@shard_pinned` | type | none | Enforced | Values/resources are pinned to their owner shard; owned values cannot cross. |
| `@sealed` | type | none | Enforced | Cannot be extended. |
| `@pure` | fn | none | Parsed | No side-effect checks yet. |

---

## Reserved Attributes (parsed, not yet implemented)

`@raii`, `@arena`, `@shared`, `@weak`, and `@pure` are accepted by the parser and
target-validated, but have **no semantic effect or runtime behavior yet**. They are
reserved for future language increments and are safe to write only as forward-looking
annotations:

- `@raii` (type) — reserved for a future user-defined scope-exit destructor hook.
  Implicit drops of non-copy owned values are universal language semantics and are
  **not** gated by `@raii`.
- `@arena` (type, field, param) — reserved for arena/region allocation.
- `@shared` (type, field) — reserved.
- `@weak` (field) — reserved for weak references.
- `@pure` (fn) — reserved; no purity or side-effect checks are performed today.

---

## Function Attributes

### `@overload`

Adds a new signature for an existing function name.

Rules:
- First declaration **must not** use `@overload`.
- `@overload` must introduce a **different signature**.
- If the signature is identical, use `@override` instead.

```sg
fn parse(x: int) -> int { return x; }

@overload
fn parse(x: string) -> int { return x.to_int(); }
```

### `@override`

Replaces an existing declaration with the same signature.

Rules:
- An earlier matching declaration must already exist.
- Signature must match exactly.
- Cannot reduce visibility (`pub` must be preserved when overriding public).
- Cannot override builtin functions.
- Cannot combine with `@overload` or `@intrinsic`.

```sg
fn encode(buf: &byte[]) -> uint; // forward decl

@override
fn encode(buf: &byte[]) -> uint { return 0:uint; }
```

### `@intrinsic` (functions)

Declares a compiler/runtime-provided function.

Rules:
- Must be a **declaration only** (no body).
- Cannot combine with `@override` or `@entrypoint`.
- Allowed in any module, but only known intrinsics are implemented by backends.
- Permits raw pointer types in signatures.

```sg
@intrinsic fn rt_alloc(size: uint) -> *byte;
```

### `@entrypoint`

Marks the program entrypoint.

Modes:
- No mode: `@entrypoint` requires all parameters to have defaults.
- `@entrypoint("argv")`: parse positional arguments via `T.from_str(&string)`.
- `@entrypoint("stdin")`: parse a single parameter from stdin.
- `"env"` and `"config"` are reserved (`FutEntrypointModeEnv` / `FutEntrypointModeConfig`).

Return type:
- `nothing` or `int`, or any type that implements `ExitCode<T>` (`__to(self, int) -> int`).
- `Option<T>` and `Erring<T, E>` implement this conversion by default.

Parameter parsing:
- `"argv"` requires each non-default parameter type to implement `FromArgv<T>`.
- `"stdin"` requires a single parameter type that implements `FromStdin<T>`.

Contracts are declared in `core/entrypoint.sg`.

Runtime behavior (v1):
- `argv`: missing required arg exits with code 1; parse failures call `exit(err)`.
- `stdin`: only one parameter is supported; multiple params exit with code 7001.

```sg
@entrypoint("argv")
fn main(count: int, name: string = "guest") -> int {
    return 0;
}
```

### `@allow_to`

Allows implicit `__to` conversion when argument types do not match exactly.

- On a **function**, it applies to all parameters.
- On a **parameter**, it applies only to that parameter.

```sg
fn takes_string(@allow_to s: string) { print(s); }
```

### `@nonblocking` and `@waits_on`

- `@nonblocking` forbids operations that may wait (park/unpark) or OS-block.
- `@waits_on("field")` marks a function as potentially waiting on that field.
- They **conflict** if used together.

May-wait methods checked today (they park tasks, not OS-block):
- `Mutex.lock`
- `RwLock.read_lock` / `RwLock.write_lock`
- `Condition.wait`
- `Semaphore.acquire`
- `Channel.send` / `Channel.recv` / `Channel.close`

`@waits_on` requires a field name of type `Condition` or `Semaphore`.

### Lock Contract Attributes

`@requires_lock`, `@acquires_lock`, and `@releases_lock` reference a lock field
on the receiver type (typically `self`). They drive inter-procedural lock checks.

```sg
type Counter = { lock: Mutex, value: int };

extern<Counter> {
    @requires_lock("lock")
    fn get(self: &Counter) -> int { return self.value; }
}
```

### `@backend`

Validates an execution target string (known: `cpu`, `gpu`, `tpu`, `wasm`,
`native`). Unknown targets emit a warning. No codegen effect yet.

### `@failfast`

- Allowed on `async fn` and `@failfast async { ... }` blocks.
- Cancels sibling tasks in the same scope when one is cancelled.

### `@local`

- Allowed on `spawn` expressions: `@local spawn expr`.
- Allows capturing `@nosend` values.
- The resulting task handle is local (not sendable): it cannot be captured by `spawn`,
  sent through channels, or returned from a function.

### `@pure`

Parsed but not enforced yet. No purity checks are performed today.

### `@deprecated`

Emits a warning whenever the item is used. Optional string message.

```sg
@deprecated("use new_api")
fn old_api() { return nothing; }
```

### `@hidden`

On top-level items: makes the symbol file-private and excludes it from exports.
Using `pub` together with `@hidden` emits a warning.

---

## Type Attributes

### `@intrinsic` (types)

Declares a compiler/runtime-provided type.

Rules:
- Type must be an empty struct or contain only a single `__opaque` field.
- Full layout is allowed only in `core/intrinsics.sg` or `core_stdlib/intrinsics.sg`.
- Permits raw pointer fields inside the type.

```sg
@intrinsic
pub type Task<T> = { __opaque: int };
```

### `@packed` and `@align`

- `@packed` removes padding between fields for struct layout.
- `@align(N)` overrides alignment; `N` must be a positive power of two.
- `@packed` conflicts with `@align` on the same declaration.

### `@send` / `@nosend`

- `@send` requires all fields to be sendable (recursively).
- `@nosend` forbids crossing task boundaries.
- They conflict with each other.

### `@shard_movable` / `@shard_pinned`

`@shard_movable` marks a type whose owned values may cross shard boundaries in
`on dst { ... }` and `spawn on dst { ... }` captures. The compiler validates the
type recursively:

- primitive scalar fields, enums, `far T` handles, and recursively movable
  `own T`, tuple, and array members are accepted;
- fields or union members with `@nosend`, `@shard_pinned`, or unmarked
  user-defined types are rejected;
- `@send` and `@copy` alone are not enough for owned shard movement.

`@shard_pinned` marks a type or runtime resource that must stay on its owner
shard. Owned `@shard_pinned` values cannot be captured across crossing
boundaries; use an accepted `far T` handle or an explicit future migration path.

The two attributes conflict with each other.

```sg
@shard_movable
type Job = {
    id: uint64,
    payload: string,
};

@intrinsic
@nosend
@shard_pinned
pub type TcpConn = { __opaque: int };
```

### `@copy`

Marks a struct or union as Copy if all fields/members are Copy. Cycles are
rejected. When valid, the type becomes Copy-capable.

`@copy` answers whether a value may be DUPLICATED. It says nothing about how
the value is stored: a struct, tuple, tagged union or fixed array is a value
either way, and `@copy` only decides whether `let b = a` leaves `a` usable.

Copying one produces an INDEPENDENT value, at every depth:

```sg
@copy type Point = { x: int, y: int };

let p = Point { x = 1, y = 2 };
let mut q = p;
q.x = 99;        // p.x is still 1
```

The same holds through a by-value argument, a return, a field or element read,
and a tagged-union payload binding. A copy is reclaimed on its own; two copies
free exactly twice.

A `@copy` composite may also cross a shard boundary — the destination receives
its own value — with one exception: a composite carrying an
arbitrary-precision `int`, `uint` or `float` is refused there, because such a
field is shared by counting and the count is not safe across shards. Use a
fixed-width field type (`int64`, `float64`) for the value that crosses.

### `@sealed` / `@noinherit`

- `@sealed`: cannot be extended via inheritance or `extern<T>`.
- `@noinherit`: prevents the type from being used as a base.

### `@raii`, `@arena`, `@shared`

Parsed only; no semantic checks or runtime behavior yet. `@raii` is
reserved specifically for a future user-defined scope-exit destructor
hook: implicit drops of non-copy owned values (the drop-emission epic)
are universal language semantics and are NOT gated by `@raii`.

---

## Field Attributes

### `@readonly`

Field cannot be written after initialization.

### `@atomic`

- Field type must be `int`, `uint`, `bool`, or `*T`.
- Direct reads/writes are forbidden; use atomic intrinsics via address-of.

### `@guarded_by("lock")`

Access requires the named lock to be held. Reads allow read/write locks; writes
require mutex or write lock.

### `@align` / `@packed`

`@align` is enforced; `@packed` is accepted but currently has no field-level
layout effect.

### `@noinherit`

Field is not inherited by derived types.

### `@deprecated` / `@hidden`

Parsed for fields; only `@deprecated` currently affects diagnostics. Field-level
`@hidden` is reserved (no access checks yet).

### `@weak`, `@shared`, `@arena`

Parsed only; no semantic effect yet.

---

## Parameter Attributes

### `@allow_to`

See function attribute description; enables implicit `__to` for that parameter.

### `@arena`

Parsed only; no semantic effect yet.

---

## Statement Attribute

### `@drop`

Explicit drop/borrow end point. Only valid as `@drop binding;` with no
arguments. The target must be a binding name.

```sg
let r = &mut value;
@drop r; // ends borrow early
```

---

## Conflicts and Validation Summary

- `@packed` + `@align` (same declaration)
- `@send` + `@nosend`
- `@nonblocking` + `@waits_on`
- `@overload` + `@override`
- `@intrinsic` + `@override` or `@entrypoint`
- `@failfast` requires async context

---

## Diagnostics (selected)

- `SemaAttrPackedAlign` `@packed` conflicts with `@align`
- `SemaAttrSendNosend` `@send` conflicts with `@nosend`
- `SemaAttrNonblockingWaitsOn` `@nonblocking` conflicts with `@waits_on`
- `SemaAttrAlignNotPowerOfTwo` `@align` not power of two
- `SemaAttrBackendUnknown` `@backend` unknown target (warning)
- `SemaAttrGuardedByNotField` / `SemaAttrGuardedByNotLock` `@guarded_by` invalid field/type
- `SemaLockGuardedByViolation` `@guarded_by` access without lock
- `SemaLockNonblockingCallsWait` `@nonblocking` calls blocking operation
- `SemaAttrWaitsOnNotCondition` `@waits_on` field must be Condition/Semaphore
- `SemaAttrAtomicInvalidType` `@atomic` invalid field type
- `SemaAtomicDirectAccess` `@atomic` direct access
- `SemaAttrCopyNonCopyField` / `SemaAttrCopyCyclicDep` `@copy` validation failures
- `SemaEntrypointModeInvalid` / `SemaEntrypointNoModeRequiresNoArgs` / `SemaEntrypointReturnNotConvertible` / `SemaEntrypointParamNoFromArgv` / `SemaEntrypointParamNoFromStdin` entrypoint validation
- `FutEntrypointModeEnv` / `FutEntrypointModeConfig` reserved entrypoint modes

See `internal/diag/codes.go` for the full list.
