# DIRECTIVES.md

*Surge Directive System — Design, Semantics, and Roadmap*

## 1. Overview

Directives are Surge’s mechanism for embedding structured, statically-checked, controllable code blocks directly into source files. They act as **compile-time scenarios**—test cases, benchmarks, linters, target selectors, analyzers, documentation examples, etc.—written in pure Surge and executed by the compiler in controlled modes.

A directive is:

* Declared via **`///`**.
* Contains **valid Surge code**.
* Lives in a dedicated **directive module**, imported via `import ...::name;`.
* Has **no effect on program semantics** unless explicitly executed.
* Can see the module’s declarations, but its own declarations stay isolated.
* Is a *consumer* of a stable reflection API (meta/build/ast/type), which gives access to the program structure without exposing compiler internals.

Directives intentionally do **not** replace macros.
They are a separate, safer, structured layer built for *read-only compile-time introspection and analysis*, while macros (v2+) will enable structural code transformation.

This document defines the directive system architecture, semantics, and the incremental roadmap for v1 / v1.x / v2+.

---

## 2. Syntax

### 2.1. Directive Block

A directive block starts with `///` and ends when the next Surge object begins.

Example:

```sg
let a = 1;

/// test:
/// SimpleAddition:
///     test.eq(add(a, 1), 2);

fn add(x: int, y: int) -> int { return x + y; }
```

### 2.2. Structure

A block is structured as:

```
/// <namespace>:
/// <scenario-name>:
///     <body – valid Surge code>
```

* **namespace** — corresponds to an imported directive module, e.g. `test`, `benchmark`, `lint`, `target`.
* **scenario-name** — an identifier unique to the file.
* **body** — arbitrary Surge statements.

### 2.3. Visibility Rules

* Directive code can **see everything** visible at that point in the module:

  * imports,
  * functions,
  * types,
  * constants,
  * even declarations *below* the directive.
* Declarations **inside** a directive (functions, lets, types) are **not visible** to the module.
* Directives do **not** affect the program unless run under `--directives=gen` or `run`.

---

## 3. Directive Modules

A directive implementation lives in a module marked with:

```sg
pragma directive;
```

This module:

* is a normal Surge module,
* exports directive functions via `pub fn`,
* can be imported like any other module,
* may call compiler intrinsics (meta/build/diag/etc.) accessible *only* in directive modules.

Example:

```sg
// stdlib/directives/test.sg
pragma directive;

pub fn eq<T>(actual: T, expected: T) -> Erring<nothing, TestFail> { ... }
```

---

## 4. Directive Execution Modes

The compiler supports multiple directive modes:

### 4.1. `--directives=off` (default)

* Directive blocks are ignored.
* No directive module is generated.
* No diagnostics produced by directives.

### 4.2. `--directives=collect`

* Parser collects directive blocks.
* No typechecking, no execution.
* Useful for IDEs and tooling.

### 4.3. `--directives=gen`

* A hidden module is generated, containing functions for each scenario.
* This module participates in name resolution and typechecking.
* Directive bodies are validated but not executed.

### 4.4. `--directives=run`

* Same as `gen`, but directive functions are executed by the directive runner.
* Failures result in non-zero exit code.

### 4.5. Filters

`--directives-filter=test,benchmark,time`
Only these directive namespaces will be compiled/checked/run.

---

## 5. Semantics of Generated Directive Functions

For each directive block:

```sg
/// lint:
/// CheckTabs:
///     lint.check_whitespaces()
```

the compiler will generate:

```sg
fn __directive_lint_CheckTabs__() -> Erring<nothing, Error> {
    // translated body
    return lint.check_whitespaces();
}
```

These functions are:

* placed in a hidden module,
* compiled with full semantics,
* executed only in `run` mode.

---

## 6. Directive Categories

Below is the taxonomy of directive families, divided into:

* **Available in v1**
* **Planned for v1.x**
* **Planned for v2+ (requires macros or deeper compiler features)**

---

# 7. v1 DIRECTIVES

v1 includes only directives that require:

* no AST reflection, or
* trivial introspection (spans, text), and
* no structural code modification.

This keeps v1 manageable and stable.

---

## 7.1. `test:` — v1 Core

**Status: v1**

`test` provides assertion-based unit tests written directly inside source files.

Example:

```sg
/// test:
/// Addition:
///     test.eq(add(2, 3), 5);
```

Features:

* assertion helpers (`eq`, `assert`, etc.),
* grouping/tags (`test.group`, `test.tag`),
* custom test functions defined directly inside a directive.

No compiler intrinsics required.

---

## 7.2. `benchmark:` and `time:` — v1

**Status: v1**

Used for microbenchmarks and timing scenarios.

Example:

```sg
/// benchmark:
/// ParseSmallFile:
///     benchmark.throughput("parse", 1000, parse_small);
```

Implementation:

* simple wrapper functions in stdlib,
* uses `core.time.monotonic_now()` and `Duration`.

Does *not* require AST or meta reflection.

---

## 7.3. `doc:` / `example:` — v1

**Status: v1**

Used for:

* long-form descriptions,
* documentation generation,
* verified examples (optional).

Example:

```sg
/// doc:
/// Summary:
///     doc.summary("Adds two integers.");
```

These directives attach metadata stored in the directive runner & doc generator.

No reflection needed.

---

# 8. v1.x DIRECTIVES

(Requires readonly meta/build intrinsics)

These features depend on **readonly introspection** of:

* module,
* items,
* spans,
* source text,
* build configuration.

But do **not** need AST or type reflection.

---

## 8.1. `lint:` — v1.1

**Status: planned for v1.1**

Lint directives provide static checks executed at compile time.

### v1.1 Capabilities (text-level)

* access to `Span`,
* access to complete source via `span_source(span)`,
* access to namespace and item context.

Use-cases:

* whitespace rules,
* line length,
* prohibited patterns,
* naming conventions,
* style rules based on tokens.

Example:

```sg
/// lint:
/// NoTabs:
///     lint.check_whitespaces()
fn   foo( );
```

### Required Intrinsics

* `meta.current_item()`
* `meta.item_span(item)`
* `meta.span_source(span)`
* `emit_diagnostic(level, span, code, message)`

---

## 8.2. `target:` — v1.1

**Status: planned for v1.1**

Provides build-time control over which items participate in compilation.

Example:

```sg
/// target:
/// target.require(target.os("linux") && target.feature("simd"))
fn simd_fast_path(...) { ... }
```

Capabilities:

* inspecting build context,
* excluding items from semantic analysis,
* enabling cross-platform modularization.

Required intrinsics:

* `build_os()`, `build_arch()`, `build_feature()`, …
* `set_item_enabled(item, enabled)`

This achieves full “conditional compilation” without macros.

---

## 8.3. `lint:` / `safety:` / `analyze:` — v1.2 (AST read-only)

**Status: v1.2**

Once AST reflection is introduced, directives can:

* walk AST nodes,
* inspect control structures,
* analyze function bodies,
* find calls to “forbidden” functions,
* ensure certain patterns.

Use-cases:

* no allocations in hot paths,
* no blocking calls in async functions,
* stylistic structure checks,
* “must include else”, “no early return”, etc.

Required intrinsics:

* `meta.item_body(item) -> AstNodeHandle`
* `meta.ast_kind(node)`
* `meta.ast_child_count(node)`
* `meta.ast_child(node, i)`

No modification allowed — read-only.

---

# 9. v2+ DIRECTIVES

(require code generation or macro system)

These directives exceed the read-only model and require structural changes to the program or generation of new functions/types. They belong to v2 or later.

---

## 9.1. `trace:` + `profile:` — v2

Example:

```sg
/// trace:
/// trace.auto()
fn process_request(req: Req) -> Resp { ... }
```

Behavior:

* generates a wrapper function,
* injects tracing spans automatically.

Requires:

* ability to generate functions,
* ability to rewrite call sites,
* i.e., true macro system or limited function-wrapping API.

---

## 9.2. `ffi:` / `cbind:` / `interop:` — v2

Exporting Surge functions/types to C/Rust/TS.

Example:

```sg
/// ffi:
/// ffi.export_to_c(header: "api.h")
pub fn surge_add(a: int32, b: int32) -> int32 { ... }
```

Requires:

* type reflection,
* signature reflection,
* `emit_artifact("api.h", bytes)`,
* optional type-mapping logic.

---

## 9.3. `schema:` / `db:` / `validate:` — v2+

These would:

* inspect types deeply,
* generate SQL/migrations,
* generate validation wrappers.

Requires:

* full type reflection,
* ability to generate new declarations (macros).

---

## 9.4. `resource:` / `embed:` — v2+

Injecting external data as constants.

Example:

```sg
/// resource:
/// resource.embed_file("static/page.html", const: "PAGE")
```

Requires code generation → v2.

---

# 10. Reflection API (Meta/Build/AST/Type)

Directives rely on a structured, limited set of intrinsics.
Each intrinsic is a safe and stable surface around compiler internals.

### v1.x Meta Intrinsics (readonly)

* `ModuleHandle`, `ItemHandle`, `Span`
* `current_module()`, `current_item()`
* `item_span(item)`
* `span_source(span)`
* `emit_diagnostic(...)`

### v1.x Build Intrinsics

* `build_os()`, `build_arch()`, `build_profile()`, `build_feature()`
* `set_item_enabled(item, enabled)`

### v1.2 AST Intrinsics

* `AstNodeHandle`
* `ast_kind(node)`
* `ast_child_count(node)`
* `ast_child(node, i)`
* `item_body(item)`

### v2+ Type Reflection

* `TypeHandle`, `FnSigHandle`
* `type_name`, `type_kind`, `fn_param_type`, `fn_return_type`

---

# 11. Directive Execution Model

### 11.1. Compilation Pipeline Integration

1. Parse module → collect `DirectiveBlock`.
2. Build symbol tables.
3. Generate hidden directive module.
4. Typecheck program + directive module.
5. If `run`:

   * create a `DirectiveContext`,
   * run each directive scenario through the directive VM,
   * intrinsics access AST/build/type info via this context.
6. Produce final output (binary/VM IR).

### 11.2. Context Passing

Directive intrinsics access compiler state through a hidden directive-execution context:

```
DirectiveContext {
    ModuleID,
    ItemID,       // optional
    AST,
    HIR,
    BuildConfig,
    SymbolTable,
}
```

This context is never exposed directly to Surge code.

---

# 12. Future Direction

Directives represent a middle layer between:

* the **pure language**, and
* the **future macro system**.

They are:

* strictly read-only (v1.x),
* strongly typed,
* statically checked,
* isolated from main program semantics,
* but powerful enough for high-value compile-time tooling:

  * tests,
  * benchmarks,
  * docs,
  * style rules,
  * static analysis,
  * target selection,
  * early forms of interop.

When macros arrive in v2+, directives become the “declarative” layer that configures and drives macros, rather than competing with them.

---

# 13. Version Summary

| Feature Category                       | Status        | Notes                                     |
| -------------------------------------- | ------------- | ----------------------------------------- |
| `test:`                                | **v1**        | Already usable, no reflection needed      |
| `benchmark:` / `time:`                 | **v1**        | Simple stdlib impl                        |
| `doc:` / `example:`                    | **v1**        | Adds documentation metadata               |
| `group:` / `tag:`                      | **v1**        | Extends test runner                       |
| `lint:` (text only)                    | **v1.1**      | Requires spans and diagnostics            |
| `target:`                              | **v1.1**      | Requires build context + item enabling    |
| `lint:` / `safety:` / `analyze:` (AST) | **v1.2**      | Requires AST reflection                   |
| `ffi export`                           | **v1.2 / v2** | Needs type reflection + artifact emission |
| `trace:` / wrappers                    | **v2**        | Requires macros / codegen                 |
| `schema:` / `db:`                      | **v2+**       | Deep reflection + code emission           |
| `resource:` / `embed:`                 | **v2+**       | Needs macros / const generation           |

---

# 14. Closing Notes

Directives are intentionally designed to evolve:

* **v1** gives immediate utility (tests, benchmarks, docs) with zero semantic risk.
* **v1.x** introduces powerful read-only reflection enabling linting, static analysis, and platform specialization.
* **v2+** unlocks structural transformations and integration with a macro system.

The architecture ensures that:

* no duplication of parser or typechecker ever occurs,
* directive code remains pure, predictable Surge,
* reflection intrinsics expose only safe, immutable views,
* and each feature tier can be introduced independently without destabilizing the compiler.
