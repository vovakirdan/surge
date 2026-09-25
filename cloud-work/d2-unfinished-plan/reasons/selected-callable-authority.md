# selected callable lacks its published callable authority

| Measure | Value |
|---|---:|
| Programs carrying it | 13 |
| Programs where it is the only root reason | 2 |
| Rows | 271 |
| Of those programs, `core_stdlib` copies | 10 |

Counts are measured (user form, all files, `census.json`). Claims marked "(read from code)" were not run.

## Where it is raised

- **`internal/sema/return_origin_selected_callable.go:33`.** The symbol the checker selected for a call maps to no published `CallableCandidate`.
- In the three non-core programs the selected symbol is the synthetic copy the resolver makes for an imported export (read from code: `symbols.tryResolveImportSymbol` and `syntheticSymbolForExport`). A glob import, or a module renamed by its `pragma`, leaves that copy unmapped.
- A scratch debug print confirmed the mismatch (measured). In `import_all.sg` the selected symbol has an empty declaration and module path `.../import_all_test/mymodule`. In `module_multitest/main.sg` the selected symbol has module path `.../module_multitest/bar`, while the candidate lives in `.../foo/foo.sg`, which declares `pragma module::bar`.
- The missing fact is the identity of the imported function.

## Corpus examples

1. **`sema/valid/import_all.sg:8`**: `let result: int = moduleFunc();`. This is 1 row, its only root.
2. **`sema/valid/module_multitest/main.sg:9`**: `print(hello());`. This is 1 row, its only root.

The third non-core carrier is `stdlib/time_duration_conversions.sg:7`, `let diff = d.sub(earlier);`, which also needs N-STDLIB-TIME.

## Sound transfer and size

- **N-IMPORT-IDENTITY.** Map the synthetic import symbol to the candidate its export resolves to.
- **The prototype is only a heuristic (16 lines, measured).** It takes the unique candidate with the same name and exactly the same parameter types, result type and receiver. It freed all three programs.
- **A real packet must read the resolver's export record.** A name, even with a signature, selects nothing (`internal/sema/return_origin_core_identity.go:8`). Estimate M, 60 to 150 lines (read from code).

## Unsoundness risk

- **A heuristic match can pick a different export** with the same name and signature from another module. It would then apply the wrong body summary. The export record avoids that.
- **Core modules must stay out.** The heuristic without that exclusion broke `TestH2TripwireRefusedG5ModuleTimeout`, which lost its row "selected callable lacks its published callable authority" at `ci.timeout(leak(), 1000)`. With module paths `core` and `core/...` excluded, that test passes, all 10 driver tripwire tests and all 17 VM tripwire tests pass, and the three programs still become ok (measured).

## DEBT-365 and DEBT-368

DEBT.md:229, closing column: "the tripwire rows name the four changes that still meet this row: ..., and an identity for explicitly imported `core/intrinsics` members". This packet must leave that identity to the tripwire's owner.

## Owner decision

None needed if the packet excludes core modules. Including them is the tripwire decision already named in DEBT-365. For the copies, see `core-copies.md`.
