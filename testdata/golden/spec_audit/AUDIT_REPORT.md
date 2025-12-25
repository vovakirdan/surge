# Surge Language Specification Audit Report

**Date:** 2025-12-24
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
| Nested block comments | 📋 SPEC ISSUE | Spec says "nesting allowed" but `/* Nested /* block */ comment */` fails to parse |
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
| Fixed-width numerics | ❌ NOT IMPLEMENTED | `int8`, `uint64`, etc. parse as names only |

### §2.2 Arrays
| Feature | Status | Notes |
|---------|--------|-------|
| Growable array `T[]` | ✅ PASS | |
| Fixed-length `T[N]` | ✅ PASS | |
| Array indexing | ✅ PASS | |
| `len(arr)` | 🐛 MIR BUG | `MIR validation failed: unknown type` |

### §2.3 Ownership & References
| Feature | Status | Notes |
|---------|--------|-------|
| `own T` | ⚠️ PARTIAL | Sema works, distinct from `T` |
| `&T` (shared borrow) | ⚠️ PARTIAL | Sema works |
| `&mut T` | ⚠️ PARTIAL | Sema works |
| `*T` (raw pointer) | 🚫 RESTRICTED | Backend-only (`extern`/`@intrinsic`); rejected in user code (covered by sema raw-pointer tests) |
| Method with `&self` | ✅ PASS | Fixed: VM derefs ref receiver |

### §2.4 Generics
| Feature | Status | Notes |
|---------|--------|-------|
| Generic functions | ✅ PASS | |
| Generic types | ✅ PASS | `type Box<T> = { value: T }` |
| Turbofish syntax | ✅ PASS | `id::<int>(42)` |
| Type inference | ✅ PASS | From arguments |
| Generic tags as types | 🐛 SEMA BUG | `let x: Tag<T>` fails |

### §2.5 User-defined Types
| Feature | Status | Notes |
|---------|--------|-------|
| Struct | ✅ PASS | |
| `@readonly` fields | ✅ PASS | |
| Literal enum | ❌ NOT IMPLEMENTED | `type Color = "black" \| "white"` |
| Integer enum | ❌ NOT IMPLEMENTED | `enum HttpStatus: int` |
| Auto-increment enum | ❌ NOT IMPLEMENTED | `enum Direction` |
| Struct extension | ❌ NOT IMPLEMENTED | `type Child = Parent : { ... }` |

### §2.6-2.9 nothing, Tags, Option, Erring
| Feature | Status | Notes |
|---------|--------|-------|
| `nothing` type | ✅ PASS | |
| Custom tag declaration | ⚠️ PARTIAL | Parses but can't use as type in bindings |
| `Option<T>` | ✅ PASS | |
| `Some(v)` / `nothing` | ✅ PASS | |
| `Erring<T, Error>` | 🐛 MIR BUG | Error struct causes MIR issues |
| `T?` sugar | ✅ PASS | |
| `T!` sugar | 🐛 MIR BUG | Error struct involved |

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
| `const` | 🐛 MIR BUG | `unknown local symbol` |
| Top-level `let` | 🐛 MIR BUG | Same issue |
| Default initialization | 📋 SPEC ISSUE | Spec says "0 for int", VM panics "used before initialization" |

### §3.2 Control Flow
| Feature | Status | Notes |
|---------|--------|-------|
| `if`/`else` | ✅ PASS | |
| `while` | ✅ PASS | |
| C-style `for` | ✅ PASS | |
| `for...in` | 🐛 VM BUG | `unimplemented: rvalue kind 11` |
| `break`/`continue` | ✅ PASS | |
| `return` | ✅ PASS | |

### §3.4 Indexing & Slicing
| Feature | Status | Notes |
|---------|--------|-------|
| Array indexing | ✅ PASS | |
| Index assignment | ✅ PASS | |
| String indexing | ✅ PASS | Returns code point |
| Negative indices | ❌ NOT TESTED | |
| Range slicing | ❌ NOT TESTED | |

### §3.6 Compare (Pattern Matching)
| Feature | Status | Notes |
|---------|--------|-------|
| `finally` pattern | ✅ PASS | |
| Binding pattern | ✅ PASS | |
| `Some(v)`/`nothing` | ✅ PASS | |
| Int literal patterns | 🐛 VM BUG | `expected bigint, got int` |
| Bool literal patterns | ❌ NOT TESTED | |

**Test files:** `s03_*.sg`

---

## §4 Functions & Methods

### §4.1 Function Declarations
| Feature | Status | Notes |
|---------|--------|-------|
| Basic functions | ✅ PASS | |
| No return type (nothing) | ✅ PASS | |
| Variadic `...args` | ❌ NOT TESTED | |

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
| Null coalescing `??` | 🐛 VM BUG | `unimplemented: binary op ??` |
| String concat `+` | ✅ PASS | |
| String repeat `*` | ❌ NOT IMPLEMENTED | |
| `is` operator | 🐛 MIR BUG | `unknown local symbol` |
| `heir` operator | ❌ NOT TESTABLE | Requires struct extension |
| Cast `to` | ✅ PASS | |

**Test files:** `s06_*.sg`

---

## §7 Literals & Inference

| Feature | Status | Notes |
|---------|--------|-------|
| Numeric defaults | ✅ PASS | int, float |
| String indexing | ✅ PASS | |
| Range literals | ❌ NOT TESTED | |
| String methods | ❌ NOT TESTED | |
| Array methods | ❌ NOT TESTED | |

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
| Channels | ❌ NOT TESTED | VM not implemented |
| `parallel map/reduce` | ❌ v2+ FEATURE | |

**Test file:** `s09_concurrency_sema.sg`

---

## §10-11 Stdlib & Error Handling

| Feature | Status | Notes |
|---------|--------|-------|
| `print()` single arg | ✅ PASS | |
| `print()` multi arg | 📋 SPEC ISSUE | Spec says space-separated, only first arg printed |
| `to string` casts | ✅ PASS | |
| `Erring<T, Error>` Success | ✅ PASS | |
| `Erring<T, Error>` Error | 🐛 MIR BUG | Error struct issues |
| `Option<T>` | ✅ PASS | |

**Test files:** `s10_stdlib.sg`, `s11_error_handling.sg`

---

## Priority Issues

### 🔴 Critical (Blocks basic usage)
1. **MIR `unknown type` bug** - affects `len()`, `const`, top-level `let`, generics
2. **VM `expected struct, got ref`** - ✅ fixed; `&self` methods now work
3. **VM `unsupported intrinsic`** - blocks module imports at runtime

### 🟠 High (Common features broken)
1. **for...in loop** - VM not implemented
2. **Tuples** - VM not implemented
3. **Null coalescing `??`** - VM not implemented
4. **`is` operator** - MIR bug
5. **compare int literal patterns** - VM bug

### 🟡 Medium (Spec features missing)
1. **Literal enums** - not implemented
2. **Integer enums** - not implemented
3. **Struct extension** - not implemented
4. **String repeat `*`** - not implemented
5. **Nested block comments** - spec says allowed, parser rejects

### 🟢 Low (Future features)
1. **Fixed-width numerics** - parse only
2. **async/spawn/await** - sema only
3. **Channels** - not implemented
4. **parallel map/reduce** - v2+ feature

---

## Recommendations

### 📝 Spec Updates Needed
1. **Nested block comments** - Either implement or update spec to say not supported
2. **Default initialization** - Update spec to clarify variables require initialization
3. **print() variadic** - Update spec if multi-arg is intentionally first-only

### 🔧 Implementation Fixes Needed
1. **MIR type resolution** - Many issues stem from unknown type in MIR phase
2. **VM reference handling** - ✅ fixed for `&self` methods
3. **VM module linking** - Imported functions marked as unsupported intrinsic
4. **VM for-in loop** - rvalue kind 11 not implemented
5. **VM tuples** - rvalue kind 6 not implemented
6. **VM null coalescing** - binary op ?? not implemented
7. **compare literal patterns** - bigint/int type mismatch

### 🚀 Features to Implement (Priority Order)
1. Enums (literal, integer, auto-increment)
2. Struct extension
3. For-in loops (VM)
4. Tuples (VM)
5. ✅ fixed: `&self` method calls (VM)

---

## Test Files Summary

| File | Section | Status |
|------|---------|--------|
| `s01_lexical.sg` | §1 | ✅ PASS (except nested comments) |
| `s02_types_primitives.sg` | §2.1 | ✅ PASS |
| `s02_types_arrays.sg` | §2.2 | ⚠️ PARTIAL |
| `s02_types_ownership.sg` | §2.3 | ⚠️ PARTIAL |
| `s02_types_generics.sg` | §2.4 | ⚠️ PARTIAL |
| `s02_types_userdefined.sg` | §2.5 | ⚠️ PARTIAL |
| `s02_types_tags_option.sg` | §2.6-2.9 | ⚠️ PARTIAL |
| `s02_types_tuples.sg` | §2.10 | 🐛 VM BUG |
| `s02_types_contracts.sg` | §2.12 | ✅ PASS |
| `s03_expr_variables.sg` | §3.1 | ⚠️ PARTIAL |
| `s03_control_flow.sg` | §3.2 | ✅ PASS |
| `s03_for_in.sg` | §3.2 | 🐛 VM BUG |
| `s03_indexing.sg` | §3.4 | ✅ PASS |
| `s03_compare.sg` | §3.6 | ⚠️ PARTIAL |
| `s04_functions.sg` | §4.1 | ✅ PASS |
| `s04_attributes.sg` | §4.2 | ✅ PASS |
| `s04_extern.sg` | §4.4 | ⚠️ PARTIAL |
| `s05_modules.sg` | §5 | 🐛 VM BUG |
| `s06_operators.sg` | §6 | ⚠️ PARTIAL |
| `s06_heir.sg` | §6.3 | ❌ NOT TESTABLE |
| `s07_literals.sg` | §7 | ⚠️ PARTIAL |
| `s08_overload.sg` | §8 | ✅ PASS |
| `s09_concurrency_sema.sg` | §9 | ✅ SEMA PASS |
| `s10_stdlib.sg` | §10 | ⚠️ PARTIAL |
| `s11_error_handling.sg` | §11 | ⚠️ PARTIAL |
