# Surge Language Specification (Draft 5)

> **Status:** Draft for review
> **Scope:** Full language surface for tokenizer → parser → semantics. VM/runtime details are out of scope here and live in RUNTIME.md / BYTECODE.md.

---

## 0. Design Goals

* **Safety by default:** explicit ownership/borrows; no hidden implicit mutability.
* **Deterministic semantics:** operator/overload resolution is fully specified.
* **Orthogonality:** features compose cleanly (e.g., generics + ownership + methods via `extern<T>`).
* **Compile-time rigor, runtime pragmatism:** statically-typed core with dynamic-width numeric families (`int`, `uint`, `float`).
* **Testability:** first-class doc-tests via `/// test:` directives.
* **Explicit over implicit:** prefer clear, verbose constructs over clever shortcuts that obscure ownership or control flow.
* **Ownership clarity:** every operation's ownership semantics should be obvious from the syntax.

---

## 1. Lexical Structure

### 1.1. Whitespace & Newlines

Whitespace (space, tab, CR, LF) separates tokens and has no semantic meaning except within string literals and for line-based comments.

### 1.2. Comments

* Line: `// ...` (until end-of-line)
* Block: `/* ... */` (nesting allowed)
* Directive: `///` introduces tooling directives (tests, future lints). See §13.

### 1.3. Identifiers

```
Ident ::= [A-Za-z_][A-Za-z0-9_]*
```

Identifiers are case-sensitive. `snake_case` is conventional for values and functions; `CamelCase` for type names.

### 1.4. Keywords

```
pub, fn, let, mut, if, else, while, for, in, break, continue,
import, type, extern, return, signal, compare, spawn,
true, false, nothing, is, finally, async, await, macro, pragma,
@pure, @overload, @override, @backend, @deprecated,
@packed, @align, @shared, @atomic, @raii, @arena, @weak, @readonly, @hidden, @noinherit, @sealed
```

### 1.5. Literals

* Integer: `0`, `123`, `0xFF`, `0b1010`, underscores allowed for readability: `1_000`.
* Float: `1.0`, `0.5`, `1e-9`, `2.5e+10`.
* String: `"..."` (UTF-8), escape sequences `\n \t \" \\` and `\u{hex}`.
* Bool: `true`, `false`.
* Absence value: `nothing` is the single "no value" literal used for void/null/absent semantics.

---

## 2. Types

Types are written postfixed: `name: Type`.

### 2.1. Primitive Families

* **Dynamic-width numeric families** (arbitrary width, implementation-defined precision, but stable semantics):

  * `int` – signed integer of unbounded width.
  * `uint` – unsigned integer of unbounded width.
  * `float` – floating-point of high precision (≥ IEEE754-64 semantics guaranteed; implementations may be wider).
* **Fixed-size numerics** (layout-specified):

  * `int8, int16, int32, int64`, `uint8, uint16, uint32, uint64`.
  * `float16, float32, float64`.
* **Other primitives**:

  * `bool` – logical; no implicit cast to/from numeric.
  * `string` – a sequence of Unicode scalar values (code points). Layout: dynamic array of code points.

**Coercions:**

* Fixed → dynamic of same family: implicit and lossless (e.g., `int32 -> int`).
* Dynamic → fixed **never implicit**. Explicit cast required; may trap if out of range.
* Between numeric families: no implicit. Explicit casts defined; may round or trap depending on target.

### 2.2. Arrays

`T[]` is a growable, indexable sequence of `T` with zero-based indexing.

* Indexing calls magic methods: `__index(i:int) -> T` and `__index_set(i:int, v:T) -> nothing`.
* Iterable if `extern<T[]> { __range() -> Range<T> }` is provided (stdlib provides this for arrays).

### 2.3. Ownership & References

* `own T` – owning value, moved by default.
* `&T` – shared immutable borrow (read-only view).
* `&mut T` – exclusive mutable borrow.
* `*T` – raw pointer (unsafe operations; deref requires explicit `deref(*p)` call).

Borrowing rules:

* While a `&mut T` exists, no other `&` or `&mut` borrows to the same value may exist.
* While any `&T` borrows exist, mutation of the underlying value is forbidden (the value is frozen).
* Lifetimes are lexical; the compiler emits diagnostics for aliasing violations.

**Moves & Copies:**

* Primitive fixed-size types and `bool` are `Copy`; `string` and arrays are `own` by default (move). The compiler may optimize small-string copies, but semantics are move.
* Assignment `x = y;` moves if `y` is `own` and `T` not `Copy`. Borrowing uses `&`/`&mut` operators: `let r: &T = &x;`, `let m: &mut T = &mut x;`.

**Function parameters:**

* `fn f(x: own T)`: takes ownership.
* `fn f(x: &T)`: shared-borrows; caller retains ownership; lifetime is lexical.
* `fn f(x: &mut T)`: exclusive mutable borrow.
* `fn f(x: *T)`: raw; callee must not assume safety.

### Ownership and threads

To avoid data races the following conservative rule applies:

* Only `own T` values may be moved (transferred) into spawned tasks/threads.
* Borrowed references `&T` and `&mut T` are not allowed to cross thread boundaries (attempting to do so is a compile-time error).

This rule simplifies early implementation and preserves soundness of the ownership model without a full cross-thread borrow-checker.

### 2.4. Generics

Generic parameters must be declared explicitly with angle brackets: `<T, U, ...>`. Implicit type variables (using a bare `T` without declaration) are not supported.

* Functions: `fn id<T>(x: T) -> T { return x; }`
* Extern methods/blocks: `extern<T> { ... }`

### 2.5. User-defined Types

* **Newtype:** `type MyInt = int;` creates a distinct nominal type that inherits semantics of `int` but can override magic methods via `extern<MyInt>`. (Different from a pure alias.)
* **Struct:** `type Person = { age:int, name:string, @readonly weight:float }`.

  * Fields are immutable unless variable is `mut`. `@readonly` forbids writes even through `mut` bindings.
* **Literal enums:** `type Color = "black" | "white";` Only the listed literals are allowed values.
* **Type alias:** `type Number = int | float;` a type that admits any member type; overload resolution uses the best matching member (§8).

#### Struct extension

```sg
type Person = { age:int, @hidden weight:float, @readonly name:string }

type PersonSon = Person : { patronymic: string = "" }

let p: Person    = { age=20, weight=80.0, name="Alex" }
let s: PersonSon = p            // patronymic picks the default ""
// Assigning PersonSon back to Person is illegal (strict nominality).
```

- `type Child = Base : { ... }` inherits all fields from `Base` and appends new ones.
- Defaults initialise missing fields; absence of a default requires callers to provide the field explicitly.
- `@hidden` fields remain hidden outside `extern<Base>` and initialisers. `@readonly` fields stay immutable after construction.
- Assigning from a child to its base is forbidden (types remain nominal).
- Field name clashes trigger `E_FIELD_CONFLICT` during expansion.
- Methods defined in `extern<Base>` are visible on `Child`. Override behaviour lives in `extern<Child>` with `@override` marking intentional replacements.

### 2.6. `nothing` — the single absence value

`nothing` is the sole inhabitant of the type `nothing` and represents "no value" for void returns, null-like states, and explicit absence.

- Functions without an explicit `-> Type` return `nothing`. `fn f() {}` is sugar for `fn f() -> nothing { return nothing; }`.
- `nothing` has no literal shorthand other than the keyword itself. There is no `unit`/`()` type in Surge.
- Context must accept the type `nothing`; using `nothing` where the target type cannot absorb it emits `E_AMBIGUOUS_NOTHING`.
- Arrays remain homogeneous: `[nothing, nothing]` has type `nothing[]`. Mixing `nothing` with other literals requires an explicit union or alias accommodating both members.

### 2.7. Tags (`tag`) and tagged unions

Tags describe explicit union constructors and are declared ahead of use:

```sg
tag Ping();
tag Pair<A, B>(A, B);
```

- `Name(args...)` constructs a value whose discriminant is `Name` and whose payload tuple matches the declaration.
- The declaration parentheses list payload types; `tag Ping();` produces a payload-less constructor, `tag Pair<A, B>(A, B);` expects two arguments.
- Tags live in their own namespace. They are not callable functions and they are not first-class types. To pass a constructor, wrap it in a closure: `fn(x:T) -> Alias<T> { return Name(x); }`.
- When an identifier resolves both to a tag and to a function and is used in the form `Ident(...)`, resolution fails with `E_AMBIGUOUS_CONSTRUCTOR_OR_FN` (§3.6, §15).

Tags participate in alias unions as variants. They may declare generic parameters ahead of the payload list: `tag Some<T>(T);` introduces a tag family parameterised by `T`.

### 2.8. Alias unions

`type` can describe an alias that builds sum types with or without tags:

```sg
// Untagged members (minimal surface)
type Number = int | float
type MaybeInt = int | nothing

// Tagged members (recommended for public APIs)
tag Left(L); tag Right(R);
type Either<L, R> = Left(L) | Right(R)
```

- Untagged unions rely on runtime type tests: `compare v { x if x is int => ... }`.
- Tagged unions enable concise and exhaustive pattern matching. For tagged unions the compiler checks that all declared tags are covered in `compare`, unless a `finally` arm is present; missing variants trigger `E_NONEXHAUSTIVE_MATCH`.
- Mixing many untagged structural types may lead to `E_AMBIGUOUS_UNION_MEMBERS` when the runtime cannot tell members apart (identical or overlapping runtime layout without a discriminant). Prefer tags for evolving APIs and stability.

### 2.9. Option and Result via tags

The standard library defines canonical constructors and aliases:

```sg
tag Some<T>(T); tag Ok<T>(T); tag Error<E>(E);

type Option<T> = Some(T) | nothing
type Result<T, E> = Ok(T) | Error(E)

fn head<T>(xs: T[]) -> Option<T> {
  if (xs.len == 0) { return nothing; }
  return Some(xs[0]);
}

fn parse(s: string) -> Result<int, Error> {
  if (s == "42") { return Ok(42); }
  let e: Error = { message: "bad", code: 1 };
  return Error(e); // explicit constructor required
}

compare head([1, 2, 3]) {
  Some(v) => print(v),
  nothing => print("empty")
}

compare parse("x") {
  Ok(v)      => print("ok", v),
  Error(err) => print("err", err)
}
```

Rules:

- There is no auto-wrapping when returning from `-> Result<T, E>`; use `Ok(...)` or `Error(...)`. Returning a bare payload yields `E_EXPECTED_TAGGED_VARIANT`.
- `Option<T>` also requires explicit constructors. Future toolchains may allow opt-in auto-wrapping, but the base language keeps construction explicit.
- `nothing` remains the shared absence literal for both Option and other contexts (§2.6).

### 2.10. Memory Management Model

Surge uses **pure ownership** (similar to Rust) for predictable performance:

**Core principles:**
- All heap-allocated data has exactly one owner at any time
- Ownership can be transferred (moved) or temporarily borrowed
- No garbage collector - deterministic cleanup via RAII
- Reference cycles must be broken explicitly using weak references (future extension)

**Rationale for scientific computing and AI backend:**
- Predictable performance characteristics (no GC pauses)
- Zero-cost abstractions
- Fine-grained control over memory layout
- Suitable for real-time and high-performance computing

**Future extensions:**
- Weak references for breaking cycles: `weak<T>`
- Arena allocators for bulk allocation patterns
- Custom allocators for GPU memory management

**Trade-offs:**
- Higher learning curve compared to GC languages
- More explicit lifetime management required
- Better performance and predictability for target domains

---

## 3. Expressions & Statements

### 3.1. Variables

* Declaration: `let name: Type = expr;`
* Mutability: `let mut x: Type = expr;` allows assignment `x = expr;` and in-place updates.
* Declaration without an initializer: `let x: Type;` (see default-initialisation rules below).
* Top-level `let` is allowed as an item; items are private by default and can be exported with `pub let`.

**Default initialisation rules:**

- Numeric types → `0` / `0.0`; `bool` → `false`; `string` → `""`.
- Arrays, maps, and other collection literals → empty instance of that container.
- Structs → every field must have an explicit default; otherwise `let x: Struct;` is rejected (`E_MISSING_FIELD_DEFAULT`).
- Untagged unions and aliases → declaration without an initializer is rejected unless every member has a well-defined zero/empty default (`E_UNDEFINED_DEFAULT`).
- Tagged unions (`Option`, `Result`, `Either`, …) → require an explicit initializer (`E_UNDEFINED_DEFAULT`).

Top-level `let` initialization and cycles:

* Top-level `let` items are executed at module initialization time in textual order within a module.
* Cyclic initialization among top-level `let`s is a compile-time error `E_CYCLIC_TOPLEVEL_INIT`.

### 3.2. Control Flow

* If: `if (cond) { ... } else if (cond) { ... } else { ... }`
* While: `while (cond) { ... }`
* For counter: `for (init; cond; step) { ... }` where each part may be empty.
* For-in iteration: `for item:T in xs:T[] { ... }` requires `__range()`.
* `break`, `continue`, `return expr?;`.

For loops (two syntactic forms):

1) C-style counter:

```
for ( init? ; cond? ; post? ) { body }
```

* init: `let` or expression statement
* cond: boolean expression (defaults true if omitted)
* post: expression statement

2) For-in (foreach):

```
for pattern (":" Type)? in Expr { body }
```

* `pattern` may be an identifier; future iterations may add destructuring.
* Type annotation is optional: the parser accepts `for item in seq { ... }` and leaves element-type inference to later semantic analysis.
* When `: Type` is supplied it must describe a valid type; malformed annotations emit `E_FOR_MISSING_TYPE` diagnostics.

Parser diagnostics:

* `E_FOR_MISSING_IN` — `for`-in form lacks `in`.
* `E_FOR_BAD_HEADER` — mismatched semicolons in C-style `for`.

### 3.3. Semicolons

* Statements end with `;` except block bodies and control-structure headers.

### 3.4. Indexing & Slicing

* `arr[i]` desugars to `arr.__index(i)`.
* `arr[i] = v` desugars to `arr.__index_set(i, v)`.

### 3.5. Signals (Reactive Bindings)

* `signal name := expr;` binds `name` to the value of `expr`, re-evaluated automatically when any of its dependencies change.
* Signals are single-assignment; you cannot assign to `name` directly.
* Update semantics: changes propagate in topological order per turn. The bound expression must be `@pure`; side-effects inside signal evaluation are disallowed. Violations emit `E_SIGNAL_NOT_PURE`.

### 3.6. Compare (Pattern Matching)

`compare` is an expression that branches on a value against patterns. It evaluates to the expression of the first matching arm.

```
compare value {
  pattern1 => expr1;
  pattern2 => expr2;
  finally  => fallback;
}
```

Patterns (baseline set):

- `finally` – wildcard, matches anything (default case).
- Literals – `123`, `"str"`, `true`, `false`.
- Bindings – `name` binds the matched value in that arm.
- Tagged constructors – `Tag(p)` such as `Some(x)`, `Ok(v)`, `Error(e)`; payload patterns recurse.
- `nothing` – matches the absence literal of type `nothing`.
- Conditional patterns – `x if x is int` where `x` is bound and condition is checked.

Examples:

```sg
compare v {
  Some(x) => print(x),
  nothing => print("empty")
}

compare r {
  Ok(v)      => print("ok", v),
  Error(err) => print("err", err)
}
```

Notes:

- Arms are tried top-to-bottom; the first match wins.
- `=>` separates pattern from result expression and is only valid within `compare` arms and parallel constructs.
- Tagged unions must cover every declared tag (or provide `finally`) or emit `E_NONEXHAUSTIVE_MATCH`. Untagged unions skip this exhaustiveness check.
- If both a tag constructor and a function named `Ident` are in scope, using `Ident(...)` emits `E_AMBIGUOUS_CONSTRUCTOR_OR_FN`.

---

## 4. Functions & Methods

### 4.1. Function Declarations

```
fn name(params) -> RetType? { body }
params := name:Type (, ...)* | ... (variadic)
```

* Example: `fn add(a:int, b:int) -> int { return a + b; }`
* Variadics: `fn print(...args) { ... }`

Variadics: `...args` denotes a variadic parameter and desugars to an array parameter (e.g., `args: T[]`). Overload resolution treats variadic candidates as matching variable arity with an added cost versus exact-arity matches.

**Return type semantics:**

* Functions without `-> RetType` return `nothing` and may omit explicit `return` statements.
  * `fn main() { ... }` - valid, implicit `return nothing;`
  * `fn main() { return nothing; }` - also valid, explicitly returns the absence value
* Functions with `-> RetType` must return a value of that type.
  * `fn answer() -> int { return 42; }`

### 4.2. Attributes

Attributes are a **closed set** provided by the language. User-defined attributes are not supported.

#### A. Contracts, Behavior, and Overloading

* `@pure` *(fn)* — function has no side effects, is deterministic, cannot mutate non-local state. Required for execution in signals and parallel contexts. Violations emit `E_PURE_VIOLATION`.
* `@overload` *(fn)* — declares an overload of an existing function name with a distinct signature. Must not be used on the first declaration of a function name; doing so emits `E_OVERLOAD_FIRST_DECL`. Incompatible with `@override`.
* `@override` *(fn)* — replaces an existing implementation for a target type. Only valid within `extern<T>` and `extern<Newtype>` blocks. Attempting to override primitive base types directly emits `E_PRIMITIVE_SEALED` (use newtype instead). Incompatible with `@overload`.

  **Exception:** `@override` may be used outside `extern<T>` only if the target symbol is local to the current module (declared earlier in the same module) and previously had no body implementation. This allows completing forward declarations. Attempting to override functions from standard library/imports or any public symbols from other modules emits `E_OVERRIDE_FORBIDDEN_TARGET`.
* `@intrinsic` *(fn)* — marks function as a language intrinsic (implementation provided by runtime/compiler). Intrinsics are declared only as function declarations without body (`fn name(...): Ret;`) in the special module `core/intrinsics` and made available to other code through standard library re-exports. User code cannot declare intrinsics outside `core/intrinsics`. Violations emit errors per §21.

#### B. Code Generation and ABI

* `@backend("cpu"|"gpu"|Ident)` *(fn|block)* — execution target hint for backend-specific lowering. Unsupported targets emit `W_BACKEND_UNSUPPORTED` or error in strict mode.
* `@packed` *(type|field)* — tightly packed memory layout without padding.
* `@align(N)` *(type|field)* — memory alignment requirement. Conflicts with `@packed` when alignment `N` is impossible emit `E_ATTR_CONFLICT`.

#### C. Memory and Resources

* `@raii` *(type)* — enables automatic resource cleanup (destructor) when values leave scope.
* `@arena` *(type|field|param)* — hint for arena-based memory allocation policy.
* `@weak` *(field)* — weak reference for breaking cycles (reserved for future extension).
* `@shared` *(type|field)* — marks data as thread-safe. Does not override the rule that only `own` values may cross thread boundaries.
* `@atomic` *(field)* — atomic operations for lock-free data access.

#### D. Visibility, Inheritance, and Structural Rules

* `@readonly` *(field)* — field cannot be written even through `mut` bindings.
* `@hidden` *(field)* — field is hidden outside `extern<Base>` blocks and initializers.
* `@noinherit` *(field)* — field is **not inherited** when extending types via `type Child = Base : {...}`.
* `@sealed` *(type)* — type cannot be extended through `Base : {...}` inheritance. Attempting extension emits `E_TYPE_SEALED`.

#### E. Concurrency Contracts

* `@guarded_by("lock")` *(field)* — field is protected by the specified mutex or read-write lock. The string `"lock"` must refer to a field name of type `Mutex` or `RwLock` in the same struct.
* `@requires_lock("lock")` *(fn)* — function requires the caller to hold the specified lock when called. The string `"lock"` must refer to a field name of type `Mutex` or `RwLock`.
* `@acquires_lock("lock")` *(fn)* — function acquires the specified lock and holds it until function exit.
* `@releases_lock("lock")` *(fn)* — function releases the specified lock before function exit.
* `@waits_on("cond")` *(fn)* — function may block waiting on the specified condition variable or semaphore. The string `"cond"` must refer to a field name of type `Condition` or `Semaphore`.
* `@send` *(type)* — type can be safely transferred between tasks/threads (move-safe). Conflicts with `@nosend`.
* `@nosend` *(type)* — type is forbidden from being transferred between tasks/threads. Conflicts with `@send`.
* `@nonblocking` *(fn)* — function performs no blocking waits. Conflicts with `@waits_on`.

All string parameters (`"lock"`, `"cond"`) must be **field names or parameter names**, not expressions or function calls.

#### Closed Set Rule

Attributes are a **closed set** defined by the language. Tests, benchmarks, and timing measurements are implemented through the directive system (§13), not attributes. Attributes affect language semantics and ABI; directives are toolchain mechanisms that do not modify program semantics.

#### Applicability Matrix

| Attribute        |  Fn | Block | Type | Field | Param |
| ---------------- | :-: | :---: | :--: | :---: | :---: |
| @pure            |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |
| @overload        |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |
| @override        |  ✅* |   ❌   |   ❌  |   ❌   |   ❌   |
| @intrinsic       |  ✅** |   ❌   |   ❌  |   ❌   |   ❌   |
| @backend         |  ✅  |   ✅   |   ❌  |   ❌   |   ❌   |
| @packed          |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |
| @align           |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |
| @raii            |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |
| @arena           |  ❌  |   ❌   |   ✅  |   ✅   |   ✅   |
| @weak            |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |
| @shared          |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |
| @atomic          |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |
| @readonly        |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |
| @hidden          |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |
| @noinherit       |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |
| @sealed          |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |
| @guarded_by      |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |
| @requires_lock   |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |
| @acquires_lock   |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |
| @releases_lock   |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |
| @waits_on        |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |
| @send            |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |
| @nosend          |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |
| @nonblocking     |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |

*`@override` — only within `extern<T>` and `extern<Newtype>` blocks.
**`@intrinsic` — only on function declarations (FnDecl) without body.

#### Typical Conflicts

* `@overload` + first function declaration → `E_OVERLOAD_FIRST_DECL`
* `@override` + `@overload` on same declaration → `E_ATTR_CONFLICT`
* `@packed` + impossible `@align(N)` → `E_ATTR_CONFLICT`
* `@sealed` + attempt to extend type → `E_TYPE_SEALED`
* `@send` + `@nosend` on same type → `E_CONC_CONTRACT_CONFLICT`
* `@nonblocking` + `@waits_on` on same function → `E_CONC_CONTRACT_CONFLICT`

#### Limitations for @intrinsic

* Target platform and ABI for intrinsics are fixed in RUNTIME.md.
* List of permitted names is restricted: `rt_alloc`, `rt_free`, `rt_realloc`, `rt_memcpy`, `rt_memmove`. Any other names → error.
* Intrinsics cannot have body; any attempts to provide implementation or call `@intrinsic` outside `core/intrinsics` → errors (§21).

Parser behavior:

* Unknown attributes produce `W_UNKNOWN_ATTRIBUTE` by default. `--strict-attributes` mode promotes this to `E_UNKNOWN_ATTRIBUTE`.
* Attributes used on unsupported targets emit `E_ILLEGAL_ATTRIBUTE_TARGET`.

### 4.3. Overloading Rules

* A function name may have multiple signatures only when each additional signature is declared with `@overload`.
* Call resolution (§8) is deterministic and based on exact types after generic instantiation and ownership adjustments.

### 4.4. Methods via `extern<T>`

```
extern<T> {
  fn method(self: &T, ...)
  fn __index(self:&T, i:int) -> Elem
  // magic operators live here
}
```

* Methods are dispatched statically by the receiver type (`T`). No dynamic dispatch.
* Built-in magic names implement operators and protocols (see below).

### 4.5. Macros

Macros provide compile-time code generation and metaprogramming capabilities:

```sg
macro assert_eq(left: expr, right: expr) {
    if (!(left == right)) {
        panic("Assertion failed: " + stringify(left) + " != " + stringify(right));
    }
}

// Usage
fn test_something() {
    assert_eq(2 + 2, 4); // Expands to assertion code
}
```

**Macro rules:**
* Macros are declared with `macro` keyword followed by parameter list
* Macro parameters have types like `expr`, `ident`, `type`, `block`, `meta`
* Macros are called like functions: `macro_name(args)`
* Macros expand at compile-time before type checking
* Recursive macro expansion is limited to prevent infinite loops

**Built-in macro functions:**
* `stringify(expr)` – converts expression to string literal
* `type_name_of<T>()` – returns type name as string
* `size_of<T>()` – returns size of type in bytes
* `align_of<T>()` – returns alignment requirement of type

**Implementation Status:** Macros are reserved for future iterations. The syntax and semantics are specified for completeness, but core implementation work should focus on other features first. Macros will be added after the base language is stable.

---

## 5. Modules & Imports

### 5.1. Files & Modules

Each file is a module. Folder hierarchy maps to module paths.

### 5.2. Importing

* Module or submodule: `import math/trig;`
* Specific item: `import math/trig::sin;`
* Aliasing: `import math/trig::sin as sine;`

**Name resolution order:** local scope > explicit imports > prelude/std.

---

## 6. Operators & Magic Methods

### 6.1. Operator Table

* Arithmetic: `+ - * / %` → `__add __sub __mul __div __mod`
* Comparison: `< <= == != >= >` → `__lt __le __eq __ne __ge __gt` (must return `bool`)
* Type checking: `is` – checks type identity (see §6.2)
* Logical: `&& || !` – short-circuiting; operate only on `bool`.
* Indexing: `[]` → `__index __index_set`
* Unary: `+x -x` → `__pos __neg`
* Abs: `abs(x)` → `__abs`
* To-string: used by `print` → `__to_string() -> string`
* Range: `for in` → `__range() -> Range<T>` where `Range<T>` yields `T` via `next()`.
* Result propagation: `expr?` — if `expr` is `Result<T,E>`, yields `T` or returns the value `Error(e)` from the current function (see §11).
* Compound assignment: `+= -= *= /= %= &= |= ^= <<= >>=` → corresponding operation + assign.
* Ternary: `condition ? true_expr : false_expr` → conditional expression.
* Null coalescing: `optional ?? default` → returns default if optional is `nothing`.
* Range creation: `start..end`, `start..=end`, `..end`, `start..` → range operators.
* String operators: `string * count` → string repetition, `string + string` → concatenation.
* Array operators: `array + array` → concatenation, `array[index]` → element access.

```
// Rationale:
// Compound assignment provides clear semantics: x += 1
// C-style increment removed due to ownership ambiguity
// Safe-navigation (?.) is not part of Surge; prefer compare for Option
```

### 6.2. Type Checking Operator (`is`)

The `is` operator performs runtime type checking and returns a `bool`. It checks the essential type identity, ignoring ownership modifiers:

**Rules:**
* `42 is int` → `true`
* `nothing is nothing` → `true`
* `Option<int> is int` → `false` (it's `Option<int>`, not `int`)
* `own T is T` → `true` (ownership doesn't change type essence)
* Mutability of a binding does not affect `is`: `let mut x:T; (x is T) == true`.
* `&T is T` → `false` (reference is different type)
* `*T is T` → `false` (pointer is different type)

**Examples:**
```sg
let x: int = 42;
let y: Option<int> = Some(42);
let z: &int = &x;

print(x is int);        // true
print(y is int);        // false
print(y is Option<int>); // true
print(z is int);        // false
print(z is &int);       // true
```

### 6.3. Assignment

* `=` move/assign; compound ops `+=` etc. desugar to method + assign if defined.

### 6.4. Cast Operator (`to`)

The `to` operator performs explicit type conversions with syntax `Expr to Type`.

**Precedence:** Postfix operator with same precedence as `.await`, before `?` (see §14).

**Built-in cast rules:**

* **Identical type:** NOP (no operation).
* **Fixed → dynamic (same family):** Always allowed, e.g., `int32 to int`.
* **Dynamic → fixed (same family):** Explicit only, may trap at runtime if value exceeds target range.
* **Between numeric families:** Explicit only with defined semantics:
  * `int` ↔ `float`: standard conversion with potential precision loss.
  * `int` ↔ `uint`: may trap on negative values or overflow.
  * Between fixed types of different sizes: may trap if significant bits are lost.
* **Reference and pointer types:** `&T`, `&mut T`, and `*T` cannot be cast via `to` (compile error).
* **Tag constructors:** No casting to/from tags; use constructors and `compare` matching.

**Examples:**
```sg
let a: int32 = 300000;
let b: int = a to int;        // fixed→dynamic, always safe
let c: int16 = a to int16;    // may trap if value > int16 range
let d: float = 42 to float;   // int→float conversion
```

### 6.5. Custom Cast Protocol (`__cast`)

User-defined types can implement custom cast behavior via the `__cast` magic method:

```sg
extern<From> {
  fn __cast<To>(self: From) -> To
}
```

**Resolution rules:**

1. If `From == To` → NOP (no cast needed).
2. If built-in cast exists → use built-in rule.
3. Otherwise, search for `__cast<To>` implementation in `extern<From>`.
4. Multiple implementations → `E_AMBIGUOUS_CAST`.
5. No implementation found → `E_NO_CAST`.

**Orphan rule:** At least one of `From` or `To` must be local to the current module.

**Overlapping implementations:** Conflicting `__cast` definitions emit `E_CAST_OVERLAP`.

**Restrictions:**
* Direct calls to `__cast` are forbidden; only the `to` operator may invoke it.
* The `to` operator is the sole interface for casting.

**Examples:**
```sg
type UserId = uint64;

extern<UserId> {
  fn __cast<uint64>(self: UserId) -> uint64 {
    return (self: uint64);
  }
}

extern<uint64> {
  fn __cast<UserId>(self: uint64) -> UserId {
    return (self: UserId);
  }
}

let uid: UserId = 42:uint64 to UserId;
let raw: uint64 = uid to uint64;
```

---

## 7. Literals & Inference

### 7.1. Numeric Literal Typing

* Integer literals default to `int`.
* Float literals default to `float`.
* Suffixes allowed: `123:int32`, `1.0:float32` to select fixed types.

### 7.2. String Implementation

* `string` stores Unicode scalar values (code points); `"\u{1F600}"` represents a single code point.
* **Default indexing:** `s[i]` uses char-based (Unicode code points) access
* **Implementation:** Hybrid approach using rope data structure with position caching for O(1) amortized performance
* **Indexing complexity:** O(1) amortized for most access patterns, O(n) worst case for random access

**String methods:**
* `len_chars() -> int` – length in Unicode code points (default)
* `len_bytes() -> int` – length in UTF-8 bytes
* `len_graphemes() -> int` – length in user-perceived characters (grapheme clusters)
* `char_at(i: int) -> Option<char>` – get character at code point index
* `byte_at(i: int) -> Option<uint8>` – get byte at byte index
* `grapheme_at(i: int) -> Option<string>` – get grapheme cluster at grapheme index

**Examples:**
```sg
let text = "Hello 👋 World";
print(text.len_chars());      // 13 (code points)
print(text.len_bytes());      // 15 (UTF-8 bytes, emoji takes 4 bytes)
print(text.len_graphemes());  // 13 (user-perceived characters)
print(text[6]);               // 👋 (7th code point)
```

---

## 8. Overload Resolution & Conversions

Given a call `f(a1, ..., an)` with candidate signatures `Si`:

1. **Arity filter:** discard candidates with different arity (taking variadics into account).
2. **Generic instantiation:** try to infer generic parameters from actuals; if ambiguous, error.
3. **Ownership adjustment:** allow passing `own T` where `&T` is expected if an implicit borrow is permitted; moving `own T` into `own T` consumes the argument.
4. **Type match with coercion graph:** for each parameter, compute minimal cost to coerce actual type into formal type:

   * exact: 0
   * fixed → dynamic of same family: 1
   * numeric literal fitting: 1
   * explicit-only or impossible: ∞
5. **Best candidate:** sum costs; choose minimal sum. Ties → ambiguous call error.
6. **Qualifiers:** purity/`@backend` must be compatible with call context.

**Cast operator exclusion:** The `to` operator does **not** participate in overload resolution. Function signature selection happens first, then users may explicitly insert `to` casts as needed. Numeric literal fitting (§7.1) remains a separate rule from explicit casting.

Union alias `type Number = int | float` participates by expanding to candidates for each member type; the best member is chosen.

### Option conversions

No implicit conversion inserts `Some(...)`; `Option<T>` construction is always explicit (§2.9). Overload resolution treats values of type `Option<T>` distinctly from `T`.

---

## 9. Concurrency Primitives

### 9.1. Channels

`channel<T>` is a typed FIFO provided by the standard library. Core ops (from std):

* `make_channel<T>(cap:uint) -> own channel<T>`
* `send(ch:&channel<T>, v:own T)`
* `recv(ch:&channel<T>) -> Option<T>`         // blocking receive
* `try_recv(ch:&channel<T>) -> Option<T>`     // non-blocking receive
* `recv_timeout(ch:&channel<T>, ms:int) -> Option<T>`
* `close(ch:&channel<T>)`

The standard library also provides `choose { ... }` to select among ready operations. Channels are FIFO; fairness across multiple senders/receivers is not specified.

**Standard Library Synchronization Types:**

The following types are provided by the standard library for synchronization and are referenced by concurrency contract attributes:

* `Mutex` — mutual exclusion lock
* `RwLock` — reader-writer lock allowing multiple readers or single writer
* `Condition` — condition variable for thread coordination
* `Semaphore` — counting semaphore for resource control

These types are used in conjunction with concurrency contract attributes (§4.2.E) to express locking requirements and data protection invariants.

### 9.2. Parallel Map / Reduce

* `parallel map xs with args => func` executes `func` over `xs` elements concurrently; `func` must be `@pure`.
* `parallel reduce xs with init, args => func` reduces in parallel; `func` must be associative and `@pure`.

Grammar (surface):

```
ParallelMap    := "parallel" "map" Expr "with" ArgList "=>" Expr
ParallelReduce := "parallel" "reduce" Expr "with" Expr "," ArgList "=>" Expr
ArgList        := "(" (Expr ("," Expr)*)? ")" | "()"
```

Restriction: `=>` is valid only in these `parallel` constructs and within `compare` arms (§3.6). Any other use is a parse error `PARSE_FAT_ARROW_OUTSIDE_PARALLEL`.

### 9.3. Backend Selection

`@backend("gpu")`/`@backend("cpu")` may annotate functions or blocks. If the target is unsupported, a diagnostic is emitted or a fallback is chosen by the compiler based on flags.

### 9.4. Tasks and spawn semantics

* `spawn expr` launches a new task to evaluate `expr` asynchronously. If `expr` has type `T`, `spawn expr` has type `Task<T>` (a join handle).
* `join(t: Task<T>) -> Result<T, Cancelled>` waits for completion; on normal completion returns `Ok(value)`, on cooperative cancellation returns `Error(Cancelled)`.
* `t.cancel()` requests cooperative cancellation; tasks can check via `task::is_cancelled()`.
* Moving values into `spawn` consumes them (ownership semantics). Only `own` values may be moved into tasks.

### 9.5. Async/Await Model (Structured Concurrency)

Surge provides structured concurrency with async/await for managing asynchronous operations:

**Async Functions:**
```sg
async fn fetch_data(url: string) -> Result<Data, Error> {
    let response = http_get(url).await?;
    let data = parse_response(response).await?;
    return Ok(data);
}

async fn process_multiple_urls(urls: string[]) -> Result<Data[], Error> {
    let results: Result<Data, Error>[] = [];
    for url in urls {
        let data = fetch_data(url).await?;
        results.push(Ok(data));
    }
    return Ok(results);
}
```

**Structured Concurrency Blocks:**
```sg
async {
    let task1 = spawn fetch_data("url1");
    let task2 = spawn fetch_data("url2");
    let task3 = spawn fetch_data("url3");

    let r1 = task1.await?;
    let r2 = task2.await?;
    let r3 = task3.await?;

    // automatic cleanup on block exit
    // all spawned tasks are automatically cancelled if not awaited
}
```

**Key properties:**
* Async blocks provide automatic resource cleanup
* Tasks spawned within an async block are automatically cancelled when the block exits
* `await` can only be used within `async` functions or `async` blocks
* Async functions return `Future<T>` which must be awaited or spawned
* Structured concurrency ensures no "fire-and-forget" tasks that can leak

---

## 10. Standard Library Conventions

* `print(...args)` – variadic, calls `__to_string` on each arg, concatenates with spaces, appends newline.
* Core protocols provided for primitives and arrays: `__to_string`, `__abs`, numeric ops, comparisons, `__range`, `__index`.
* Newtypes can `@override` their magic methods in `extern<NewType>` blocks; built-ins for **primitive** base types are sealed (cannot be overridden for the primitive itself).

---

## 11. Error Handling & Traps (Surface)

* Casts from dynamic to fixed-size that overflow trap at runtime.
* Division by zero and invalid numeric operations trap.
* Index out of bounds traps unless `__index` is overridden to handle it differently.
* Ambiguous overloads and missing methods are compile-time errors.

(Full trap catalogue and error codes live in DIAGNOSTICS.md / RUNTIME.md.)

### Error types and inheritance

The predicate `(Child heir Base)` is a type-level check surfaced as a boolean expression; implementations may reuse it for future generic constraints.

```sg
type Error = { message: string, code: uint }

// Derived error with extra data
type MyError = Error : { path: string }
```

- `type Child = Base : { ... }` copies all fields from `Base` and extends the struct with additional fields. Field names must remain unique; conflicts emit `E_FIELD_CONFLICT`.
- Fields may provide defaults (`field: T = expr`). Missing defaults require callers to populate the field explicitly.
- Attributes on inherited fields (`@hidden`, `@readonly`) retain their behaviour. New fields may declare their own attributes.
- Methods declared in `extern<Base>` apply to `Child`. Overrides live in `extern<Child>` with `@override` for clarity.
- `(Child heir Base)` is a runtime predicate returning `bool` and captures the declared inheritance chain.
- `is` remains strictly nominal: `e is Error` is false if `e: MyError`. Use `heir` or tagged results for family checks.

```sg
fn open_file(p: string) -> Result<string, MyError> {
  let e: MyError = { message: "denied", code: 401, path: p };
  return Error(e);
}

let ok = (MyError heir Error); // true
```

### Recoverable errors: Result<T, E> and `?` propagation

`Result<T, E>` is defined via tags (§2.9). The `?` operator expects `Result<_, E>` and propagates the `Error(E)` branch: if the operand yields `Error(e)`, the surrounding function returns `Error(e)` immediately.

```sg
fn parse_int(s: string) -> Result<int, Error> {
  if (s == "42") { return Ok(42); }
  let e: Error = { message: "bad int", code: 2 };
  return Error(e);
}

fn read_and_parse() -> Result<int, Error> {
  let line = read_line()?;      // propagates Error(..) if present
  let v = parse_int(line)?;     // requires parse_int to return Result
  return Ok(v);
}
```

Returning a bare payload where `Result<T, E>` is expected emits `E_EXPECTED_TAGGED_VARIANT`; use `Ok(...)` / `Error(...)` explicitly.

Traps remain for unrecoverable faults (OOB, internal assertion, certain cast traps). A richer effects model may be added later; this draft focuses on explicit tagged results plus traps.

---

## 12. Examples

```sg
// Generics and overloading
@pure @overload
fn add(a:int, b:int) -> int { return a + b; }

@pure @overload
fn add(a:float, b:float) -> float { return a + b; }

fn demo() {
  let x = add(2, 3);        // int
  let y = add(2.0, 3.5);    // float
  print(x, y);
}
```

```sg
// Newtype with override
newtype MyInt = int;
extern<MyInt> {
  @pure @override fn __add(a: MyInt, b: MyInt) -> MyInt { return 42:int; }
}
```

```sg
// Literal enum and union alias
type Color = "black" | "white";
type Number = int | float;

fn show(c: Color) { print(c); }
fn absn(x: Number) -> Number { return abs(x); }
```

```sg
// Struct and readonly
type Person = { age:int, name:string, @readonly weight:float }

fn birthday(mut p: Person) { p.age = p.age + 1; }
```

```sg
// Signals
signal total := sum(prices);
// any change to prices recomputes total (sum must be @pure)
```

```sg
// Basic casting
let a: int32 = 300000;
let b: int = a to int;        // fixed→dynamic
let c: int16 = a to int16;    // may trap
```

```sg
// Custom casting with newtype
type UserId = uint64;

extern<UserId> {
  fn __cast<uint64>(self: UserId) -> uint64 { return (self: uint64); }
}

extern<uint64> {
  fn __cast<UserId>(self: uint64) -> UserId { return (self: UserId); }
}

let uid: UserId = 42:uint64 to UserId;
let raw: uint64 = uid to uint64;
```

```sg
// Struct casting
type Point2D = { x: float32, y: float32 }
type Point3D = { x: float32, y: float32, z: float32 = 0.0 }

extern<Point2D> {
  fn __cast<Point3D>(self: Point2D) -> Point3D {
    return { x: self.x, y: self.y, z: 0.0 };
  }
}

let p3 = ({x: 1.0, y: 2.0}: Point2D) to Point3D;
```

```sg
// Union injection via casting
type Number = int | float
let i: int = 42;
let n: Number = i to Number;  // injection into union
```

```sg
// Option and compare
fn maybe_head<T>(xs: T[]) -> Option<T> {
  if (xs.len == 0) { return nothing; }
  return Some(xs[0]);
}

fn demo_option() {
  let v = maybe_head([1, 2, 3]);
  compare v {
    nothing   => print("empty");
    Some(x)   => print(x);
  }
}
```

```sg
// Channels (blocking + try)
let ch = make_channel<int>(0);
// spawn omitted; assume a sender exists
let v = recv(&ch);          // Option<int>
compare try_recv(&ch) {
  nothing => print("empty");
  Some(x) => print(x);
}
```

```sg
// Compare with conditional patterns and finally
type NumberOrString = Number | string | nothing

fn classify_value(value: NumberOrString) {
  compare value {
    x if x is int => print("integer: ", x);
    42            => print("exactly 42");
    nothing       => print("absent");
    finally       => print("default case");
  }
}
```

```sg
// Async function with structured concurrency
async fn process_data(urls: string[]) -> Result<Data[], Error> {
    async {
        let tasks: Task<Result<Data, Error>>[] = [];

        for url in urls {
            let task = spawn fetch_data(url);
            tasks.push(task);
        }

        let results: Result<Data, Error>[] = [];
        for task in tasks {
            let result = task.await?;
            results.push(result);
        }

        return Ok(results);
    }
}
```

```sg
// @noinherit field example
type Base = { @noinherit internal_id:uint64, name:string }
type Public = Base : { display:string } // field internal_id not inherited in Public
```

```sg
// @sealed type example
@sealed
type Conn = { fd:int, @noinherit secret:uint64 }

type SafeConn = Conn : { tag:string } // error: E_TYPE_SEALED
```

```sg
// @guarded_by and @requires_lock example
type BankAccount = {
  lock: Mutex,
  @guarded_by("lock") balance: int
}

extern<BankAccount> {
  @requires_lock("lock")
  fn withdraw(self: &mut BankAccount, amount: int) -> bool {
    if (self.balance >= amount) {
      self.balance = self.balance - amount;
      return true;
    }
    return false;
  }
}
```

```sg
// @waits_on example for blocking receive
type MessageQueue<T> = {
  items: T[],
  not_empty: Condition,
  lock: Mutex
}

extern<MessageQueue<T>> {
  @waits_on("not_empty")
  fn recv<T>(self: &MessageQueue<T>) -> T {
    // Implementation would wait on condition variable
    return self.items[0];
  }
}
```

```sg
// @send and @nosend type examples
@send
type SafeCounter = { @atomic value: int }

@nosend
type FileHandle = { fd: int, @readonly path: string }
```

```sg
// RwLock with @guarded_by example
type CachedData = {
  rwlock: RwLock,
  @guarded_by("rwlock") cache: string[]
}

extern<CachedData> {
  @requires_lock("rwlock") @nonblocking
  fn read_cache(self: &CachedData) -> string[] {
    return self.cache;
  }

  @requires_lock("rwlock")
  fn update_cache(self: &mut CachedData, data: string[]) {
    self.cache = data;
  }
}
```

```sg

/// test:
/// AddBasic:
///   test.eq(add(2, 3), 5);
///   test.eq(add(-1, 1), 0);
// Test directive example
// the function can be defined both before and after the directive
fn add(a: int, b: int) -> int { return a + b; }

/// test:
/// SecondExample:
// you can also write comments inside the directive
///   test.le(add(2, 3), 5); // and this comment is valid
// you can also define a function inside the directive, but it will be visible only inside the directive
/// fn return_42() -> int { return 42; }
///   test.eq(return_42(), add(40, 2));
// but u can't use the function outside the directive:
return_42(); // error: E_UNDEFINED_FUNCTION
```

```sg
// Forward declaration and later implementation
// forward-declare (no body)
fn encode_frame(buf:&byte[], out:&mut byte[]) -> uint;

// ... (other code)

// local implementation (same module)
@override
fn encode_frame(buf:&byte[], out:&mut byte[]) -> uint {
  // real body
  return 0:uint;
}
```

```sg
// core/intrinsics example (only in special module)
// core/intrinsics.sg
@intrinsic fn rt_alloc(size:uint, align:uint) -> *byte;
@intrinsic fn rt_free(ptr:*byte, size:uint, align:uint) -> nothing;
@intrinsic fn rt_realloc(ptr:*byte, old_size:uint, new_size:uint, align:uint) -> *byte;
@intrinsic fn rt_memcpy(dst:*byte, src:*byte, n:uint) -> nothing;
@intrinsic fn rt_memmove(dst:*byte, src:*byte, n:uint) -> nothing;

// Note: intrinsics have no function bodies
```

```sg
// Benchmark directive example
fn factorial(n: int) -> int {
  return compare n {
    0 | 1 => 1,
    x => x * factorial(x - 1)
  };
}

/// benchmark:
/// FactorialPerf:
///   benchmark.measure(factorial(10));
```

```sg
// Time measurement directive example
/// time:
/// DataProcessing:
///   let data = generate_large_dataset();
///   let result = time.measure(process_data(data));
///   time.report("Processing time", result);
```

---

## 13. Directives (`///`)

Directives provide an extensible system for toolchain functionality such as testing, benchmarking, timing, and custom tooling scenarios.

### 13.1. General Directive Syntax

```
/// <namespace>:
/// <name>:
///   <body...>   // free-form Surge code
```

* `<namespace>` — identifier (e.g., `test`, `benchmark`, `time`, `tool:myplugin`).
* `<name>` — scenario identifier unique within the file.
* `<body>` — **Surge code** executed in an isolated directive context.

### 13.2. Visibility and Scope

* Directives have **read-only access** to the same names available at their declaration site (module scope).
* Items declared **within** directives are **not visible** to the rest of the program.
* Directives do not modify program typization or ABI.

### 13.3. Directive Execution

* The compiler/driver executes directives **only** when enabled by flags:
  * `--compile-directives` (enable directive execution)
  * `--directive-filter=<ns[:name]>` (filter execution to specific namespaces/scenarios)
* Each directive executes as a **separate scenario** (equivalent to a separate file/test).
* Directive code may call standard API functions: `test.eq(...)`, `benchmark.measure(...)`, `time.measure(...)`, etc. (provided by std as regular functions).

### 13.4. Built-in (Standard) Directives

* **`test:`** — executes test scenarios; standard conventions:
  * `test.eq(actual, expected) -> Result<nothing, Error>`
  * `test.le(a, b)`, `test.ok(cond)`, etc.
* **`benchmark:`** — benchmarking (ignored in release builds by default unless explicitly enabled).
* **`time:`** — timing measurements (for local analysis, ignored in release builds by default).

### 13.5. Execution Model Equivalence

Directives translate to isolated execution contexts. For reference, a directive:

```
/// test:
/// SumIsCorrect:
///   test.eq(add(1, 2), 3);
```

is equivalent to creating a hidden wrapper function and calling it in the test runner:

```
fn __directive_test_SumIsCorrect__() -> Result<nothing, Error> {
  return test.eq(add(1, 2), 3);
}
```

(The function does not appear in the binary if directives are not executed.)

### 13.6. User-defined Directives (Extensions)

Users can declare **directive modules** using `pragma directive` at the top of a module file:

```sg
pragma directive

// directive name declaration (singular)
pub type DirectiveName = "mycheck";

// API — regular functions available as mycheck.<fn>
pub fn check_invariant(x: int) -> Result<nothing, Error> { ... }
```

Such modules are imported normally. After import, directive blocks become available:

```
/// mycheck:
/// FooInvariant:
///   mycheck.check_invariant(42);
```

**Rules:** Directive modules **must not** modify the ABI of main code; their functions execute only during directive execution.

### 13.7. Target Directives (Conditional Compilation)

Target directives `/// target:` provide conditional compilation based on platform, features, and build configuration:

```sg
/// target: os = "linux"
fn linux_only_function() -> string {
    return "Linux implementation";
}

/// target: feature = "networking"
fn network_function() -> bool {
    return true;
}

/// target: all(arch = "x86_64", feature = "performance")
fn optimized_function() -> int {
    return 42;
}

/// target: any(os = "macos", os = "ios")
fn apple_function() -> string {
    return "Apple platform";
}

/// target: not(feature = "minimal")
fn full_feature_function() -> string {
    return "Full version";
}
```

**Target conditions:**
* `os = "linux" | "windows" | "macos" | "ios" | "android"`
* `arch = "x86_64" | "arm64" | "x86" | "arm"`
* `feature = "feature_name"` (build-time feature flags)
* `debug_assertions` (debug vs release builds)
* `test` (test compilation mode)
* Logical operators: `all(...)`, `any(...)`, `not(...)`

### 13.8. Examples

**Unit test:**
```
/// test:
/// AddSmall:
///   test.eq(add(2, 3), 5);
```

**Benchmark:**
```
/// benchmark:
/// AddBench:
///   benchmark.measure(|| { let mut s=0; for i:int in 0..1_000_000 { s+=i; } return s; });
```

**Custom directive:**
```
/// lint:deadcode:
/// UnusedStuff:
///   lint.deadcode.scan_module();
```

---

## 14. Precedence & Associativity

From highest to lowest:

1. `[]` (index), call `()`, member `.`, await `.await`, `to Type` (cast operator), postfix `?`
2. `+x -x !x` (prefix unary)
3. `* / %`
4. `+ -` (binary)
5. `<< >>` (bitwise shift)
6. `& ^ |` (bitwise operations)
7. `..` `..=` (range operators)
8. `< <= > >= == != is` (all comparison operators have same precedence, left-associative)
9. `&&`
10. `||`
11. `? :` (ternary, right-associative)
12. `??` (null coalescing)
13. `=` `+=` `-=` `*=` `/=` `%=` `&=` `|=` `^=` `<<=` `>>=` (assignment, right-associative)

**Type checking precedence:**
Type checking with `is` has the same precedence as equality operators. Use parentheses for complex expressions:
```sg
x is int && y is string  // OK
(x is int) == true       // explicit grouping recommended
```

Short-circuiting for `&&` and `||` is guaranteed.

Note: `=>` is not a general expression operator; it is reserved for `parallel map` / `parallel reduce` (§9.2) and for arms in `compare` expressions (§3.6).

### Member access precedence

Member access `.`, await `.await`, and cast `to Type` are postfix operators and bind tightly together with function calls and indexing. This resolves ambiguous parses, e.g., `a.f()[i].g()` parses as `(((a.f())[i]).g)()`; `future.await?` parses as `((future.await) ?)` with `.await` applied before the postfix `?`; `value to Type?` parses as `((value to Type) ?)`.

---

## 15. Name & Visibility

* Items are `pub` (public) or private by default. (Default: private.)
* `pub fn`, `pub type`, `pub let` export items from the module.

### 15.1. Resolving `Ident(...)`

- In expression or pattern position the form `Ident(...)` is ambiguous if both a tag constructor and a function named `Ident` are visible. The parser emits `E_AMBIGUOUS_CONSTRUCTOR_OR_FN`.

---

## 16. Compilation Model

### 16.1. Generic Monomorphization

Surge uses monomorphization for generics to achieve zero runtime cost:

* **Compile-time expansion:** Each generic instantiation creates a separate compiled function
* **Type specialization:** `fn id<T>(x: T) -> T` compiled as `fn id_int`, `fn id_string`, etc.
* **Code generation:** No generic dispatch at runtime; all type resolution happens at compile time
* **Binary size:** Trade-off between runtime performance and binary size

**Example:**
```sg
fn identity<T>(x: T) -> T { return x; }

let a = identity(42);      // generates identity_int
let b = identity("hello"); // generates identity_string
let c = identity(3.14);    // generates identity_float
```

**Compilation process:**
1. Parse and type-check generic functions
2. Collect all instantiation sites
3. Generate specialized versions for each unique type combination
4. Replace generic calls with calls to specialized functions
5. Dead code elimination removes unused instantiations

**Benefits:**
* Zero runtime cost for generics
* Full optimization opportunities for each instantiation
* No virtual dispatch overhead
* Predictable performance characteristics

**Limitations:**
* Increased compilation time for heavily generic code
* Larger binary sizes with many instantiations
* Compile-time blow-up possible with recursive generic instantiations

## 17. Advanced Type System Features

**Implementation Priority:**
1. Union types (§17.1) - core language feature
2. Tuple types (§17.2) - standard feature for multiple returns
3. Phantom types (§17.3) - advanced feature for type safety

### 17.1. Union Types

Union aliases (§2.8) support both untagged and tagged composition. Untagged unions rely on runtime type tests:

```sg
type Number = int | float

extern<Number> {
  fn __add(a: Number, b: Number) -> Number {
    return compare (a, b) {
      (x if x is int,   y if y is int)   => (x + y):Number,
      (x if x is int,   y if y is float) => (x:float + y):Number,
      (x if x is float, y if y is int)   => (x + y:float):Number,
      (x if x is float, y if y is float) => (x + y):Number,
    };
  }
}
```

Tagged unions provide exhaustiveness checking and clearer APIs; see §2.7 for constructors and §3.6 for matching rules. If the runtime cannot distinguish untagged members, emit `E_AMBIGUOUS_UNION_MEMBERS`.

### 17.2. Tuple Types

```sg
compare (a, b) {
  (x, y) if x is int && y is float => print(x + y),
  finally => print("unsupported")
}
```

Tuples group multiple values together:

```sg
type Point = (float, float);
type NamedTuple = (name: string, age: int);

fn get_coordinates() -> (float, float) {
    return (10.5, 20.3);
}

let (x, y): (float, float) = get_coordinates();
```

### 17.3. Phantom Types

Phantom types provide compile-time type safety without runtime overhead:

```sg
newtype UserId<T> = int;
newtype ProductId<T> = int;

type User = { id: UserId<User>, name: string }
type Product = { id: ProductId<Product>, name: string }

// Prevents mixing user and product IDs
fn get_user(id: UserId<User>) -> Option<User> { ... }
```

## 18. Open Questions / To Confirm

* Should `string` be `Copy` for small sizes or always `own`? (Proposed: always `own`.)
* Exact GPU backend surface (kernels, memory spaces). For now, `@backend` is a hint.
* Raw pointers `*T`: do we gate deref behind an `@unsafe` block? (Proposed: yes in later iteration; tokenizer still recognizes `*T`.)

---

## 19. Grammar Sketch (extract)

Note: `MacroDef` is reserved for a future iteration. The syntax is specified for completeness; the core implementation should focus on the rest of the language first. Implementations should parse `macro` items but reject them with `E_MACRO_UNSUPPORTED`.

Directives are parsed as "out-of-band" nodes attached to the module. The parser collects directive blocks alongside regular items but they do not participate in the main program syntax tree.

```
Module     := PragmaDirective? (Item | DirectiveBlock)*
PragmaDirective := "pragma" "directive"
DirectiveBlock := "///" Namespace ":" Newline
                  "///" Ident ":" Newline
                  ( "///" BodyLine Newline )+
Namespace  := Ident | Ident ":" Ident
BodyLine   := <any characters except newline>
Item       := Visibility? (Fn | AsyncFn | MacroDef | TagDecl | NewtypeDef | TypeDef | LiteralDef | AliasDef | ExternBlock | Import | Let)
Visibility := "pub"
Fn         := FnDef | FnDecl
FnDef      := Attr* "fn" Ident GenericParams? ParamList RetType? Block
FnDecl     := Attr* "fn" Ident GenericParams? ParamList RetType? ";"
AsyncFn    := Attr* "async" "fn" Ident GenericParams? ParamList RetType? Block
MacroDef   := "macro" Ident MacroParamList Block
MacroParamList := "(" (MacroParam ("," MacroParam)*)? ")"
MacroParam := Ident ":" MacroType | "..." Ident ":" MacroType
MacroType  := "expr" | "ident" | "type" | "block" | "meta"
Attr       := "@pure" | "@overload" | "@override" | "@intrinsic" | "@backend(" Str ")" | "@deprecated(" Str ")" | "@packed" | "@align(" Int ")" | "@shared" | "@atomic" | "@raii" | "@arena" | "@weak" | "@readonly" | "@hidden" | "@noinherit" | "@sealed"
GenericParams := "<" Ident ("," Ident)* ">"
ParamList  := "(" (Param ("," Param)*)? ")"
Param      := Ident ":" Type | "..."
RetType    := "->" Type
TagDecl    := "tag" Ident GenericParams? "(" ParamTypes? ")" ";"
TypeDecl   := "type" Attr* Ident GenericParams? "=" TypeBody ";"
TypeBody   := StructBody | UnionBody | Type
StructBody := "{" Field ("," Field)* "}"
Field      := Attr* Ident ":" Type
UnionBody  := UnionMember ("|" UnionMember)*
UnionMember:= "nothing" | Ident "(" ParamTypes? ")" | Type
ExternBlock:= "extern<" Type ">" Block
Import     := "import" Path ("::" Ident ("as" Ident)?)? ";"
Path       := Ident ("/" Ident)*
Block      := "{" Stmt* "}"
Stmt       := Let | While | For | If | Spawn ";" | Async | Expr ";" | Break ";" | Continue ";" | Return ";" | Signal ";"
Let        := "let" ("mut")? Ident (":" Type)? ("=" Expr)? ";"
While      := "while" "(" Expr ")" Block
For        := "for" "(" Expr? ";" Expr? ";" Expr? ")" Block | "for" Ident ":" Type "in" Expr Block
If         := "if" "(" Expr ")" Block ("else" If | "else" Block)?
Return     := "return" Expr?
Signal     := "signal" Ident ":=" Expr
Async      := "async" "{" Stmt* "}"
Expr       := Compare | Spawn | TypeHeirPred | TupleLit | ... (standard precedence)
TypeHeirPred := "(" CoreType " heir " CoreType ")"
TupleLit   := "(" Expr ("," Expr)+ ")"
AwaitExpr  := Expr "." "await"           // awaits a Future; only valid in async fn/block
Spawn      := "spawn" Expr
Compare    := "compare" Expr "{" Arm (";" Arm)* ";"? "}"
Arm        := Pattern "=>" Expr
Pattern        := "finally" | Literal | "nothing" | Ident
                | Ident "(" PatternArgs? ")"
                | TuplePattern
                | Ident "if" Expr
PatternArgs   := Pattern ("," Pattern)*
TuplePattern  := "(" Pattern ("," Pattern)+ ")"
Type           := Ownership? (TupleType | CoreType) Suffix*
Ownership      := "own" | "&" | "&mut" | "*"
CoreType       := Ident ("<" Type ("," Type)* ">")?
TupleType      := "(" Type ("," Type)+ ")"
ParamTypes     := Type ("," Type)*
Suffix         := "[]"
```

**Note:** `@intrinsic` is permitted only on `FnDecl` (function declarations without body).

---

## 20. Compatibility Notes

* Built-ins for primitive base types are sealed; you cannot `@override` them directly. Use `type New = int;` and override on the newtype.
* Dynamic numerics (`int/uint/float`) allow large results; casts to fixed-width may trap.
* Attributes affecting memory layout and ABI (`@packed`, `@align`) are part of the language specification and cannot be replaced by directives. Directives do not modify type layout or ABI contracts.
* Concurrency contract attributes describe *analyzable requirements* and do not change language semantics at runtime. Violations may not always be statically checkable; in such cases the compiler emits `W_CONC_UNVERIFIED` and defers verification to linters or runtime debug tools.
* **Directive vs Attribute distinction**: Attributes are closed-set language features that affect compilation, type checking, or runtime behavior. Directives are extensible annotations that provide metadata for external tools without changing language semantics. Tests, benchmarks, and documentation have been moved from attributes to the directive system to maintain the distinction.
* **Language intrinsics**: Intrinsics constitute a fixed, small set and are declared in the module `core/intrinsics`. Their implementation is described in RUNTIME.md. Using `@intrinsic` outside this module is forbidden. They serve basic memory management operations (`rt_alloc`, `rt_free`, `rt_realloc`) and byte copying (`rt_memcpy`, `rt_memmove`).

## 21. Diagnostics Overview (selected)

Stable diagnostic codes used by the parser and early semantic checks:

* `E_MISSING_FIELD_DEFAULT` — struct declared without defaults for one or more fields that lack explicit initialisers.
* `E_UNDEFINED_DEFAULT` — type lacks a well-defined default for zero-initialisation (untagged unions, aliases, tagged unions without explicit value).
* `E_GENERIC_UNDECLARED` — generic parameter used but not declared.
* `E_AMBIGUOUS_NOTHING` — `nothing` used without contextual type.
* `E_AMBIGUOUS_CONSTRUCTOR_OR_FN` — `Ident(...)` could resolve to either a tag constructor or a function.
* `E_MOVE_BORROWED_TO_THREAD` — cannot move borrowed reference into spawned task.
* `E_SIGNAL_NOT_PURE` — signals require @pure expression.
* `PARSE_FAT_ARROW_OUTSIDE_PARALLEL` — `=>` reserved for compare arms and parallel constructs.
* `E_FOR_MISSING_IN` — `for`-in missing `in` token.
* `E_FOR_BAD_HEADER` — malformed C-style `for` header.
* `E_ILLEGAL_ATTRIBUTE_TARGET` — attribute not allowed on this target.
* `W_UNKNOWN_ATTRIBUTE` — unknown attribute.
* `E_CYCLIC_TOPLEVEL_INIT` — cyclic top-level initialization.
* `E_AMBIGUOUS_OVERLOAD` — ambiguous overload resolution.
* `E_MISMATCH_RESULT_USE` — Result used where plain value expected (use `?` or compare).
* `E_NONEXHAUSTIVE_MATCH` — tagged union match missing variants without `finally`.
* `E_EXPECTED_TAGGED_VARIANT` — bare payload returned where a tagged constructor was required.
* `E_AMBIGUOUS_UNION_MEMBERS` — untagged union members cannot be distinguished at runtime.
* `E_FIELD_CONFLICT` — field name repeated while extending a struct (`type Child = Base : { ... }`).
* `E_MACRO_UNSUPPORTED` — `macro` definitions are parsed but not supported in this iteration.

**Cast Operations:**

* `E_NO_CAST(From, To)` — no available cast implementation found from `From` to `To`.
* `E_AMBIGUOUS_CAST(From, To)` — multiple `__cast` implementations found for the same cast.
* `E_CAST_OVERLAP(From, To)` — conflicting `__cast` implementations detected.
* `E_CAST_REF_KIND` — invalid cast attempted on reference or pointer type (`&T`, `&mut T`, `*T`).
* `E_CAST_OUT_OF_RANGE` — runtime trap: value exceeds target type range during built-in numeric cast.

**Attributes & Backend:**

* `E_ATTR_CONFLICT` — conflicting attributes (e.g., incompatible `@align` with `@packed`).
* `E_ATTR_DUPLICATE` — duplicate attribute with incompatible parameters.
* `E_PRIMITIVE_SEALED` — attempt to `@override` primitive base type (use newtype instead).
* `E_TYPE_SEALED` — attempt to extend sealed type via `type Child = Base : {...}`.
* `E_OVERLOAD_FIRST_DECL` — first function declaration marked with `@overload`.
* `E_PURE_VIOLATION` — violation of `@pure` contract detected during frontend analysis.
* `W_BACKEND_UNSUPPORTED` — unsupported `@backend` target (warning; escalates to error in strict mode).

**Concurrency Contracts:**

* `E_CONC_UNKNOWN_GUARD` — lock field referenced in concurrency attribute not found.
* `E_CONC_BAD_GUARD_TYPE` — field referenced by concurrency attribute is not `Mutex` or `RwLock`.
* `E_CONC_LOCK_CONTRACT` — function call violates `@requires_lock` contract.
* `E_CONC_CONTRACT_CONFLICT` — conflicting concurrency attributes (e.g., `@send` + `@nosend`, `@nonblocking` + `@waits_on`).
* `W_CONC_UNVERIFIED` — analyzer cannot prove lock contract compliance (warning; verification deferred to linter or runtime debug).

**Intrinsics & Forward Decls:**

* `E_FN_BODY_MISSING` — function declared as `fn ...;` without body and is not an intrinsic or subsequent implementation in the same module.
* `E_INTRINSIC_FORBIDDEN_CONTEXT` — `@intrinsic` used outside module `core/intrinsics`.
* `E_INTRINSIC_DISALLOWED_NAME` — function name under `@intrinsic` not in permitted list (`rt_alloc`, `rt_free`, `rt_realloc`, `rt_memcpy`, `rt_memmove`).
* `E_INTRINSIC_HAS_BODY` — intrinsic declared with function body.
* `E_OVERRIDE_FORBIDDEN_TARGET` — `@override` on free function attempts to replace symbol not from current module (std/import).
* `E_OVERRIDE_REDEFINITION` — repeated `@override` for already implemented function in module.

**Directives:**

* `E_DIRECTIVE_UNKNOWN_NAMESPACE` — directive uses undeclared namespace (no matching `pragma directive` declaration).
* `E_DIRECTIVE_MALFORMED_SYNTAX` — directive block has invalid syntax or structure.
* `E_DIRECTIVE_MISSING_HANDLER` — directive namespace declared but no handler module found.
* `W_DIRECTIVE_UNUSED` — directive block present but no tool processes it.

Note: Channel closure is a runtime condition; `send` returns `Result<nothing, ChannelClosed>`. The corresponding runtime diagnostic lives in RUNTIME.md, not as a compile-time diagnostic.
