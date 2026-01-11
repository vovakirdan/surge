# Current Issues

Source: `make check` run after latest changes (implicit borrow adjustment in HIR).

## Failing Tests / Parity

- `TestLLVMParity/http_response_bytes`: exit code mismatch (vm=1, llvm=0).
- `TestLLVMParity/http_chunked_response`: exit code mismatch (vm=1, llvm=0).
- `TestLLVMParity/http_json_helpers`: exit code mismatch (vm=1, llvm=-1).
- `TestLLVMParity/http_server`: keepalive scenario failed (EOF). VM panic: local "buf" used after move at `stdlib/http/http.sg:1416:5`.
- `TestLLVMParity/http_connect`: stdout mismatch (vm empty, llvm "client_ok").
- `TestLLVMParity/walkdir_for_in`: exit code mismatch (vm=1, llvm=0).

## VM Panic

- `TestVMJsonSuite`: VM panic "local \"p\" used after move" at `stdlib/json/parser.sg:563:36`.
- Also reported: `missing argv argument "x"` during the same test run.
