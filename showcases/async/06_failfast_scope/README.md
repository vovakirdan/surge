## 06 — Fail-fast Scope

Structured concurrency with automatic cancellation on error.

### What it demonstrates
- `@failfast` attribute for async functions
- Automatic cancellation of sibling tasks when one fails or is cancelled
- Propagating cancellation up the call stack

### Run
```bash
surge run showcases/async/06_failfast_scope/main.sg
```
