## 05 — Async Cancellation

Demonstrates explicit task cancellation and checkpoints.

### What it demonstrates
- Spawning tasks with `task`
- `checkpoint().await()` for cooperative cancellation
- `t.cancel()` to signal cancellation
- Handling `Cancelled` result

### Run
```bash
surge run showcases/async/05_cancellation/main.sg
```
