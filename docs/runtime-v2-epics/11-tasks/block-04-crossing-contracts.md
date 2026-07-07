# Block 04: Crossing Contracts Test Matrix

This document is the documentary test-case matrix for Epic 11 Block 4 only. It
defines the golden fixture inventory for `crosses`, `@shard_movable`,
`@shard_pinned`, and their interaction with `@send`, `@copy`, `@nosend`,
`@local spawn`, `spawn`, `on`, and `spawn on`.

It is not an implementation plan. Parser, semantic-analysis, lowering/backend,
and documentation work must use this matrix as the fixture source before code
changes begin.

## Contract Summary

- `crosses` is a function-level effect keyword.
- The only accepted `crosses` placement is after the parameter list and before
  the return type.
- A function containing `on`, containing `spawn on`, calling a `crosses`
  function, or using remote `far Task<T>` operations must be marked `crosses`.
- `crosses` does not change the return type.
- `crosses` does not imply `async`.
- `async` does not imply `crosses`.
- `@shard_movable` and `@shard_pinned` target type declarations only.
- `@shard_movable` and `@shard_pinned` conflict on the same type.
- `@send` alone does not imply shard movement for user-defined types.
- `@copy` alone does not imply shard movement for user-defined types.
- `@nosend` values may be captured by `@local spawn` and must not cross through
  `on`, `spawn on`, or ordinary `spawn`.
- `@shard_pinned` values must not cross as `own T`; they may be represented by
  `far T` only when a safe handle exists.
- Captures into `on` and `spawn on` bodies fall into exactly three accepted
  categories: (a) `Copy` values (including `Placement`), (b) `own @shard_movable`
  values, and (c) `far T` handles captured by move (affine). Borrowed, `@nosend`,
  owned `@shard_pinned`, and unmarked owned user-defined captures are rejected.
- `far T` handles (including `far Task<T>`) are affine (move-only) in Epic 11;
  copyable `far` handles are postponed.
- An `own @shard_movable` value moved into an `on` or `spawn on` body is dropped
  on the destination shard if it is not returned from the body.

## Diagnostic Codes

Exact diagnostic codes are allocated. Block 4 is the single owner of the
crossing-effect (`crosses`) family and the crossing-capture family; Blocks 2 and
3 reference these codes rather than allocating their own (see the
shared-diagnostics ownership table in `11-tasks/README.md`). Allocation follows
the reuse-first policy: reuse an existing diagnostic where one already expresses
the invariant, allocate new in the `SEM` range for genuinely new invariants, and
route postponed surfaces to the `FUT` (7xxx) range.

| Code | Invariant |
| --- | --- |
| `SEM3162` | A function performs crossing work but is not marked `crosses`. |
| `SEM3163` | A non-`crosses` function calls a `crosses` function. |
| `SYN2034` | `crosses` appears anywhere except after the parameter list and before the return type. |
| `SEM3173` | `@crosses` is used as an attribute. |
| `SYN2035` | `crosses` is used on a non-function target. |
| `SYN2036` | Function-type syntax for `crosses fn(...) -> T` is used. |
| `SYN2016` | `@shard_movable` or `@shard_pinned` is used on a non-type target. |
| `SEM3172` | `@shard_movable` and `@shard_pinned` are both present on one type. |
| `SEM3171` | A `@shard_movable` type contains a non-shard-movable field or member. |
| `SEM3169` | A user-defined `@send` type crosses as `own T` without `@shard_movable`. |
| `SEM3170` | A user-defined `@copy` type crosses as `own T` without `@shard_movable`. |
| `SEM3165` | A borrowed value is captured into a crossing boundary. |
| `SEM3166` | A `@nosend` value crosses a task or shard boundary outside `@local spawn`. |
| `SEM3167` | A `@shard_pinned` value crosses as `own T`. |
| `SEM3168` | An unmarked owned user-defined value (not `@shard_movable`) is captured/moved into a crossing boundary; also covers capturing a local `Task<T>` into `spawn on`. |
| `SEM3174` | `@local spawn on` is used. |
| `SEM3164` | `far Task<T>.await()` or `far Task<T>.cancel()` is used outside a `crosses` function. |

## `crosses` Grammar Placement Matrix

| Row | Fixture | Form | Status | Diagnostic |
| --- | --- | --- | --- | --- |
| CROSSES-GRAMMAR-001 | `crosses_positive_fn_on.sg` | `fn route(req: own Request) crosses -> TaskResult<Response> { ... }` | Valid | none |
| CROSSES-GRAMMAR-002 | `crosses_positive_fn_no_return_type.sg` | `fn notify(ch: far Channel<Event>) crosses { ... }` | Valid | none |
| CROSSES-GRAMMAR-003 | `crosses_positive_async_fn_on.sg` | `async fn route(req: own Request) crosses -> Response { ... }` | Valid | none |
| CROSSES-GRAMMAR-004 | `crosses_positive_fn_decl.sg` | `fn remote_ready(ch: far Channel<Event>) crosses -> TaskResult<bool>;` | Valid | none |
| CROSSES-GRAMMAR-005 | `crosses_positive_return_far_task.sg` | `fn start(job: own Job) crosses -> far Task<Result> { ... }` | Valid | none |
| CROSSES-GRAMMAR-006 | `crosses_negative_attribute.sg` | `@crosses fn route(req: Request) -> Response { ... }` | Invalid | `SEM3173` |
| CROSSES-GRAMMAR-007 | `crosses_negative_prefix_fn.sg` | `crosses fn route(req: Request) -> Response { ... }` | Invalid | `SYN2034` |
| CROSSES-GRAMMAR-008 | `crosses_negative_after_return_type.sg` | `fn route(req: Request) -> Response crosses { ... }` | Invalid | `SYN2034` |
| CROSSES-GRAMMAR-009 | `crosses_negative_before_params.sg` | `fn route crosses(req: Request) -> Response { ... }` | Invalid | `SYN2034` |
| CROSSES-GRAMMAR-010 | `crosses_negative_duplicate.sg` | `fn route(req: Request) crosses crosses -> Response { ... }` | Invalid | `SYN2034` |
| CROSSES-GRAMMAR-011 | `crosses_negative_type_target.sg` | `type crosses Data = { id: uint64 };` | Invalid | `SYN2035` |
| CROSSES-GRAMMAR-012 | `crosses_negative_field_target.sg` | `type Data = { crosses id: uint64 };` | Invalid | `SYN2035` |
| CROSSES-GRAMMAR-013 | `crosses_negative_let_target.sg` | `let crosses data = make_data();` | Invalid | `SYN2035` |
| CROSSES-GRAMMAR-014 | `crosses_negative_block_target.sg` | `crosses { ret value; }` | Invalid | `SYN2035` |
| CROSSES-GRAMMAR-015 | `crosses_negative_fn_type.sg` | `let cb: crosses fn(Request) -> Response = route;` | Invalid | `SYN2036` |
| CROSSES-GRAMMAR-016 | `crosses_positive_let_identifier.sg` | `let crosses: int = 1;` (contextual keyword: `crosses` stays a usable identifier outside signature position) | Valid | none |

## `crosses` Effect Propagation Matrix

| Row | Fixture | Case | Status | Diagnostic |
| --- | --- | --- | --- | --- |
| CROSSES-EFFECT-001 | `crosses_positive_fn_on.sg` | `on dst { ... }` inside `fn ... crosses` | Valid | none |
| CROSSES-EFFECT-002 | `crosses_positive_spawn_on_in_crosses_fn.sg` | `spawn on dst { ... }` inside `fn ... crosses` | Valid | none |
| CROSSES-EFFECT-003 | `crosses_positive_call_crosses_from_crosses.sg` | A `crosses` function calls another `crosses` function. | Valid | none |
| CROSSES-EFFECT-004 | `crosses_positive_async_fn_on.sg` | `async fn ... crosses` contains `on`. | Valid | none |
| CROSSES-EFFECT-005 | `crosses_positive_non_async_crosses_on.sg` | Non-async `fn ... crosses` contains `on`. | Valid | none |
| CROSSES-EFFECT-006 | `crosses_positive_async_without_crosses_no_remote.sg` | `async fn` without crossing work omits `crosses`. | Valid | none |
| CROSSES-EFFECT-007 | `crosses_negative_on_missing_crosses.sg` | `on dst { ... }` appears in a non-`crosses` function. | Invalid | `SEM3162` |
| CROSSES-EFFECT-008 | `crosses_negative_spawn_on_missing_crosses.sg` | `spawn on dst { ... }` appears in a non-`crosses` function. | Invalid | `SEM3162` |
| CROSSES-EFFECT-009 | `crosses_negative_call_missing_crosses.sg` | A non-`crosses` function calls a `crosses` function. | Invalid | `SEM3163` |
| CROSSES-EFFECT-010 | `crosses_negative_async_on_missing_crosses.sg` | `async fn` contains `on` but omits `crosses`. | Invalid | `SEM3162` |
| CROSSES-EFFECT-011 | `crosses_negative_far_task_await_missing_crosses.sg` | `far Task<T>.await()` appears in a non-`crosses` function. | Invalid | `SEM3164` |
| CROSSES-EFFECT-012 | `crosses_negative_far_task_cancel_missing_crosses.sg` | `far Task<T>.cancel()` appears in a non-`crosses` function. | Invalid | `SEM3164` |

## Capture Legality Matrix

| Row | Fixture | Boundary | Captured value | Status | Diagnostic |
| --- | --- | --- | --- | --- | --- |
| CAPTURE-001 | `capture_positive_copy_on.sg` | `on dst` | Built-in `Copy` value | Valid | none |
| CAPTURE-002 | `capture_positive_copy_spawn_on.sg` | `spawn on dst` | Built-in `Copy` value | Valid | none |
| CAPTURE-003 | `capture_positive_shard_movable_on.sg` | `on dst` | `own @shard_movable` value | Valid | none |
| CAPTURE-004 | `capture_positive_shard_movable_spawn_on.sg` | `spawn on dst` | `own @shard_movable` value | Valid | none |
| CAPTURE-005 | `capture_positive_far_handle_on.sg` | `on conn` | `far TcpConn` handle where `TcpConn` is `@shard_pinned` | Valid | none |
| CAPTURE-006 | `capture_positive_nosend_local_spawn.sg` | `@local spawn expr` | `@nosend` value | Valid | none |
| CAPTURE-015 | `capture_positive_shard_movable_dropped_on_dest.sg` | `on dst` / `spawn on dst` | `own @shard_movable` value not returned from the body | Valid; the value is dropped on the destination shard | none |
| CAPTURE-007 | `capture_negative_borrow_on.sg` | `on dst` | `&T` or `&mut T` capture | Invalid | `SEM3165` |
| CAPTURE-008 | `capture_negative_borrow_spawn_on.sg` | `spawn on dst` | `&T` or `&mut T` capture | Invalid | `SEM3165` |
| CAPTURE-009 | `capture_negative_nosend_on.sg` | `on dst` | `own @nosend` value | Invalid | `SEM3166` |
| CAPTURE-010 | `capture_negative_nosend_spawn.sg` | `spawn expr` | `own @nosend` value | Invalid | `SEM3166` |
| CAPTURE-011 | `capture_negative_nosend_spawn_on.sg` | `spawn on dst` | `own @nosend` value | Invalid | `SEM3166` |
| CAPTURE-012 | `capture_negative_pinned_on.sg` | `on dst` | `own @shard_pinned` value | Invalid | `SEM3167` |
| CAPTURE-013 | `capture_negative_pinned_spawn_on.sg` | `spawn on dst` | `own @shard_pinned` value | Invalid | `SEM3167` |
| CAPTURE-016 | `capture_negative_unmarked_owned_on.sg` | `on dst` | `own UserType` that is not `@shard_movable` | Invalid | `SEM3168` |
| CAPTURE-017 | `capture_negative_unmarked_owned_spawn_on.sg` | `spawn on dst` | `own UserType` that is not `@shard_movable` (e.g. a local `Task<T>`) | Invalid | `SEM3168` |
| CAPTURE-014 | `capture_negative_local_spawn_on.sg` | `@local spawn on dst` | Any capture | Invalid | `SEM3174` |

Drop-site contract (CAPTURE-015): when an `own @shard_movable` value is moved
into an `on` or `spawn on` body and not returned from that body, its owner
becomes the destination shard and it is dropped there. Epic 11 records this as a
compile-only contract (the positive fixture proves it type-checks); the runtime
drop location is realized by the Phase 4 transport epic.

## Attribute Target And Conflict Matrix

| Row | Fixture | Attribute form | Status | Diagnostic |
| --- | --- | --- | --- | --- |
| ATTR-001 | `attr_positive_shard_movable_type.sg` | `@shard_movable type Job = { ... };` | Valid | none |
| ATTR-002 | `attr_positive_shard_pinned_type.sg` | `@shard_pinned type TcpConn = { ... };` | Valid | none |
| ATTR-003 | `attr_positive_shard_pinned_nosend_type.sg` | `@shard_pinned @nosend type TcpConn = { ... };` | Valid | none |
| ATTR-004 | `attr_negative_movable_fn_target.sg` | `@shard_movable fn f() { ... }` | Invalid | `SYN2016` |
| ATTR-005 | `attr_negative_movable_field_target.sg` | `type Job = { @shard_movable id: uint64 };` | Invalid | `SYN2016` |
| ATTR-006 | `attr_negative_movable_param_target.sg` | `fn f(@shard_movable job: Job) { ... }` | Invalid | `SYN2016` |
| ATTR-007 | `attr_negative_movable_block_target.sg` | `@shard_movable { ret value; }` | Invalid | `SYN2016` |
| ATTR-008 | `attr_negative_pinned_fn_target.sg` | `@shard_pinned fn f() { ... }` | Invalid | `SYN2016` |
| ATTR-009 | `attr_negative_pinned_field_target.sg` | `type Conn = { @shard_pinned fd: int };` | Invalid | `SYN2016` |
| ATTR-010 | `attr_negative_pinned_param_target.sg` | `fn f(@shard_pinned conn: Conn) { ... }` | Invalid | `SYN2016` |
| ATTR-011 | `attr_negative_pinned_block_target.sg` | `@shard_pinned { ret value; }` | Invalid | `SYN2016` |
| ATTR-012 | `attr_negative_movable_pinned_conflict.sg` | `@shard_movable @shard_pinned type Resource = { ... };` | Invalid | `SEM3172` |

## Recursive Movability Validation Matrix

| Row | Fixture | Type shape | Status | Diagnostic |
| --- | --- | --- | --- | --- |
| MOVABLE-001 | `movable_positive_primitives.sg` | `@shard_movable` type with primitive fields only | Valid | none |
| MOVABLE-002 | `movable_positive_nested_movable.sg` | `@shard_movable` type containing another `@shard_movable` type | Valid | none |
| MOVABLE-003 | `movable_positive_array_of_movable.sg` | `@shard_movable` type containing `Job[]` where `Job` is `@shard_movable` | Valid | none |
| MOVABLE-004 | `movable_positive_far_handle_field_explicit.sg` | `@shard_movable` type containing `far TcpConn` with valid handle-lifetime rules | Valid | none |
| MOVABLE-005 | `movable_negative_unmarked_user_field.sg` | `@shard_movable` type containing an unmarked user-defined field | Invalid | `SEM3171` |
| MOVABLE-006 | `movable_negative_nested_unmarked_user_field.sg` | Recursive field validation reaches an unmarked user-defined type | Invalid | `SEM3171` |
| MOVABLE-007 | `movable_negative_array_of_unmarked_user_type.sg` | `@shard_movable` type containing `Payload[]` where `Payload` is unmarked | Invalid | `SEM3171` |
| MOVABLE-008 | `movable_negative_pinned_field.sg` | `@shard_movable` type containing an owned `@shard_pinned` field | Invalid | `SEM3171` |
| MOVABLE-009 | `movable_negative_nosend_field.sg` | `@shard_movable` type containing an owned `@nosend` field | Invalid | `SEM3171` |
| MOVABLE-010 | `movable_negative_far_handle_field_unmarked_owner.sg` | Unmarked user-defined type containing `far T` crosses as `own T` | Invalid | `SEM3171` |

## `@send`, `@copy`, `@nosend`, And Locality Matrix

| Row | Fixture | Case | Status | Diagnostic |
| --- | --- | --- | --- | --- |
| LOCALITY-001 | `locality_positive_send_and_movable_on.sg` | User type marked both `@send` and `@shard_movable` crosses via `on` as `own T`. | Valid | none |
| LOCALITY-002 | `locality_positive_copy_and_movable_on.sg` | User type marked both `@copy` and `@shard_movable` crosses via `on` as `own T`. | Valid | none |
| LOCALITY-003 | `locality_positive_nosend_local_spawn.sg` | `@nosend` value captured by `@local spawn expr`. | Valid | none |
| LOCALITY-004 | `locality_positive_movable_regular_spawn.sg` | `@shard_movable` value captured by ordinary `spawn expr` when it is also task-sendable. | Valid | none |
| LOCALITY-005 | `locality_negative_send_only_on.sg` | User type marked `@send` but not `@shard_movable` crosses via `on` as `own T`. | Invalid | `SEM3169` |
| LOCALITY-006 | `locality_negative_send_only_spawn_on.sg` | User type marked `@send` but not `@shard_movable` crosses via `spawn on` as `own T`. | Invalid | `SEM3169` |
| LOCALITY-007 | `locality_negative_copy_only_on.sg` | User type marked `@copy` but not `@shard_movable` crosses via `on` as `own T`. | Invalid | `SEM3170` |
| LOCALITY-008 | `locality_negative_copy_only_spawn_on.sg` | User type marked `@copy` but not `@shard_movable` crosses via `spawn on` as `own T`. | Invalid | `SEM3170` |
| LOCALITY-009 | `locality_negative_nosend_spawn.sg` | `@nosend` value captured by ordinary `spawn expr`. | Invalid | `SEM3166` |
| LOCALITY-010 | `locality_negative_nosend_on.sg` | `@nosend` value crosses via `on`. | Invalid | `SEM3166` |
| LOCALITY-011 | `locality_negative_nosend_spawn_on.sg` | `@nosend` value crosses via `spawn on`. | Invalid | `SEM3166` |
| LOCALITY-012 | `locality_negative_local_spawn_on.sg` | `@local spawn on dst { ... }` combines local and remote placement. | Invalid | `SEM3174` |

## `@shard_pinned` And Far Handle Matrix

| Row | Fixture | Case | Status | Diagnostic |
| --- | --- | --- | --- | --- |
| PINNED-001 | `pinned_positive_far_handle_param.sg` | Function accepts `far TcpConn` where `TcpConn` is `@shard_pinned`. | Valid | none |
| PINNED-002 | `pinned_positive_on_far_handle.sg` | `on conn { ... }` anchors work to `conn: far TcpConn`. | Valid | none |
| PINNED-003 | `pinned_positive_pinned_nosend_resource.sg` | Resource type uses both `@shard_pinned` and `@nosend`. | Valid | none |
| PINNED-004 | `pinned_negative_owned_on_capture.sg` | `own TcpConn` captured into `on dst` where `TcpConn` is `@shard_pinned`. | Invalid | `SEM3167` |
| PINNED-005 | `pinned_negative_owned_spawn_on_capture.sg` | `own TcpConn` captured into `spawn on dst` where `TcpConn` is `@shard_pinned`. | Invalid | `SEM3167` |
| PINNED-006 | `pinned_negative_owned_regular_spawn_nosend.sg` | `@shard_pinned @nosend` resource captured into ordinary `spawn expr`. | Invalid | `SEM3166` |
| PINNED-007 | `pinned_negative_movable_conflict.sg` | `@shard_pinned @shard_movable` appears on one type. | Invalid | `SEM3172` |

## Positive Golden Fixture Inventory

| Fixture | Rows covered |
| --- | --- |
| `crosses_positive_fn_on.sg` | CROSSES-GRAMMAR-001, CROSSES-EFFECT-001 |
| `crosses_positive_fn_no_return_type.sg` | CROSSES-GRAMMAR-002 |
| `crosses_positive_async_fn_on.sg` | CROSSES-GRAMMAR-003, CROSSES-EFFECT-004 |
| `crosses_positive_fn_decl.sg` | CROSSES-GRAMMAR-004 |
| `crosses_positive_return_far_task.sg` | CROSSES-GRAMMAR-005 |
| `crosses_positive_spawn_on_in_crosses_fn.sg` | CROSSES-EFFECT-002 |
| `crosses_positive_call_crosses_from_crosses.sg` | CROSSES-EFFECT-003 |
| `crosses_positive_non_async_crosses_on.sg` | CROSSES-EFFECT-005 |
| `crosses_positive_async_without_crosses_no_remote.sg` | CROSSES-EFFECT-006 |
| `crosses_positive_let_identifier.sg` | CROSSES-GRAMMAR-016 |
| `capture_positive_copy_on.sg` | CAPTURE-001 |
| `capture_positive_copy_spawn_on.sg` | CAPTURE-002 |
| `capture_positive_shard_movable_on.sg` | CAPTURE-003 |
| `capture_positive_shard_movable_spawn_on.sg` | CAPTURE-004 |
| `capture_positive_far_handle_on.sg` | CAPTURE-005 |
| `capture_positive_nosend_local_spawn.sg` | CAPTURE-006 |
| `capture_positive_shard_movable_dropped_on_dest.sg` | CAPTURE-015 |
| `attr_positive_shard_movable_type.sg` | ATTR-001 |
| `attr_positive_shard_pinned_type.sg` | ATTR-002 |
| `attr_positive_shard_pinned_nosend_type.sg` | ATTR-003 |
| `movable_positive_primitives.sg` | MOVABLE-001 |
| `movable_positive_nested_movable.sg` | MOVABLE-002 |
| `movable_positive_array_of_movable.sg` | MOVABLE-003 |
| `movable_positive_far_handle_field_explicit.sg` | MOVABLE-004 |
| `locality_positive_send_and_movable_on.sg` | LOCALITY-001 |
| `locality_positive_copy_and_movable_on.sg` | LOCALITY-002 |
| `locality_positive_nosend_local_spawn.sg` | LOCALITY-003 |
| `locality_positive_movable_regular_spawn.sg` | LOCALITY-004 |
| `pinned_positive_far_handle_param.sg` | PINNED-001 |
| `pinned_positive_on_far_handle.sg` | PINNED-002 |
| `pinned_positive_pinned_nosend_resource.sg` | PINNED-003 |

## Negative Golden Fixture Inventory

| Fixture | Rows covered | Diagnostic |
| --- | --- | --- |
| `crosses_negative_attribute.sg` | CROSSES-GRAMMAR-006 | `SEM3173` |
| `crosses_negative_prefix_fn.sg` | CROSSES-GRAMMAR-007 | `SYN2034` |
| `crosses_negative_after_return_type.sg` | CROSSES-GRAMMAR-008 | `SYN2034` |
| `crosses_negative_before_params.sg` | CROSSES-GRAMMAR-009 | `SYN2034` |
| `crosses_negative_duplicate.sg` | CROSSES-GRAMMAR-010 | `SYN2034` |
| `crosses_negative_type_target.sg` | CROSSES-GRAMMAR-011 | `SYN2035` |
| `crosses_negative_field_target.sg` | CROSSES-GRAMMAR-012 | `SYN2035` |
| `crosses_negative_let_target.sg` | CROSSES-GRAMMAR-013 | `SYN2035` |
| `crosses_negative_block_target.sg` | CROSSES-GRAMMAR-014 | `SYN2035` |
| `crosses_negative_fn_type.sg` | CROSSES-GRAMMAR-015 | `SYN2036` |
| `crosses_negative_on_missing_crosses.sg` | CROSSES-EFFECT-007 | `SEM3162` |
| `crosses_negative_spawn_on_missing_crosses.sg` | CROSSES-EFFECT-008 | `SEM3162` |
| `crosses_negative_call_missing_crosses.sg` | CROSSES-EFFECT-009 | `SEM3163` |
| `crosses_negative_async_on_missing_crosses.sg` | CROSSES-EFFECT-010 | `SEM3162` |
| `crosses_negative_far_task_await_missing_crosses.sg` | CROSSES-EFFECT-011 | `SEM3164` |
| `crosses_negative_far_task_cancel_missing_crosses.sg` | CROSSES-EFFECT-012 | `SEM3164` |
| `capture_negative_borrow_on.sg` | CAPTURE-007 | `SEM3165` |
| `capture_negative_borrow_spawn_on.sg` | CAPTURE-008 | `SEM3165` |
| `capture_negative_nosend_on.sg` | CAPTURE-009 | `SEM3166` |
| `capture_negative_nosend_spawn.sg` | CAPTURE-010 | `SEM3166` |
| `capture_negative_nosend_spawn_on.sg` | CAPTURE-011 | `SEM3166` |
| `capture_negative_pinned_on.sg` | CAPTURE-012 | `SEM3167` |
| `capture_negative_pinned_spawn_on.sg` | CAPTURE-013 | `SEM3167` |
| `capture_negative_unmarked_owned_on.sg` | CAPTURE-016 | `SEM3168` |
| `capture_negative_unmarked_owned_spawn_on.sg` | CAPTURE-017 | `SEM3168` |
| `capture_negative_local_spawn_on.sg` | CAPTURE-014 | `SEM3174` |
| `attr_negative_movable_fn_target.sg` | ATTR-004 | `SYN2016` |
| `attr_negative_movable_field_target.sg` | ATTR-005 | `SYN2016` |
| `attr_negative_movable_param_target.sg` | ATTR-006 | `SYN2016` |
| `attr_negative_movable_block_target.sg` | ATTR-007 | `SYN2016` |
| `attr_negative_pinned_fn_target.sg` | ATTR-008 | `SYN2016` |
| `attr_negative_pinned_field_target.sg` | ATTR-009 | `SYN2016` |
| `attr_negative_pinned_param_target.sg` | ATTR-010 | `SYN2016` |
| `attr_negative_pinned_block_target.sg` | ATTR-011 | `SYN2016` |
| `attr_negative_movable_pinned_conflict.sg` | ATTR-012 | `SEM3172` |
| `movable_negative_unmarked_user_field.sg` | MOVABLE-005 | `SEM3171` |
| `movable_negative_nested_unmarked_user_field.sg` | MOVABLE-006 | `SEM3171` |
| `movable_negative_array_of_unmarked_user_type.sg` | MOVABLE-007 | `SEM3171` |
| `movable_negative_pinned_field.sg` | MOVABLE-008 | `SEM3171` |
| `movable_negative_nosend_field.sg` | MOVABLE-009 | `SEM3171` |
| `movable_negative_far_handle_field_unmarked_owner.sg` | MOVABLE-010 | `SEM3171` |
| `locality_negative_send_only_on.sg` | LOCALITY-005 | `SEM3169` |
| `locality_negative_send_only_spawn_on.sg` | LOCALITY-006 | `SEM3169` |
| `locality_negative_copy_only_on.sg` | LOCALITY-007 | `SEM3170` |
| `locality_negative_copy_only_spawn_on.sg` | LOCALITY-008 | `SEM3170` |
| `locality_negative_nosend_spawn.sg` | LOCALITY-009 | `SEM3166` |
| `locality_negative_nosend_on.sg` | LOCALITY-010 | `SEM3166` |
| `locality_negative_nosend_spawn_on.sg` | LOCALITY-011 | `SEM3166` |
| `locality_negative_local_spawn_on.sg` | LOCALITY-012 | `SEM3174` |
| `pinned_negative_owned_on_capture.sg` | PINNED-004 | `SEM3167` |
| `pinned_negative_owned_spawn_on_capture.sg` | PINNED-005 | `SEM3167` |
| `pinned_negative_owned_regular_spawn_nosend.sg` | PINNED-006 | `SEM3166` |
| `pinned_negative_movable_conflict.sg` | PINNED-007 | `SEM3172` |

## Implementation Items (Prelude And Stdlib)

These are the source-level changes Block 4 requires. They are recorded here as
tasks; this document does not perform them. stdlib API changes are permitted
within the Runtime V2 refactoring (user approval 2026-07-07).

- Add `@shard_pinned` to the prelude `TcpConn` type in `core/intrinsics.sg`
  (lines 82-85). It already carries `@intrinsic @nosend`, so the result is
  `@intrinsic @nosend @shard_pinned`. Apply the same reasoning to the sibling
  `TcpListener` type (also `@intrinsic @nosend`).
- Register `@shard_movable` and `@shard_pinned` in the closed attribute registry
  `internal/ast/attr_catalog.go`, targeting type declarations only
  (`AttrTargetType`); field, parameter, and block placements reject at parse time
  via `SynAttributeNotAllowed` (2016), which backs the `ATTR-*` target rows.
- Add the shard-attribute semantic rules in
  `internal/sema/attr_validation_types.go`: mutual exclusion of `@shard_movable`
  and `@shard_pinned`, recursive `@shard_movable` field/member validation, and
  the `@send`/`@copy` insufficiency checks.
- Declare the `Placement` intrinsic type and the `pool` / `distributed` /
  `shard(id)` / `shard_count()` prelude surface in `core/intrinsics.sg`; the
  VM/backend intrinsic dispatch must recognize the names (runtime placement
  scaffolding already exists on the runtime side).
- stdlib consumers to verify and, if needed, adjust: `stdlib/net/net.sg` and
  `stdlib/http/server.sg` use `TcpConn`; confirm they still compile once
  `TcpConn` is `@shard_pinned`, and adjust the net/http API as required.

## Required Future Test Obligations

These obligations are part of the Block 4 fixture contract and must be covered
after the golden files above are introduced.

### Parser And AST

- Parse `crosses` only after the parameter list and before the return type on
  `fn`, `async fn`, and function declarations.
- Preserve a function-level `crosses` effect bit in the AST.
- Reject `@crosses` through the invalid-attribute fixture path.
- Reject prefix `crosses fn`, `crosses` after return type, duplicate
  `crosses`, non-function `crosses` targets, and `crosses fn(...) -> T`
  function-type syntax through the negative fixture set.
- Parse `@shard_movable` and `@shard_pinned` as closed-set type attributes
  only.
- Reject `@local spawn on` as a distinct parsed surface with
  `SEM3174`.

### Semantic Analysis

- Enforce `crosses` propagation for `on`, `spawn on`, calls to `crosses`
  functions, and remote `far Task<T>.await()` / `cancel()`.
- Prove that `async` and `crosses` are independent effects.
- Enforce crossing capture legality for `Copy`, `own @shard_movable`, borrowed
  values, `@nosend`, `@shard_pinned`, and `far T` handles.
- Enforce recursive `@shard_movable` field/member validation.
- Enforce deterministic insufficiency diagnostics for user-defined `@send`
  without `@shard_movable`.
- Enforce deterministic insufficiency diagnostics for user-defined `@copy`
  without `@shard_movable`.
- Enforce `@shard_movable` / `@shard_pinned` conflict detection.
- Preserve existing `@nosend` behavior for `@local spawn` and reject the same
  capture through ordinary `spawn`, `on`, and `spawn on`.

### Lowering And Backend

- Lowering must receive the AST `crosses` effect and must not infer crossing
  from unrelated async/task state.
- Lowering must reject any Block 4 negative fixture before backend-specific
  transport code is reached.
- Lowering tests must include one accepted `on` in a `crosses` function, one
  accepted `spawn on` in a `crosses` function, and one accepted remote
  `far Task<T>` operation in a `crosses` function.
- Backend tests must prove that valid Block 4 contracts do not fall through to
  backend panics when Phase 4 syntax is enabled.
- Backend/configuration tests must keep unsupported Phase 4 configurations on
  the documented diagnostic path instead of parser ambiguity or runtime crash.
