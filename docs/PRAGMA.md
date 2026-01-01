# Pragma in Surge (current status)
[English](PRAGMA.md) | [Russian](PRAGMA.ru.md)

`pragma` is a declaration of **file/module properties** read by the frontend. It
influences build and import rules, but it is not a normal AST node for
expressions or operators.

## 1) Basic Rules

- `pragma` must be the **first significant line** of the file.
- **Only one** `pragma` per file.
- Format: a comma-separated list of identifiers.
- Position violations are reported as `SynPragmaPosition`.

```sg
pragma module, no_std
```

Each element is a **pragma key** (identifier). Some keys support an **explicit
name** in the form `name::value`.

```sg
pragma module::bounded
```

Unknown keys are **parsed** but currently **ignored** (reserved for the
future).

---

## 2) Implemented Pragmas

### 2.1 `pragma module` and `pragma binary`

**Purpose:** declare a multi-file module within a directory.

- If **at least one** file in a directory contains `pragma module` or
  `pragma binary`, **all** `.sg` files in that directory must have the same
  pragma.
- `module` and `binary` cannot be mixed in one directory.
- Module name:
  - by default = directory name;
  - can be overridden via `::name`.
- `binary` requires **exactly one** `@entrypoint` in the module.

```sg
// in every file of the directory
pragma module;
```

```sg
// explicit module name
pragma module::bounded;
```

```sg
// executable module
pragma binary::run_app;
```

**Diagnostics (project-level):**
- `ProjMissingModulePragma` — if some files are missing the pragma.
- `ProjInconsistentModuleName` — if names do not match.

---

### 2.2 `pragma directive`

Marks a module as **directive-capable**, so it can be used in `///` directives.

- In `--directives=collect|gen|run` modes the compiler checks that a directive
  namespace matches an imported module with `pragma directive`.
- Without this pragma the module cannot be used in directives.

```sg
pragma directive;

pub fn eq<T>(a: T, b: T) -> bool { ... }
```

---

### 2.3 `pragma no_std`

Switches a module to no-stdlib mode:

- `stdlib/...` imports are rewritten to `core/...`.
- Error `SemaNoStdlib` is emitted with a hint for the correct import.
- The `no_std` value must be **consistent** across all files of a multi-file
  module. A mismatch emits `ProjInconsistentNoStd`.

```sg
pragma module, no_std
```

---

### 2.4 `pragma strict` and `pragma unsafe`

These keys are **parsed** but **not applied** in the current version.

- `strict` — reserved for a strict mode (warnings → errors, tighter style rules).
- `unsafe` — reserved for future `unsafe {}` blocks.

---

## 3) Reserved Pragmas (no behavior yet)

The following ideas are supported as **reserved keys**, but are not implemented
yet:

- `pragma feature(...)`
- `pragma build ...`
- `pragma version ...`
- `pragma export(...)`
- `pragma cache ...`

If you need such a key, it will parse but will not affect compilation.

---

## 4) Examples

```sg
// regular multi-file module
pragma module
```

```sg
// binary module with entrypoint
pragma binary
@entrypoint
fn main() -> int { return 0; }
```

```sg
// module without stdlib
pragma module, no_std
import core/format;
```
