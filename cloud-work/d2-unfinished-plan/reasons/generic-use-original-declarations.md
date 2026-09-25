# generic use lacks its exact original callable declarations

| Measure | Value |
|---|---:|
| Programs carrying it | 10 |
| Programs where it is the only root reason | 0 |
| Rows | 15 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

Every program carrying this reason is a `core_stdlib` copy. The shared analysis and the owner question are in `core-copies.md`.

## Where it is raised

- **`internal/sema/return_origin_generics.go:62`.** A finalized generic use whose callee or caller template maps to no original declaration, or whose template arity disagrees.

## Corpus examples

Each copy program analyses the whole copied directory, so the same row appears in all ten copies.

1. **`testdata/golden/core_stdlib/base.sg`**, with a row at `core_stdlib/format.sg:101`, `out = append_fmt_arg(out, args[arg_index]);`.
2. **`testdata/golden/core_stdlib/string.sg`**, with a row at `core_stdlib/format.sg:101`, the same row.

## Transfer, size and risk

When the real `core/` is diagnosed as the root program by absolute path, this reason does not appear (measured). It exists only because the copy is a user module, so no transfer is proposed. Option B of owner question 1 removes it with no analysis change.

## DEBT-365 and DEBT-368

None named. DEBT.md:229 says the D2 gate rests on core being clean when it is analysed as a dependency. Measured: in the 98 unfinished programs that are not copies, root rows inside `core/` appear only at generic or deferred sites that the program itself instantiates. There are 12 such sites in 9 programs, for example `core/option.sg:12` with `T` a Task. None of this reason's rows is among them.

## Owner decision

Yes, owner question 1. See `core-copies.md`.
