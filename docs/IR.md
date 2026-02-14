# Surge Intermediate Representations (HIR/MIR)

This document describes the **actual** IR pipeline in the current compiler
version, not the roadmap.

---

## 1. Overview

```
AST + Sema
   │
   ▼
HIR (typed) + borrow graph + move plan
   │
   ▼
Monomorphization (generic -> concrete)
   │
   ▼
MIR (CFG + instr/term)
   │
   ▼
CFG simplification + switch_tag recognition
   │
   ▼
Async lowering (poll state machines)
   │
   ▼
MIR validation
   │
   ▼
VM execution
```

Key packages:

- HIR: `internal/hir`
- Monomorphization: `internal/mono`
- MIR: `internal/mir`
- ABI layout: `internal/layout`
- VM: `internal/vm`

---

## 2. HIR (High-level IR)

HIR is built after successful semantic analysis:

- Input: AST + `sema` results
- Output: `hir.Module` (typed tree of functions and expressions)

### 2.1. What `hir.Lower` does

`internal/hir/lower.go`:

- minimal desugaring (e.g. removing ExprGroup)
- normalization of high-level constructs:
  - `compare` -> conditional branches
  - `for` -> `while`
- **does not** desugar async/spawn (that happens in MIR)

### 2.2. Borrow graph and move plan

HIR includes extra artifacts for analysis and debugging:

- `BorrowGraph`: borrow edges and events (borrow/move/write/drop)
- `MovePlan`: move policy for locals (`MoveCopy`, `MoveAllowed`, ...)

Construction: `internal/hir/borrow_build.go`.

### 2.3. How to inspect HIR

Commands:

```bash
surge diag file.sg --emit-hir
surge diag file.sg --emit-borrow   # with borrow graph + move plan
```

Dump: `hir.DumpWithOptions`.

---

## 3. Monomorphization (generic -> concrete)

`internal/mono` turns generic HIR into concrete instantiations:

- uses an instantiation map (`mono.InstantiationMap`)
- instantiations are collected in sema with `--emit-instantiations`
- supports DCE (dead code elimination) for mono versions

CLI flags:

```bash
surge diag file.sg --emit-instantiations
surge diag file.sg --emit-mono --mono-dce --mono-max-depth=64
```

Dump: `mono.DumpMonoModule`.

Note: `--emit-mono` is supported only for single files (directories are
rejected).

---

## 4. MIR (Mid-level IR)

MIR is a CFG + instructions + terminators.
Structures: `internal/mir/*`.

### 4.1. Lowering to MIR

`mir.LowerModule` takes mono-HIR and builds `mir.Module`:

- locals, blocks, instructions
- constants and static strings
- ABI layout metadata (`layout.LayoutEngine`)
- tag/union layout tables

### 4.2. MIR passes

`surge diag --emit-mir` runs the following steps:

1. `SimplifyCFG` — removes trivial `goto`
2. `RecognizeSwitchTag` — turns `if` chains into `switch_tag`
3. `SimplifyCFG` again
4. `LowerAsyncStateMachine` — async lowering
5. `SimplifyCFG` again
6. `Validate` — checks MIR invariants

### 4.3. MIR dump

```bash
surge diag file.sg --emit-mir
```

Dump: `mir.DumpModule`.

Note: `--emit-mir` is supported only for single files.

---

## 5. Async lowering

`mir.LowerAsyncStateMachine`:

- turns `async fn` into a **poll state machine**
- splits `await` into separate suspend blocks
- saves/restores live locals across suspensions
- adds structured concurrency (`rt_scope_*`)

Note:

- `await` inside loops is supported.

---

## 6. Entrypoint lowering

If `@entrypoint` exists, MIR builds a synthetic function
`__surge_start` (`internal/mir/entrypoint_*.go`).

It:

- handles `@entrypoint("argv")` / `@entrypoint("stdin")`
- parses arguments via `from_str`
- returns the correct exit code

---

## 7. Execution (VM)

MIR is executed by the VM (`internal/vm`).

Useful flags:

```bash
surge run file.sg --vm-trace
```

---

## 8. Where to look

- HIR: `internal/hir/*`
- Borrow graph: `internal/hir/borrow_build.go`
- Monomorphization: `internal/mono/*`
- MIR: `internal/mir/*`
- Async lowering: `internal/mir/async_*`
- Entrypoint: `internal/mir/entrypoint_*.go`
- VM: `internal/vm/*`
