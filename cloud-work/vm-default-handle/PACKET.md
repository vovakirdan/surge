# Packet: the VM's default for a core runtime handle

- **Branch:** `cloud/vm-default-handle`, cut from `validation/step7-d2-on-d1` at `1b122bfa`.
- **Commits:** three. Two work-in-progress commits were pushed while the suites ran; the final commit is `cloud: VM default for runtime handles`.
- **Not done here, by instruction:** no PR, no push to any other branch, and `docs/runtime-v2-epics/DEBT.md` is untouched. The ledger text is in §8, ready to paste.
- **Scope:** VM only (`internal/vm`). The native runtime and the LLVM backend are unchanged.

## 1. Defect

**The VM cannot build the default value of a core runtime handle; the native backend can.**

A runtime handle is a type `types.Interner.IsRuntimeHandleType` accepts. Those are the families sema marks: `Task`, `Channel` and `Range` (`internal/sema/type_decl_core.go:89-93`). A `Mutex` is built on `Channel<nothing>` (`core/sync.sg:4-5`; `docs/runtime-v2-epics/23b-inline-storage-and-typed-carriers.md:256-257`), so it reaches the same default.

Measured at `1b122bfa`, with commands in §5:

| program | VM | native |
|---|---|---|
| `let c = default::<Channel<int>>(); ...` | `panic VM1999: storage: type#1509 has 1 members but 0 layout offsets` | runs |
| `let h = default::<HolderCh>();` (`HolderCh = { ch: Channel<int>, n: int }`) | same panic | runs |
| `let m = default::<Mutex>();` | same panic (`type#70`) | runs |
| `let mut ch: Channel<int>;`, `let mut slot: Task<int>;`, `q.pop().safe()` on an empty `Task<int>[]` | same panic (with the gate bypassed, see §5.3) | runs |

**Cause, read from code:**
- **Native.** `emitDefaultValue` returns a null pointer for a runtime handle: "Runtime-owned handles use a null pointer as their uninitialized sentinel" (`internal/backend/llvm/emit_intrinsics_default.go:68-73`).
- **VM.** `defaultValue` had no such case. A handle is a nominal struct, so it fell into the struct case, `defaultStruct` (`internal/vm/intrinsic_default.go:145-162` at base). That built the handle's private `__opaque` member through `buildStruct`.
- **Where it failed.** The layout registry records no field offsets for a runtime handle, which is a handle word: the panic's own numbers are 1 declared member against 0 offsets. The VM's storage classifies such a type as `cellHandle` through `RuntimeHandlePayloads` (`internal/vm/storage_shape.go:74-76`), and `compositeMembers` refuses it as an aggregate (`internal/vm/storage_shape.go:210-213`).

## 2. Model

I read `docs/RUNTIME_V2.md`, `docs/RUNTIME_MODEL_EXPLAINED.ru.md`, `docs/RUNTIME.md` and `docs/runtime-v2-epics/`, including `DEBT.md`, `19-`, `20-`, `23-`, `23b-`, `24-` (lines 1-290 and 928-1060), `HANDOFF.md` and `NOTES.md`. `docs/CONCURRENCY.md` (v1) was not used.

**What the model says about handles:**
- **A handle is one word, not an object graph.** "a `Task<T>` or `Channel<T>` handle may remain one word … runtime resources … are not turned into inline object graphs" (`docs/runtime-v2-epics/23-storage-model-and-typed-carrier-abi.md:147-151`). So a default handle is a handle word, not a struct built member by member.
- **Channel.** "`Channel<T>` is a copyable handle … copying a handle retains, dropping a copy releases, and the last release destroys the object" (`docs/RUNTIME_V2.md:771-775`). "a handle may stay one machine word while its copy and drop carry work" (`:777-779`).
- **Task.** A task handle is an entitlement:
  - "Dropping a `LIVE` handle changes only that entitlement to `DROPPED`" (`23-storage…md:540-541`);
  - `clone` "creates another result entitlement" (`:548-549`);
  - "`cancel(&Task<T>)` through any live local handle is task-global" and "Dropping or consuming one handle releases only its entitlement" (`:571-575`);
  - a cold task is "ended unrun if its last handle goes first" (`docs/RUNTIME.md:89-91`).

  A null handle names no task and holds no entitlement, so none of this bookkeeping may run for it. On the VM that holds already: `resourceFreed` ignores a word ≤ 0 (`internal/vm/resource.go:168-170`).
- **Parity is required.** The VM's purpose is "diagnostics and parity" and the backends "share language semantics" (`docs/RUNTIME.md:43-48`). The acceptance item is "VM and LLVM agree on value/copy/move/drop/borrow semantics" (`23-storage…md:870`). Parity is compared single-threaded (owner ruling, `docs/runtime-v2-epics/DEBT.md:132`, RV2-DEBT-188).
- **Null tolerance on the drop path is load-bearing natively.** "drop idempotence in this compiler rests ENTIRELY on that null-after-drop store plus null tolerance in every drop entry point" (`DEBT.md:319`, RV2-DEBT-066). "`rt_task_handle_drop(NULL)` for a slot that WAS emptied is guarded separately: `TestRuntimeV2TaskHandleDropTreatsAnEmptySlotAsNothing`" (`DEBT.md:405`, RV2-DEBT-258).

**What the model does not say.** None of the documents defines a default, uninitialized, null or sentinel runtime handle. Specifically, none says:
- what `let x: Task<int>;` or `default::<Channel<T>>()` produces;
- what `await`, `cancel`, `send` or `recv` must do on such a handle.

The only definition in the tree is the native code: the emitter's null (`emit_intrinsics_default.go:68-73`) and the C runtime's NULL checks (§3). This packet makes the VM match that code. Whether the null is the language's contract is **for the owner to rule on**; see §9.

## 3. Fix

| file | change |
|---|---|
| `internal/vm/runtime_handle_null.go` (new) | `nullRuntimeHandle` (Task/Channel → `MakeResource(0, T)`, Range → `MakeHandleRange(0, T)`); `isNullRuntimeHandle` (a typed null, or the `nothing` a zeroed handle cell decodes to, looked through a reference); the native refusal words. |
| `internal/vm/intrinsic_default.go` | `defaultValue` asks `IsRuntimeHandleType` before the kind switch and returns the null, as the emitter does. |
| `internal/vm/async_runtime.go` | `taskIDFromValue` and `channelIDFromValue` refuse a null with the native words. Every task and channel operation in the VM resolves its handle through one of these two (`gopls references`: 9 and 8 call sites). |
| `internal/vm/vm_dispatch_async_timeout.go` | A cancelled task answers pending at `timeout` **before** resolving the target, as `rt_timeout_poll` does. |

**Why `H == 0` and not `nothing`.** A heap value with `H == 0` is already inert on every VM lifetime path:
- `dropValue` skips it (`drop.go`);
- `cloneForShare` and `duplicateValue` return it uncounted (`eval_data.go`, `clone_value.go`);
- `releaseContainedValue` passes it by (`heap.go`);
- `handleBits` stores it as 0 (`storage_cell.go`).

So the VM gets a native-like null without a new convention. A typed `nothing` was rejected: `coerceToSlotType` turns a `nothing` stored into a union slot into that union's `nothing` arm. `Some(default::<Channel<int>>())` would then silently become `nothing`. A zeroed handle **cell** still decodes as `nothing` (`storage_ops.go`, `handleValue`), which is why the refusal accepts both spellings.

**Per-operation behaviour on a null handle.** "Probe" means the gate-bypassed measurement in §5.3.

| operation | native on NULL (file:line) | VM at base | VM after | pinned by |
|---|---|---|---|---|
| default of Task/Channel/Range | null pointer (`emit_intrinsics_default.go:68-73`) | VM1999 panic | `H == 0` | e2e (Channel, Mutex); internal (Task, Range, alias, `own`) |
| drop Task | returns (`rt_task_lifetime.c:296-302`) | never reached | no-op | internal inert row; probe `task_drop` |
| drop / copy Channel | return (`rt_channel_refcount.c:51-67`) | never reached | no-op | e2e `default_dropped_unused` |
| handed to a task as a parameter (frame retain) | retain returns | never reached | no-op | e2e `default_dropped_unused` |
| stored in a member, array slot or `Option`, then overwritten | stores the word; drop of NULL returns | never reached | handle cell 0; overwrite drops nothing | e2e `default_overwritten`, `channel_binding` |
| channel send / recv, sync | `panic_msg("async: null channel handle")` via `channel_from_handle` (`rt_channel_lane.h:499-505`; `rt_channel_sync.c:443-445`, `:495-497`) | VM1999 at default | `panic VM1203: async: null channel handle` | e2e `null_send`, `null_recv`, `null_member_send` |
| channel send / recv, in a task | same (`rt_async_channel_send.c:38-40`, `rt_async_channel.c:180-182`) | VM1999 | same words | e2e `null_send_in_task`, `null_recv_in_task` |
| try_send / try_recv / close | same (`rt_channel_sync.c:19-21`, `:54-56`, `rt_channel_close.c:9-11`) | VM1999 | same words | e2e rows |
| select arm | cancelled → -1 first (`rt_async_select.c:179-186`); arms in order, stops at first winner; reached null arm → panic | VM1999 | same order, same words | e2e `null_select_arm`; probes `sel_*` |
| Mutex lock | the channel recv above | VM1999 | same words | e2e `null_mutex_lock` |
| await (sync and in a task), cancel, clone, spawn of a value, select task arm | `panic_msg("invalid task handle")` via `task_from_handle` (`rt_async_state.c:516-522`; `rt_async_task.c:466-471`, `:249-254`, `:535-540`, `:551-552`, `:229-234`; `rt_async_select.c:260`) | VM1999 | `panic VM1203: invalid task handle` | internal refusal row; probes |
| `timeout` of a null Task | panic, unless the current task is cancelled, which answers pending first (`rt_async_select.c:56-63`) | resolved the handle first | same order, same words | probes `timeout_null`, `cancelled_timeout_null`; e2e `cancelled_task_timeout` (live target) |
| Range drop / overwrite | `rt_range_free` returns (`rt_range.c:92-95`) | never reached | no-op | probes `range_drop`, `range_overwrite` |
| **Range slice, iterate** | slice: NULL read as the **whole range** (`range_bounds`, `rt_string.c:239`, `rt_array.c:211`); `for` / `next`: unchecked load through NULL in emitted IR, SIGSEGV (139 run directly, 255 through `surge run`) | never reached | `panic VM1203: invalid handle 0` | **diverges**; §9, second ledger row |
| `rt_scope_register_child` | returns unless the scope is the caller's active one (`rt_async_scope.c:184-186`) | never reached | refuses | not aligned; unreachable (non-`pub` in `core/intrinsics.sg`, no emitter) |

The VM prints the native words inside its own frame. VM: `panic VM1203: <words>` plus location and backtrace. Native: `surge: fatal [PANIC]: <words>` (`rt_fatal.c:44-52`). Both exit 1: `cmd/surge/run.go`'s VM-error branch, and `_exit(1)` natively. The tests compare the words and the exit code.

## 4. Tests

### 4.1 e2e: `internal/vm/runtime_v2_default_runtime_handle_e2e_test.go`

This is `TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends`, with no build tag.
- Every row states one expected `(stdout, exit, refusal words)`. The `vm` leg and the `llvm` leg each assert it, and the parent asserts that the two legs agree.
- The VM leg runs in process. The LLVM leg builds through `buildRuntimeV2CrossingSource` and runs with `SURGE_THREADS=1` (RV2-DEBT-188), under `valgrind --leak-check=full` when valgrind is installed. Any memcheck error fails a row; a clean row must also lose zero bytes.
- The LLVM leg needs `SURGE_SKIP_TIMEOUT_TESTS=0`, like every native row in this package. With `=1` (the Makefile default) it skips.

| row | shape |
|---|---|
| `channel_binding` | the reported program with its implicit default spelled out: `let mut ch = default::<Channel<int>>(); ch = Channel::<int>::new(1:uint); …; return 3;` |
| `default_dropped_unused` | a null bound, copied, stored in a member, wrapped in a `Mutex`, wrapped in `Some`, held in a task frame across a suspension, and passed to a task as a parameter; all dropped unused |
| `default_overwritten` | a member and an array slot holding a null, overwritten with live channels, then used |
| `null_send`, `null_recv`, `null_close`, `null_try_send`, `null_try_recv` | each use of a null channel in `main` |
| `null_send_in_task`, `null_recv_in_task`, `null_select_arm` | the async lowering of each use, after a suspension |
| `null_member_send` | a send on a defaulted member |
| `null_mutex_lock` | locking a default `Mutex` |
| `cancelled_task_timeout` | a cancelled task reaching `timeout` with a **live** target; see §9 |

### 4.2 Below the gate: `internal/vm/runtime_handle_null_internal_test.go`

- `TestDefaultOfARuntimeHandleIsTheNullHandle`: Task, Channel, Range, an alias of Task and `own Task` default to their null, and allocate nothing.
- `TestAnUnmarkedLookalikeIsNotANullHandle`: a user struct with the same one-member shape, not marked, still defaults as a composite. This is the control.
- `TestNullRuntimeHandleIsInertOnEveryLifetimePath`: drop, `cloneForShare` and `duplicateValue` leave the heap counters unchanged, and the handle cell encodes 0.
- `TestNullRuntimeHandleIsRefusedWithTheNativeWords`: a typed null and a zeroed-cell `nothing` are both refused with `invalid task handle` and `async: null channel handle` (`PanicInvalidHandle`).

### 4.3 Shapes that cannot be e2e rows at this base

The D2 return-origin gate refuses each of these at `1b122bfa`. Measured with `surge run` on the base build; the full list is in §5.3.

| shape | gate's reasons |
|---|---|
| any **Task** default: `default::<Task<int>>()`, `let t: Task<int>;`, a struct with a Task member, `q.pop().safe()`, `with_len` | `opaque result borrowed-state classification is unsupported`, `callee returned an unproved source`, `generic opaque use requires its type-dependent effect transfer` (`core/option.sg`, `core/array.sg`) |
| any **Range** default | the same opaque-result reason |
| a `let` with no initializer of a handle type (`let mut ch: Channel<int>;`) | `default result is not proven Defaultable`, `generic use lacks its original typed operation` |
| `Array::<Channel<int>>::with_len(n)` | the generic-opaque and unproved-source reasons |

These are pinned by the internal rows (§4.2) and measured end to end with the gate bypassed (§5.3). The planned N-DEFHANDLE packet makes some of them build.

## 5. Evidence

### 5.1 New tests after the fix

```
$ SURGE_STDLIB=$PWD SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^(TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends|TestDefaultOfARuntimeHandleIsTheNullHandle|TestAnUnmarkedLookalikeIsNotANullHandle|TestNullRuntimeHandleIsInertOnEveryLifetimePath|TestNullRuntimeHandleIsRefusedWithTheNativeWords)$' -count=1 -v
--- PASS: TestDefaultOfARuntimeHandleIsTheNullHandle (0.00s)
--- PASS: TestAnUnmarkedLookalikeIsNotANullHandle (0.00s)
--- PASS: TestNullRuntimeHandleIsInertOnEveryLifetimePath (0.00s)
--- PASS: TestNullRuntimeHandleIsRefusedWithTheNativeWords (0.00s)
--- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends (132.45s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/channel_binding (9.05s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/channel_binding/vm (0.65s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/channel_binding/llvm (8.39s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_dropped_unused (9.34s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_dropped_unused/vm (0.74s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_dropped_unused/llvm (8.60s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_overwritten (9.28s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_overwritten/vm (0.77s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_overwritten/llvm (8.51s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send (9.24s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send/vm (0.77s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send/llvm (8.47s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv (9.56s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv/vm (0.78s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv/llvm (8.79s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_close (9.43s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_close/vm (0.80s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_close/llvm (8.63s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_send (9.27s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_send/vm (0.74s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_send/llvm (8.53s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_recv (9.54s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_recv/vm (0.82s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_recv/llvm (8.72s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send_in_task (9.51s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send_in_task/vm (0.80s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send_in_task/llvm (8.72s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv_in_task (9.74s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv_in_task/vm (0.77s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv_in_task/llvm (8.96s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_select_arm (9.66s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_select_arm/vm (0.80s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_select_arm/llvm (8.86s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_member_send (9.43s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_member_send/vm (0.75s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_member_send/llvm (8.68s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_mutex_lock (9.76s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_mutex_lock/vm (0.76s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_mutex_lock/llvm (9.01s)
    --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/cancelled_task_timeout (9.63s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/cancelled_task_timeout/vm (0.84s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/cancelled_task_timeout/llvm (8.79s)
PASS
ok  	surge/internal/vm	132.457s
exit=0
```

### 5.2 Counterfactual: the fix reverted, the tests kept

Run on the final code, with the three changed files put back to their base contents and the new file removed. The tests are unchanged; the tree was restored afterwards.

```
$ git checkout 1b122bfa -- internal/vm/async_runtime.go internal/vm/intrinsic_default.go internal/vm/vm_dispatch_async_timeout.go && rm internal/vm/runtime_handle_null.go
$ SURGE_STDLIB=$PWD SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^(TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends|TestDefaultOfARuntimeHandleIsTheNullHandle|TestAnUnmarkedLookalikeIsNotANullHandle|TestNullRuntimeHandleIsInertOnEveryLifetimePath|TestNullRuntimeHandleIsRefusedWithTheNativeWords)$' -count=1 -v
--- FAIL: TestDefaultOfARuntimeHandleIsTheNullHandle (0.00s)
--- PASS: TestAnUnmarkedLookalikeIsNotANullHandle (0.00s)
--- FAIL: TestNullRuntimeHandleIsInertOnEveryLifetimePath (0.00s)
--- FAIL: TestNullRuntimeHandleIsRefusedWithTheNativeWords (0.00s)
--- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends (130.38s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/channel_binding (9.19s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/channel_binding/vm (0.79s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/channel_binding/llvm (8.41s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_dropped_unused (9.36s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_dropped_unused/vm (0.93s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_dropped_unused/llvm (8.43s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_overwritten (9.33s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_overwritten/vm (0.85s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/default_overwritten/llvm (8.47s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send (9.16s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send/vm (0.84s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send/llvm (8.33s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv (9.06s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv/vm (0.79s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv/llvm (8.28s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_close (9.22s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_close/vm (0.82s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_close/llvm (8.40s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_send (9.57s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_send/vm (0.86s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_send/llvm (8.71s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_recv (9.37s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_recv/vm (0.84s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_try_recv/llvm (8.52s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send_in_task (9.27s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send_in_task/vm (0.88s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_send_in_task/llvm (8.39s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv_in_task (9.25s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv_in_task/vm (0.86s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_recv_in_task/llvm (8.38s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_select_arm (9.28s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_select_arm/vm (0.78s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_select_arm/llvm (8.50s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_member_send (9.35s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_member_send/vm (0.79s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_member_send/llvm (8.55s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_mutex_lock (9.31s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_mutex_lock/vm (0.80s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/null_mutex_lock/llvm (8.52s)
    --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/cancelled_task_timeout (9.66s)
        --- FAIL: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/cancelled_task_timeout/vm (0.85s)
        --- PASS: TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends/cancelled_task_timeout/llvm (8.80s)
FAIL
FAIL	surge/internal/vm	130.395s
FAIL
exit=1

# the failure each red leg reported:
runtime_handle_null_internal_test.go:87: the default of Task must be the null handle, got an error: panic VM1999: storage: type#19 has 1 members but 0 layout offsets
runtime_handle_null_internal_test.go:121: default of type#19: panic VM1999: storage: type#19 has 1 members but 0 layout offsets
runtime_handle_null_internal_test.go:155: a null task (invalid) must be refused as the native runtime refuses it, got panic VM1003: expected Task, got invalid
runtime_v2_default_runtime_handle_e2e_test.go:304: vm reported a runtime error for a clean row: panic VM1999: storage: type#1509 has 1 members but 0 layout offsets
runtime_v2_default_runtime_handle_e2e_test.go:304: vm reported a runtime error for a clean row: panic VM1999: storage: type#1509 has 1 members but 0 layout offsets
runtime_v2_default_runtime_handle_e2e_test.go:304: vm reported a runtime error for a clean row: panic VM1999: storage: type#1509 has 1 members but 0 layout offsets
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_send as stdout="" exit=1 fault="storage: type#1509 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_recv as stdout="" exit=1 fault="storage: type#1509 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_close as stdout="" exit=1 fault="storage: type#1509 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_try_send as stdout="" exit=1 fault="storage: type#1509 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_try_recv as stdout="" exit=1 fault="storage: type#1509 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_send_in_task as stdout="" exit=1 fault="storage: type#1510 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_recv_in_task as stdout="" exit=1 fault="storage: type#1510 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_select_arm as stdout="" exit=1 fault="storage: type#1510 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_member_send as stdout="" exit=1 fault="storage: type#1509 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:306: vm ran null_mutex_lock as stdout="" exit=1 fault="storage: type#70 has 1 members but 0 layout offsets", want stdout="before\n" exit=1 fault="async: null channel handle"
runtime_v2_default_runtime_handle_e2e_test.go:304: vm reported a runtime error for a clean row: panic VM1002: async payload owner capability mismatch
```

**Result:**
- **VM legs:** all 14 fail. Thirteen stop at the default with the original `VM1999 … 0 layout offsets`. `cancelled_task_timeout` fails with the base `VM1002`.
- **Native legs:** all 14 pass. Native is untouched by the fix, so its expectation is independent of it.
- **Internal rows:** 3 of the 4 fail. The lookalike control passes both ways, as a control should.

### 5.3 Probe matrix: 56 programs, gate bypassed for measurement only

Two things here are measurement-only and are **not** in the commit:
- **The gate bypass.** `probes/gate-bypass.patch` makes `returnOriginVerdict` answer "not blocked" when the analysis is incomplete, in a scratch build.
- **The table itself.** `probes/probe_matrix.txt` covers each program in `probes/*.sg`. For each it records whether the **gated** base build accepts it, then the VM before the fix, the VM after the fix and native. All runs use `SURGE_THREADS=1`.

The script is `probes/probe_matrix.py`.

To reproduce, build three compilers into one directory and run the script with the probes in `probes/`:

```
git worktree add --detach /tmp/base 1b122bfa
(cd /tmp/base && go build -o $BIN/surge-base ./cmd/surge/ \
   && git apply <this dir>/probes/gate-bypass.patch && go build -o $BIN/surge-bypass-base ./cmd/surge/)
# on this branch:
git apply cloud-work/vm-default-handle/probes/gate-bypass.patch && go build -o $BIN/surge-bypass-fix ./cmd/surge/ && git checkout -- internal/driver
SURGE_BIN_DIR=$BIN SURGE_STDLIB=$PWD python3 cloud-work/vm-default-handle/probes/probe_matrix.py
```

- **Gate at base** is the verdict of the unpatched base build.
- **VM before** uses the base build with the gate bypassed; **VM after** uses this branch with the gate bypassed.
- **Native** is `surge run --backend llvm` of this branch with the gate bypassed. The fix does not touch the native path.
- A **refusal** is shown by its words; the VM's panic code is kept.
- `rc=255` is how `surge run` reports a native child killed by a signal. Run directly, `range_iter` exits 139 (SIGSEGV); under valgrind it is `Invalid read of size 1 … Address 0x13`.

| probe | gate at base | VM before | VM after | native | after = native |
|---|---|---|---|---|---|
| `cancelled_select_null.sg` | accepted | rc=1  VM1999 storage: type#1510 has 1 members but 0 layout offsets | rc=90 | rc=90 | SAME |
| `cancelled_select_null2.sg` | accepted | rc=1  VM1999 storage: type#1510 has 1 members but 0 layout offsets | rc=1 out=[child-start\|] VM1203 async: null channel handle | rc=1 out=[child-start\|] async: null channel handle | SAME |
| `cancelled_timeout_null.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=90 | rc=90 | SAME |
| `cancelled_timeout_valid.sg` | accepted | rc=90 | rc=90 | rc=90 | SAME |
| `ch_array.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=0 out=[ch-array 2\|] | rc=0 out=[ch-array 2\|] | SAME |
| `ch_array_withlen.sg` | refused | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=0 out=[withlen 4\|] | rc=0 out=[withlen 4\|] | SAME |
| `ch_array_withlen_send.sg` | refused | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `ch_async.sg` | accepted | rc=1  VM1999 storage: type#1510 has 1 members but 0 layout offsets | rc=4 out=[after-checkpoint\|] | rc=4 out=[after-checkpoint\|] | SAME |
| `ch_async_send.sg` | accepted | rc=1  VM1999 storage: type#1510 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `ch_close.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `ch_copy.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=0 out=[ch-copy\|] | rc=0 out=[ch-copy\|] | SAME |
| `ch_drop.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=0 out=[ch-drop\|] | rc=0 out=[ch-drop\|] | SAME |
| `ch_field.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=0 out=[ch-field 0\|] | rc=0 out=[ch-field 0\|] | SAME |
| `ch_field_overwrite.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=0 out=[ch-field-overwrite 9\|] | rc=0 out=[ch-field-overwrite 9\|] | SAME |
| `ch_field_send.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `ch_letbind.sg` | refused | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=3 out=[ch-letbind\|] | rc=3 out=[ch-letbind\|] | SAME |
| `ch_option.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=0 out=[some\|] | rc=0 out=[some\|] | SAME |
| `ch_overwrite.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=0 out=[ch-overwrite 5\|] | rc=0 out=[ch-overwrite 5\|] | SAME |
| `ch_param.sg` | accepted | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=0 out=[ch-param\|] | rc=0 out=[ch-param\|] | SAME |
| `ch_param_task.sg` | accepted | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=0 out=[param-task 4\|] | rc=0 out=[param-task 4\|] | SAME |
| `ch_param_task_send.sg` | accepted | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `ch_recv.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `ch_select.sg` | accepted | rc=1  VM1999 storage: type#1510 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `ch_send.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `ch_tryrecv.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `ch_trysend.sg` | accepted | rc=1  VM1999 storage: type#1509 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `mutex_default.sg` | accepted | rc=1  VM1999 storage: type#70 has 1 members but 0 layout offsets | rc=0 out=[mutex-default\|] | rc=0 out=[mutex-default\|] | SAME |
| `mutex_lock.sg` | accepted | rc=1  VM1999 storage: type#70 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `range_drop.sg` | refused | rc=1  VM1999 storage: type#181 has 1 members but 0 layout offsets | rc=0 out=[range-drop\|] | rc=0 out=[range-drop\|] | SAME |
| `range_iter.sg` | refused | rc=1  VM1999 storage: type#181 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid handle 0 | rc=255 out=[before\|] | DIFF |
| `range_next.sg` | refused | rc=1  VM1999 storage: type#181 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid handle 0 | rc=255 out=[before\|] | DIFF |
| `range_overwrite.sg` | refused | rc=1  VM1999 storage: type#181 has 1 members but 0 layout offsets | rc=0 out=[range-overwrite 3\|] | rc=0 out=[range-overwrite 3\|] | SAME |
| `range_slice_arr.sg` | refused | rc=1  VM1999 storage: type#181 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid handle 0 | rc=0 out=[before\|after 3\|] | DIFF |
| `range_slice_str.sg` | refused | rc=1  VM1999 storage: type#181 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid handle 0 | rc=0 out=[before\|after abcdef\|] | DIFF |
| `sel_default_arm.sg` | accepted | rc=1  VM1999 storage: type#1510 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `sel_null_then_ready.sg` | accepted | rc=1  VM1999 storage: type#1510 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 async: null channel handle | rc=1 out=[before\|] async: null channel handle | SAME |
| `sel_ready_then_null.sg` | accepted | rc=1  VM1999 storage: type#1510 has 1 members but 0 layout offsets | rc=10 out=[before\|after 10\|] | rc=10 out=[before\|after 10\|] | SAME |
| `sel_task_ready_then_null.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=10 out=[before\|after 10\|] | rc=10 out=[before\|after 10\|] | SAME |
| `spawn_null.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid task handle | rc=1 out=[before\|] invalid task handle | SAME |
| `task_array.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=0 out=[task-array 7\|] | rc=0 out=[task-array 7\|] | SAME |
| `task_async.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=4 out=[after-checkpoint\|] | rc=4 out=[after-checkpoint\|] | SAME |
| `task_async_await.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid task handle | rc=1 out=[before\|] invalid task handle | SAME |
| `task_await.sg` | refused | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid task handle | rc=1 out=[before\|] invalid task handle | SAME |
| `task_cancel.sg` | refused | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid task handle | rc=1 out=[before\|] invalid task handle | SAME |
| `task_clone.sg` | refused | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid task handle | rc=1 out=[before\|] invalid task handle | SAME |
| `task_drop.sg` | refused | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=0 out=[task-drop\|] | rc=0 out=[task-drop\|] | SAME |
| `task_field.sg` | refused | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=0 out=[task-field 0\|] | rc=0 out=[task-field 0\|] | SAME |
| `task_letbind.sg` | refused | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=4 out=[task-letbind\|] | rc=4 out=[task-letbind\|] | SAME |
| `task_overwrite.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=7 | rc=7 | SAME |
| `task_pop.sg` | refused | rc=1  VM1999 storage: type#1508 has 1 members but 0 layout offsets | rc=5 out=[task-pop\|] | rc=5 out=[task-pop\|] | SAME |
| `task_scope_drop.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=0 out=[scope-ok\|] | rc=0 out=[scope-ok\|] | SAME |
| `task_select.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid task handle | rc=1 out=[before\|] invalid task handle | SAME |
| `timeout_null.sg` | refused | rc=1  VM1999 storage: type#1507 has 1 members but 0 layout offsets | rc=1 out=[before\|] VM1203 invalid task handle | rc=1 out=[before\|] invalid task handle | SAME |
| `timeout_valid.sg` | accepted | rc=5 out=[after-timeout\|] | rc=5 out=[after-timeout\|] | rc=5 out=[after-timeout\|] | SAME |
| `to_after_checkpoint.sg` | accepted | rc=1 out=[after-checkpoint\|] VM1002 async payload owner capability mismatch | rc=90 out=[after-checkpoint\|] | rc=90 out=[after-checkpoint\|] | SAME |
| `to_after_recv.sg` | accepted | rc=90 | rc=90 | rc=90 | SAME |

56 probes; after = native on 52; differing: `range_iter.sg`, `range_next.sg`, `range_slice_arr.sg`, `range_slice_str.sg`

- **Before the fix,** 52 of the 56 probes build a handle default, and every one dies on the VM at that default with `VM1999 … 0 layout offsets`. The other four never build one: `cancelled_timeout_valid`, `timeout_valid`, `to_after_recv` and `to_after_checkpoint`. Of those, `to_after_checkpoint` is a cancelled task at `timeout` with a live target, and it died on the VM with `VM1002` where native finishes the task cancelled.
- **After the fix,** 52 of 56 agree with native. The four that differ are the `Range` uses (ledger row 2).
- **The gated build accepts 30 probes, and all 30 agree after the fix.** The 26 it refuses are every `Task` and `Range` default, `let mut ch: Channel<int>;`, and `with_len`.

## 6. `internal/vm`, base vs after, both backends, plain and `runtime_v2_pending`

For each tree, the command is the following, with `<backend>` ∈ {`vm`, `llvm`} and `<tagflag>` ∈ {none, `-tags runtime_v2_pending`}:

```
SURGE_STDLIB=<tree> SURGE_SKIP_TIMEOUT_TESTS=1 SURGE_BACKEND=<backend> go test <tagflag> ./internal/vm -count=1 -json --timeout 3000s
```

- The trees are the base (`1b122bfa`) and the after state, both as detached worktrees. The after state is a local snapshot of this branch's code (`6a33c50f`, not pushed). The branch differs from it only in comment lines at the top of `runtime_v2_default_runtime_handle_e2e_test.go` (`git diff 6a33c50f HEAD -- internal/`). The counterfactual in §5.2 also ran on that snapshot.
- `SURGE_SKIP_TIMEOUT_TESTS=1` is the Makefile's `test` default. It skips native e2e legs, so the native legs of the new rows are covered by §5.1, which runs with `=0`.
- The failing-name sets are compared by `probes/failing_sets.py`.

| leg | base failing | after failing | failing at base only | failing after only | new names after (all pass or skip) |
|---|---|---|---|---|---|
| `SURGE_BACKEND=vm`, no tag | 70 | 70 | none | none | 47 (33 pass, 14 skip) |
| `SURGE_BACKEND=vm`, `-tags runtime_v2_pending` | 113 | 113 | none | none | 47 (33 pass, 14 skip) |
| `SURGE_BACKEND=llvm`, no tag | 66 | 66 | none | none | 47 (33 pass, 14 skip) |
| `SURGE_BACKEND=llvm`, `-tags runtime_v2_pending` | 109 | 109 | none | none | 47 (33 pass, 14 skip) |

**The failing-name sets are identical at base and after, on all four legs.** The full lists are in `failing-sets.txt`; the counts include subtests. No test was left unfinished and no leg timed out. Each leg exits 1 at base as it does after, because of those same failures.

**The only difference is the new tests,** which is the intended change. The 47 new names per leg are:
- the 4 internal rows;
- the e2e parent and its 14 row parents;
- 14 `vm` legs, which pass;
- 14 `llvm` legs, which skip under `SURGE_SKIP_TIMEOUT_TESTS=1` and are covered by §5.1.

**No existing failure is fixed.** None of the base failures is this defect: every program that reaches a handle default is either refused by the D2 gate or new in this packet.

Run times per leg, base → after: VM no tag 322 s → 367 s; VM pending 1575 s → 1601 s; LLVM no tag 245 s → 250 s; LLVM pending 1510 s → 1542 s.

Also on this branch, with the probes present:
- `go build ./...` and `go vet ./internal/vm` are clean, and `gofmt` reports nothing.
- `EPIC_BASE=1b122bfa ./scripts/runtime_v2_file_size_check.sh --worktree` passes: 6 files, 0 violations.
- `go test ./internal/gatecheck ./internal/goldencheck` passes.

## 7. Valgrind

- In §5.1, every native leg ran under `valgrind --leak-check=full` (valgrind 3.22.0). There were no memcheck errors on any row, and zero bytes definitely lost on the four clean rows.
- The refusal rows exit through `_exit(1)`, so their leak totals are not asserted.

## 8. Ledger text for `docs/runtime-v2-epics/DEBT.md`

These are for the maintainer to paste into the Open Debt table. The ids are the next free ones at `1b122bfa`, where the highest is RV2-DEBT-381; renumber if another packet took them.

**Row 1: the defect this packet closes.**

```
| RV2-DEBT-382 | **THE VM COULD NOT BUILD THE DEFAULT OF A CORE RUNTIME HANDLE, WHICH THE NATIVE BACKEND MAKES A NULL.** `default::<Channel<int>>()`, a struct or `Mutex` default with a channel inside, and -- with the D2 gate bypassed -- `let mut ch: Channel<int>;`, `let mut slot: Task<int>;` and `Option.safe()` on an empty `Task<int>[]` all panicked on the VM at the default with `VM1999: storage: type#N has 1 members but 0 layout offsets`, and ran natively. The emitter answers a runtime handle's default with a null pointer (`internal/backend/llvm/emit_intrinsics_default.go:68-73`); the VM had no such case, built the handle's private member through `defaultStruct`, and the storage walk refused it (`internal/vm/storage_shape.go:210-213`). No model document defines a default or null handle: the null is defined only by the emitter and the C runtime's NULL checks (`task_from_handle`, `runtime/native/rt_async_state.c:516-522`; `channel_from_handle`, `rt_channel_lane.h:499-505`). Beside it, found by this packet's parity probes: a cancelled task that reaches `timeout` with a LIVE target panicked `VM1002: async payload owner capability mismatch` on the VM where native finishes it cancelled, because the VM resolved the target before asking whether the task was cancelled (`rt_timeout_poll` asks first, `runtime/native/rt_async_select.c:56-60`). Found by the D2 unfinished-plan census as "found on the way" (`cloud-work/d2-unfinished-plan/PLAN.md` §6, branch `cloud/d2-unfinished-plan`, measured at `7131fb2e`); the timeout half on 2026-09-25. | Closed 2026-09-25 (`cloud/vm-default-handle`, "cloud: VM default for runtime handles": the VM's default of a runtime handle is the null handle, `H == 0`, inert on drop, copy and store as NULL is natively; `taskIDFromValue` and `channelIDFromValue` refuse it with the native words, `invalid task handle` and `async: null channel handle`; `timeout` answers pending for a cancelled task before it resolves the target. 14 e2e rows give the same stdout, exit code and refusal on both backends, native under valgrind with no memcheck error and zero loss on the clean rows; with the fix reverted all 14 VM legs fail and the native legs pass. `cloud-work/vm-default-handle/PACKET.md`) | The VM (`internal/vm/intrinsic_default.go`, `internal/vm/runtime_handle_null.go`, `internal/vm/async_runtime.go`, `internal/vm/vm_dispatch_async_timeout.go`) | `TestRuntimeV2DefaultRuntimeHandleIsNullOnBothBackends` passes on both backends with `SURGE_SKIP_TIMEOUT_TESTS=0`, and the four rows of `runtime_handle_null_internal_test.go` pass. The `Task` and `Range` shapes the D2 gate refuses at `1b122bfa` become e2e rows in that test when N-DEFHANDLE lets them build. |
```

**Row 2: found on the way. Native only; not fixed here.**

```
| RV2-DEBT-383 | **A NULL `Range` IS A WHOLE RANGE TO A NATIVE SLICE AND AN UNCHECKED LOAD TO A NATIVE `for`.** The default of `Range<T>` is NULL natively (`internal/backend/llvm/emit_intrinsics_default.go:68-73`) and on the VM (RV2-DEBT-382), and once it is USED the backends part and native never refuses: `s[r]` and `a[r]` read NULL as the full range (`range_bounds`, `runtime/native/rt_string.c:239`, `rt_array.c:211`) and hand back the whole string or array, while `for i in r` and `r.next()` load the range kind through NULL in the emitted IR (`internal/backend/llvm/emit_iter.go`) and the process dies of SIGSEGV at address 0x13 with no message (valgrind: `Invalid read of size 1`). The VM refuses all four with `panic VM1203: invalid handle 0`. Unreachable from a program the D2 gate accepts at `1b122bfa`, which refuses every `Range` default; measured with the gate bypassed (`cloud-work/vm-default-handle/probes`, 2026-09-25). | Open | The owner, for the contract -- is a NULL `Range` a whole range or a refusal? -- then the native runtime (`range_bounds`) and the LLVM backend (`emit_iter.go`) | A used NULL `Range` is answered the same way, with one message, on both backends, pinned by e2e rows once a `Range` default builds (N-DEFHANDLE), or by a C stand and an emitter row before then. |
```

## 9. Unverified, and for the owner

1. **The null is not in the model.** No V2 document defines a default, uninitialized or null runtime handle, or what a used one does (§2). This packet matches the VM to the only definition in the tree, which is the native code. The owner should rule on two points:
   - that a runtime handle's default is the null handle;
   - that using a null handle is a refusal with these words.

   The ruling belongs in `docs/RUNTIME_V2.md`. N-DEFHANDLE will make these defaults reachable from source.
2. **No `Task` or `Range` e2e row is possible.** The D2 gate refuses every such default at `1b122bfa` (§4.3).
   - Their VM behaviour is pinned by the internal rows only.
   - Their two-backend parity is measured only with the scratch gate bypass (§5.3), which is not committed.
   - The bypass lets through programs whose return-origin analysis is unfinished. Those probe rows therefore say how each backend runs the program, not that the program is sound.
3. **`Range` diverges when used.** See ledger row 2. The VM keeps its own loud refusal. It copies neither native behaviour: the silent whole-range read, nor the segfault.
4. **`rt_scope_register_child` is not aligned.** Natively it returns silently when the scope is not the caller's active scope. The VM refuses a null there unconditionally. The function is non-`pub` in `core/intrinsics.sg`, and no emitter or lowering site calling it was found. Left as is.
5. **The root cause of `VM1002` was not investigated.** The `VM1002: async payload owner capability mismatch` that a cancelled task hit at `timeout` with a live target is gone because the VM no longer takes that path, which native never takes either. The fault inside the VM's timeout-task claim was not traced. Another route into that claim with a cancelled current task has not been ruled out.
6. **The timeout row is pinned single-threaded.** With `SURGE_THREADS=4` the native run races the cancel against the target's completion: 12 runs gave rc=90 ten times and rc=5 twice. That is scheduling, not a backend difference, so the row runs with `SURGE_THREADS=1`, per RV2-DEBT-188.
7. **Only the words and the exit code are compared.** The failure frames differ: the VM prints `panic VM1203: …` with a location and a backtrace, native prints `surge: fatal [PANIC]: …`.
8. **A zeroed handle cell loses the null's type.** It decodes as an untyped `nothing` (`handleValue`), not as the typed null. The two task and channel resolvers accept both spellings. A future consumer that switches on `Value.Kind` must too.
9. **`far` handles are out of scope and were not measured.** This is read from code:
   - `far T` is not a runtime handle type (`IsRuntimeHandleType` does not unwrap `KindFar`).
   - Natively, the emitter has no default for it: `emitDefaultValue` has no `KindFar` case.
   - On the VM, `defaultValue` has no `KindFar` case either, so it answers "default not implemented for type kind far".
10. **Another path into the null was not reached.** `defaultValue` is also called when a value moves out of a projection (`internal/vm/eval.go:241`), and it now yields the null there too. No program the gate accepts reaches that path with a handle: partial moves lower to `field_move` plus residual drops.
11. **N-DEFHANDLE forward check.** Of the plan's five N-DEFHANDLE programs, two have an entrypoint: `vm_async_suite/t14_loop_join.sg` and `sema/valid/ownership/for_in_reads_then_pop_drains.sg`. Neither executes a handle default at run time; the `safe()` default sits on the `nothing` path they never take. With the gate bypassed, both run the same on the unfixed VM, the fixed VM and native (`sum=6`, rc 0; rc 10). The other three are sema-only.
12. **The base suite ran under load.** It ran while other measurements used the same 4 CPUs. Timing-sensitive tests are skipped under `SURGE_SKIP_TIMEOUT_TESTS=1`, so the effect should be run time only. It was not controlled.
