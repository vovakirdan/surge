# generic tag use disagrees with its original typed call

| Measure | Value |
|---|---:|
| Programs carrying it | 4 |
| Programs where it is the only root reason | 3 |
| Rows | 9 |
| Of those programs, `core_stdlib` copies | 0 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_tags.go:195`** (`checkTagUse`). A finalized generic use of a tag constructor such as `Some` or `Success`, at a site that is not a typed call expression. These are the implicit wraps the checker inserts. The missing fact is which constructor instance that wrap is.

## Corpus examples

1. **`mono/option_implicit_wrap.sg:3`**: `let x: int? = 1;`, and `:4`. This is its only root.
2. **`sema/valid/return_type_and_sugar.sg:6`**: `return 1; // should be auto wrapped to Some(1) in clear context`, plus lines 10 and 15. This is its only root.

The others are `hir/option_erring.sg:2` and `sema/valid/recursive_handles.sg`.

## Sound transfer and size

- **N-TAGCONV, S, prototype 51 lines, measured.** Find the `ImplicitConversion` of kind Some or Success the checker recorded at the use's site. Map its `Callee` to the canonical declaration through the publication's root-to-local symbols, and require that to equal the use's `CalleeTemplate`. Also require a nongeneric, live caller and `TemplateArgs == [conversion.Source]`. The packet certifies only the instance. The value transfer stays the wrapped expression's.
- **Named refusals** cover every other case, for example "generic tag conversion in a generic caller needs its exact type-dependent payload transfer".
- **What it frees.** `hir/option_erring.sg`, `mono/option_implicit_wrap.sg` and `sema/valid/return_type_and_sugar.sg`. With N-ALIAS-ARG it also frees `recursive_handles.sg`.

## Unsoundness risk

Low. The certificate is by declaration identity, and a generic caller keeps a named refusal.

## DEBT-365 and DEBT-368

None named.

## Owner decision

None needed.
