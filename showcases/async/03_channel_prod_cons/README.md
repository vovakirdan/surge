## 03 — Channel Producer/Consumer

Classic producer-consumer pattern.

### What it demonstrates
- `Channel<T>` operations: `send` and `recv`
- Suspension points without explicit `await` syntax on channel ops
- Iterating over channel messages
- Closing channels

### Run
```bash
surge run showcases/async/03_channel_prod_cons/main.sg
```
