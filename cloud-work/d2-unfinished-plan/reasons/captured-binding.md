# captured binding requires origin finalization

| Measure | Value |
|---|---:|
| Programs carrying it | 10 |
| Programs where it is the only root reason | 0 |
| Rows | 80 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

Every program carrying this reason is a `core_stdlib` copy. The shared analysis and the owner question are in `core-copies.md`.

## Where it is raised

- **`internal/sema/return_origin_expr.go:54`.** An identifier that resolves to a symbol outside this body's locals and parameters, and is not a reference-free constant. In the copies it is the tag name in `Some(...)` and `Success(...)`, whose symbol the copy's unit cannot finalize (read from code).

## Corpus examples

Each copy program analyses the whole copied directory, so the same row appears in all ten copies.

1. **`testdata/golden/core_stdlib/base.sg`**, with a row at `core_stdlib/array.sg:163`, `return Some(i to uint);`.
2. **`testdata/golden/core_stdlib/string.sg`**, with a row at `core_stdlib/array.sg:182`, `return Some(i to uint);`.

## Transfer, size and risk

This reason also stands when the real `core/` is the root program. It appears at the 8 explicit tag-constructor sites listed in `core-copies.md` (measured). N-CORE-ROOT-TAGS (S to M, estimate) would resolve an explicit tag constructor's symbol to its canonical core declaration when core is the root unit. Risk is low: identity by declaration, not by name.

## DEBT-365 and DEBT-368

None named. DEBT.md:229 says the D2 gate rests on core being clean when it is analysed as a dependency. Measured: in the 98 unfinished programs that are not copies, root rows inside `core/` appear only at generic or deferred sites that the program itself instantiates. There are 12 such sites in 9 programs, for example `core/option.sg:12` with `T` a Task. None of this reason's rows is among them.

## Owner decision

Yes, owner question 1. See `core-copies.md`.
