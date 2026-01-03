## 08 — Timeout & Race

Advanced async control flow primitives.

### What it demonstrates
- `race { ... }` block to pick the first completing task
- `timeout(task, ms)` helper for deadlines
- Cancelling "losing" tasks automatically

### Run
```bash
surge run showcases/async/08_timeout_race/main.sg
```
