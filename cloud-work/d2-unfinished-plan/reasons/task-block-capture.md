# an `async` or `blocking` block that captures a value which can hold a reference, a storage loan or a task needs its capture origin

| Measure | Value |
|---|---:|
| Programs carrying it | 3 |
| Programs where it is the only root reason | 2 |
| Rows | 3 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_task_blocks.go:59-63`**, with the text defined at `:15`. A capture of an `async` or `blocking` block whose type is not crossing-inert, meaning it can hold a reference, a storage loan or a task.

## Corpus examples

1. **`vm_async_suite/t22_select_wait_recv.sg:5`**: `let prod = spawn async { sleep(5).await(); ch.send(7); ret 0; }`, which captures `ch: Channel<int>` by value. This is its only root. `t23_select_wait_timer.sg:5` is the same.
2. **`sema/valid/concurrency/task_created_in_current_scope.sg:18`**: `return async { slot = async { ret 42; }; ... }`, which captures `slot: Task<int>`.

## Sound transfer and size

- **N-CHAN-CAPTURE, S, prototype 23 lines, measured.** A capture by an `async` block of a reference-counted handle whose payload types are all crossing-inert is admitted. The block holds its own counted reference to the channel. Only `Channel<T>` is such a handle today (`internal/types/refcounted_handle.go:21`), and a copy of the handle may live in another frame (`docs/RUNTIME_V2.md:771-775`).
- **What it frees.** `t22_select_wait_recv.sg` and `t23_select_wait_timer.sg`. Both then run to their golden `.out` on both backends.
- **Model, code and run agree** for the forms this relies on (measured):
  - Probe `s01`: a channel captured by a block that outlives the frame that made the channel.
  - Probe `s04`: a channel parameter captured and parked through `*out`.
  - Both print `v=7` and exit 7 on the VM and on LLVM, and valgrind reports 0 errors natively.
- **These stay refused:**
  - `&Channel<int>` captured, canary `f09`.
  - A `blocking` capture, refused by SEM3168 (`f10`).
  - `Channel<Task<_>>`, because a Task is not crossing-inert (read from code).
- **The Task capture in `task_created_in_current_scope.sg` is owner question 2.**

## Unsoundness risk and fences

DEBT.md:229 names this capture row as the fence of R-b(body), a body that captures a reference parameter, a value carrying one, or a by-value array parameter, with its handle parked. The ledger says: "a block's capture must hold no reference, no storage loan and no task; row `reference_capture_stays_refused` of `TestAnalyzeTaskBlocks` ... removed by whoever relaxes that capture test; first: the TC-13 pin for a body's parameter captures". It also lists "a channel" among the captures that keep a named refusal.

- **What the prototype relaxes.** Only by-value counted handles with inert payloads. No reference, loan or array is admitted.
- **Tests under the prototype (measured).** `TestAnalyzeTaskBlocks`, including `reference_capture_stays_refused`, passes, as do all `TestTaskCheck*` and `TestH2Tripwire*` tests.
- **What the ledger still requires.** Because it names a prerequisite for relaxing this test, the coordinator must agree that a by-value counted handle is outside R-b(body), and must re-measure probe P-PARK-BODY, before the packet lands.

## DEBT-365 and DEBT-368

R-b(body), N-TASK-23/24's capture row.

## Owner decision

- **`task_created_in_current_scope.sg`:** yes, see `owner-q2-task-values.md`.
- **N-CHAN-CAPTURE:** a sequencing sign-off, as above. The transfer does not change the language.
