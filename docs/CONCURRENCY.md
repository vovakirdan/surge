# Surge Concurrency Model v1
[English](CONCURRENCY.md) | [Russian](CONCURRENCY.ru.md)

> **Status:** Native/LLVM backends run the MT executor; VM is single-threaded for correctness and diagnostics. `blocking { ... }` is supported only on native/LLVM. `strict::nonblocking` is not implemented yet (future work).
> **Scope:** async/await, Task/TaskResult, spawn, channels, cancellation, timeouts, MT executor modes, blocking { }, strict::nonblocking (future)
> **Out of scope:** data-parallel keywords (`parallel map/reduce`), `signal`

---

## 1. Model at a Glance

Surge uses a stackless Task model with cooperative scheduling. The executor may be
single-worker or multi-worker; this is a runtime property and does not change the
Task abstraction.

- Tasks are **stackless state machines**, not OS threads.
- A task runs until it hits a suspension point (`await`, channel ops, `sleep`, `checkpoint`).
- Task-context waiting is implemented via **park/unpark**; the worker keeps running other tasks.
- `spawn` schedules a task for concurrent execution.
- Cancellation is **cooperative** and observed only at suspension points.
- MT execution uses multiple worker threads; a task is never polled concurrently by multiple workers.
- OS-blocking is not allowed on executor worker threads; use `blocking { ... }` for OS-blocking work.
- If workers > 1 and hardware has multiple cores, true parallelism happens naturally (no separate "MC" feature).
- Backend reality: native/LLVM use the MT executor; VM is single-worker and focuses on correctness/diagnostics.

This keeps the ownership model sound across workers without cross-worker-thread borrow checking.

---

## 2. Terminology: Waiting vs OS-Blocking

Surge distinguishes task-level waiting from OS-thread blocking:

- **May wait**: an operation that can suspend a task (park/unpark). In task
  context, waiting means the task parks and the worker keeps running other tasks.
- **OS-blocking**: an operation that blocks the OS thread (sync file I/O, system
  mutexes, foreign calls). OS-blocking is forbidden on executor worker threads.
- **Task parking**: the executor stops polling a task until a wakeup event
  (channel ready, timer fires, task completes).
- **Thread blocking**: the OS scheduler stops a worker thread; other tasks on
  that worker cannot run.
- **Async waiting vs blocking pool execution**: async waiting parks a task on
  the executor; blocking pool execution runs code on dedicated blocking threads
  and returns a `Task<T>` that is awaited like any other task.

In core APIs, some methods are labeled "blocking" to mean "may wait"
(e.g., `Channel.send`/`recv`); this is task parking, not OS-blocking.

All task-context waits (channels, timers, joins, select) are implemented via
park/unpark in the runtime.

---

## 3. Task and TaskResult

Core definitions live in `core/intrinsics.sg`:

```sg
pub type Task<T> = { __opaque: int };

pub tag Cancelled();
pub type TaskResult<T> = Success(T) | Cancelled;

extern<Task<T>> {
    @intrinsic pub fn clone(self: &Task<T>) -> Task<T>;
    @intrinsic pub fn cancel(self: &Task<T>) -> nothing;
    @intrinsic pub fn await(self: own Task<T>) -> TaskResult<T>;
}
```

Key points:

- `Task<T>` is an opaque handle to a **stackless state machine**.
- `.await()` **consumes** `own Task<T>` and returns `TaskResult<T>`.
- Use `handle.clone()` if you need multiple handles.
- `cancel()` is best-effort; tasks observe cancellation at suspension points.

Example:

```sg
let t = spawn fetch_user(42);
compare t.await() {
    Success(user) => print(user.name);
    Cancelled() => print("cancelled");
}
```

---

## 4. async Functions and async Blocks

```sg
async fn fetch_user(id: int) -> User {
    let raw = http_get("/users/" + id).await();
    return parse(raw);
}

let t: Task<User> = fetch_user(42);
```

- `async fn` returns `Task<T>` immediately; it does not run until awaited or scheduled with `spawn`.
- `async { ... }` creates an anonymous `Task<T>` from a block.
- `blocking { ... }` is also a Task-producing block (see "Blocking Scope").

`@failfast` is allowed on **async functions** and **async blocks**:

```sg
@failfast
async fn pipeline() -> nothing {
    let a = spawn step_a();
    let b = spawn step_b();

    compare a.await() {
        Success(_) => nothing;
        Cancelled() => return;
    };
    compare b.await() {
        Success(_) => nothing;
        Cancelled() => return;
    };
}
```

Failfast means: if a child task completes with `Cancelled`, the scope cancels
remaining children and the parent returns `Cancelled`.

---

## 5. spawn

```sg
spawn expr
@local spawn expr
```

Rules:

- `expr` must be a `Task<T>` (async function call, async block, or `blocking { ... }` block).
- `spawn` schedules the task and returns a `Task<T>` handle.
- Only `own` values may cross the task boundary.
- `spawn` requires sendable captures; `@nosend` types are rejected (`SemaNosendInSpawn`).
- `@local spawn` allows `@nosend` captures, but the resulting task handle is local (not sendable):
  it cannot be captured by `spawn`, sent through channels, or returned from a function.
- `spawn checkpoint()` is warned as useless (`SemaSpawnCheckpointUseless`).

Example:

```sg
async fn work(x: int) -> int { return x * 2; }

let t1 = spawn work(10);
let t2 = spawn work(20);

compare t1.await() {
    Success(v) => print("t1=" + (v to string));
    Cancelled() => print("t1 cancelled");
}
compare t2.await() {
    Success(v) => print("t2=" + (v to string));
    Cancelled() => print("t2 cancelled");
}
```

MT-ready invariants:

- `spawn` captures must be Sendable; `@local spawn` allows `@nosend`.
- Local task handles cannot cross sendable boundaries (capture in `spawn`, return, channel send).
- `Task<T>` is SuspendSafe across `await` (containers are SuspendSafe if their elements are).
- Task containers must be drained (`pop` + `await`) before scope exit.

---

## 6. await

`.await()` is a **method call** and returns `TaskResult<T>`:

```sg
compare fetch_user(42).await() {
    Success(user) => print("ok");
    Cancelled() => print("cancelled");
}
```

Rules:

- Allowed inside `async` functions/blocks and `@entrypoint` functions.
- Rejected in plain sync functions (`SemaIntrinsicBadContext`).
- `await` inside loops is supported.

---

## 7. Structured Concurrency (Scopes)

Surge enforces structured concurrency in sema:

- Spawned tasks must be **awaited or returned**.
- Leaking a task out of scope produces errors:
  - `SemaTaskNotAwaited` (3107)
  - `SemaTaskEscapesScope` (3108)
  - `SemaTaskLeakInAsync` (3109)
  - `SemaTaskLifetimeError` (3110)

At runtime, each async function/block creates a scope. On scope exit, the
runtime joins all children before completing. Returning a `Task<T>` transfers
responsibility to the caller. This applies to tasks created by `async { ... }`,
async functions, and `blocking { ... }`.

---

## 8. Cancellation, Timeouts, and Yielding

Intrinsics:

```sg
@intrinsic pub fn checkpoint() -> Task<nothing>;
@intrinsic pub fn sleep(ms: uint) -> Task<nothing>;
@intrinsic pub fn timeout<T>(t: Task<T>, ms: uint) -> TaskResult<T>;
```

Notes:

- `checkpoint().await()` yields to the scheduler and checks cancellation.
- `sleep(ms).await()` suspends for `ms` (virtual time by default).
- `timeout(t, ms)` waits up to `ms` and returns `Success` or `Cancelled`.
  It cancels the target on deadline.
- VM timers run in virtual time by default. Real-time mode is available only in
  the VM (`surge run --backend=vm --real-time`).
- Native/LLVM timers use executor time (virtual) and do not currently expose a
  real-time switch.
- Cancellation is cooperative and does not preempt OS-blocking calls (see "Blocking Scope").

Example:

```sg
let t = spawn slow_call();
compare timeout(t, 500:uint) {
    Success(v) => print("done " + (v to string));
    Cancelled() => print("timed out");
}
```

### Select and Race

`select` waits on multiple awaitable operations (task `.await()`, channel
`recv`/`send`, `sleep`, `timeout`) and returns the chosen arm result.

Rules:

- Arms are checked top-to-bottom; the first ready arm wins (deterministic tie-break).
- If `default` is present and no arms are ready, `default` executes immediately.
- Without `default`, the task parks until an arm becomes ready.
- `select` does not cancel losing arms.

`race` shares the same syntax and selection rules, but **cancels losing Task arms**
(non-task arms are not cancelled).

Example:

```sg
let v = select {
    ch.recv() => 1;
    sleep(10).await() => 2;
    default => 0;
};

let r = race {
    t1.await() => 1;
    t2.await() => 2;
};
```

---

## 9. Channels

`Channel<T>` is a typed FIFO handle (copyable):

```sg
let ch = make_channel::<int>(16);
ch.send(42);
let v = ch.recv();
```

API (core intrinsics):

- `make_channel<T>(capacity: uint) -> own Channel<T>`
- `Channel<T>::new(capacity: uint) -> own Channel<T>`
- `send(self: &Channel<T>, value: own T) -> nothing` (may wait)
- `recv(self: &Channel<T>) -> Option<T>` (may wait)
- `try_send(self: &Channel<T>, value: own T) -> bool`
- `try_recv(self: &Channel<T>) -> Option<T>`
- `close(self: &Channel<T>) -> nothing` (may wait)

Notes:

- `send`/`recv`/`close` are suspension points in async code (task parking).
- `recv` returns `nothing` when the channel is closed and empty.
- Sending to a closed channel is a runtime error.
- `@nosend` values cannot be sent through channels (`SemaChannelNosendValue`).

---

## 10. Executor Model and Scheduling

### 10.1 Executor Modes (runtime property)

- The executor may run with a single worker or multiple workers.
- Native/LLVM backends use the MT executor; VM is single-worker.
- MT is a runtime configuration; the Task abstraction and language semantics do not change.
- If workers > 1 and hardware has multiple cores, tasks can run in parallel.
- Worker count is configured at runtime (e.g., `SURGE_THREADS`); the default is
  based on CPU count.

### 10.1.1 VM backend vs native/LLVM

- The VM exists for correctness, diagnostics, and deterministic execution during development.
- VM scheduling is single-worker and may use virtual time or fuzzed scheduling; native/LLVM are MT and use real time timers.
- Some features are backend-specific: `blocking { ... }` is supported only on native/LLVM; the VM rejects it.

### 10.2 Core MT invariants

- A task is never polled concurrently by multiple workers.
- Executor worker threads must not perform OS-blocking operations.
- Suspension points park a task until a wakeup event, releasing the worker.

### 10.3 Scheduling modes

- **Parallel mode:** multiple workers with nondeterministic interleavings; only
  program synchronization defines ordering.
- **Seeded (reproducible) mode:** scheduling decisions are deterministic given
  the same seed and the same external event order. This is best-effort.
  Limits include external I/O completion order, system time, OS scheduling of
  blocking pool threads, FFI, and any other nondeterministic inputs.
- **VM mode:** single-worker deterministic FIFO by default; optional fuzzed
  scheduling with a fixed seed for reproducible interleavings (VM only).

### 10.4 Testing policy (parity vs MT)

- VM/LLVM parity tests force `threads=1` to compare backend semantics, not scheduler interleavings.
- MT correctness is validated by a separate MT test suite that runs with workers > 1.

### 10.5 Fairness and CPU-bound work

Fairness is guaranteed for **Ready** tasks in single-worker mode under
cooperative scheduling:

- **F1 (round-robin for Ready tasks):** with a finite ready set, each Ready task
  is polled again after at most `N-1` polls of other Ready tasks (where `N` is
  the current ready-set size).
- **F2 (one poll per step):** each scheduler step performs exactly one poll; a
  yielded task is requeued to the back of the ready queue, and a parked task is
  not requeued.
- **F3 (determinism):** in default mode, ordering is FIFO by task/wake order; in
  seeded mode, the choice is deterministic given the seed and event order.

In parallel mode, there is no global FIFO ordering and fairness is best-effort
across workers.

Tight CPU loops without suspension points can starve other tasks. The runtime
does **not** insert yields or preempt tasks by default. Use `checkpoint().await()`
to cooperate. Safepoint checks / preemption may exist only as an explicit,
future opt-in mode and are not guaranteed unless enabled.

---

## 11. Blocking Scope (`blocking { ... }`)

`blocking { ... }` is an explicit block expression for OS-blocking work. It
returns `Task<T>` and runs on a dedicated blocking pool, not on executor workers.

Rules:

- `blocking { ... }` executes outside executor worker threads.
- It returns `Task<T>` and is awaited via `.await()` (or scheduled with `spawn`).
- Like `async { ... }`, it does not execute until scheduled or awaited.
- Cancellation is best-effort; OS-blocking calls may not be preemptable.
- Overuse of `blocking { ... }` is a performance risk (thread saturation,
  scheduling overhead, and latency).
- Backend support: native/LLVM only. The VM backend rejects `blocking { ... }`
  because it is single-threaded and has no blocking pool.
- Blocking pool size is configured at runtime (default = worker count; override
  with `SURGE_BLOCKING_THREADS`).

Example:

```sg
let t = blocking {
    return read_file(path);
};

compare t.await() {
    Success(data) => print(data);
    Cancelled() => print("cancelled");
}
```

---

## 12. Future: Strict Nonblocking Mode (`pragma strict::nonblocking`)

`pragma strict::nonblocking` is **reserved** and has no effect today.

Current behavior:

- `@nonblocking` is enforced on functions (see `docs/ATTRIBUTES.md`).
- The compiler rejects calls to known may-wait operations from `@nonblocking`
  functions (e.g., `Mutex.lock`, `Condition.wait`, `Semaphore.acquire`,
  `Channel.send`/`recv`/`close`).
- `@waits_on("field")` is enforced and conflicts with `@nonblocking`.

If/when `strict::nonblocking` is implemented, it will apply the same checks
transitively to task-context code. This is future work only.

---

## 13. Guarantees and Non-Guarantees

Guarantees (contract):

- A task is never polled concurrently by multiple workers.
- Structured concurrency: tasks must be awaited or returned; scopes own their tasks.
- Suspension points park tasks instead of blocking executor workers.
- Cancellation is cooperative and observed at suspension points.
- OS-blocking is forbidden on executor workers; OS-blocking must be isolated in `blocking { ... }`.

Non-guarantees (explicitly not promised):

- Determinism in parallel mode (scheduling order is nondeterministic).
- Global FIFO ordering or fairness across multiple workers.
- Automatic preemption or yield insertion in CPU-bound loops.
- Preemption of OS-blocking calls (cancellation is best-effort).
- Full reproducibility in seeded mode when external events differ.

---

## 14. Limitations (v1)

- The VM backend is single-worker today; MT execution is a runtime option for other backends.
- `blocking { ... }` is not supported in the VM backend.
- `parallel map/reduce` and `signal` remain reserved keywords (not supported).

See `docs/PARALLEL.md` for the status of parallel features.

---

## 15. Implementation Notes / Open Observations (Non-Normative)

- The term "blocking" is already used in core APIs to mean "may wait" (task
  parking), which can be confused with OS-blocking despite the distinction.
- `@nonblocking` currently treats channel ops (`send`/`recv`/`close`) as blocking,
  which can reject common async patterns in `@nonblocking` code; clearer guidance
  may be needed if `strict::nonblocking` is implemented later.
- Seeded scheduling depends on external event order; reproducibility boundaries
  may need more explicit guidance in test tooling.
