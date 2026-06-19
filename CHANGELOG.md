# Changelog

This changelog is curated manually.
It currently starts with the `ret` block-values work and does not backfill older project history yet.

## [0.1.12] - 2026-06-19

### 🚀 Features

- *(install)* Add release archive installer
- *(cli)* Add surge doctor

### 📚 Documentation

- *(readme)* Use installed surge in Russian quickstart

## [0.1.11] - 2026-06-19

### 🚀 Features

- *(module)* Implement git module synchronization and update functionality
- *(stdlib)* Add safe random helpers
- Add Surge Issue Fixer skill and agent configuration
- *(stdlib)* Add stable hash module
- *(runtime)* Trace native net poll counters
- *(runtime)* Expose native heap stats
- *(runtime)* Trace compensation high-water mark
- *(runtime)* Add channel wake placement experiment
- *(runtime)* Support live async trace dumps
- *(runtime)* Include task kind counts in trace snapshots
- *(runtime)* Break down trace tasks by status
- *(runtime)* Trace task-context channel blocking
- *(runtime)* Hint channel request handoffs

### 🐛 Bug Fixes

- *(result)* Change exit function return type from nothing to T
- *(attrs)* Standardize spacing in attribute declarations
- *(sema)* Accept exhaustive compare return blocks
- *(runtime)* Prune completed async scope children
- *(llvm)* Load task handles before runtime task calls
- *(sema)* Surface channel send diagnostics correctly
- *(compiler)* Restore Erring<Option<T>, E> pipeline
- *(sema)* Resolve imported JsonValue method params
- *(frontend)* Handle shorthand casts consistently
- *(mir)* Discard control-flow results in side-effect contexts
- *(sema)* Diagnose compare scrutinee reuse
- *(mir)* Preserve typed default arg lowering (#76)
- *(vm)* Reborrow shared calls from map mut refs
- *(llvm)* Lower time monotonic_now
- *(llvm)* Tighten monotonic_now follow-up
- *(vm)* Preserve borrowed async frame locals
- *(vm)* Retain async borrow backing selectively
- *(vm)* Address review follow-ups
- *(vm)* Roll back task-state pin collection safely
- *(mir)* Avoid double deref for field reborrows
- Lower duration intrinsics (closes #82)
- *(llvm)* Lower concrete clone methods (closes #85) (#86)
- *(lsp)* Resolve shadowed call targets (#89)
- *(driver)* Reject wrong explicit module import paths (#90)
- *(mir)* Short-circuit logical operators (closes #87)
- *(llvm)* Lower fields through array element refs (closes #88)
- *(sema)* Lower imported magic methods (closes #98)
- *(stdlib)* Address hash review feedback
- *(runtime)* Wake net poller on new waiters
- *(stdlib/net)* Buffer tcp reads and writes
- *(runtime)* Enable TCP_NODELAY on TCP sockets
- *(runtime)* Preserve poll context for single-thread awaits
- *(runtime)* Keep channel fanout progressing
- *(runtime)* Exclude channel waits from running workers
- *(runtime)* Keep channel fanout progressing under small thread counts
- *(sema)* Diagnose blocking channel calls in nonblocking functions
- *(runtime)* Make live trace snapshots reliable
- *(runtime)* Prioritize net join continuations
- *(runtime)* Reuse native listener addresses
- *(runtime)* Address async scheduler review

### 💼 Other

- *(tooling)* Unify version metadata and checks
- *(runtime)* Add native channel request-reply baseline
- *(runtime)* Include trace counters in channel benchmark
- *(runtime)* Add channel scheduler matrix

### 🚜 Refactor

- *(sema)* Split method call checking units
- *(target-selection)* Consolidate command target resolution logic

### 📚 Documentation

- *(stdlib)* Remove hash planning drafts
- *(runtime)* Document scheduler invariants
- *(runtime)* Document async runtime diagnostics
- *(runtime)* Document native async runtime

### ⚡ Performance

- *(runtime)* Inject channel handoff receivers
- *(runtime)* Avoid wake signal on yielded tasks
- *(runtime)* Defer channel recv ack wake signals
- *(runtime)* Avoid signal on buffered channel refill
- *(runtime)* Coallocate buffered channel storage

### 🧪 Testing

- *(vm)* Cover async borrow capture rejection
- Update golden files
- Allow empty file-size check set
- *(runtime)* Add native executor trace diagnostics
- *(runtime)* Cover compensation scheduler counters
- *(runtime)* Cover non-yielding channel handoffs

### ⚙️ Miscellaneous Tasks

- Update .gitignore to include scribe/
- Ignore local skvc package

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
