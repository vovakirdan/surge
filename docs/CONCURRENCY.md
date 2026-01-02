# Surge Concurrency Model v1
[English](CONCURRENCY.md) | [Russian](CONCURRENCY.ru.md)

> **Status:** Implemented in VM (single-threaded cooperative scheduler)
> **Scope:** async/await, Task/TaskResult, spawn, channels, cancellation, timeouts
> **Out of scope:** OS-thread parallelism, signals, parallel map/reduce

---

## 1. Model at a Glance

Surge v1 uses a **single-threaded** executor with **cooperative scheduling**:

- Tasks are **state machines**, not OS threads.
- A task runs until it hits a suspension point (`await`, channel ops, `sleep`, `checkpoint`).
- `spawn` **schedules** a task for concurrent execution.
- Cancellation is **cooperative** and observed only at suspension points.

This keeps the ownership model sound without cross-thread borrow checking.

---

## 2. Task and TaskResult

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

- `Task<T>` is an opaque handle to a state machine.
- `.await()` **consumes** `own Task<T>` and returns `TaskResult<T>`.
- Use `task.clone()` if you need multiple handles.
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

## 3. async Functions and async Blocks

```sg
async fn fetch_user(id: int) -> User {
    let raw = http_get("/users/" + id).await();
    return parse(raw);
}

let t: Task<User> = fetch_user(42);
```

- `async fn` returns `Task<T>` immediately; it does not run until awaited or spawned.
- `async { ... }` creates an anonymous `Task<T>` from a block.

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

## 4. spawn

```sg
spawn expr
```

Rules:

- `expr` must be a `Task<T>` (async function call or async block).
- `spawn` schedules the task and returns a `Task<T>` handle.
- Only `own` values may cross the spawn boundary.
- `@nosend` types are rejected in spawn (`SemaNosendInSpawn`).
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

---

## 5. await

`.await()` is a **method call** and returns `TaskResult<T>`:

```sg
compare fetch_user(42).await() {
    Success(user) => print(user.name);
    Cancelled() => print("cancelled");
}
```

Rules:

- Allowed inside `async` functions/blocks and `@entrypoint` functions.
- Rejected in plain sync functions (`SemaIntrinsicBadContext`).
- `await` inside loops is currently **not supported** (MIR lowering rejects it).

---

## 6. Structured Concurrency (Scopes)

Surge enforces structured concurrency in sema:

- Spawned tasks must be **awaited or returned**.
- Leaking a task out of scope produces errors:
  - `SemaTaskNotAwaited` (3107)
  - `SemaTaskEscapesScope` (3108)
  - `SemaTaskLeakInAsync` (3109)
  - `SemaTaskLifetimeError` (3110)

At runtime, each async function/block creates a scope. On scope exit, the
runtime joins all children before completing. Returning a `Task<T>` transfers
responsibility to the caller.

---

## 7. Cancellation, Timeouts, and Yielding

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
- Timers run in virtual time by default. Real-time mode can be enabled via
  `surge run --real-time`.

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

## 8. Channels

`Channel<T>` is a typed FIFO handle (copyable):

```sg
let ch = make_channel::<int>(16);
ch.send(42);
let v = ch.recv();
```

API (core intrinsics):

- `make_channel<T>(capacity: uint) -> own Channel<T>`
- `Channel<T>::new(capacity: uint) -> own Channel<T>`
- `send(self: &Channel<T>, value: own T) -> nothing` (blocking)
- `recv(self: &Channel<T>) -> Option<T>` (blocking)
- `try_send(self: &Channel<T>, value: own T) -> bool`
- `try_recv(self: &Channel<T>) -> Option<T>`
- `close(self: &Channel<T>) -> nothing`

Notes:

- `send`/`recv` are suspension points in async code.
- `recv` returns `nothing` when the channel is closed and empty.
- Sending to a closed channel is a runtime error.
- `@nosend` values cannot be sent through channels (`SemaChannelNosendValue`).

---

## 9. Scheduler Fairness (v1)

Fairness is guaranteed for **Ready** tasks in the single-thread executor under
cooperative scheduling:

- **F1 (round-robin for Ready tasks):** with a finite ready set, each Ready task
  is polled again after at most `N-1` polls of other Ready tasks (where `N` is
  the current ready-set size).
- **F2 (one poll per step):** each scheduler step performs exactly one poll; a
  yielded task is requeued to the back of the ready queue, and a parked task is
  not requeued.
- **F3 (determinism):** in default mode, ordering is FIFO by spawn/wake order;
  in fuzz mode, the choice is randomized but Ready tasks remain eligible and
  cannot be starved.

This guarantee only applies to tasks that reach suspension points (`await`,
`checkpoint`, channel ops, `sleep`). A CPU-bound loop without suspension can
still monopolize execution.

---

## 10. Limitations (v1)

- Single-threaded runtime; no true parallelism.
- `parallel map/reduce` and `signal` are reserved keywords (not supported).
- CPU-bound tasks that never suspend can monopolize execution.

See `docs/PARALLEL.md` for the status of parallel features.
