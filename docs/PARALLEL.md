# Parallelism in Surge (v1 status)

> **Short version:** v1 has no real parallelism. There is only cooperative
> concurrency via `async`/`spawn` and channels. The keywords `parallel` and
> `signal` are reserved and not supported.

---

## 1. What exists in v1

### 1.1. Cooperative concurrency

- Single execution thread; tasks switch at suspension points.
- Tools: `async`, `spawn`, `.await()`, `Channel<T>`.
- Await result: `TaskResult<T> = Success(T) | Cancelled`.

See `docs/CONCURRENCY.md` for the precise model.

### 1.2. v1 limitations

- **No parallelism** across multiple cores.
- `parallel map/reduce` is not supported (error `FutParallelNotSupported`).
- `signal` is not supported (error `FutSignalNotSupported`).
- `await` inside loops is currently forbidden (lowering limitation).

---

## 2. Data-parallel alternative in v1

If you need to process a collection, use `spawn` + await. In v1 you cannot
`await` in loops, so awaiting tasks is structured via recursion:

```sg
async fn await_all<T>(tasks: Task<T>[], idx: int, mut out: T[]) -> T[] {
    if idx >= (len(tasks) to int) { return out; }
    compare tasks[idx].await() {
        Success(v) => out.push(v);
        Cancelled() => return [];
    };
    return await_all(tasks, idx + 1, out);
}

async fn concurrent_map<T, U>(xs: T[], f: fn(T) -> U) -> U[] {
    let mut tasks: Task<U>[] = [];
    for x in xs {
        tasks.push(spawn f(x));
    }
    return await_all(tasks, 0, []);
}
```

If tasks need to communicate, use `Channel<T>`.

---

## 3. Reserved constructs

### 3.1. `parallel map/reduce`

The syntax is reserved but rejected in v1:

```sg
parallel map xs with (x) => x * x
parallel reduce xs with 0, (acc, x) => acc + x
```

Current status: error `FutParallelNotSupported`.

### 3.2. `signal`

The syntax is reserved but rejected in v1:

```sg
signal total := price + tax;
```

Current status: error `FutSignalNotSupported`.

---

## 4. v2+ plan (brief)

- Real parallelism across multiple threads.
- Data-parallel constructs (`parallel map/reduce`).
- Reactive computations (`signal`).

Details will be clarified as the implementation progresses; in v1 this is
**not part of the specification**.
