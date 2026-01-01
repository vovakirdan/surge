# DIRECTIVES.md
[English](DIRECTIVES.md) | [Russian](DIRECTIVES.ru.md)
> Примечание: этот файл пока не переведен; содержимое совпадает с английской версией.

Surge directives are **structured doc-comment blocks** (`///`) that let you attach tool-driven scenarios (tests, benchmarks, lint checks) to code.

This document covers **current behavior (v1)** and the **planned roadmap**.

---

## 1. Current Status (v1)

### 1.1 What is implemented

- Directives are **parsed** when `--directives=collect|gen|run` is enabled.
- Each directive block is **attached to the next item** in the file (function/type/let/etc.).
- The compiler **validates directive namespaces** against imports when directives are enabled.
- `--directives=run` executes a **stub runner** that prints scenarios as *SKIPPED* (no codegen/execution yet).

### 1.2 What is *not* implemented yet

- No directive code generation.
- No directive body type-checking or execution.
- Scenario names (named test cases) are not supported yet.

---

## 2. Implemented Syntax (v1)

A directive block is a **contiguous `///` block** with:

1) First non-empty line: `<namespace>:`
2) Next lines: must start with `<namespace>.` or `<namespace>::`

Example:

```sg
import stdlib/directives::test;

/// test:
/// test.eq(add(1, 2), 3)
/// test.eq(add(2, 2), 4)
fn add(a: int, b: int) -> int { return a + b; }
```

Notes:
- Empty `///` line **ends** the directive block.
- Any `///` block that does **not** match the namespace rules is treated as a normal doc comment.
- Namespaces must be valid identifiers (`[A-Za-z_][A-Za-z0-9_]*`).

---

## 3. Namespace Validation (implemented)

When `--directives=collect|gen|run` is enabled, the compiler validates:

1) The namespace (`test` in `/// test:`) **must be an imported module**.
2) That module must have `pragma directive`.

Diagnostics:

| Code | Error |
|------|-------|
| `SemaDirectiveUnknownNamespace` | Directive namespace is not an imported module |
| `SemaDirectiveNotDirectiveModule` | Directive namespace module lacks `pragma directive` |

---

## 4. CLI Flags (implemented)

```bash
surge diag --directives=off|collect|gen|run --directives-filter=test,bench
```

- `off` (default): directives are ignored.
- `collect`: directives are parsed and namespaces validated.
- `gen`: currently behaves like `collect` (reserved for future codegen).
- `run`: runs the **stub runner** (prints SKIPPED for each scenario).

`--directives-filter` currently affects **only** `run` mode (what the stub prints).
Default filter is `test`. An empty filter (`--directives-filter=`) runs all namespaces.

---

## 5. Directive Modules (implemented)

A directive module is an **ordinary Surge module** marked with:

```sg
pragma directive
```

It can export helper functions used inside directive blocks.

Example:

```sg
// stdlib/directives/test.sg
pragma directive

pub fn eq<T>(a: T, b: T) -> bool { return a == b; }
```

---

## 6. Planned Syntax (roadmap)

The following structure is planned but **not implemented yet**:

```sg
/// test:
/// SumIsCorrect:
///     test.eq(add(1, 2), 3)
```

Planned rules:
- `<scenario>` names are unique per file.
- Bodies are full Surge statements.
- Directive bodies are type-checked in a generated module.

---

## 7. Planned Codegen & Execution (roadmap)

Future `gen/run` modes will:

- Generate a hidden module with one function per directive block.
- Type-check the directive bodies.
- Execute them via a directive runner.

This is not wired up yet; the current runner is a stub.

---

## 8. Quick Examples (current)

```sg
import stdlib/directives::test;

/// test:
/// test.eq(len("hi"), 2)
fn foo() -> nothing { return nothing; }
```

```bash
surge diag --directives=collect file.sg
surge diag --directives=run --directives-filter=test file.sg
```
