# Block 02: `on` Placement Crossing Test Matrix

This document is the test-case matrix and fixture inventory for Epic 11 Block 2:
`on dst { ... }` placement crossing blocks.

It is documentary only. It does not define parser, semantic-analysis, lowering,
runtime, or public-example implementation work.

## Scope

Block 2 covers immediate placement crossing:

```sg
on dst {
    ret value;
}
```

Contract:

- `on` is a contextual keyword (identifier everywhere except the crossing
  position; see the epic "Keyword Strategy").
- `dst` is a typed destination expression.
- Placement destinations are `Placement` values.
- Accepted far-handle destinations are owner-anchored remote handles explicitly
  allowed by Epic 11.
- `far Task<T>` is not an `on` destination.
- The crossing waits for completion and evaluates to `TaskResult<T>`.
- The body result is produced by `ret expr;`.
- `return` inside an `on` body is invalid.
- The enclosing function or block must satisfy the Block 4 `crosses`
  requirement.

## Contract Dependencies

Block 2 depends on these contracts without expanding their test matrices:

| Dependency | Block | Required surface for Block 2 |
| --- | --- | --- |
| `far T` type modifier | Block 1 | Enables `far Channel<T>` and `far TcpConn` destination typing. |
| `crosses` function effect | Block 4 | Required for functions or blocks that contain `on`. |
| `@shard_movable` | Block 4 | Required for owned user-defined value captures that cross shards. |
| `@shard_pinned` | Block 4 | Required to reject owned shard-pinned captures and allow `far T` handle use. |
| `@send` / `@nosend` | Existing attributes plus Block 4 | Attributes remain constraints; they are not type-use modifiers. |
| `TaskResult<T>` | Existing async contract | `on` wraps the body result as `TaskResult<T>`. |

`spawn on dst { ... }`, `far Task<T>.await()`, and `far Task<T>.cancel()` remain
Block 3 surfaces and are not covered here except where `far Task<T>` is rejected
as an `on` destination.

## Valid Destination Matrix

| Row | Destination | Fixture | Expected result type | Contract |
| --- | --- | --- | --- | --- |
| ON-DST-V001 | `pool` | `on_positive_pool.sg` | `TaskResult<T>` | `pool` is a prelude `Placement` value for CPU-bound Tier 2 placement. |
| ON-DST-V002 | `distributed` | `on_positive_distributed.sg` | `TaskResult<T>` | `distributed` is a prelude `Placement` value selected by the runtime. |
| ON-DST-V003 | `shard(id)` | `on_positive_shard_call.sg` | `TaskResult<T>` | `shard(id)` returns `Placement`; `id` must type-check as `ShardId`. |
| ON-DST-V004 | `dst: Placement` | `on_positive_placement_var.sg` | `TaskResult<T>` | A local variable of type `Placement` is a valid destination. |
| ON-DST-V005 | `route_for(id) -> Placement` | `on_positive_placement_fn.sg` | `TaskResult<T>` | A function call returning `Placement` is a valid destination expression. |
| ON-DST-V006 | `ch: far Channel<T>` | `on_positive_far_channel_anchor.sg` | `TaskResult<nothing>` | The destination is the channel owner shard; operations through `ch` are anchored. |
| ON-DST-V007 | `conn: far TcpConn` | `on_positive_far_tcpconn_close.sg` | `TaskResult<nothing>` | Control-only operations on the remote TCP owner are accepted. |

## Invalid Destination Matrix

Every negative fixture in this table asserts the exact diagnostic code in the
fixture metadata.

| Row | Destination | Fixture | Diagnostic code | Fix availability | Required message shape |
| --- | --- | --- | --- | --- | --- |
| ON-DST-N001 | `t: far Task<T>` | `on_negative_far_task_destination.sg` | `SEM3143` | Fixable: use `t.await()` or `t.cancel()` in a `crosses` context. | `far Task<T>` is not an `on` destination. |
| ON-DST-N002 | `blocking` | `on_negative_blocking_destination.sg` | `FUT7012` | Fixable: use existing `blocking { ... }` or choose a `Placement`. | `on blocking` is not part of Epic 11. |
| ON-DST-N003 | ordinary value | `on_negative_integer_destination.sg` | `SEM3144` | Fixable: pass `Placement`, `shard(id)`, or an accepted `far` handle. | Destination type is not `Placement` or accepted `far` handle. |
| ON-DST-N004 | type name | `on_negative_type_destination.sg` | `SEM3145` | Fixable: use a value of type `Placement`. | Type names are not placement targets. |
| ON-DST-N005 | bare function name | `on_negative_bare_fn_destination.sg` | `SEM3146` | Fixable: call the function if it returns `Placement`. | Function value is not a placement target. |
| ON-DST-N006 | function call returning non-`Placement` | `on_negative_fn_returns_int_destination.sg` | `SEM3144` | Fixable: change the callee to return `Placement` or use a valid destination. | Destination expression type is invalid. |
| ON-DST-N007 | `shard(non_shard_id)` | `on_negative_shard_id_type.sg` | `SEM3015` | Fixable: convert to `ShardId` before calling `shard`. | `shard` argument must type-check as `ShardId`. |

## Body And Result Matrix

| Row | Shape | Fixture | Expected type or diagnostic | Fix availability | Contract |
| --- | --- | --- | --- | --- | --- |
| ON-BODY-V001 | `on pool { ret 1; }` | `on_positive_pool.sg` | `TaskResult<int>` | N/A | `ret expr;` produces the body value and `on` wraps it. |
| ON-BODY-V002 | `on pool { ret nothing; }` | `on_positive_ret_nothing_result.sg` | `TaskResult<nothing>` | N/A | `nothing` is a valid body value. |
| ON-BODY-V003 | assigned to `TaskResult<T>` | `on_positive_taskresult_binding.sg` | `TaskResult<T>` | N/A | Direct assignment proves result wrapping. |
| ON-BODY-V004 | returned from `crosses -> TaskResult<T>` | `on_positive_taskresult_return.sg` | `TaskResult<T>` | N/A | Function return type matches `on` result. |
| ON-BODY-N001 | `return expr;` inside body | `on_negative_return_in_body.sg` | `SEM3147` | Fixable: replace with `ret expr;`. | `return` cannot exit through a crossing block. |
| ON-BODY-N002 | missing `ret` in value context | `on_negative_missing_ret_value.sg` | `SEM3148` | Fixable: add `ret expr;`. | The block must produce the requested `T`. |
| ON-BODY-N003 | body result assigned to `T` | `on_negative_assign_unwrapped_result.sg` | `SEM3149` | Fixable: bind `TaskResult<T>` or handle `Success` / `Cancelled`. | `on` returns `TaskResult<T>`, not `T`. |
| ON-BODY-N004 | body result returned from `-> T` function | `on_negative_return_unwrapped_result.sg` | `SEM3149` | Fixable: change return type to `TaskResult<T>` or unwrap explicitly. | Function return type must account for `TaskResult<T>`. |

## Capture Legality Matrix

| Row | Capture | Fixture | Expected type or diagnostic | Fix availability | Contract |
| --- | --- | --- | --- | --- | --- |
| ON-CAP-V001 | Copy primitive | `on_positive_copy_capture.sg` | `TaskResult<T>` | N/A | Copy values may be captured. |
| ON-CAP-V002 | `own @shard_movable` value | `on_positive_shard_movable_capture.sg` | `TaskResult<T>` | N/A | Owned shard-movable values may cross. |
| ON-CAP-V003 | `far T` handle by move (affine) | `on_positive_far_handle_capture.sg` | `TaskResult<T>` | N/A | The local handle is moved in; the remote resource is not moved. |
| ON-CAP-V004 | `Placement` value (captured or computed in body) | `on_positive_placement_capture.sg` | `TaskResult<T>` | N/A | `Placement` is `Copy` and shard-movable; e.g. calling `shard(id)` inside the body. |
| ON-CAP-N001 | `&T` capture | `on_negative_shared_borrow_capture.sg` | `SEM3165` (Block 4) | Fixable: copy or move an allowed owned value. | Borrowed captures cannot cross. |
| ON-CAP-N002 | `&mut T` capture | `on_negative_mut_borrow_capture.sg` | `SEM3165` (Block 4) | Fixable: move an allowed owned value after ending the borrow. | Mutable borrowed captures cannot cross. |
| ON-CAP-N003 | `@nosend` value capture | `on_negative_nosend_capture.sg` | `SEM3166` (Block 4) | Not safely fixable automatically. | `@nosend` forbids crossing task boundaries. |
| ON-CAP-N004 | owned `@shard_pinned` value capture | `on_negative_shard_pinned_capture.sg` | `SEM3167` (Block 4) | Fixable only by using an accepted `far T` handle. | Shard-pinned resources cannot cross as owned values. |
| ON-CAP-N005 | non-`@shard_movable` user value moved as `own T` | `on_negative_unmarked_owned_capture.sg` | `SEM3168` (Block 4) | Fixable: mark and validate the type as `@shard_movable` or avoid crossing it. | User-defined owned values need shard-movable eligibility. |

## Far-Handle Owner Anchor Matrix

When the destination is a `far T` handle, that handle proves the owner shard for
operations through that same handle only.

| Row | Shape | Fixture | Expected type or diagnostic | Fix availability | Contract |
| --- | --- | --- | --- | --- | --- |
| ON-ANCHOR-V001 | `on ch { ch.send(own msg); ret nothing; }` | `on_positive_far_channel_anchor.sg` | `TaskResult<nothing>` | N/A | `ch` is the destination and the anchored operation target. |
| ON-ANCHOR-V002 | `on conn { conn.close(); ret nothing; }` | `on_positive_far_tcpconn_close.sg` | `TaskResult<nothing>` | N/A | `conn` is the destination and the anchored control target. |
| ON-ANCHOR-N001 | `on a { b.send(own msg); ret nothing; }` | `on_negative_unanchored_far_channel.sg` | `SEM3150` | Not safely fixable automatically. | `b` is not proven to share owner with destination `a`. |
| ON-ANCHOR-N002 | `on conn_a { conn_b.close(); ret nothing; }` | `on_negative_unanchored_far_tcpconn.sg` | `SEM3150` | Not safely fixable automatically. | A different far handle is not anchored by the destination. |
| ON-ANCHOR-N003 | local method call on `far T` outside `on` | `on_negative_far_operation_outside_on.sg` | `SEM3142` | Fixable: wrap the accepted operation in `on handle { ... }`. | Acting through a far handle requires an accepted crossing surface. |

## `far TcpConn` Control-Only Matrix

`far TcpConn` is accepted in Block 2 only for remote owner control operations.
The control-operation set is closed in Epic 11 to exactly `{ close() }`; every
other socket operation (read, write, accept, and any other control op) is
rejected. Remote socket I/O is rejected in Epic 11.

| Row | Operation | Fixture | Expected type or diagnostic | Fix availability | Contract |
| --- | --- | --- | --- | --- | --- |
| ON-TCP-V001 | `conn.close()` | `on_positive_far_tcpconn_close.sg` | `TaskResult<nothing>` | N/A | Remote close is the only accepted control operation. |
| ON-TCP-N001 | `conn.read(...)` | `on_negative_tcpconn_read.sg` | `SEM3151` | Not safely fixable automatically. | Remote read through `far TcpConn` is not accepted in Epic 11. |
| ON-TCP-N002 | `conn.write(...)` | `on_negative_tcpconn_write.sg` | `SEM3151` | Not safely fixable automatically. | Remote write through `far TcpConn` is not accepted in Epic 11. |
| ON-TCP-N003 | `conn.accept(...)` | `on_negative_tcpconn_accept.sg` | `SEM3151` | Not safely fixable automatically. | Remote socket I/O and resource migration are outside Block 2. |

## Crossing Requirement Matrix

| Row | Enclosing context | Fixture | Expected type or diagnostic | Fix availability | Contract |
| --- | --- | --- | --- | --- | --- |
| ON-CROSS-V001 | `fn f(...) crosses -> TaskResult<T>` | `on_positive_crosses_fn.sg` | `TaskResult<T>` | N/A | A `crosses` function may contain `on`. |
| ON-CROSS-V002 | `async fn f(...) crosses -> T` handling `TaskResult<T>` | `on_positive_async_crosses_fn.sg` | Checked result handling | N/A | `async` does not replace `crosses`; both may appear. |
| ON-CROSS-N001 | non-`crosses` function contains `on` | `on_negative_missing_crosses.sg` | `SEM3162` (Block 4) | Fixable: add `crosses` to the enclosing function signature. | `on` requires the enclosing crossing effect. |
| ON-CROSS-N002 | `on` inside context where suspension is illegal | `on_negative_illegal_suspend_context.sg` | `SEM3152` | Not safely fixable automatically. | `on` is allowed only where suspension is legal. |

## Nested Crossing Matrix

| Row | Shape | Fixture | Expected diagnostic | Fix availability | Contract |
| --- | --- | --- | --- | --- | --- |
| ON-NEST-N001 | `on pool { on distributed { ret v; }; ret v; }` | `on_negative_nested_on_placement.sg` | `SEM3153` | Fixable: split the crossings into sequential outer-scope operations. | Nested `on` is postponed in Epic 11. |
| ON-NEST-N002 | `on ch { on conn { ret nothing; }; ret nothing; }` | `on_negative_nested_on_far_handle.sg` | `SEM3153` | Fixable: perform one anchored crossing per outer scope. | A crossing block cannot open a second crossing. |

## Contextual Keyword Back-Compat Matrix

`on` is a contextual keyword (see the epic "Keyword Strategy"). It is recognized
as a crossing keyword only at an expression head (`on <expr> { ... }`) and
immediately after `spawn`; everywhere else it remains an ordinary identifier and
pre-existing code that uses `on` as an identifier must keep parsing.

| Row | Shape | Fixture | Expected result | Contract |
| --- | --- | --- | --- | --- |
| ON-KW-V001 | `let on = 1;` | `on_positive_on_identifier_let.sg` | Parses; `on` binds as an identifier. | `on` is a valid identifier outside crossing position. |
| ON-KW-V002 | `let x = on + 1;` reading the `on` binding | `on_positive_on_identifier_use.sg` | Parses; `on` reads the binding. | Using an `on` identifier as a value is unaffected. |
| ON-KW-V003 | `on dst { ret value; }` at expression head | (covered by ON-DST rows) | Parses as a crossing block. | Crossing recognition is position-scoped, not lexical. |

## Compare-Over-`on` And Statement-Position Matrix

| Row | Shape | Fixture | Expected type or diagnostic | Fix availability | Contract |
| --- | --- | --- | --- | --- | --- |
| ON-CMP-V001 | `compare on pool { ret v; } { Success(v) => ...; Cancelled() => ...; }` | `on_positive_compare_on.sg` | Matched `TaskResult<T>` arms | N/A | `compare` may match an `on` crossing result directly. |
| ON-STMT-V001 | `on pool { ret v; };` in statement position, result discarded | `on_positive_on_statement_discard.sg` | `TaskResult<T>` discarded | N/A | `on` is allowed in statement position with its `TaskResult<T>` discarded. |

## Backend And Feature-Gate Matrix

Epic 11 delivers the `on` surface plus lowering guards only; the Phase 4 crossing
transport is postponed (see the epic "Epic 11 Execution Scope"). A backend or
configuration that parses and type-checks `on` but cannot lower or execute the
crossing must emit a deterministic diagnostic instead of crashing, panicking,
leaking, or falling through to an ambiguous backend error.

| Row | Condition | Fixture | Expected type or diagnostic | Fix availability | Contract |
| --- | --- | --- | --- | --- | --- |
| ON-GATE-N001 | Phase 4 placement-crossing lowering unavailable | `on_negative_backend_unavailable.sg` | `FUT7014` | Fixable only by selecting/enabling a supported backend/configuration. | `on` execution is guarded until the Phase 4 transport epic. |

## Diagnostic Code Inventory

| Code | Allocation rule |
| --- | --- |
| `SEM3143` | Reuse if a destination-kind diagnostic exists; otherwise allocate for `far Task<T>` destination rejection. |
| `FUT7012` | Postponed surface: reuse the `FUT`-range blocking diagnostic (e.g. `FutBlockingNotSupported`) unless the parser already owns a precise unsupported-form code. |
| `SEM3144` | Reuse generic type mismatch only if it can name the accepted destination classes. |
| `SEM3145` | Reuse existing value-required diagnostic if it reports type-name misuse deterministically. |
| `SEM3146` | Reuse function-value diagnostic if it provides the call-returning-`Placement` fix. |
| `SEM3015` | Reuse existing call argument type mismatch for `shard(id)`. |
| `SEM3147` | Allocate for `return` escaping through crossing block. |
| `SEM3148` | Reuse block-result diagnostic if it is explicit about `ret`. |
| `SEM3149` | Reuse type mismatch only if the message names `TaskResult<T>` wrapping. |
| `SEM3150` | Allocate for unproven far-handle owner relation. |
| `SEM3142` | Reuse Block 1 local-operation-on-`far T` diagnostic (`SEM3142`) if exact. |
| `SEM3151` | Allocate for rejected remote socket I/O through `far TcpConn`. |
| `SEM3152` | Reuse existing await/suspension-context diagnostic if exact. |
| `SEM3153` | Allocate for nested crossing rejection. |
| `FUT7014` | Postponed transport: allocate in the `FUT` (7xxx) range for Phase 4 placement-crossing lowering being unavailable. |

The capture and crossing-effect diagnostics used by Block 2 are owned by Block 4,
not allocated here: `SEM3165` (ON-CAP-N001/N002),
`SEM3166` (ON-CAP-N003), `SEM3167`
(ON-CAP-N004), `SEM3168` (ON-CAP-N005), and
`SEM3162` (ON-CROSS-N001). See the shared-diagnostics ownership
table in `11-tasks/README.md`.

## Positive Golden Fixture Inventory

| Fixture | Matrix rows covered |
| --- | --- |
| `on_positive_pool.sg` | ON-DST-V001, ON-BODY-V001 |
| `on_positive_distributed.sg` | ON-DST-V002 |
| `on_positive_shard_call.sg` | ON-DST-V003 |
| `on_positive_placement_var.sg` | ON-DST-V004 |
| `on_positive_placement_fn.sg` | ON-DST-V005 |
| `on_positive_far_channel_anchor.sg` | ON-DST-V006, ON-ANCHOR-V001 |
| `on_positive_far_tcpconn_close.sg` | ON-DST-V007, ON-ANCHOR-V002, ON-TCP-V001 |
| `on_positive_ret_nothing_result.sg` | ON-BODY-V002 |
| `on_positive_taskresult_binding.sg` | ON-BODY-V003 |
| `on_positive_taskresult_return.sg` | ON-BODY-V004 |
| `on_positive_copy_capture.sg` | ON-CAP-V001 |
| `on_positive_shard_movable_capture.sg` | ON-CAP-V002 |
| `on_positive_far_handle_capture.sg` | ON-CAP-V003 |
| `on_positive_placement_capture.sg` | ON-CAP-V004 |
| `on_positive_crosses_fn.sg` | ON-CROSS-V001 |
| `on_positive_async_crosses_fn.sg` | ON-CROSS-V002 |
| `on_positive_on_identifier_let.sg` | ON-KW-V001 |
| `on_positive_on_identifier_use.sg` | ON-KW-V002 |
| `on_positive_compare_on.sg` | ON-CMP-V001 |
| `on_positive_on_statement_discard.sg` | ON-STMT-V001 |

## Negative Golden Fixture Inventory

| Fixture | Matrix row | Diagnostic code | Fix availability |
| --- | --- | --- | --- |
| `on_negative_far_task_destination.sg` | ON-DST-N001 | `SEM3143` | Fixable |
| `on_negative_blocking_destination.sg` | ON-DST-N002 | `FUT7012` | Fixable |
| `on_negative_integer_destination.sg` | ON-DST-N003 | `SEM3144` | Fixable |
| `on_negative_type_destination.sg` | ON-DST-N004 | `SEM3145` | Fixable |
| `on_negative_bare_fn_destination.sg` | ON-DST-N005 | `SEM3146` | Fixable |
| `on_negative_fn_returns_int_destination.sg` | ON-DST-N006 | `SEM3144` | Fixable |
| `on_negative_shard_id_type.sg` | ON-DST-N007 | `SEM3015` | Fixable |
| `on_negative_return_in_body.sg` | ON-BODY-N001 | `SEM3147` | Fixable |
| `on_negative_missing_ret_value.sg` | ON-BODY-N002 | `SEM3148` | Fixable |
| `on_negative_assign_unwrapped_result.sg` | ON-BODY-N003 | `SEM3149` | Fixable |
| `on_negative_return_unwrapped_result.sg` | ON-BODY-N004 | `SEM3149` | Fixable |
| `on_negative_shared_borrow_capture.sg` | ON-CAP-N001 | `SEM3165` (Block 4) | Fixable |
| `on_negative_mut_borrow_capture.sg` | ON-CAP-N002 | `SEM3165` (Block 4) | Fixable |
| `on_negative_nosend_capture.sg` | ON-CAP-N003 | `SEM3166` (Block 4) | Not safely fixable automatically |
| `on_negative_shard_pinned_capture.sg` | ON-CAP-N004 | `SEM3167` (Block 4) | Fixable only through accepted `far T` handle use |
| `on_negative_unmarked_owned_capture.sg` | ON-CAP-N005 | `SEM3168` (Block 4) | Fixable |
| `on_negative_unanchored_far_channel.sg` | ON-ANCHOR-N001 | `SEM3150` | Not safely fixable automatically |
| `on_negative_unanchored_far_tcpconn.sg` | ON-ANCHOR-N002 | `SEM3150` | Not safely fixable automatically |
| `on_negative_far_operation_outside_on.sg` | ON-ANCHOR-N003 | `SEM3142` | Fixable |
| `on_negative_tcpconn_read.sg` | ON-TCP-N001 | `SEM3151` | Not safely fixable automatically |
| `on_negative_tcpconn_write.sg` | ON-TCP-N002 | `SEM3151` | Not safely fixable automatically |
| `on_negative_tcpconn_accept.sg` | ON-TCP-N003 | `SEM3151` | Not safely fixable automatically |
| `on_negative_missing_crosses.sg` | ON-CROSS-N001 | `SEM3162` (Block 4) | Fixable |
| `on_negative_illegal_suspend_context.sg` | ON-CROSS-N002 | `SEM3152` | Not safely fixable automatically |
| `on_negative_nested_on_placement.sg` | ON-NEST-N001 | `SEM3153` | Fixable |
| `on_negative_nested_on_far_handle.sg` | ON-NEST-N002 | `SEM3153` | Fixable |
| `on_negative_backend_unavailable.sg` | ON-GATE-N001 | `FUT7014` | Fixable only by selecting/enabling a supported backend/configuration |

## Follow-Up Compiler Test Inventory

These tests are added after the golden matrix exists and before Block 2 is
marked complete.

| Test surface | Required coverage |
| --- | --- |
| Lexer / parser | `on` contextual-keyword recognition at the expression head only, with `let on = 1;` still parsing as an identifier; destination expression parse; body block parse; `compare on` and statement-position `on`; rejection of postponed `on blocking`; no interaction with `spawn on`. |
| AST | Dedicated `on` crossing node; destination expression retained; body block retained; `ret` represented as block-local result. |
| Name resolution | `pool`, `distributed`, `shard`, placement variables, and placement-returning functions resolve as values. |
| Destination sema | Accept `Placement`, `far Channel<T>`, and accepted `far TcpConn`; reject `far Task<T>` and non-destinations. |
| Result sema | Body `ret T` produces outer `TaskResult<T>`; assignment and return contexts enforce wrapping. |
| Body sema | `ret` accepted; `return` rejected; missing result rejected in value contexts. |
| Capture sema | Copy (including `Placement`), `own @shard_movable`, `far T` handle by move, borrow rejection, `@nosend` rejection, `@shard_pinned` owned rejection, unmarked owned user-value rejection. |
| Owner-anchor sema | Destination `far` handle anchors only itself; unanchored far-handle operations are rejected. |
| `far TcpConn` sema | Control-only operations accepted; read/write/accept rejected. |
| Crosses sema | `on` requires enclosing `crosses`; `async` alone is insufficient. |
| Lowering / backend smoke | Minimal accepted placement crossing lowers to a crossing operation returning `TaskResult<T>`; rejected forms do not reach lowering. |

## Review Checklist

- Every accepted destination in Epic 11 Block 2 has a positive fixture.
- Every invalid destination in Epic 11 Block 2 has a negative fixture.
- Every negative fixture names one diagnostic code.
- Every negative fixture records fix availability.
- `TaskResult<T>` wrapping is tested in binding and function-return contexts.
- `ret` and `return` are tested separately.
- Capture legality covers Copy, `own @shard_movable`, `far T` handle by move,
  `Placement`, borrowed values, `@nosend`, owned `@shard_pinned`, and unmarked
  owned user values.
- Far-handle owner anchoring rejects same-type but unproven handles.
- `far TcpConn` control operations are closed to `{ close() }`; read/write/accept
  are rejected as remote I/O.
- `far Task<T>` is explicitly rejected as an `on` destination.
- `on` back-compatibility as an identifier (`let on = 1;`) has positive fixtures.
- `compare on` and statement-position `on` with a discarded `TaskResult<T>` have
  positive fixtures.
- Phase 4 backend-unavailable behavior has a deterministic negative fixture.
- The capture and crossing-effect diagnostics are Block 4-owned; Block 2
  references them and does not allocate them.
- `crosses` appears only as a Block 4 dependency and requirement.
- The document does not add Block 1, Block 3, or Block 4 implementation scope.
