## 02 — Fan-out / Fan-in

Pattern for parallelizing work and collecting results.

### What it demonstrates
- Spawning multiple worker tasks (Fan-out)
- Storing task handles in an array
- Awaiting all tasks and aggregating results (Fan-in)

### Run
```bash
surge run showcases/async/02_fanout_fanin/main.sg
```
