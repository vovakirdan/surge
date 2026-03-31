# Compiler And Runtime Follow-ups

Short notes collected during stdlib work when something looked like a compiler or runtime sharp edge but did not block the current task.

## 2026-03-20

- Return analysis can still reject a function after a `compare` whose every arm performs `return`.
  Observed while writing `testdata/llvm_parity/http_parse_request_strict.sg`, where an extra trailing `return 0;` was needed to satisfy sema.

- Long-lived async scopes retain every registered child task id in the native runtime scope registry.
  Relevant code paths:
  - `runtime/native/rt_async_scope.c`
  - `runtime/native/rt_async_state.c`
  The scope tracks `active_children`, but `scope->children` is append-only until scope exit. This matters for servers and other long-running supervisors that keep spawning child tasks.

- Full LLVM parity still has an unrelated `select_timeout` mismatch (`vm=0 llvm=1`).
  This was present after the HTTP batch and should be debugged separately before leaning harder on `select`/`race` for timeout-heavy stdlib paths.

- `surge diag` and `surge build` can disagree on task-leak reporting in some async patterns.
  While iterating on `stdlib/http::serve`, a spawn pattern around accepted connections passed `diag` but failed under `build` with `task is neither awaited nor returned`.

- `Channel<TcpConn>` currently failed method resolution at `send(...)` inside `stdlib/http::serve`.
  A temporary workaround was to queue raw socket handles (`int`) and reconstruct `TcpConn` inside workers.

- `string -> byte[]` conversion currently accepts owned `string`, but rejects `&string` with `SEM3015: cannot cast &string to [byte]`.
  Observed while deduplicating local `string_to_bytes(...)` helpers in `stdlib/http` and `stdlib/json/parser`.
  Current workaround is `borrowed_string.__clone() to byte[]`.

- Method resolution currently trips on user-defined methods whose parameter type is `stdlib/json::JsonValue`.
  Observed while adding `stdlib/http::Context`; both `ctx.json(...)` and `ctx.json_response(...)` failed with `SEM3046: no matching overload`, while an equivalent free function compiled.
  Current workaround is `http.context_json(&mut ctx, status, &value)`.
