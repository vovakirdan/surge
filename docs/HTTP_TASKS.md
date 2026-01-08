# HTTP Follow-ups

- Allow concurrent connection handling in `http.serve`: current design processes one connection at a time because `TcpConn` is `@nosend` and cannot cross `task` boundaries; a sendable handle or runtime-sanctioned clone would unblock per-connection tasks.
- Allow `Task<T>` values in local containers across awaits so HTTP can queue handler tasks for pipelining; current semantics raise `SEM3110 task container cannot escape its scope`.
- Add a client-side `net.connect` (intrinsic + stdlib wrapper) to let Surge tests and examples drive HTTP servers without external harnesses.
- Add a stdlib helper to convert `byte[]` to `string` efficiently; the server currently builds strings byte-by-byte to reuse the existing request parser.
- Add `type FnType = [async] fn(args) -> T` support