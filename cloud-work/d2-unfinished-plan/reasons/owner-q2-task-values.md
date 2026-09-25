# Owner question 2: Task values whose borrows return-origin cannot see

Everything here was measured unless it says "(read from code)".

## The four programs

| Program | Row | What the Task does |
|---|---|---|
| `sema/valid/fn_type_async.sg:11` | opaque-result-classification at `let t = f(1, 2);` | A Task comes from calling an `async fn` through a function value, `f: AsyncAdder`. |
| `sema/valid/ret_async_body.sg:23` | "an `async` or `blocking` block whose value can hold ... a task needs its payload origin" | The block's value is a `Task<int>`, so the function returns `Task<Task<int>>`. |
| `sema/valid/concurrency/task_created_in_current_scope.sg:18` | task-block-capture | An `async` block assigns to `slot`, a `Task<int>` binding declared outside it, and the block is returned. |
| `sema/valid/clone_semantics/task_clone_uninstantiated_generic.sg:9` | opaque-call-effects at `return handle.clone();` | A `Task<T>` is cloned through `&Task<T>` inside a generic function nobody instantiates. |

In plain words, each program makes or moves a Task in a place where return-origin would have to know what that Task borrows. Return-origin does not own that fact. The task check owns what running tasks borrow (owner ruling 2026-09-15, cited in DEBT.md:229).

## The invariants in the way

- **DEBT.md:229, residual R-i.** A task handed out as the payload of an awaited task. "fence: NoBorrowedState on the result of `await` and of `timeout`, unsupported for a Task payload ... removed by whoever certifies a Task payload out of a join, and also by Wave D4b ... so D4b may land only after R-i is closed, and the row `borrowing_task_payload_stays_refused` is the tripwire that turns red if it lands first".
- **`internal/types/refcounted_handle.go:21-22`.** "Only `Channel<T>` qualifies today. `Task<T>` joins when its handle count and entitlement are settled (Wave D4b)".
- **`docs/RUNTIME_V2.md:1118-1121`.** "The handle's move state and the referent's pin state are DIFFERENT facts ... referent safety is decided by definite completion, not by the syntactic presence of a join."

## What running it shows

This is a scratch probe with the gate bypassed; the programs themselves have no entry point. The probe is the `inner_creation_in_outer_binding` function of `task_created_in_current_scope.sg`, called from a sync `@entrypoint` that runs other code and then awaits the returned Task.

- **LLVM** prints `v=42` and exits 42. valgrind reports 0 errors.
- **The VM panics before the Task exists**, with `panic VM1999: storage: type#1507 has 1 members but 0 layout offsets` at `let mut slot: Task<int>;`. A binding `let mut ch: Channel<int>;` with no initializer panics the same way, while `let mut n: int;` does not. The cause is that the VM builds a runtime handle's default from its layout (`internal/vm/intrinsic_default.go:145-162`), while LLVM uses a null pointer (`internal/backend/llvm/emit_intrinsics_default.go:67-73`) (read from code). This VM defect is separate from this question and is listed in PLAN.md.

## Options

| Option | Frees | Cost | Risk |
|---|---:|---|---|
| A. Wait for R-i to close and D4b to mark `Task` a counted handle, then extend N-CHAN-CAPTURE's counted-handle rule to `Task` | 4, later | no return-origin work now | The programs stay unfinished until D4b. |
| B. The task check publishes a per-Task fact, "borrows nothing from any frame", and return-origin reads it at these four sites | 4 | M to L in the task check, S in return-origin | A new contract between the two analyses, which must stay in step with R-i. |
| C. Refuse the four forms by a named rule and move the programs to `invalid/` | 0 left unfinished | S | Narrows the language. `LANGUAGE.md` presents all four forms as valid. |

## The question

For a Task that return-origin cannot see into (made through a function value, carried as a task's payload, captured by an `async` block, or cloned through `&Task<T>`), should return-origin wait for R-i and D4b (A), read a new per-task fact from the task check (B), or refuse by a named rule (C)?
