# Epic 11 Block 1: `far` Type Modifier Test Matrix

This document is the test-case matrix and fixture inventory for Block 1 of
Epic 11. It is documentary only: it records the parser, semantic-analysis,
diagnostic, fixture, and later lowering obligations for `far T`.

Implementation work, test files, diagnostic allocation, and commits are outside
this document.

## Source Contract

- Epic source: `docs/runtime-v2-epics/11-explicit-crossing-language-surface.md`,
  Block 1.
- Current language grammar source: `docs/LANGUAGE.md`, especially ownership
  type forms, arrays, generics, diagnostics overview, and grammar sketch.
- Attribute boundary source: `docs/ATTRIBUTES.md`. `far` is a type modifier,
  not an attribute, not a directive, not an item modifier, and not a function
  effect.

The accepted Block 1 grammar shape is:

```text
Type := LocalPrefix? FarPrefix? BaseType Suffix*
LocalPrefix := "own" | "&" | "&mut"
FarPrefix := "far"
```

This extends the current language grammar, which has a single optional
ownership prefix before the core type. The extension must preserve existing
array and generic precedence. Raw pointer forms involving `far` are rejected in
user code and are covered separately by the raw-pointer matrix.

## Test Groups

| Group | Purpose |
| --- | --- |
| `FAR-LEX` | Keyword, reserved identifier, and token-position coverage. |
| `FAR-PARSE` | Accepted and rejected type grammar shapes. |
| `FAR-PREC` | Array, fixed-array, generic, and prefix precedence. |
| `FAR-ID` | Type identity and alias/generic distinctness. |
| `FAR-CAP` | Remote-handle-capable versus non-capability base types. |
| `FAR-OWN` | Ownership and borrow rules for the local handle value. |
| `FAR-OPS` | Local operation rejection outside an accepted remote context. |
| `FAR-DIAG` | Diagnostic code allocation and exact negative-fixture mapping. |
| `FAR-FIX` | Positive and negative golden fixture inventory. |
| `FAR-FOLLOW` | Follow-up unit, sema, and lowering no-op/type-preservation checks. |

## Lexical And Keyword Matrix

Block 1 reserves `far` as a hard keyword: it is added to the lexer keyword table
and is no longer a valid identifier in any position (unlike the contextual
keywords `on` and `crosses`, which stay identifiers). Identifier use of `far`
must fail deterministically once the keyword is enabled.

| ID | Source shape | Expected result | Diagnostic |
| --- | --- | --- | --- |
| `FAR-LEX-001` | `type Remote<T> = far T;` | Parses `far` as a type modifier. | none |
| `FAR-LEX-002` | `fn f(x: far TcpConn) -> nothing;` | Parses `far` in parameter type position. | none |
| `FAR-LEX-003` | `fn f() -> far Task<int>;` | Parses `far` in return type position. | none |
| `FAR-LEX-NEG-001` | `let far: int = 1;` | Reserved keyword cannot be used as a binding identifier. | `SYN2031` |
| `FAR-LEX-NEG-002` | `fn far() -> nothing { return nothing; }` | Reserved keyword cannot be used as a function name. | `SYN2031` |
| `FAR-LEX-NEG-003` | `type far = int;` | Reserved keyword cannot be used as a type alias name. | `SYN2031` |
| `FAR-LEX-NEG-004` | `extern<far> {}` | Reserved keyword cannot be used as a base type name. | `SYN2031` |

Compatibility coverage must include at least one migration-negative fixture for
pre-existing user code that used `far` as an identifier. The fixture records the
exact diagnostic and the fix shape: rename the identifier.

## Parser Shape Matrix

| ID | Type shape | Expected result | Contract |
| --- | --- | --- | --- |
| `FAR-PARSE-001` | `far T` | valid | Core remote handle type form. |
| `FAR-PARSE-002` | `own far T` | valid | Moves ownership of the local handle. |
| `FAR-PARSE-003` | `&far T` | valid | Shared borrow of the local handle. |
| `FAR-PARSE-004` | `&mut far T` | valid | Exclusive borrow of the local handle. |
| `FAR-PARSE-005` | `far Channel<T>` | valid | Remote handle to a channel endpoint. |
| `FAR-PARSE-006` | `far Task<T>` | valid | Remote task handle type. |
| `FAR-PARSE-007` | `Channel<far T>` | valid | Generic argument may be a `far` type. |
| `FAR-PARSE-008` | `Task<far T>` | valid | Generic result may be a `far` type. |
| `FAR-PARSE-009` | `type RemoteConn = far TcpConn;` | valid | Alias type position accepts `far`. |
| `FAR-PARSE-010` | `type Holder = { conn: far TcpConn };` | valid | Field type position accepts `far`. |
| `FAR-PARSE-NEG-001` | `far far T` | invalid | Nested remote handles are rejected. |
| `FAR-PARSE-NEG-002` | `far own T` | invalid | Remote owned values are rejected. |
| `FAR-PARSE-NEG-003` | `far &T` | invalid | Remote borrowed lifetimes are rejected. |
| `FAR-PARSE-NEG-004` | `far &mut T` | invalid | Remote mutable borrowed lifetimes are rejected. |
| `FAR-PARSE-NEG-005` | `far *T` | invalid | Remote raw pointers are rejected. |
| `FAR-PARSE-NEG-006` | `*far T` | invalid | Raw pointers to remote handles are rejected in user code. |
| `FAR-PARSE-NEG-007` | `far fn(int) -> int` | invalid | Remote function handles are postponed and rejected in Epic 11. |
| `FAR-PARSE-NEG-008` | `far extern<T>` | invalid | `extern<T>` is not a value capability type. |
| `FAR-PARSE-NEG-009` | `far fn f() -> nothing { return nothing; }` | invalid | `far` is not an item modifier. |
| `FAR-PARSE-NEG-010` | `far type Remote = TcpConn;` | invalid | `far` is not an item modifier. |

## Precedence Matrix

Array postfix syntax binds tighter than prefix modifiers. These rows are
load-bearing and must remain explicit in fixtures and AST/unit tests.

| ID | Type shape | Expected meaning | Expected result |
| --- | --- | --- | --- |
| `FAR-PREC-001` | `far T[]` | `far (T[])`: postfix `[]` binds tighter than the `far` prefix. | parses; sema-rejects as postponed (`FUT7009`) |
| `FAR-PREC-002` | `far T[N]` | `far (T[N])`: postfix `[N]` binds tighter than the `far` prefix. | parses; sema-rejects as postponed (`FUT7009`) |
| `FAR-PREC-003` | `Channel<far T>` | Local channel carrying remote handles. | valid when `far T` is valid |
| `FAR-PREC-004` | `Task<far T>` | Local task returning a remote handle. | valid when `far T` is valid |
| `FAR-PREC-005` | `far Channel<T>` | Remote handle to a channel endpoint owned elsewhere. | valid |
| `FAR-PREC-006` | `far Task<T>` | Remote handle to a task owned elsewhere. | valid |
| `FAR-PREC-007` | `&far T[]` | Shared borrow of the local `far (T[])` value. | parses; sema-rejects as postponed (far arrays are postponed) |
| `FAR-PREC-008` | `&mut far Channel<T>` | Exclusive borrow of the local remote channel handle. | valid |
| `FAR-PREC-NEG-001` | `far (T)[]` | Grouped single type does not create a local array of far handles. | `SEM3140` |
| `FAR-PREC-NEG-002` | `(far T)[]` | Local array of remote handles is postponed until an accepted grouping form exists. | `FUT7010` |

Block 1 must not invent a new grouping syntax to express a local array of remote
handles. The accepted spelling for a local collection of handles in Block 1 is a
generic container such as `Channel<far T>` or an existing explicit type form
that the parser already supports. Parenthesized type forms that would change
`far` precedence are invalid in this block.

## Type Identity Matrix

`far T` is a distinct type former. Local ownership modifiers apply to the local
handle, not to the remote resource.

| ID | Case | Expected result | Diagnostic |
| --- | --- | --- | --- |
| `FAR-ID-001` | `type RemoteConn = far TcpConn; let x: RemoteConn;` | Alias resolves to `far TcpConn`. | none |
| `FAR-ID-002` | Assign `far TcpConn` to `TcpConn`. | rejected; remote handle is not the resource. | `SEM3015` |
| `FAR-ID-003` | Assign `TcpConn` to `far TcpConn`. | rejected; resource value is not a remote handle. | `SEM3015` |
| `FAR-ID-004` | Pass `far TcpConn` to parameter `far TcpConn`. | accepted. | none |
| `FAR-ID-005` | Pass `far TcpConn` to parameter `TcpConn`. | rejected. | `SEM3015` |
| `FAR-ID-006` | Pass `own far TcpConn` to parameter `own far TcpConn`. | accepted; moves local handle. | none |
| `FAR-ID-007` | `Channel<far T>` versus `far Channel<T>`. | distinct types; no implicit conversion. | `SEM3015` |
| `FAR-ID-008` | `Task<far T>` versus `far Task<T>`. | distinct types; no implicit conversion. | `SEM3015` |
| `FAR-ID-009` | `far Channel<T>` versus `far TcpConn`. | distinct types; no implicit conversion. | `SEM3015` |
| `FAR-ID-010` | `x is far TcpConn` for value `x: far TcpConn`. | accepted; `far` participates in type identity as a distinct type former. | none |

The `is` row is a required documented test row because `LANGUAGE.md` defines
type identity behavior for ownership and references. The implementation slice
must support `far` as a right-hand type operand and must not erase `far` to
`T`.

## Capability Matrix

Block 1 only accepts `far` over types that are remote-handle-capable under the
Epic 11 contract.

The remote-handle-capable base types for Epic 11 are defined once in the epic
document (Block 1, "Remote-Handle-Capable Types"): the intrinsic `Channel<T>`,
the intrinsic `Task<T>`, and `@shard_pinned` types. Arrays are not
remote-handle-capable in Epic 11.

| ID | Type shape | Expected result | Contract |
| --- | --- | --- | --- |
| `FAR-CAP-001` | `far Channel<T>` | valid | Remote channel endpoint handle. |
| `FAR-CAP-002` | `far Task<T>` | valid | Remote task handle. |
| `FAR-CAP-003` | `far TcpConn` | valid when `TcpConn` is `@shard_pinned`. | Remote handle to a pinned resource. |
| `FAR-CAP-005` | `far UserType` where `UserType` is `@shard_pinned`. | valid | `@shard_pinned` (Block 4) is the marker that makes a user-defined type remote-handle-capable. |
| `FAR-CAP-NEG-001` | `far nothing` | invalid | Absence has no remote capability semantics. |
| `FAR-CAP-NEG-002` | `far unit` | invalid | Unit has no remote capability semantics. |
| `FAR-CAP-NEG-003` | `far Error` | invalid | Plain values cross by copy or move, not by remote handle. |
| `FAR-CAP-NEG-004` | `far int` | invalid | Primitive value crosses by copy. |
| `FAR-CAP-NEG-005` | `far string` | invalid | Plain owned value crosses by move/copy rules, not remote handle. |
| `FAR-CAP-NEG-006` | `far LocalOnly` where `LocalOnly` is `@nosend`. | invalid | `far` does not bypass local-only crossing rules. |
| `FAR-CAP-NEG-007` | `far PlainStruct` with no remote capability marker. | invalid | Plain structs are not remote-handle-capable by default. |
| `FAR-CAP-NEG-008` | `far T[]` | invalid; postponed. | Arrays are not remote-handle-capable in Epic 11; `far T[]` parses but sema-rejects as a postponed surface (`FUT7009`). |

`FAR-CAP-005` is a cross-block dependency on Block 4's `@shard_pinned` marker.
Block 1 fixtures must use already-known intrinsic/core types for standalone
coverage and reserve custom `@shard_pinned` cases for integration fixtures.

## Local Handle Ownership Matrix

These rows prove that ownership and borrowing operate on the local handle value
only. They do not grant ownership, borrowing, mutation, or direct access to the
remote resource. `far T` handles are affine (move-only) in Epic 11: a `far T`
handle cannot be copied, so a copy attempt is rejected as an ordinary
use-after-move. Copyable `far` handles are postponed (see the epic Postponed
Surfaces and `RV2-DEBT-025`).

| ID | Case | Expected result | Diagnostic |
| --- | --- | --- | --- |
| `FAR-OWN-001` | `fn take(x: own far TcpConn) -> nothing` called with `own h`. | accepted; local handle is moved. | none |
| `FAR-OWN-002` | Use local handle after moving it into `own far T`. | rejected by ordinary move rules. | existing move diagnostic or `SEM3130` |
| `FAR-OWN-003` | `fn view(x: &far TcpConn) -> nothing` called with `h: far TcpConn`. | accepted; local handle is shared-borrowed. | none |
| `FAR-OWN-004` | `fn edit(x: &mut far TcpConn) -> nothing` called with `mut h: far TcpConn`. | accepted; local handle is exclusively borrowed. | none |
| `FAR-OWN-005` | Mutate local handle while `&far T` exists. | rejected by ordinary borrow rules. | existing borrow diagnostic or `SEM3018` |
| `FAR-OWN-006` | Take `&far T` while `&mut far T` exists. | rejected by ordinary borrow rules. | existing borrow diagnostic or `SEM3018` |
| `FAR-OWN-007` | `@drop r;` where `r: &far T`. | accepted; ends local handle borrow. | none |
| `FAR-OWN-008` | Copy a `far T` handle, then use both the source and the copy. | rejected; `far` handles are affine (move-only), so the second use is a use-after-move. | reuse `SemaUseAfterMove` (3130) as the ordinary non-Copy move diagnostic |
| `FAR-OWN-NEG-001` | Treat `own far T` as `far own T`. | rejected; remote ownership is not represented. | `SEM3137` |
| `FAR-OWN-NEG-002` | Treat `&far T` as `far &T`. | rejected; remote borrowed lifetime is not represented. | `SEM3138` |
| `FAR-OWN-NEG-003` | Treat `&mut far T` as `far &mut T`. | rejected; remote mutable borrowed lifetime is not represented. | `SEM3138` |

## Local Operation Rejection Matrix

Local operations on `far T` are rejected unless the specific operation is
accepted by Epic 11. Block 1 must cover the rejection behavior; Blocks 2 and 3
own the accepted remote execution forms.

| ID | Operation shape | Expected result | Diagnostic |
| --- | --- | --- | --- |
| `FAR-OPS-NEG-001` | `conn.close();` where `conn: far TcpConn` outside `on conn { ... }`. | rejected. | `SEM3142` |
| `FAR-OPS-NEG-002` | `conn.read(buf);` where `conn: far TcpConn` outside `on`. | rejected. | `SEM3142` |
| `FAR-OPS-NEG-003` | `ch.send(own msg);` where `ch: far Channel<T>` outside `on ch { ... }`. | rejected. | `SEM3142` |
| `FAR-OPS-NEG-004` | `ch.recv();` where `ch: far Channel<T>` outside `on ch { ... }`. | rejected. | `SEM3142` |
| `FAR-OPS-NEG-006` | `value.field` where `value: far Struct`. | rejected. | `SEM3142` |
| `FAR-OPS-NEG-007` | `value to T` where `value: far T`. | rejected; `far` does not participate in value casts. | `SEM3015` |
| `FAR-OPS-NEG-008` | Define `extern<far T> { fn m(self: &far T) -> int; }` and call it locally. | rejected unless explicitly accepted by later contract. | `SEM3142` |
| `FAR-OPS-INT-001` | `on conn { conn.close(); ret nothing; }` where `conn: far TcpConn`. | integration obligation for Block 2. | none after Block 2 |
| `FAR-OPS-INT-002` | `t.await()` where `t: far Task<T>`. | integration obligation for Block 3 remote task operations. | none after Block 3 |
| `FAR-OPS-INT-003` | `t.cancel()` where `t: far Task<T>`. | integration obligation for Block 3 remote task operations. | none after Block 3 |

The integration rows are not Block 1 implementation requirements. They are
recorded so Block 1 does not accidentally reject the later accepted forms with
an unrecoverable parser shape.

Indexing a `far T[]` binding has no local-operation row because `far T[]` is a
postponed surface: the type itself sema-rejects (`FAR-CAP-NEG-008`,
`FAR-PREC-001`) before any indexing operation is reachable.

## Raw Pointer Restrictions

Raw pointers remain backend/FFI-only in current Surge. Block 1 must keep that
restriction intact.

| ID | Type shape | Context | Expected result | Diagnostic |
| --- | --- | --- | --- | --- |
| `FAR-PTR-NEG-001` | `far *T` | user type position | rejected. | `SEM3129` |
| `FAR-PTR-NEG-002` | `*far T` | user type position | rejected. | `SEM3129` |
| `FAR-PTR-NEG-003` | `far *T` | `extern<T>` signature | rejected; remote raw pointer handles are not an FFI escape hatch. | `SEM3129` |
| `FAR-PTR-NEG-004` | `*far T` | `@intrinsic` declaration | rejected unless an explicit compiler-internal exception is documented in the implementation notes. | `SEM3129` |

## Diagnostic Allocation Matrix

Diagnostic codes are allocated. Each row below binds to an exact code (a new
`SEM`/`SYN`/`FUT` code or a reused existing code). The full placeholder-to-code
mapping is recorded in `11-tasks/README.md`.

Allocation rules follow the reuse-first policy: where an Epic 11 negative row
maps onto an existing invariant, reuse the existing code if it renders `far`
correctly; allocate a new code only for genuinely new invariants, and route
postponed surfaces to the `FUT` (7xxx) range. New Epic 11 codes are reserved in
`internal/diag/codes_crossing.go`; reused codes remain in `internal/diag/codes.go`.

| Code | Allocation rule (reuse-first) | Rows |
| --- | --- | --- |
| `SYN2031` | Reuse the existing reserved-keyword-as-identifier diagnostic if one exists; otherwise allocate new (SYN range). Message shape: "`far` is a reserved keyword; rename this identifier". Fix: rename identifier. | `FAR-LEX-NEG-*` |
| `SEM3136` | Allocate new (SEM range); new invariant. Message shape: "nested `far` handles are not allowed". Fix: remove one `far` only when semantics remain local-handle equivalent. | `FAR-PARSE-NEG-001` |
| `SEM3137` | Allocate new (SEM range); new invariant. Message shape: "`far own T` is invalid; move `own T` through `on` or `spawn on`". Fix: rewrite as `own far T` only when local-handle move is intended. | `FAR-PARSE-NEG-002`, `FAR-OWN-NEG-001` |
| `SEM3138` | Allocate new (SEM range); new invariant. Message shape: "`far &T` and `far &mut T` are invalid remote lifetimes". Fix: rewrite as `&far T` or `&mut far T` only when borrowing the local handle is intended. | `FAR-PARSE-NEG-003`, `FAR-PARSE-NEG-004`, `FAR-OWN-NEG-002`, `FAR-OWN-NEG-003` |
| `SEM3129` | Reuse `SemaRawPointerNotAllowed` (3129) if it renders `far`; else allocate new. Message shape: "`far *T` is not allowed". Fix: none. | `FAR-PARSE-NEG-005`, `FAR-PTR-NEG-001`, `FAR-PTR-NEG-003` |
| `SEM3129` | Reuse `SemaRawPointerNotAllowed` (3129) if it renders `far`; else allocate new. Message shape: "`*far T` is not allowed in user code". Fix: none. | `FAR-PARSE-NEG-006`, `FAR-PTR-NEG-002`, `FAR-PTR-NEG-004` |
| `FUT7011` | Allocate new in the `FUT` (7xxx) range; postponed remote-function-handle surface. Message shape: "function types cannot be used as `far` remote handles yet". Fix: none. | `FAR-PARSE-NEG-007` |
| `SEM3139` | Allocate new (SEM range); new invariant. Message shape: "`extern<T>` is not a value capability for `far`". Fix: none. | `FAR-PARSE-NEG-008` |
| `SYN2015` | Reuse `SynModifierNotAllowed` (2015). Message shape: "`far` is a type modifier, not an item modifier". Fix: move `far` into the type position when applicable. | `FAR-PARSE-NEG-009`, `FAR-PARSE-NEG-010` |
| `SEM3140` | Allocate new (SEM range) if grouped single-type syntax reaches the type parser. Message shape: "grouping does not change `far` array precedence". Fix: use a supported generic container. | `FAR-PREC-NEG-001` |
| `FUT7010` | Allocate new in the `FUT` (7xxx) range; postponed local-array-of-`far`-handles surface. Message shape: "local arrays of `far` handles are not supported yet". Fix: use a supported generic container. | `FAR-PREC-NEG-002` |
| `FUT7009` | Allocate new in the `FUT` (7xxx) range; `far T[]` (remote handle to an array) is postponed. Message shape: "array types cannot be used as `far` remote handles yet". Fix: use a remote-handle-capable type instead of an array. | `FAR-PREC-001`, `FAR-PREC-002`, `FAR-CAP-NEG-008` |
| `SEM3015` | Reuse `SemaTypeMismatch` (3015); it must preserve `far` in rendered types (show both `far T` and `T`). Fix: none unless an explicit handle/resource conversion exists. | `FAR-ID-002`, `FAR-ID-003`, `FAR-ID-005`, `FAR-ID-007`, `FAR-ID-008`, `FAR-ID-009` |
| `SEM3141` | Allocate new (SEM range); new invariant. Message shape: "`far` requires a remote-handle-capable type". Fix: use `own T` crossing or add `@shard_pinned` (Block 4). | `FAR-CAP-NEG-001` through `FAR-CAP-NEG-007` |
| `SEM3130` | Reuse `SemaUseAfterMove` (3130) if it renders `far`; else allocate new. | `FAR-OWN-002` |
| `SEM3018` | Reuse `SemaBorrowConflict` (3018) if it renders `far`; else allocate new. | `FAR-OWN-005`, `FAR-OWN-006` |
| `SEM3130` | Reuse `SemaUseAfterMove` (3130); a `far` handle is affine, so a copy attempt is an ordinary non-Copy move / use-after-move. | `FAR-OWN-008` |
| `SEM3142` | Allocate new (SEM range); new invariant. Message shape: "operation on `far T` requires an accepted remote context". Fix: wrap in `on handle { ... }` only for operations accepted by Block 2. | `FAR-OPS-NEG-001` through `FAR-OPS-NEG-004`, `FAR-OPS-NEG-006`, `FAR-OPS-NEG-008` |
| `SEM3015` | Reuse `SemaTypeMismatch` (3015) if it renders `far`; else allocate new (SEM range). Message shape: "`far T` cannot be cast to `T`". Fix: none. | `FAR-OPS-NEG-007` |

Every negative golden fixture must assert the exact allocated code, not only the
message text.

## Positive Golden Fixture Inventory

Fixtures live with the Epic 11 crossing language goldens under
`testdata/golden/crossing/block01/valid/` and are visible to `make
golden-check` once Block 1 lands. Each file name must map back to one or more
matrix rows.

| Fixture | Rows | Required contents |
| --- | --- | --- |
| `far_positive_alias.sg` | `FAR-PARSE-009`, `FAR-ID-001` | `type RemoteConn = far TcpConn;` and a binding using the alias. |
| `far_positive_param.sg` | `FAR-LEX-002`, `FAR-PARSE-001` | Function parameter `conn: far TcpConn`. |
| `far_positive_return_task.sg` | `FAR-LEX-003`, `FAR-PARSE-006` | Return type `far Task<int>`. |
| `far_positive_field.sg` | `FAR-PARSE-010` | Struct field typed as `far TcpConn`. |
| `far_positive_generic_arg.sg` | `FAR-PARSE-007`, `FAR-PREC-003` | `Channel<far Task<int>>` or equivalent nested generic. |
| `far_positive_remote_channel.sg` | `FAR-PARSE-005`, `FAR-PREC-005` | `far Channel<Message>` distinct from `Channel<far Message>`. |
| `far_positive_own_handle.sg` | `FAR-PARSE-002`, `FAR-OWN-001` | `own far TcpConn` parameter and move of local handle. |
| `far_positive_borrow_handle.sg` | `FAR-PARSE-003`, `FAR-OWN-003`, `FAR-OWN-007` | `&far TcpConn` borrow plus `@drop`. |
| `far_positive_mut_borrow_handle.sg` | `FAR-PARSE-004`, `FAR-OWN-004` | `&mut far TcpConn` borrow of a mutable local handle binding. |
| `far_positive_is_type_operand.sg` | `FAR-ID-010` | `x is far TcpConn` proves `far` participates in type identity operands. |

## Negative Golden Fixture Inventory

| Fixture | Rows | Required diagnostic |
| --- | --- | --- |
| `far_negative_reserved_binding.sg` | `FAR-LEX-NEG-001` | `SYN2031` |
| `far_negative_reserved_function.sg` | `FAR-LEX-NEG-002` | `SYN2031` |
| `far_negative_reserved_type.sg` | `FAR-LEX-NEG-003` | `SYN2031` |
| `far_negative_reserved_extern_target.sg` | `FAR-LEX-NEG-004` | `SYN2031` |
| `far_negative_nested.sg` | `FAR-PARSE-NEG-001` | `SEM3136` |
| `far_negative_remote_own.sg` | `FAR-PARSE-NEG-002`, `FAR-OWN-NEG-001` | `SEM3137` |
| `far_negative_remote_borrow.sg` | `FAR-PARSE-NEG-003`, `FAR-OWN-NEG-002` | `SEM3138` |
| `far_negative_remote_mut_borrow.sg` | `FAR-PARSE-NEG-004`, `FAR-OWN-NEG-003` | `SEM3138` |
| `far_negative_remote_raw_pointer.sg` | `FAR-PARSE-NEG-005`, `FAR-PTR-NEG-001` | `SEM3129` |
| `far_negative_raw_pointer_handle.sg` | `FAR-PARSE-NEG-006`, `FAR-PTR-NEG-002` | `SEM3129` |
| `far_negative_function_handle.sg` | `FAR-PARSE-NEG-007` | `FUT7011` |
| `far_negative_extern_target.sg` | `FAR-PARSE-NEG-008` | `SEM3139` |
| `far_negative_item_modifier_fn.sg` | `FAR-PARSE-NEG-009` | `SYN2015` |
| `far_negative_item_modifier_type.sg` | `FAR-PARSE-NEG-010` | `SYN2015` |
| `far_negative_grouping_unsupported.sg` | `FAR-PREC-NEG-001` | `SEM3140` |
| `far_negative_local_array_postponed.sg` | `FAR-PREC-NEG-002` | `FUT7010` |
| `far_negative_array_remote.sg` | `FAR-PREC-001`, `FAR-CAP-NEG-008` | `FUT7009` |
| `far_negative_fixed_array_remote.sg` | `FAR-PREC-002` | `FUT7009` |
| `far_negative_assign_handle_to_resource.sg` | `FAR-ID-002`, `FAR-ID-005` | `SEM3015` |
| `far_negative_assign_resource_to_handle.sg` | `FAR-ID-003` | `SEM3015` |
| `far_negative_channel_identity.sg` | `FAR-ID-007` | `SEM3015` |
| `far_negative_task_identity.sg` | `FAR-ID-008` | `SEM3015` |
| `far_negative_nothing.sg` | `FAR-CAP-NEG-001` | `SEM3141` |
| `far_negative_unit.sg` | `FAR-CAP-NEG-002` | `SEM3141` |
| `far_negative_error.sg` | `FAR-CAP-NEG-003` | `SEM3141` |
| `far_negative_primitive.sg` | `FAR-CAP-NEG-004` | `SEM3141` |
| `far_negative_string.sg` | `FAR-CAP-NEG-005` | `SEM3141` |
| `far_negative_nosend_bypass.sg` | `FAR-CAP-NEG-006` | `SEM3141` |
| `far_negative_plain_struct.sg` | `FAR-CAP-NEG-007` | `SEM3141` |
| `far_negative_use_after_handle_move.sg` | `FAR-OWN-002` | `SEM3130` (reuse `SemaUseAfterMove` 3130) |
| `far_negative_copy_handle.sg` | `FAR-OWN-008` | `SEM3130` (reuse `SemaUseAfterMove` 3130) |
| `far_negative_handle_borrow_conflict.sg` | `FAR-OWN-005`, `FAR-OWN-006` | `SEM3018` (reuse `SemaBorrowConflict` 3018) |
| `far_negative_local_method_call.sg` | `FAR-OPS-NEG-001` | `SEM3142` |
| `far_negative_local_channel_send.sg` | `FAR-OPS-NEG-003` | `SEM3142` |
| `far_negative_local_field_access.sg` | `FAR-OPS-NEG-006` | `SEM3142` |
| `far_negative_cast_to_resource.sg` | `FAR-OPS-NEG-007` | `SEM3015` |

## Follow-Up Unit And Sema Test Obligations

These are not golden fixture files. They are the required non-lexical coverage
for the implementation slice after the documentary matrix is accepted.

| Obligation | Required proof |
| --- | --- |
| Parser AST shape | `far T`, `own far T`, `&far T`, `&mut far T`, `far T[]`, and `Channel<far T>` produce distinct AST/type-form nodes. |
| Keyword reservation | `far` is tokenized as a reserved keyword and rejected as an identifier with the allocated diagnostic. |
| Type identity | Sema preserves `far` as a type former and does not collapse `far T` to `T`. |
| Alias resolution | `type Remote = far T` resolves to the same canonical far type used directly. |
| Generic identity | `Channel<far T>` and `far Channel<T>` remain distinct after generic instantiation. |
| Capability checks | Non-capability base types reject before lowering. |
| Local handle ownership | Move, shared borrow, mutable borrow, early drop, and borrow conflict behavior apply to the local handle. |
| Raw pointer restrictions | `far *T` and `*far T` fail in user code and do not leak to backend-specific pointer handling. |
| Local operation rejection | Method call, field access, indexing, channel ops, and casts on `far T` outside accepted remote contexts fail in sema. |
| Diagnostic rendering | Every negative row renders the exact code and preserves `far` in type names. |

## Future Lowering No-Op And Type-Preservation Checks

Block 1 does not implement transport, `on`, `spawn on`, remote task lifecycle,
or remote resource operations. Later lowering coverage for Block 1 must be
limited to type-preservation behavior:

| Check | Required proof |
| --- | --- |
| No implicit resource move | Lowering a valid `far T` binding never lowers as a move of `T`. |
| Handle-only move | Lowering `own far T` moves the local handle representation only. |
| Handle borrow | Lowering `&far T` and `&mut far T` borrows the local handle representation only. |
| Type-preserved generic lowering | `Channel<far T>` and `far Channel<T>` remain distinct in lowered type metadata. |
| Rejection before backend | Invalid local operations on `far T` fail before VM/LLVM/backend code generation. |

## Cross-Block Integration Obligations

These entries are recorded only to keep Block 1 compatible with later blocks.
They do not expand Block 1 implementation scope.

| Later block | Integration obligation |
| --- | --- |
| Block 2: `on dst { ... }` | Accept `on handle { ... }` as the context where selected operations on `far T` become legal. |
| Block 3: `spawn on dst { ... }` | Produce and operate on `far Task<T>` for remote task lifecycle rows. |
| Block 4: crossing contracts | Define the custom type markers that make user-defined `T` remote-handle-capable. |
