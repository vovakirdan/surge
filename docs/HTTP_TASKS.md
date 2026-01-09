# HTTP Follow-ups

- Add a client-side `net.connect` (intrinsic + stdlib wrapper) to let Surge tests and examples drive HTTP servers without external harnesses.
- Add a stdlib helper to convert `byte[]` to `string` efficiently; the server currently builds strings byte-by-byte to reuse the existing request parser.
- Add `type FnType = [async] fn(args) -> T` support