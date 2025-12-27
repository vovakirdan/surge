# Surge Language Specification Audit Report

**Date:** 2025-12-26
**Spec Version:** Draft 7 (docs/LANGUAGE.md)
**Test Location:** testdata/golden/spec_audit/

## Summary

This audit tested each chapter of LANGUAGE.md against the current implementation.
Categories:
- ✅ **PASS** - Implemented and working
- ⚠️ **PARTIAL** - Some features work, some don't
- ❌ **NOT IMPLEMENTED** - Feature described in spec but not implemented
- 🐛 **BUG** - Implemented but broken (sema or runtime)
- 📋 **SPEC ISSUE** - Implementation differs from spec, consider doc update

---

## §1 Lexical Structure

| Feature | Status | Notes |
|---------|--------|-------|
| Line comments `//` | ✅ PASS | |
| Block comments `/* */` | ✅ PASS | |
| Nested block comments | ✅ PASS | Fixed: nested block comments are treated as trivia |
| Identifiers | ✅ PASS | |
| Keywords | ✅ PASS | |
| Integer literals | ✅ PASS | dec, hex, bin, underscores |
| Float literals | ✅ PASS | |
| String literals | ✅ PASS | |
| Bool literals | ✅ PASS | |
| `nothing` literal | ✅ PASS | |

**Test file:** `s01_lexical.sg`

---

## §2 Types

### §2.1 Primitive Families
| Feature | Status | Notes |
|---------|--------|-------|
| `int`, `uint`, `float` | ✅ PASS | |
| `bool`, `string` | ✅ PASS | |
| `nothing` | ✅ PASS | |
| Fixed-width numerics | ✅ PASS | Sema + VM support for `int8`, `uint64`, `float32`, etc.; checked arithmetic, explicit same-type ops |

### §2.2 Arrays
| Feature | Status | Notes |
|---------|--------|-------|
| Growable array `T[]` | ✅ PASS | |
| Fixed-length `T[N]` | ✅ PASS | |
| Array indexing | ✅ PASS | |
| `len(arr)` | ✅ PASS | |

### §2.3 Ownership & References
| Feature | Status | Notes |
|---------|--------|-------|
| `own T` | ✅ PASS | Distinct from `T`; non-Copy requires explicit `own expr`, Copy has compatibility |
| `&T` (shared borrow) | ✅ PASS | Borrow rules + `@drop` verified |
| `&mut T` | ✅ PASS | Exclusive borrow rules verified |
| `*T` (raw pointer) | 🚫 RESTRICTED | Backend-only (`extern`/`@intrinsic`); rejected in user code (covered by sema raw-pointer tests) |
| Method with `&self` | ✅ PASS | Fixed: VM derefs ref receiver |

### §2.4 Generics
| Feature | Status | Notes |
|---------|--------|-------|
| Generic functions | ✅ PASS | |
| Generic types | ✅ PASS | `type Box<T> = { value: T }` |
| Turbofish syntax | ✅ PASS | `id::<int>(42)` |
| Type inference | ✅ PASS | From arguments |
| Generic tags as types | ✅ PASS | `let x: Tag<T>` works |

### §2.5 User-defined Types
| Feature | Status | Notes |
|---------|--------|-------|
| Struct | ✅ PASS | |
| `@readonly` fields | ✅ PASS | |
| Literal enum | 📋 SPEC FIXED | Spec uses `enum ...` declarations (no literal union syntax) |
| Integer enum | ✅ PASS | `enum HttpStatus: int` |
| Auto-increment enum | ✅ PASS | `enum Direction` |
| Struct extension | ✅ PASS | `type Child = Parent : { ... }` |

### §2.6-2.9 nothing, Tags, Option, Erring
| Feature | Status | Notes |
|---------|--------|-------|
| `nothing` type | ✅ PASS | |
| Custom tag declaration | ✅ PASS | Tag names are valid types in bindings |
| `Option<T>` | ✅ PASS | |
| `Some(v)` / `nothing` | ✅ PASS | |
| `Erring<T, Error>` | ✅ PASS | |
| `T?` sugar | ✅ PASS | |
| `T!` sugar | ✅ PASS | |

### §2.10 Tuples
| Feature | Status | Notes |
|---------|--------|-------|
| Tuple types | ✅ SEMA PASS | |
| Tuple literals | 🐛 VM BUG | `unimplemented: rvalue kind 6` |
| Tuple destructuring | ✅ SEMA PASS | |

### §2.11 Memory Management
| Feature | Status | Notes |
|---------|--------|-------|
| Pure ownership | ✅ PASS | Move semantics work |

### §2.12 Contracts
| Feature | Status | Notes |
|---------|--------|-------|
| Contract declaration | ✅ PASS | |
| Contract bounds | ✅ PASS | `<T: HasName>` |
| Structural checking | ✅ PASS | |

**Test files:** `s02_types_*.sg`

---

## §3 Expressions & Statements

### §3.1 Variables
| Feature | Status | Notes |
|---------|--------|-------|
| `let` declaration | ✅ PASS | |
| `let mut` | ✅ PASS | |
| `const` | ✅ PASS | Fixed: const refs lower to values in MIR |
| Top-level `let` | ✅ PASS | |
| Default initialization | ✅ PASS | Implicit `default<T>()`; refs are a sema error |

### §3.2 Control Flow
| Feature | Status | Notes |
|---------|--------|-------|
| `if`/`else` | ✅ PASS | |
| `while` | ✅ PASS | |
| C-style `for` | ✅ PASS | |
| `for...in` | ✅ PASS | |
| `break`/`continue` | ✅ PASS | |
| `return` | ✅ PASS | |

### §3.4 Indexing & Slicing
| Feature | Status | Notes |
|---------|--------|-------|
| Array indexing | ✅ PASS | |
| Index assignment | ✅ PASS | |
| String indexing | ✅ PASS | Returns code point |
| Negative indices | ✅ PASS | Covered by `vm_arrays/arrays_negative_index.sg`, `vm_strings/strings_basic.sg` |
| Range slicing | ✅ PASS | Covered by `vm_arrays/arrays_slice_view.sg`, `vm_strings/strings_basic.sg` |

### §3.6 Compare (Pattern Matching)
| Feature | Status | Notes |
|---------|--------|-------|
| `finally` pattern | ✅ PASS | |
| Binding pattern | ✅ PASS | |
| `Some(v)`/`nothing` | ✅ PASS | |
| Int literal patterns | 🐛 VM BUG | `expected bigint, got int` |
| Bool literal patterns | ✅ PASS | Manual run OK (`surge run` minimal compare snippet) |

**Test files:** `s03_*.sg`

---

## §4 Functions & Methods

### §4.1 Function Declarations
| Feature | Status | Notes |
|---------|--------|-------|
| Basic functions | ✅ PASS | |
| No return type (nothing) | ✅ PASS | |
| Variadic `...args` | 🐛 VM BUG | Sema accepts variadic signatures, but VM panics on extra args; `...args` still typed as element |

### §4.2 Attributes
| Feature | Status | Notes |
|---------|--------|-------|
| `@pure` | ✅ PASS | |
| `@overload` | ✅ PASS | |
| `@entrypoint` | ✅ PASS | |
| `@allow_to` | ✅ SEMA PASS | |
| `@backend` | ✅ SEMA PASS | |

### §4.4 extern<T> Methods
| Feature | Status | Notes |
|---------|--------|-------|
| Instance methods (value self) | ✅ PASS | |
| Instance methods (`&self`) | ✅ PASS | Fixed: VM derefs ref receiver |
| Static methods returning struct | 🐛 MIR BUG | |
| `pub` visibility | ✅ PASS | |

**Test files:** `s04_*.sg`

---

## §5 Modules & Imports

| Feature | Status | Notes |
|---------|--------|-------|
| `import path::item` | ✅ SEMA PASS | |
| `import ... as alias` | ✅ SEMA PASS | |
| Cross-module calls | 🐛 VM BUG | `unsupported intrinsic` for imported functions |

**Test file:** `s05_modules.sg`

---

## §6 Operators & Magic Methods

| Feature | Status | Notes |
|---------|--------|-------|
| Arithmetic `+ - * / %` | ✅ PASS | |
| Comparison `< <= == != >= >` | ✅ PASS | |
| Logical `&& \|\| !` | ✅ PASS | |
| Unary `-` | ✅ PASS | |
| Compound assign `+= -= *= /= %=` | ✅ PASS | |
| Ternary `? :` | ✅ PASS | |
| String concat `+` | ✅ PASS | |
| String repeat `*` | ❌ NOT IMPLEMENTED | |
| `is` operator | ✅ PASS | Supports union tags |
| `heir` operator | ✅ PASS | Struct extension + union members |
| Cast `to` | ✅ PASS | |

**Test files:** `s06_*.sg`

---

## §7 Literals & Inference

| Feature | Status | Notes |
|---------|--------|-------|
| Numeric defaults | ✅ PASS | int, float |
| String indexing | ✅ PASS | |
| Range literals | ✅ PASS | Sema coverage in `sema/valid/range_literals.sg`; VM exercised via slicing |
| String methods | ✅ PASS | Covered by `vm_strings/strings_std.sg`, `vm_strings/strings_rope_std.sg` |
| Array methods | ✅ PASS | Covered by `vm_arrays/arrays_push_pop.sg`, `vm_arrays/arrays_view_pop_panics.sg` |

**Test file:** `s07_literals.sg`

---

## §8 Overload Resolution

| Feature | Status | Notes |
|---------|--------|-------|
| Type-based overloading | ✅ PASS | |
| Monomorphic preference | ✅ PASS | |

**Test file:** `s08_overload.sg`

---

## §9 Concurrency

| Feature | Status | Notes |
|---------|--------|-------|
| `async fn` declaration | ✅ SEMA PASS | |
| `@backend` attribute | ✅ SEMA PASS | |
| `spawn` expression | ✅ SEMA ONLY | VM not implemented |
| `.await()` method | ✅ SEMA ONLY | VM not implemented |
| Channels | ✅ SEMA PASS | Covered by `sema/valid/concurrency/channel_basic_ops.sg`; VM not implemented |
| `parallel map/reduce` | ❌ v2+ FEATURE | |

**Test file:** `s09_concurrency_sema.sg`

---

## §10-11 Stdlib & Error Handling

| Feature | Status | Notes |
|---------|--------|-------|
| `print()` single arg | ✅ PASS | |
| `to string` casts | ✅ PASS | |
| `Erring<T, Error>` Success | ✅ PASS | |
| `Erring<T, Error>` Error | ✅ PASS | |
| `Option<T>` | ✅ PASS | |

**Test files:** `s10_stdlib.sg`, `s11_error_handling.sg`

---

## Priority Issues

### 🔴 Critical (Blocks basic usage)
1. **VM `expected struct, got ref`** - ✅ fixed; `&self` methods now work
2. **VM `unsupported intrinsic`** - blocks module imports at runtime

### 🟠 High (Common features broken)
1. **Tuples** - VM not implemented
2. **compare int literal patterns** - VM bug

### 🟡 Medium (Spec features missing)
1. ✅ fixed: **Enums (auto/int/string)** - implemented via `enum` declarations
2. ✅ fixed: **Struct extension** - inherited fields work
3. **String repeat `*`** - not implemented
4. ✅ fixed: **Nested block comments** - implemented in lexer

### 🟢 Low (Future features)
1. ✅ fixed: **Fixed-width numerics** - sema+VM with checked arithmetic
2. **async/spawn/await** - sema only
3. **Channels** - not implemented
4. **parallel map/reduce** - v2+ feature

---

## Recommendations

### 📝 Spec Updates Needed
1. ✅ fixed: **Nested block comments** - implemented in lexer
2. **Default initialization** - Update spec to clarify variables require initialization

### 🔧 Implementation Fixes Needed
1. **VM reference handling** - ✅ fixed for `&self` methods
2. **VM module linking** - Imported functions marked as unsupported intrinsic
3. **VM tuples** - rvalue kind 6 not implemented
4. **compare literal patterns** - bigint/int type mismatch

### 🚀 Features to Implement (Priority Order)
1. Tuples (VM)
2. ✅ fixed: Enums (auto/int/string)
3. ✅ fixed: Struct extension
4. ✅ fixed: `&self` method calls (VM)

---

## Test Files Summary

| File | Section | Status |
|------|---------|--------|
| `s01_lexical.sg` | §1 | ✅ PASS (except nested comments) |
| `s02_types_primitives.sg` | §2.1 | ✅ PASS |
| `s02_types_arrays.sg` | §2.2 | ✅ PASS |
| `s02_types_ownership.sg` | §2.3 | ✅ PASS |
| `s02_types_generics.sg` | §2.4 | ⚠️ PARTIAL |
| `s02_types_userdefined.sg` | §2.5 | ✅ PASS |
| `s02_types_tags_option.sg` | §2.6-2.9 | ✅ PASS |
| `s02_types_tuples.sg` | §2.10 | 🐛 VM BUG |
| `s02_types_contracts.sg` | §2.12 | ✅ PASS |
| `s03_expr_variables.sg` | §3.1 | ✅ PASS |
| `s03_control_flow.sg` | §3.2 | ✅ PASS |
| `s03_for_in.sg` | §3.2 | ✅ PASS |
| `s03_indexing.sg` | §3.4 | ✅ PASS |
| `s03_compare.sg` | §3.6 | ⚠️ PARTIAL |
| `s04_functions.sg` | §4.1 | ✅ PASS |
| `s04_attributes.sg` | §4.2 | ✅ PASS |
| `s04_extern.sg` | §4.4 | ⚠️ PARTIAL |
| `s05_modules.sg` | §5 | 🐛 VM BUG |
| `s06_operators.sg` | §6 | ⚠️ PARTIAL |
| `s06_heir.sg` | §6.3 | ✅ PASS |
| `s07_literals.sg` | §7 | ⚠️ PARTIAL |
| `s08_overload.sg` | §8 | ✅ PASS |
| `s09_concurrency_sema.sg` | §9 | ✅ SEMA PASS |
| `s10_stdlib.sg` | §10 | ⚠️ PARTIAL |
| `s11_error_handling.sg` | §11 | ✅ PASS |
