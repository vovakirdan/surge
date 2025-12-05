# Surge Concurrency Model v1

> **Status:** Draft for implementation
> **Scope:** Async/await, tasks, channels, cancellation
> **Out of scope (v2+):** True parallelism (multi-thread), signals, parallel map/reduce

---

## 1. Design Principles

* **Simple syntax:** `spawn` and `.await()` — that's it
* **Zero-cost:** Stackless coroutines compile to state machines
* **Structured concurrency:** Tasks cannot outlive their scope
* **Explicit over implicit:** No auto-cancel, no hidden magic
* **Ownership-aware:** Only `own T` crosses task boundaries

---

## 2. Runtime Model

### 2.1. Execution Model

Surge v1 uses **single-threaded cooperative scheduling**:

```
┌─────────────────────────────────────────┐
│            Event Loop (single thread)    │
├─────────────────────────────────────────┤
│  ┌─────┐  ┌─────┐  ┌─────┐  ┌─────┐    │
│  │Task1│  │Task2│  │Task3│  │Task4│    │
│  └──┬──┘  └──┬──┘  └──┬──┘  └──┬──┘    │
│     │        │        │        │        │
│     └────────┴────────┴────────┘        │
│              Ready Queue                 │
└─────────────────────────────────────────┘
```

* All tasks run on a single OS thread
* Tasks yield control at `.await()` points
* No preemption — long CPU work blocks other tasks
* Use `checkpoint()` for CPU-bound loops

### 2.2. Stackless Coroutines

Each `async fn` compiles to a state machine:

```sg
// Source
async fn fetch(url: string) -> Data {
    let conn = connect(url).await();
    let data = read(conn).await();
    return data;
}

// Conceptually compiles to:
// enum FetchState { Start, AfterConnect(Conn), AfterRead(Data), Done }
// + step() function that advances state
```

**Benefits:**
- No heap allocation for task stack
- Predictable memory usage
- Inlinable by optimizer

---

## 3. Core Syntax

### 3.1. Async Functions

```sg
async fn name(params) -> RetType {
    // can use .await() inside
}
```

* `async fn` returns `Task<RetType>` implicitly
* Caller must `.await()` or `spawn` the result
* Cannot be called from sync context without `spawn`

```sg
async fn fetch_user(id: int) -> Erring<User, Error> {
    let response = http_get("/users/" + id).await();
    return parse_user(response);
}

// Usage
async fn main() {
    let user = fetch_user(42).await();  // Direct await
    let task = spawn fetch_user(42);     // Background task
}
```

### 3.2. Spawn

```sg
spawn expr
```

* `expr` must be of type `Task<T>` (result of async fn call)
* Returns `Task<T>` — a handle to the spawned task
* Ownership of captured values transfers to the task

```sg
let url: own string = "https://api.example.com";
let task: Task<Data> = spawn fetch(url);  // url moved into task
// url is no longer valid here
```

### 3.3. Await

```sg
task.await() -> T
```

* Method on `Task<T>`, returns `T`
* Suspends current task until `task` completes
* If `task` was cancelled, returns `Cancelled` error (see §6)

```sg
let task = spawn compute_heavy();
// ... do other work ...
let result = task.await();  // Wait for completion
```

**Chaining:**

```sg
// Each .await() unwraps the Task
let data = fetch(url).await();           // Task<Data> -> Data
let parsed = parse(data).await();         // Task<Parsed> -> Parsed

// Or in pipeline (if each returns Task)
let result = step1()
    .await()
    .then(step2)
    .await()
    .then(step3)
    .await();
```

### 3.4. Async Blocks

```sg
async {
    // statements
}
```

* Creates an anonymous `Task<T>` where `T` is the block's result type
* All tasks spawned inside are **owned by the block**
* Block waits for all spawned tasks before completing

```sg
async fn process_all(urls: string[]) -> Data[] {
    async {
        let mut tasks: Task<Data>[] = [];
        
        for url in urls {
            tasks.push(spawn fetch(url));
        }
        
        let mut results: Data[] = [];
        for task in tasks {
            results.push(task.await());
        }
        
        return results;
    }
}
```

---

## 4. Structured Concurrency

### 4.1. Scope Rules

**Rule 1:** Tasks cannot outlive their spawning scope.

```sg
async {
    let t1 = spawn work1();
    let t2 = spawn work2();
}  // Implicit: waits for t1 and t2 here
// Both tasks guaranteed complete
```

**Rule 2:** Returning a Task from async block is allowed (transfers ownership).

```sg
fn start_background() -> Task<int> {
    return spawn compute();  // Task escapes, caller owns it
}
```

**Rule 3:** Tasks spawned in `async fn` are scoped to that function.

```sg
async fn example() {
    let t = spawn work();
    // Implicit await before return
}  // t is awaited here
```

### 4.2. Nested Scopes

```sg
async fn complex() {
    async {
        let t1 = spawn phase1();
        let t2 = spawn phase1_helper();
    }  // Wait for t1, t2
    
    // Phase 1 complete, start phase 2
    async {
        let t3 = spawn phase2();
    }  // Wait for t3
}
```

### 4.3. Error Handling in Scopes

Default behavior: **Continue execution, explicit cancel**

```sg
async {
    let t1 = spawn may_fail();
    let t2 = spawn also_runs();
    
    let r1 = t1.await();
    compare r1 {
        Success(v) => process(v);
        err => {
            t2.cancel();      // Explicit cancel
            return err;       // Propagate error
        }
    }
    
    let r2 = t2.await();
    // ...
}
```

### 4.4. @failfast Attribute

For fail-fast behavior without boilerplate:

```sg
@failfast
async {
    let t1 = spawn may_fail();
    let t2 = spawn also_runs();
    
    // If any task returns Error:
    // 1. All sibling tasks are cancelled
    // 2. Block returns that Error immediately
    
    let r1 = t1.await();  // If Error -> cancel t2, return Error
    let r2 = t2.await();
    
    return Success((r1, r2));
}
```

**Semantics of @failfast:**
- Applies to `async` blocks only
- On first `Error` from any `.await()`:
  - All other spawned tasks in block receive cancel signal
  - Block immediately returns the error
- Successful tasks are not affected

---

## 5. Channels

### 5.1. Channel Creation

```sg
make_channel<T>(capacity: uint) -> own channel<T>
```

* `capacity = 0`: Rendezvous channel (synchronous handoff)
* `capacity > 0`: Buffered channel

```sg
let sync_ch = make_channel<int>(0);   // Sender blocks until receiver ready
let buf_ch = make_channel<int>(100);  // Buffer up to 100 items
```

### 5.2. Channel Operations

```sg
// Blocking operations (suspend coroutine, not OS thread)
send(ch: &channel<T>, value: own T) -> nothing;    // Blocks if full
recv(ch: &channel<T>) -> T?;                       // Blocks if empty, nothing if closed

// Non-blocking operations
try_send(ch: &channel<T>, value: own T) -> bool;   // false if full
try_recv(ch: &channel<T>) -> T?;                   // nothing if empty

// Lifecycle
close(ch: &channel<T>) -> nothing;                 // Signal no more values
```

**Ownership:** Values are moved into and out of channels.

```sg
let ch = make_channel<string>(10);

let msg: own string = "hello";
send(&ch, msg);     // msg moved into channel
// msg invalid here

let received: string? = recv(&ch);  // Ownership transferred out
```

### 5.3. Channel Patterns

**Producer-Consumer:**

```sg
async fn producer(ch: &channel<int>) {
    for i in 0..100 {
        send(ch, i);
    }
    close(ch);
}

async fn consumer(ch: &channel<int>) {
    while true {
        compare recv(ch) {
            Some(value) => process(value);
            nothing => break;  // Channel closed
        }
    }
}

async fn main() {
    let ch = make_channel<int>(10);
    
    async {
        spawn producer(&ch);
        spawn consumer(&ch);
    }
}
```

**Fan-out:**

```sg
async fn distribute(input: &channel<Work>, workers: int) {
    let mut handles: Task<nothing>[] = [];
    
    for i in 0..workers {
        handles.push(spawn worker(input));
    }
    
    for h in handles {
        h.await();
    }
}
```

### 5.4. Select (Future Extension)

> **Note:** `select` / `choose` for multiplexing channels is deferred to v1.1.
> For v1, use separate tasks per channel.

---

## 6. Cancellation

### 6.1. Cancellation Model

Surge uses **interrupt-at-await** cancellation:

1. `task.cancel()` sets a cancellation flag
2. At the next `.await()` point, the task checks the flag
3. If cancelled, `.await()` returns immediately with `Cancelled`
4. The task can handle or propagate `Cancelled`

```sg
let task = spawn long_work();

// Later...
task.cancel();  // Request cancellation

// Inside long_work, at next .await():
// - Await returns Cancelled instead of waiting
// - Task can clean up and exit
```

### 6.2. Cancellation Type

```sg
tag Cancelled();

// Task<T>.await() actually returns:
// - T if completed successfully  
// - Or propagates Erring if task returned error
// - Or Cancelled if task was cancelled
```

**Handling cancellation:**

```sg
async fn worker() {
    let result = some_io().await();
    compare result {
        Success(data) => process(data);
        Cancelled() => {
            cleanup();
            return Cancelled();  // Propagate
        }
        err => return err;
    }
}
```

### 6.3. Checkpoint for CPU-bound Work

Long CPU-bound work doesn't yield. Use `checkpoint()`:

```sg
async fn heavy_compute() -> int {
    let mut sum = 0;
    for i in 0..10_000_000 {
        sum = sum + expensive(i);
        
        if (i % 1000 == 0) {
            checkpoint().await();  // Yield + check cancellation
        }
    }
    return sum;
}
```

* `checkpoint()` returns `Task<nothing>`
* Yields to scheduler, allows other tasks to run
* Checks cancellation flag, returns `Cancelled` if set
* No-op if not in async context (for code reuse)

### 6.4. Cancellation Propagation

When a parent scope exits early, child tasks are cancelled:

```sg
async {
    let t1 = spawn work1();
    let t2 = spawn work2();
    
    return early_result;  // Before awaiting t1, t2
    
    // Implicit: t1.cancel(); t2.cancel();
    // Then wait for them to acknowledge cancellation
}
```

---

## 7. Task API

### 7.1. Task<T> Type

```sg
// Task is an opaque handle to a spawned coroutine
// Cannot be constructed directly, only via spawn

extern<Task<T>> {
    // Wait for completion, returns result or Cancelled
    fn await(self: own Task<T>) -> T;
    
    // Request cancellation (cooperative)
    fn cancel(self: &Task<T>) -> nothing;
    
    // Check if completed (non-blocking)
    fn is_done(self: &Task<T>) -> bool;
    
    // Check if cancellation was requested
    fn is_cancelled(self: &Task<T>) -> bool;
}
```

### 7.2. Utility Functions

```sg
// Await multiple tasks, collect results
fn await_all<T>(tasks: Task<T>[]) -> T[] {
    let mut results: T[] = [];
    for task in tasks {
        results.push(task.await());
    }
    return results;
}

// Race: return first completed, cancel others
async fn race<T>(tasks: Task<T>[]) -> T {
    // Implementation uses internal primitives
    // Returns result of first task to complete
    // Cancels remaining tasks
}

// Timeout wrapper
async fn timeout<T>(task: Task<T>, ms: uint) -> Erring<T, Timeout> {
    // Returns Timeout error if task doesn't complete in time
    // Cancels task on timeout
}
```

---

## 8. Interaction with Ownership

### 8.1. Moving into Tasks

Only `own T` values can be moved into spawned tasks:

```sg
let data: own Data = load();
let task = spawn process(data);  // data moved
// data invalid here
```

### 8.2. Borrowing Prohibition

References cannot cross task boundaries:

```sg
let data: Data = load();
let task = spawn process(&data);  // ERROR: &Data cannot cross task boundary
```

**Rationale:** Without multi-threading, this might seem safe, but:
- Prepares for v2 parallelism
- Avoids lifetime complexity
- Matches ownership philosophy

### 8.3. Channel Ownership

Channels transfer ownership of values:

```sg
send(&ch, owned_value);   // owned_value moved into channel
let v = recv(&ch);        // Ownership transferred to v
```

Channel handle itself is `own channel<T>`, can be borrowed for operations:

```sg
let ch: own channel<int> = make_channel(10);
send(&ch, 42);    // Borrow channel for send
recv(&ch);        // Borrow channel for recv
close(&ch);       // Borrow channel for close
// ch still valid, can be dropped or passed elsewhere
```

---

## 9. Syntax Summary

```
AsyncFn     := Attr* "async" "fn" Ident GenericParams? ParamList RetType? Block
AsyncBlock  := "async" Block
SpawnExpr   := "spawn" Expr
AwaitExpr   := Expr ".await()"

ChannelOps  := "make_channel" "<" Type ">" "(" Expr ")"
             | "send" "(" Expr "," Expr ")"
             | "recv" "(" Expr ")"
             | "try_send" "(" Expr "," Expr ")"
             | "try_recv" "(" Expr ")"
             | "close" "(" Expr ")"

Checkpoint  := "checkpoint" "()"
```

---

## 10. Examples

### 10.1. Basic Async/Await

```sg
async fn fetch_data(url: string) -> Erring<Data, Error> {
    let response = http_get(url).await();
    compare response {
        Success(r) => return parse(r);
        err => return err;
    }
}

async fn main() {
    let data = fetch_data("https://api.example.com").await();
    print(data);
}
```

### 10.2. Concurrent Fetches

```sg
async fn fetch_all(urls: string[]) -> Data[] {
    let mut tasks: Task<Data>[] = [];
    
    for url in urls {
        tasks.push(spawn fetch_data(url));
    }
    
    return await_all(tasks);
}
```

### 10.3. Producer-Consumer with Channels

```sg
async fn main() {
    let ch = make_channel<int>(10);
    
    async {
        // Producer
        spawn async {
            for i in 0..100 {
                send(&ch, i);
            }
            close(&ch);
        };
        
        // Consumer
        spawn async {
            while true {
                compare recv(&ch) {
                    Some(v) => print("Got: " + v);
                    nothing => break;
                }
            }
        };
    }
}
```

### 10.4. Timeout Pattern

```sg
async fn with_timeout() -> Erring<Data, Error> {
    let task = spawn slow_operation();
    
    compare timeout(task, 5000).await() {
        Success(data) => return Success(data);
        Timeout() => return { message: "Operation timed out", code: 408 };
    }
}
```

### 10.5. Fail-Fast Group

```sg
@failfast
async fn critical_pipeline() -> Erring<Result, Error> {
    let t1 = spawn validate_input();
    let t2 = spawn check_permissions();
    let t3 = spawn prepare_resources();
    
    // If any fails, others are cancelled immediately
    let v1 = t1.await();
    let v2 = t2.await();
    let v3 = t3.await();
    
    return Success(combine(v1, v2, v3));
}
```

### 10.6. Cancellation Handling

```sg
async fn interruptible_work() -> Erring<int, Error> {
    let mut total = 0;
    
    for i in 0..1_000_000 {
        total = total + compute(i);
        
        if (i % 10000 == 0) {
            compare checkpoint().await() {
                Cancelled() => {
                    save_progress(total, i);
                    return Cancelled();
                }
                finally => continue;
            }
        }
    }
    
    return Success(total);
}
```

---

## 11. Diagnostics

| Code | Name | Description |
|------|------|-------------|
| `SemaAsyncNotAllowed` | Async in sync context | `.await()` used outside async fn/block |
| `SemaSpawnNotTask` | Spawn non-task | `spawn` applied to non-async expression |
| `SemaBorrowTaskEscape` | Borrow crosses task | Reference passed to spawn |
| `SemaChannelTypeMismatch` | Channel type error | Send/recv type doesn't match channel |
| `SemaFailfastNotAsync` | @failfast on non-async | Attribute on non-async block |

---

## 12. Future Extensions (v2+)

* **True parallelism:** Run tasks on multiple OS threads
* **Work-stealing scheduler:** Automatic load balancing
* **select/choose:** Multiplex channel operations  
* **Task groups:** Named groups with collective operations
* **Async iterators:** `for await item in stream { }`
* **Parallel collections:** `parallel map`, `parallel reduce` (requires purity proof)

---

## 13. Implementation Notes

### 13.1. State Machine Generation

Each `async fn` generates:
1. State enum with variant per await point
2. Step function that advances state
3. Future/Task wrapper

### 13.2. Event Loop

Minimal event loop for v1:
- Ready queue (runnable tasks)
- Timer heap (for timeout)
- I/O polling (platform-specific)

### 13.3. Memory Layout

Task struct contains:
- State enum (current position)
- Local variables (lifted to struct fields)
- Result slot (for return value)
- Cancellation flag (atomic bool)
- Parent scope reference (for structured concurrency)
