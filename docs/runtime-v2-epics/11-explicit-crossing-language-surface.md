# Epic 11: Explicit Crossing Language Surface

**Goal:** implement the confirmed Surge language surface for Runtime V2 Phase 4
explicit crossing. This epic defines the first accepted syntax and semantic
contracts for remote handles, placement crossing, remote spawn, and crossing
capability markers.

**Status:** confirmed contract for task slicing. No parser, semantic-analysis,
lowering, runtime transport, or public example work may start until the
corresponding block's golden-test matrix is written first.

Small drift is allowed only through an explicit document update reviewed with
the user. Silent drift is forbidden: if a planned valid form cannot be made to
work, it must be removed from the accepted matrix or redesigned with a recorded
reason before implementation continues.

## Non-Negotiable Validity Contract

- Every construct marked valid in this document must compile once its block is
  implemented.
- Every construct marked invalid must produce a deterministic diagnostic with
  an exact diagnostic code.
- Invalid constructs must not fall through to parser ambiguity, internal
  compiler errors, runtime crashes, leaks, or backend panics.
- A program that uses only valid syntax must not leak, crash, or corrupt runtime
  state because of the new language surface.
- Runtime failure caused by explicit low-level/dunder behavior is outside this
  guarantee when the programmer intentionally invokes such behavior. Example:
  a direct OOM trigger or explicit unsafe/dunder path may be impossible to
  reject heuristically. The ordinary crossing syntax itself must still remain
  safe.
- If an automated fix is possible, the negative golden fixture must record the
  expected fix shape. If no fix is safe, the fixture must say so.

## Epic Blocks

Epic 11 is split into four independent language blocks. Each block has its own
syntax rules, invariants, diagnostics, positive golden fixtures, negative
golden fixtures, implementation tasks, and documentation closeout.

1. **Far Type Modifier:** `far T` as the type former for remote handles.
2. **Placement Crossing Block:** `on dst { ... }` and the `Placement`
   destination model.
3. **Remote Spawn Block:** `spawn on dst { ... }`, `far Task<T>`, and remote
   task lifecycle rules.
4. **Crossing Contracts:** inferred crossing function effects,
   `@shard_movable`, `@shard_pinned`, and their interaction with existing
   `@send`, `@nosend`, and `@local spawn`.

These blocks may be implemented independently after task slicing, but each
block starts with tests, not parser changes.

## Test-First Contract

For each block:

1. Write or refine the accepted syntax matrix.
2. Assign diagnostic codes before adding negative fixtures:
   - reuse existing diagnostic codes when the invariant already exists;
   - allocate new codes only for new language invariants;
   - record code, message shape, and fix availability.
3. Add golden fixtures before implementation:
   - at least one positive file per accepted lexical/syntactic form;
   - at least one negative file per invariant;
   - every negative file asserts the exact diagnostic code;
   - fixable cases record the expected fix shape.
4. Implement parser/sema/lowering until the golden tests pass.
5. Add compiler unit tests after syntax is implemented:
   - AST/name-resolution tests proving keyword parsing;
   - sema tests proving type identity, ownership, and capture rules;
   - lowering/backend tests only for the minimal semantics implemented in that
     block.
6. Update `NOTES.md` with evidence before marking the block complete.

Golden fixture names must point back to matrix rows, for example:
`far_positive_alias.sg`, `far_negative_nested.sg`,
`on_positive_placement_var.sg`, `on_negative_shared_borrow_capture.sg`,
`spawn_on_negative_local.sg`.

## Documentation Contract

Epic 11 is not complete until connected language documents are updated:

- `docs/LANGUAGE.md` is updated from Draft 8 to Draft 9.
- `docs/LANGUAGE.ru.md` receives the matching Draft 9 update.
- `docs/ATTRIBUTES.md` and `docs/ATTRIBUTES.ru.md` document
  `@shard_movable` and `@shard_pinned`.
- `docs/RUNTIME_V2.md` replaces the old working-name wording for `far`, `on`,
  `spawn on`, and inferred crossing effects, and marks postponed surfaces as
  future work.
- Public examples are added only after syntax, semantic, and minimal lowering
  tests exist.

### Draft 9 Documentation Scope

The concrete update points recorded for the Draft 9 pass are:

- `docs/LANGUAGE.md` (Draft 8 -> Draft 9): add `far` to the type-form grammar
  and reserved-keyword list; document `on` / `spawn on`, inferred crossing
  effects, and the fact that `crosses` remains an ordinary identifier; add the
  `Placement` / `pool` / `distributed` / `shard(id)` prelude surface; record
  `far T[]` and copyable `far` handles as postponed.
- `docs/LANGUAGE.ru.md`: the matching Draft 9 update, kept in step with
  `LANGUAGE.md`.
- `docs/ATTRIBUTES.md` and `docs/ATTRIBUTES.ru.md`: add `@shard_movable` and
  `@shard_pinned` (targets, conflict rule, recursive field validation, and their
  relationship to `@send` / `@nosend`).
- `docs/RUNTIME_V2.md`: replace working-name wording for `far`, `on`,
  `spawn on`, and inferred crossing effects; state the Epic 11 execution scope
  (surface plus lowering guards, transport postponed) and the
  backend-unavailable diagnostic contract.
- `docs/CONCURRENCY.md`: cross-reference the crossing surface where it describes
  task and channel boundaries, if a Draft 9 touch point is needed.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/LANGUAGE.md`
- `docs/LANGUAGE.ru.md`
- `docs/ATTRIBUTES.md`
- `docs/ATTRIBUTES.ru.md`
- `docs/CONCURRENCY.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/DEBT.md`
- current parser grammar and keyword table
- current semantic checks for ownership, borrows, `@send`, `@nosend`,
  `@local spawn`, `blocking`, `async`, `Task<T>`, and channels
- `core/intrinsics.sg` — prelude intrinsic declarations for `TcpConn`,
  `TcpListener`, `Task<T>`, `Channel<T>`, and the new `Placement` / `shard` /
  `pool` / `distributed` surface; the site where `@shard_pinned` is added to
  TCP runtime handles
- `internal/token/keywords.go` and `internal/token/kind.go` — keyword table for
  the hard-reserved `far`
- `internal/diag/codes.go` — diagnostic-code registry (reuse-first allocation)
- `internal/ast/attr_catalog.go` — closed-set attribute registry for
  `@shard_movable` and `@shard_pinned`
- `internal/sema/attr_validation_types.go` — semantic validation of type
  attributes
- `stdlib/net/net.sg` and `stdlib/http/server.sg` — current consumers of
  `TcpConn`, affected by the shard-mobility markers

## Starting State

Epic 10 closed the owner-safety work needed before entering this surface:

- copied TCP handles are stable runtime handle ids, not OS fds or native
  pointers;
- native net entrypoints canonicalize handles before touching owner/fd/lifetime
  state;
- stdlib HTTP no longer sends raw `TcpConn.__opaque` values through worker
  channels;
- no Phase 4 language syntax, inbound queues, remote messages, eventfd credits,
  remote `select`, or remote-free routing has been implemented.

Current language constraints:

- `own T`, `&T`, `&mut T`, and backend-only `*T` are prefix type forms.
- Raw pointers are not permitted in user code except `extern` and
  `@intrinsic` declarations.
- Only `own T` values may cross task boundaries today.
- Borrows (`&T`, `&mut T`) cannot cross worker-thread boundaries.
- `@nosend` forbids crossing task boundaries; `@local spawn` is the explicit
  local escape hatch for `@nosend` captures.
- Attributes are a closed-set language feature and are not general type-use
  modifiers.

## Non-Goals

Epic 11 does not implement:

- remote `select` coordinator;
- remote-free queues;
- resource migration syntax;
- `on blocking { ... }`;
- remote function handles or remote closures;
- public examples before syntax and semantic tests exist.

These surfaces are postponed, not accidentally omitted.

## Keyword Strategy

Epic 11 introduces two crossing source words with two different lexical
strategies:

- `far` is a hard reserved keyword. It is added to the lexer keyword table and
  can no longer be used as an ordinary identifier. Existing code that used `far`
  as an identifier must be renamed; that migration is covered by Block 1's
  reserved-identifier fixtures.
- `on` is a contextual keyword. It is not added to the lexer keyword table and
  continues to tokenize as an identifier. The parser recognizes it only in its
  crossing positions: `on` at an expression head (`on <expr> { ... }`) and
  immediately after `spawn`. Everywhere else it remains an ordinary identifier,
  so pre-existing code such as `let on = 1;` keeps parsing unchanged. Block 2
  carries the positive back-compatibility fixtures for this.
- `crosses` is not a keyword. It remains an ordinary identifier; crossing is an
  inferred semantic effect.

This split is deliberate: `far` is a type-position modifier with no reasonable
identifier collision cost, while `on` is a common enough English word that
reserving it would break existing programs. The former `crosses` marker was
retired entirely because the compiler can infer the effect.

## Epic 11 Execution Scope

Epic 11 delivers the language surface only: lexer, parser, and semantic-analysis
support for `far`, `on`, `spawn on`, inferred crossing effects,
`@shard_movable`, and `@shard_pinned`, plus lowering guards. It does not deliver
the Phase 4 crossing transport.

- Every crossing execution path (`on`, `spawn on`, and remote `far Task<T>`
  operations) emits a deterministic backend-unavailable diagnostic (the
  backend/configuration diagnostic family) until the Phase 4 transport epic
  lands. The guard applies to `on` placement crossing as well as `spawn on`.
- Because there is no transport, all positive golden fixtures in this epic are
  compile-only: they prove the form parses and type-checks, not that it runs.
- There is no producer of a `far Channel<T>`, `far TcpConn`, or `far Task<T>`
  value in Epic 11 except function parameters (and, for `far Task<T>`, the
  `spawn on` expression, whose execution is itself guarded). Fixtures that need
  a `far` handle therefore take it as a parameter, which is why those fixtures
  are compile-only by construction.

## Block 1: `far` Type Modifier

### Meaning

`far T` is a local, typed remote handle to a value or resource whose owner is
on another shard. It does not move `T` to the local shard. It owns or borrows
only the local handle value, never the remote resource itself.

`far` is a type modifier. It is not an attribute, not an item modifier, and not
a function effect.

Valid shape:

```sg
type RemoteConn = far TcpConn;

fn register(conn: far TcpConn) -> nothing {
    return nothing;
}

fn start(job: own Job) -> far Task<Result> {
    return spawn on distributed {
        ret run_job(own job);
    };
}
```

Invalid shape:

```sg
far fn f() -> nothing { return nothing; } // diagnostic: far is not an item modifier
```

### Grammar Rule

For the first implementation slice, `far` appears after an optional local
ownership/borrow prefix and before the base type:

```text
Type := LocalPrefix? FarPrefix? BaseType Suffix*
LocalPrefix := "own" | "&" | "&mut" | "*"
FarPrefix := "far"
```

This ordering is intentional:

- `own far T` moves the local handle.
- `&far T` borrows the local handle.
- `&mut far T` mutably borrows the local handle.
- `far own T`, `far &T`, and `far &mut T` are invalid because they would imply
  remote ownership or remote borrowed lifetimes.

Postfix array syntax keeps the existing Surge precedence: postfix binds tighter
than prefix, so `far T[]` parses as `far (T[])`. Arrays are not
remote-handle-capable in Epic 11 (see Remote-Handle-Capable Types), so the form
parses but semantic analysis rejects it as postponed. A local array of remote
handles is likewise postponed until the language has an accepted single-type
grouping syntax or another explicit form.

### Remote-Handle-Capable Types

This is the single canonical definition of "remote-handle-capable" for Epic 11.
A base type is remote-handle-capable, and therefore a valid operand for `far`,
if and only if it is one of:

- the intrinsic `Channel<T>`;
- the intrinsic `Task<T>` (as produced by `spawn on`);
- a type marked `@shard_pinned` (Block 4), such as the prelude `TcpConn`.

Nothing else is remote-handle-capable in Epic 11. Arrays are explicitly not
remote-handle-capable in this epic: `far T[]` remains grammatically parseable
(postfix `[]` binds tighter than the `far` prefix), but semantic analysis
rejects it as a postponed surface. Plain owned values, primitives, `nothing`,
`unit`, and unmarked user structs are not remote-handle-capable; they cross by
copy or by `own T` move through `on` / `spawn on`, not by remote handle.

### Valid Type Matrix

| Form | Status | Contract |
| --- | --- | --- |
| `far T` | Valid when `T` is remote-handle-capable | Core remote handle form. |
| `far Channel<T>` | Valid | Remote channel endpoint owned by another shard. |
| `far Task<T>` | Valid | Remote task handle produced by `spawn on`. |
| `far TcpConn` | Valid if `TcpConn` is `@shard_pinned` | Remote handle to a shard-pinned resource, not a moved socket. |
| `far T[]` | Postponed (parses, sema-rejects) | Local arrays of remote handles and remote handles to arrays are postponed in Epic 11; the form parses but semantic analysis rejects it. |
| `Channel<far T>` | Valid when `far T` is valid | Local channel carrying remote handles. |
| `Task<far T>` | Valid when `far T` is valid | Local task that returns a remote handle. |
| `own far T` | Valid | Moves ownership of the local handle. |
| `&far T` | Valid | Shared borrow of the local handle. |
| `&mut far T` | Valid | Exclusive borrow of the local handle, not the remote resource. |

### Invalid Type Matrix

| Form | Diagnostic Contract |
| --- | --- |
| `far far T` | Nested remote handles are invalid. |
| `far own T` | Remote owned values are invalid; crossing moves `own T` through `on` or `spawn on`. |
| `far &T` | Borrowed remote lifetimes are invalid. |
| `far &mut T` | Borrowed remote lifetimes are invalid. |
| `far *T` | Remote raw pointers are invalid in user code. |
| `*far T` | Raw pointers to remote handles are invalid in user code. |
| `far fn(...) -> T` | Remote function handles are postponed and invalid in Epic 11. |
| `far extern<T>` | `extern<T>` is an item/block target, not a value capability. |
| `far nothing` | Remote absence has no capability semantics. |
| `far unit` | Remote unit has no capability semantics. |
| `far Error` | Plain values cross by copy/move, not by remote handle. |
| `far` before item keywords | `far` is a type modifier only. |

### Operation Rules

- Local operations on `far T` are invalid unless the specific `far` type has an
  accepted remote-handle operation in this document.
- Acting through `far T` requires `on handle { ... }`, except for the accepted
  `far Task<T>.await()` and `far Task<T>.cancel()` operations.
- A `far T` handle is affine (move-only) in Epic 11. Moving the handle transfers
  the local handle value; it does not copy or move the remote resource, and the
  moved-from binding is used-after-move. Copyable `far` handles are postponed
  (see Postponed Surfaces).
- `far T` does not bypass `@nosend` or `@shard_pinned` for moving the resource.

### Test Obligations

Positive golden fixtures:

- type alias: `type RemoteConn = far TcpConn;`
- parameter: `fn f(conn: far TcpConn) -> nothing`;
- return type: `fn f() -> far Task<int>`;
- generic argument: `Channel<far Task<int>>`;
- `own far T`, `&far T`, and `&mut far T`.

Negative golden fixtures:

- every invalid matrix row above;
- local method call on `far T` outside `on`;
- `far` used as an item modifier.

## Block 2: `on dst { ... }` Placement Crossing Block

### Meaning

`on dst { ... }` is an immediate crossing block. It moves execution to a
placement target, waits for the remote completion, and resumes the current task
with a `TaskResult<T>`.

It is the common "do this there and wait here" form. It is not a spawn form and
does not return a task handle.

```sg
fn score(req: own Request) -> TaskResult<int> {
    return on pool {
        ret compute_score(own req);
    };
}
```

`on` is a language keyword. `dst` is a typed placement target expression. It is
not a type name and not an arbitrary function name.

### Placement Model

The first implementation slice introduces a core intrinsic destination type:

```sg
@intrinsic
pub type Placement = { __opaque: int };

pub type ShardId = uint32;

@intrinsic
pub const pool: Placement;

@intrinsic
pub const distributed: Placement;

@intrinsic
pub fn shard(id: ShardId) -> Placement;
```

`Placement` is a `Copy`, shard-movable intrinsic value type. It may be stored in
a variable, passed to and returned from functions, and captured into `on` and
`spawn on` bodies as an ordinary `Copy` capture, including by computing it
inside the body (for example calling `shard(id)` within the crossing body).

`pool` and `distributed` are prelude placement values, not general keywords.
`shard(id)` is a function returning `Placement`. A user may store a placement
in a variable:

```sg
fn route_for(user_id: uint64) -> Placement {
    return shard((user_id % shard_count()) as ShardId);
}

fn route(req: own Request, user_id: uint64) -> TaskResult<Response> {
    let dst: Placement = route_for(user_id);
    return on dst {
        ret handle_request(own req);
    };
}
```

`shard(id)` with an out-of-range shard id is compile-valid: `id` is an ordinary
`ShardId` value and the compiler cannot in general prove its range. The runtime
behavior for an out-of-range placement is a recorded contract for the Phase 4
transport epic: it must be a deterministic runtime error and must never be
undefined behavior, a panic, or a crash. Epic 11 records this contract only; it
does not implement the runtime dispatch.

### Destination Matrix

| Destination | Status | Example | Contract |
| --- | --- | --- | --- |
| `pool` | Valid | `on pool { ret crunch(own data); }` | CPU-bound Tier 2 placement. |
| `distributed` | Valid | `on distributed { ret work(own job); }` | Runtime chooses an eligible shard. |
| `shard(id)` | Valid | `on shard(id) { ret work(own job); }` | Explicit shard placement. |
| `dst: Placement` | Valid | `on dst { ret process(own req); }` | Computed placement. |
| function call returning `Placement` | Valid | `on route_for(user_id) { ret process(own req); }` | The call result is the destination. |
| `far Channel<T>` | Valid | `on ch { ch.send(own msg); ret nothing; }` | Destination is the channel owner shard. |
| `far Task<T>` | Invalid as `on` destination | `on t { ... }` | Remote task operations use `t.await()` and `t.cancel()` directly. |
| `far TcpConn` | Valid for control-only operations | `on conn { conn.close(); ret nothing; }` | Remote I/O is not accepted in Epic 11. |
| `blocking` | Invalid in Epic 11 | `on blocking { ... }` | Existing `blocking {}` remains the syscall-offload construct. |
| ordinary value | Invalid | `on 1 { ... }` | Value is not a placement target. |
| type name | Invalid | `on Job { ... }` | Type is not a placement target. |
| bare function name | Invalid | `on route_for { ... }` | Call the function if it returns `Placement`. |

### Body Rules

- `ret expr;` produces the block result.
- `return` from inside `on` is invalid. Crossing blocks cannot return through
  the enclosing function.
- Captures fall into exactly three accepted categories: (a) `Copy` values,
  (b) `own @shard_movable` values, and (c) `far T` handles captured by move
  (affine). `Placement` is `Copy` and therefore capturable under (a).
- Borrowed captures are invalid.
- `@nosend` value captures are invalid.
- `@shard_pinned` value captures are invalid unless captured as `far T`.
- The block returns `TaskResult<T>`.
- `on` is allowed only where suspension is legal. Its enclosing function is
  inferred as `MayCross`.
- An `on` block may be matched directly with `compare on dst { ... } { ... }`,
  handling the `TaskResult<T>` arms (`Success(v)` / `Cancelled()`) in place.
- `on` is allowed in statement position with its `TaskResult<T>` discarded, the
  same as any other expression statement.

### Far-Handle Owner Rule

When `dst` is a `far T` handle, that handle anchors the remote owner for the
block. The block may act through that handle. Other `far` handles inside the
same block are not assumed to share the owner.

Valid:

```sg
fn send_job(ch: far Channel<Job>, job: own Job) -> TaskResult<nothing> {
    return on ch {
        ch.send(own job);
        ret nothing;
    };
}
```

Invalid:

```sg
fn send_two(a: far Channel<Job>, b: far Channel<Job>, job: own Job) -> TaskResult<nothing> {
    return on a {
        b.send(own job); // diagnostic: b is not proven to be owned by destination a
        ret nothing;
    };
}
```

Nested `on` blocks are postponed in Epic 11. A block cannot open a second
crossing inside an active crossing block.

### Test Obligations

Positive golden fixtures:

- `on pool { ret value; }`;
- `on distributed { ret value; }`;
- `on shard(id) { ret value; }`;
- `on dst { ret value; }` where `dst: Placement`;
- `on route_for(id) { ret value; }`;
- `on ch { ch.send(own msg); ret nothing; }`;
- `on conn { conn.close(); ret nothing; }` as the accepted control-only
  `far TcpConn` operation.

Negative golden fixtures:

- invalid destination matrix rows;
- borrowed capture;
- `@nosend` value capture;
- `@shard_pinned` value capture;
- `return` inside `on`;
- nested `on`;
- operation on an unanchored `far` handle;
- remote socket read/write through `far TcpConn`.

## Block 3: `spawn on dst { ... }` Remote Spawn Block

### Meaning

`spawn on dst { ... }` creates remote or placed work and returns a
`far Task<T>` handle. It is the join-later form. It does not wait for the
remote completion immediately.

```sg
fn start(job: own Job) -> far Task<Result> {
    return spawn on distributed {
        ret run_job(own job);
    };
}
```

### Destination Matrix

| Destination | Status | Example | Contract |
| --- | --- | --- | --- |
| `distributed` | Valid | `spawn on distributed { ret run(own job); }` | Runtime chooses an eligible shard. |
| `pool` | Valid | `spawn on pool { ret crunch(own data); }` | CPU-bound Tier 2 task. |
| `shard(id)` | Valid | `spawn on shard(id) { ret run(own job); }` | Explicit shard placement. |
| `dst: Placement` | Valid | `spawn on dst { ret run(own job); }` | Computed placement. |
| function call returning `Placement` | Valid | `spawn on route_for(id) { ret run(own job); }` | The call result is the destination. |
| `far T` handle destination | Invalid in Epic 11 | `spawn on ch { ... }` | Owner-anchored remote tasks are postponed. Use `on ch` for immediate remote handle operations. |
| `blocking` | Invalid in Epic 11 | `spawn on blocking { ... }` | Existing `blocking {}` remains separate. |

### Body Rules

- `ret expr;` produces the remote task result.
- `return` from inside `spawn on` is invalid.
- Captures fall into exactly three accepted categories: (a) `Copy` values,
  (b) `own @shard_movable` values, and (c) `far T` handles captured by move
  (affine). `Placement` is `Copy` and therefore capturable under (a).
- Borrowed captures are invalid.
- `@nosend` value captures are invalid.
- `@shard_pinned` value captures are invalid unless captured as `far T`.
- A local `Task<T>` is not `@shard_movable`, so capturing one into a
  `spawn on` body is a capture violation, not an await error.
- `@local spawn on dst` is invalid. Local spawn and remote placement are
  mutually exclusive.
- The result type is `far Task<T>`.

### Remote Task Rules

`far Task<T>` has two accepted remote task operations in Epic 11:

```sg
fn wait_result(t: far Task<Result>) -> TaskResult<Result> {
    return t.await();
}

fn cancel_remote(t: far Task<Result>) -> TaskResult<nothing> {
    return t.cancel();
}
```

- `t.await()` on `far Task<T>` is a remote crossing operation and returns
  `TaskResult<T>`.
- `t.cancel()` on `far Task<T>` is a remote crossing operation and returns
  `TaskResult<nothing>`.
- Both operations infer `MayCross` on the enclosing function.
- These methods are distinct from local `Task<T>` methods even if the surface
  spelling is intentionally familiar.
- `far Task<T>` is not a valid `on` destination in Epic 11. This avoids nested
  `TaskResult<TaskResult<T>>` and keeps remote task UX direct.

### Test Obligations

Positive golden fixtures:

- `spawn on distributed { ret value; } -> far Task<T>`;
- `spawn on pool { ret value; } -> far Task<T>`;
- `spawn on shard(id) { ret value; } -> far Task<T>`;
- returning `far Task<T>` from a function whose `MayCross` effect is inferred.

Negative golden fixtures:

- `@local spawn on dst`;
- borrowed capture;
- `@nosend` value capture;
- `@shard_pinned` value capture;
- `return` inside `spawn on`;
- invalid `far Task<T>.await()` result use;
- invalid `far Task<T>.cancel()` result use;
- `spawn on ch` where `ch: far Channel<T>`;
- `spawn on` in a backend/configuration where Phase 4 is unavailable.

## Block 4: Crossing Contracts

### Inferred Crossing Function Effect

The explicit `crosses` keyword was retired by design change D17 on
2026-07-08. Crossing is now an inferred semantic effect stored in
`Result.FunctionEffects[fn].MayCross`; there is no surface keyword and no
programmer-facing marker requirement.

The compiler infers `MayCross` from:

- `on dst { ... }`;
- `spawn on dst { ... }`;
- `far Task<T>.await()`;
- `far Task<T>.cancel()`;
- direct calls to functions already inferred as `MayCross`.

Examples:

```sg
fn route(req: own Request) -> TaskResult<Response> {
    return on distributed {
        ret handle(own req);
    };
}

fn start(job: own Job) -> far Task<Result> {
    return spawn on pool {
        ret process(own job);
    };
}

fn wait_result(t: far Task<Result>) -> TaskResult<Result> {
    return t.await();
}

fn outer(req: own Request) -> TaskResult<Response> {
    return route(own req); // `MayCross` propagates through the direct call.
}
```

Retired syntax:

```sg
fn route(req: Request) crosses -> Response { ... } // no longer valid syntax
crosses fn route(req: Request) -> Response { ... } // no longer valid syntax
@crosses fn route(req: Request) -> Response { ... } // unknown attribute
```

Retired diagnostics: `SEM3162`, `SEM3163`, `SEM3164`, `SYN2034`, `SYN2035`,
and `SYN2036` are reserved and must not be reused.

Remaining debt: higher-order/function-type effect propagation and possible
cross-module export propagation are tracked as `RV2-DEBT-024`.

### `@shard_movable`

`@shard_movable` is a type attribute. It declares that owned values of this
type may cross shard boundaries.

```sg
@shard_movable
type Job = {
    id: uint64,
    payload: string,
};
```

Rules:

- Target: type declarations only.
- The compiler recursively validates fields/members.
- Every field/member must be shard-movable.
- `@shard_movable` conflicts with `@shard_pinned`.
- `@send` does not imply `@shard_movable`.
- `@copy` does not imply `@shard_movable` for user-defined types.
- Built-in primitives and compiler-known immutable value types are
  shard-movable. User-defined structs/unions must use `@shard_movable` if they
  cross as `own T`.
- Types containing `far T` handles are not automatically shard-movable; the
  owning type must explicitly declare `@shard_movable` and the compiler must
  validate handle-lifetime rules.

### `@shard_pinned`

`@shard_pinned` is a type attribute. It declares that values of this type have
runtime state registered to a specific shard and cannot cross as `own T`.

```sg
@shard_pinned
@nosend
type TcpConn = {
    __opaque: int,
};
```

Rules:

- Target: type declarations only.
- `@shard_pinned` conflicts with `@shard_movable`.
- A shard-pinned value cannot be captured by `on` or `spawn on` as `own T`.
- A shard-pinned type can have `far T` handles only when the runtime provides a
  safe handle constructor.
- `@shard_pinned` does not replace `@nosend`. The two attributes express
  different contracts:
  - `@nosend`: value cannot cross task boundaries;
  - `@shard_pinned`: value cannot cross shard boundaries as a resource.
- Runtime resource types such as sockets must use both unless a task-boundary
  exception is documented with a narrower proof.

### Contract Test Obligations

Positive golden fixtures:

- `on` infers `MayCross`;
- `spawn on` infers `MayCross`;
- `far Task<T>.await()` / `.cancel()` infer `MayCross`;
- a direct call chain propagates `MayCross`;
- `crosses` remains an ordinary identifier;
- `@shard_movable` value captured by `own` in `on`;
- `@shard_pinned` value represented by `far T`;
- `@nosend` value accepted by `@local spawn` but rejected by `spawn on`.

Negative golden fixtures:

- `@shard_pinned` + `@shard_movable`;
- `@send` treated as sufficient for shard movement;
- `@copy` treated as sufficient for user-defined shard movement;
- crossing a `@nosend` value by `own`;
- crossing a borrowed capture;
- crossing a `@shard_pinned` value by `own`.

## Postponed Surfaces

These are explicitly postponed and must diagnose if used in Epic 11:

- `on blocking { ... }`;
- `spawn on blocking { ... }`;
- nested `on` blocks;
- remote `select` and remote `race`;
- resource migration syntax;
- remote socket read/write through `far TcpConn`;
- remote closures and `far fn(...) -> T`;
- higher-order/function-type crossing-effect syntax;
- using `far Task<T>` as an `on` destination;
- copyable `far` handles (a future opt-in capability, working name `@far_copy`);
  `far` handles are affine in Epic 11;
- `far` arrays (`far T[]`) and local arrays of `far` handles; arrays are not
  remote-handle-capable in Epic 11.

The reason is the same for all: they require additional lifetime, cancellation,
or transport semantics that are not necessary for the first explicit crossing
slice. They are not allowed as silent partial behavior.

## Diagnostics Contract

The implementation tasks must assign exact codes before writing negative
fixtures. The required diagnostic families are:

- `far` used outside type position;
- invalid nested `far`;
- remote borrowed type;
- remote raw pointer type;
- remote plain-value handle;
- local operation on `far T`;
- invalid `on` destination;
- invalid `spawn on` destination;
- `return` inside `on` or `spawn on`;
- nested `on`;
- borrowed capture into crossing;
- `@nosend` capture into crossing;
- shard-pinned value capture into crossing;
- operation on an unanchored `far` handle inside `on`;
- `@local spawn on`;
- invalid `far Task<T>.await()` / `.cancel()` result use;
- using `far Task<T>` as an `on` destination;
- invalid shard mobility attribute target;
- `@shard_movable` / `@shard_pinned` conflict;
- unsupported backend/configuration for Phase 4 syntax.

Diagnostic messages must name the invariant:

- "borrowed values cannot cross shard boundaries";
- "this operation would move a shard-pinned resource; use a far handle or
  explicit migration";
- "remote channel send must be written inside `on <that channel>`";
- "this remote handle is not anchored by the current `on` destination".

## Reference Usage

### Remote Channel Send

```sg
@shard_movable
type Job = {
    id: uint64,
    payload: string,
};

fn send_job(ch: far Channel<Job>, job: own Job) -> TaskResult<nothing> {
    return on ch {
        ch.send(own job);
        ret nothing;
    };
}
```

### CPU Pool Work

```sg
@shard_movable
type Request = {
    id: uint64,
    body: string,
};

fn score(req: own Request) -> TaskResult<int> {
    return on pool {
        ret compute_score(own req);
    };
}
```

### Distributed Spawn

```sg
fn start(job: own Job) -> far Task<Result> {
    return spawn on distributed {
        ret run_job(own job);
    };
}
```

### Explicit Shard Placement

```sg
fn route_for(user_id: uint64) -> Placement {
    return shard((user_id % shard_count()) as ShardId);
}

fn route(req: own Request, user_id: uint64) -> TaskResult<Response> {
    return on route_for(user_id) {
        ret handle_request(own req);
    };
}
```

### Shard-Pinned Resource Handle

```sg
@shard_pinned
@nosend
type TcpConn = {
    __opaque: int,
};

fn close_remote(conn: far TcpConn) -> TaskResult<nothing> {
    return on conn {
        conn.close();
        ret nothing;
    };
}
```

The `far TcpConn` control-operation set is closed in Epic 11 to exactly
`{ close() }`. No other operation (read, write, accept, or any other socket
control) is accepted on a `far TcpConn` in this epic. Remote read/write on
`far TcpConn` is postponed beyond Epic 11.

## Acceptance Criteria Before Task Slicing

The epic is ready for task slicing when:

- every block has positive and negative matrix rows converted into golden-test
  fixture names;
- every negative row has an exact diagnostic code assigned or explicitly marked
  as a new-code allocation;
- every accepted form has at least one positive golden fixture;
- every invariant has at least one negative golden fixture;
- Draft 9 documentation scope is recorded for `LANGUAGE.md` and
  `LANGUAGE.ru.md`;
- `ATTRIBUTES.md` / `ATTRIBUTES.ru.md` updates for `@shard_movable` and
  `@shard_pinned` are planned;
- `docs/RUNTIME_V2.md` update points are listed;
- no implementation task starts before the relevant golden fixtures exist.
