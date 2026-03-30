# Changelog

This changelog is curated manually.
It currently starts with the `ret` block-values work and does not backfill older project history yet.

## Unreleased

### Language

- Added explicit `ret` block exits for brace-block expressions.
- Accepted both `ret;` and `ret nothing;` as `nothing`-valued block exits.
- Kept short `=> expr` compare arms unchanged while introducing explicit block-local exits for brace blocks.

### Diagnostics

- Added migration warnings for legacy implicit block values, with a fix-it that inserts `ret`.
- Added diagnostics for block expressions that can fall through without producing a required value.
- Added diagnostics for conflicting block-result types collected through `ret`.
- Tightened compare-arm diagnostics so consumed compare values still require a consistent result type, while discarded control-flow compares remain valid.
- Propagated return-path checks through `ret` blocks so task-return and trivial-recursion diagnostics still fire on nested value paths.

### Compiler

- Fixed expected-type propagation for `ret` payloads and nested compare/block expressions.
- Distinguished explicit function `return` from synthetic block-value tails during sema flow analysis.
- Refined abrupt-exit and reachability analysis for compare arms and block expressions.
- Hardened block-result collection so discarded block tails do not leak false value-flow requirements into surrounding control-flow code.

### Tests

- Added regression coverage for `ret` across parser, sema, HIR, MIR, driver, VM, and golden snapshots.
- Refreshed older block-expression golden cases to use `ret` where they depended on the old implicit block-value style.
