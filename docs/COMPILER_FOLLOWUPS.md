# Compiler And Runtime Follow-ups

Short notes collected during stdlib work when something looked like a compiler or runtime sharp edge but did not block the current task.

## 2026-03-20

- `surge diag` and `surge build` can disagree on task-leak reporting in some async patterns.
  While iterating on `stdlib/http::serve`, a spawn pattern around accepted connections passed `diag` but failed under `build` with `task is neither awaited nor returned`.

- `string -> byte[]` conversion currently accepts owned `string`, but rejects `&string` with `SEM3015: cannot cast &string to [byte]`.
  Observed while deduplicating local `string_to_bytes(...)` helpers in `stdlib/http` and `stdlib/json/parser`.
  Current workaround is `borrowed_string.__clone() to byte[]`.

## 2026-04-20

- VM async poll corrupts control flow around `rt_exit(...)` and `panic(...)` inside async tasks.
  `internal/vm/async_runtime.go` runs poll functions against a shallow copy of `vm.Stack`, then restores `vm.Stack` and `vm.Halted` unconditionally.
  If an async task exits or panics, the child poll can drop parent-frame locals and still return `VM1999: poll function exited without async terminator` instead of propagating program termination.

- VM executor keeps completed child task ids in async scopes until scope exit.
  `internal/asyncrt/scope.go` appends every child in `RegisterChild`, but `MarkDone` never prunes them.
  This makes long-lived scopes grow monotonically and turns `join_all` / scope cancellation into scans over historical children.
  Native runtime already has a dedicated fix and harness test for the same invariant (`bd2c2f9d`), so VM parity is currently behind.

- Async scope invariant failures still panic the Go process directly.
  `internal/asyncrt/scope.go` uses raw `panic(...)` on `ExitScope` with live children, while the VM only normalizes `*VMError` panics.
  If lowering/runtime invariants drift, users get a host-process crash instead of a regular VM diagnostic.

- VM async shutdown cleanup looks incomplete for buffered channels and parked senders.
  `internal/vm/drop.go` only drops `task.State` and `task.ResultValue`, while `internal/asyncrt/channel.go` stores payloads in channel buffers and send queues.
  `rt_exit` / `rt_panic` paths also skip `checkLeaksOrPanic`, unlike `exit(ErrorLike)`, so leak detection is weaker on normal program termination.
