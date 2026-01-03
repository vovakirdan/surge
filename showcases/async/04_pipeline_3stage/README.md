## 04 — Pipeline 3-Stage

Async processing pipeline using channels.

### What it demonstrates
- 3 stages: Generator -> Processor -> Aggregator
- `Channel<T>` for communication
- Passing channels by reference (`&Channel<T>`)
- Task synchronization via channel closing

### Run
```bash
surge run showcases/async/04_pipeline_3stage/main.sg
```
