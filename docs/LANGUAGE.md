# Surge Language Specification (Draft 7)

> **Status:** Draft for review
> **Scope:** Full language surface for tokenizer → parser → semantics. VM/runtime details are out of scope here and live in RUNTIME.md / BYTECODE.md.
> **Alignment:** Synced with the current parser + partial sema; clearly marks pieces that are still planned or only parsed.

---

## 0. Design Goals

* **Safety by default:** explicit ownership/borrows; no hidden implicit mutability.
* **Deterministic semantics:** operator/overload resolution is fully specified.
* **Orthogonality:** features compose cleanly (e.g., generics + ownership + methods via `extern<T>`).
* **Compile-time rigor, runtime pragmatism:** statically-typed core with dynamic-width numeric families (`int`, `uint`, `float`).
* **Testability:** first-class doc-tests via `/// test:` directives.
* **Explicit over implicit:** prefer clear, verbose constructs over clever shortcuts that obscure ownership or control flow.
* **Ownership clarity:** every operation's ownership semantics should be obvious from the syntax.

### Implementation Snapshot (Draft 7)

- Keywords match `internal/token/kind.go`: `fn, let, const, mut, own, if, else, while, for, in, break, continue, return, import, as, type, tag, extern, pub, async, compare, finally, channel, spawn, true, false, signal, parallel, map, reduce, with, macro, pragma, to, heir, is, nothing`.
- The type checker currently resolves `int`, `uint`, `float`, `bool`, `string`, `nothing`, `unit`, ownership/ref forms (`own T`, `&T`, `&mut T`, `*T`), slices `T[]`, and sized arrays `T[N]` when `N` is a constant numeric literal. Fixed-width numerics (`int8`, `uint64`, `float32`…) are reserved symbols in the prelude but are not backed by concrete `TypeID`s yet.
- Tuple and function types parse, but sema does not yet lower them; treat them as planned surface.
- Tags and unions follow the current parser: `tag Name<T>(args...);` declares a tag item; unions accept plain types, `nothing`, or `Tag(args)` members. `Option`/`Result` plus tags `Some`/`Ok`/`Error` are injected via the prelude and resolved without user declarations; exhaustive `compare` checks are still TODO.
- Contracts (trait-like structural interfaces) are parsed and checked in sema: declaration syntax is enforced, bounds on functions/types are resolved, short/long forms are validated by arity, and structural satisfaction (fields/methods) is verified on calls, type instantiations, assignments, and returns.
- Diagnostics now use the `Lex*`/`Syn*`/`Sema*` numeric codes from `internal/diag/codes.go` instead of the earlier `E_*` placeholders.
  Legacy `E_*` labels that remain in examples below are descriptive placeholders; see §21 for the codes the compiler actually emits today.

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
pub, fn, let, mut, own, if, else, while, for, in, break, continue,
import, as, type, tag, extern, return, signal, compare, spawn, channel,
parallel, map, reduce, with, to, heir, is, async, macro, pragma,
true, false, nothing
```

Attribute names (e.g., `pure`, `override`, `packed`) are identifiers that appear after `@` rather than standalone keywords.

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

The type checker currently recognises built-in `int`, `uint`, `float`, `bool`, `string`, `nothing`, and `unit`. Fixed-width numerics are reserved identifiers in the prelude but are not yet backed by concrete `TypeID`s in sema (they parse and resolve as names only).

* **Dynamic-width numeric families** (arbitrary width, implementation-defined precision, but stable semantics):

  * `int` – signed integer of unbounded width.
  * `uint` – unsigned integer of unbounded width.
  * `float` – floating-point of high precision (≥ IEEE754-64 semantics guaranteed; implementations may be wider).
* **Fixed-size numerics** (layout-specified, planned):

  * `int8, int16, int32, int64`, `uint8, uint16, uint32, uint64`.
  * `float16, float32, float64`.
* **Other primitives**:

  * `bool` – logical; no implicit cast to/from numeric.
  * `string` – a sequence of Unicode scalar values (code points). Layout: dynamic array of code points.
  * `unit` – zero-sized marker type, primarily used internally; no literal syntax.

**Coercions:**

* Fixed → dynamic of same family: implicit and lossless (e.g., `int32 -> int`).
* Dynamic → fixed **never implicit**. Explicit cast required; may trap if out of range.
* Between numeric families: no implicit. Explicit casts defined; may round or trap depending on target.

### 2.2. Arrays

`T[]` is a growable, indexable sequence of `T` with zero-based indexing. Fixed-length arrays use `T[N]` where `N` is a constant integer literal; sema rejects non-constant lengths.

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
* Lifetimes are lexical; the compiler emits diagnostics for aliasing violations. When you need to end a borrow early, use `@drop binding;` — it marks the specific expression statement as a drop point and releases the corresponding borrow before the end of the enclosing block.

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

Generic parameters must be declared explicitly with angle brackets: `<T, U, ...>`.  
There are no implicit type variables: a bare `T` in a type position is always resolved as a normal type name unless it appears in the generic parameter list of the current declaration.

Supported generic owners:

* Functions: `fn id<T>(x: T) -> T { return x; }`
* Types (aliases, structs, unions): `type Box<T> = { value: T }`
* Tags: `tag Some<T>(T);`
* Contracts: `contract FooLike<T> { field v: T; fn get(self: T) -> T; }`
* `extern<T>` blocks: `extern<T> { fn len(self: &T) -> int; }`

Resolution rules for a type identifier `Ident` inside a generic owner:

1. If `Ident` matches one of the owner’s type parameters (`<T, U, ...>`), it is treated as a **type parameter**.
2. Otherwise, `Ident` is resolved as a regular type name (struct, alias, union, contract, tag, etc.).
3. If no matching type is found, the compiler reports an unresolved-type error (`SemaUnresolvedSymbol`).

This means that the following code is **invalid**:

```sg
fn foo(x: T) {}          // error: T is not declared anywhere
type S = { value: U };   // error: U is not declared anywhere
```

while this is valid:

```sg
fn bar<T>(x: T) {}       // T is a type parameter of `bar`

type T = { value: int }; // user-defined type T

fn use_t(x: T) {}        // here T refers to the struct above

fn shadow<T>(x: T) -> T {  // here T is the generic parameter, not the struct
  return x;
}
```

Type parameters form a local scope for their owner:

* In a function `fn f<T, U>(x: T, y: U) -> T`, both `T` and `U` are visible in the parameter list, return type, and the body.
* In a type declaration `type Box<T> = { value: T }`, `T` is visible only inside the right-hand side of `Box<T>`.
* In a contract `contract C<T, U> { ... }`, all type parameters are visible in every `field` and `fn` member.
* In a tag `tag Pair<A, B>(A, B);`, `A` and `B` are only visible in the payload list.
* In an `extern<T>` block, `T` is the type parameter of that block; methods inside the block may introduce their own `<U, ...>` which shadow outer names.

Generic monomorphization and instantiation are described in §16.1.

### 2.5. User-defined Types

* **Type alias to primitive:** `type MyInt = int;` creates a distinct nominal type that inherits semantics of `int` but can override magic methods via `extern<MyInt>`. (Different from a pure alias.)
* **Struct:** `type Person = { age:int, name:string, @readonly weight:float }`.

  * Fields are immutable unless variable is `mut`. `@readonly` forbids writes even through `mut` bindings.
  * Struct literals may specify the type inline: `let p = Person { age: 25, name: "Alex" };`. The parser only treats `TypeName { ... }` as a typed literal when `TypeName` follows the CamelCase convention so that `while ready { ... }` still parses as a control-flow block.
  * When the type is known (either via `TypeName { ... }` or an explicit annotation on the binding), the short `{expr1, expr2}` form is allowed; expressions are matched to fields in declaration order. Wrap identifier expressions in parentheses (`{(ageVar), computeName()}`) when using positional literals so they are not mistaken for field names.
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
- Zero init via `default<T>()` is defined only for types with a canonical zero:
  * primitives (`int`/`uint`/`float`/`bool`/`string`/`unit`/`nothing`) → `0`, `0.0`, `false`, `""`, `unit`, `nothing`;
  * pointers `*T` → `nothing`; references `&T`/`&mut T` have no default (compile-time error on `default`);
  * arrays/slices → element-wise `default<Elem>()`, empty slice for dynamic;
  * structs → recursively default every field/base; aliases unwrap to their target;
  * unions → only if a `nothing` variant is present (e.g. `Option`).
- `@hidden` fields remain hidden outside `extern<Base>` and initialisers. `@readonly` fields stay immutable after construction.
- Assigning from a child to its base is forbidden (types remain nominal).
- Field name clashes trigger `SynTypeFieldConflict` during parsing.
- Methods defined in `extern<Base>` are visible on `Child`. Override behaviour lives in `extern<Child>` with `@override` marking intentional replacements.
- **Implementation note:** the parser already accepts `Base : { ... }`, but sema currently keeps only the explicitly declared fields; structural inheritance/override checks are TODO.

### 2.6. `nothing` — the single absence value

`nothing` is the sole inhabitant of the type `nothing` and represents "no value" for void returns, null-like states, and explicit absence.

- Functions without an explicit `-> Type` return `nothing`. `fn f() {}` is sugar for `fn f() -> nothing { return nothing; }`.
- `nothing` has no literal shorthand other than the keyword itself. A `unit` type exists for zero-sized markers but has no literal/tuple sugar yet.
- Context must accept the type `nothing`; using it where the surrounding type is incompatible yields a type-mismatch diagnostic once the type checker knows the expected type.
- Arrays remain homogeneous: `[nothing, nothing]` has type `nothing[]`. Mixing `nothing` with other literals requires an explicit union or alias accommodating both members.

### 2.7. Tags (`tag`) and tagged unions

Tags describe explicit union constructors and are declared ahead of use:

```sg
tag Ping();
tag Pair<A, B>(A, B);
```

- Top-level syntax: `tag Name<T>(payload...);` and the semicolon is required.
- `Name(args...)` constructs a value whose discriminant is `Name` and whose payload tuple matches the declaration.
- Tags share a name slot with functions. If both exist and `Ident(...)` is used, sema reports `SemaAmbiguousCtorOrFn`.
- Tags are not first-class types. To pass a constructor, wrap it in a closure: `fn(x:T) -> Alias<T> { return Name(x); }`.
- `Some`, `Success`, and `Error` are predeclared tag symbols (see `internal/symbols/prelude.go`) and are always in scope without an explicit `tag` item.

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
- Tagged unions are parsed and lowered into `TypeUnionMemberTag` entries. Exhaustiveness checking for `compare` arms is not implemented yet in sema; treat it as planned validation.
- Mixing many untagged structural types may still be ambiguous at runtime; prefer tags for evolving APIs and stability.

### 2.9. Option and Erring via tags (sugar `T?` / `T!`)

The standard library defines canonical constructors and aliases; the postfix sugar desugars to them:

**Note:** `Erring<T, E>` is not an analogue of Rust's `Result<T, E>`, but a distinct structure in the Surge language with similar but not identical behavior. Key differences include the use of `Success<T>` instead of `Ok<T>`, direct error value returns without wrapping, and simplified syntax sugar that only supports `T!` (not `T!CustomError`).

```sg
tag Some<T>(T); tag Success<T>(T);

type Option<T> = Some(T) | nothing
type Erring<T, E: ErrorLike> = Success(T) | E

// sugar (type position only):
let maybe_num: int?      // == Option<int>
let maybe_fail: int!     // == Erring<int, Error>

fn head<T>(xs: T[]) -> Option<T> {
  if (xs.len == 0) { return nothing; }
  return Some(xs[0]);
}

fn parse(s: string) -> Erring<int, Error> { // also `int!` sugar is available
  if (s == "42") { return Success(42); }
  let e: Error = { message: "bad", code: 1 };
  return e; // Error value returned directly
}

compare head([1, 2, 3]) {
  Some(v) => print(v),
  nothing => print("empty")
}

compare parse("x") {
  Success(v) => print("ok", v),
  err        => print("err", err)
}
```

Rules:

- Construction is explicit in expressions: use `Some(...)` or `Success(...)`. In function returns, a bare `T` is accepted as `Some(T)`/`Success(T)` and `nothing` is accepted for `Option<T>`.
- `T?` is sugar for `Option<T>`; `T!` is sugar for `Erring<T, Error>` (type sugar only; no `expr?` propagation operator).
- `nothing` remains the shared absence literal for both Option and other contexts (§2.6). Exhaustiveness checking for tagged unions is planned but not wired up yet.
- `panic(msg)` materialises `Error{ message: msg, code: 1 }` and calls intrinsic `exit(Error)`.

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

### 2.11. Contracts (structural interfaces) and bounds

**What:** Contracts are structural interfaces that state required fields and method signatures. They are used to constrain generic parameters and to describe APIs types must satisfy. A contract is declared with `contract Name<T, U> { ... }` containing only `field` requirements and `fn` signatures terminated by semicolons—method bodies are forbidden inside contracts (`SynUnexpectedToken`).

**Why:** They provide type-safe ad-hoc polymorphism without nominal inheritance. Bounds on functions and types communicate requirements, drive code completion, and catch mismatches early (missing fields/methods, wrong signatures, or wrong `self` types).

**Who uses this:** Library authors defining reusable behaviours; application code constraining generic functions/types; tooling wanting structured requirements for navigation and diagnostics.

**Declaring contracts**

- Syntax:

  ```sg
  contract FooLike<T> {
      field bar: string;
      fn Bar(self: T) -> string;
  }

  contract NoParam {
      fn reset();
  }
  ```

- Members are requirements only: `fn ...;` and `field ...;` must end with `;`. Bodies are rejected (`SemaContractMethodBody` / `SynUnexpectedToken`).
- Duplicate names are illegal unless the later method is explicitly marked `@overload`. Duplicate fields (`SemaContractDuplicateField`) and non-overloaded duplicate methods (`SemaContractDuplicateMethod`) are errors.
- Attributes are validated against their targets (`field` / `fn`). Unknown or disallowed attributes raise `SemaContractUnknownAttr`.
- Every generic parameter of the contract must be used in at least one member type (`SemaContractUnusedTypeParam`). Member types must resolve (`SemaUnresolvedSymbol`).

**Bounds and short/long forms**

- Bounds attach to generic parameters with `:` and are combined with `+`: `fn save<T: JsonSerializable + Printable<T>>(x: T);`.
- Short form (`T: FooLike`) is allowed only for contracts with **0 or 1** type parameter. For single-parameter contracts the bound implicitly supplies the type parameter itself: `T: FooLike` == `T: FooLike<T>`.
- Multi-parameter contracts require the long form with all arguments: `T: PairOps<T, U>`. Missing/extra args are reported (`SemaTypeMismatch`).
- Using a non-contract name in bounds produces `SemaContractBoundNotContract` or `SemaContractBoundNotFound`. Unknown type arguments produce `SemaContractBoundTypeError`. Duplicate contracts on the same parameter are rejected (`SemaContractBoundDuplicate`).

**Where bounds apply**

- Functions: `fn f<T: FooLike, U: BarLike<T, U>>(x: T, y: U);`
- Types: `type Container<T: Clone> = { value: T }`.
- Multiple bounds per parameter are supported with `+`.
- Bounds participate in type arg substitution; nested generic args in bounds are resolved and re-checked on instantiation.

**Satisfaction rules (structural matching)**

- Matching checks run whenever a bound is instantiated:
  - Calling/generic functions (`f<Foo>(...)`), constructing generic types (`Box<Foo>`), returning `T` from a bound function, or assigning to a bound type triggers contract enforcement.
  - Validation is recursive through nested generic arguments.
- Fields: the concrete type must define every required field with the exact type (after alias resolution) and matching attributes. Missing fields → `SemaContractMissingField`; type mismatch → `SemaContractFieldTypeError`; attribute mismatch → `SemaContractFieldAttrMismatch`.
- Methods: the concrete type must provide methods with matching name, parameter list (including `self`), result type, visibility (`pub`) and `async` flag, and matching attributes. Missing → `SemaContractMissingMethod`; signature mismatch → `SemaContractMethodMismatch`; wrong self type → `SemaContractSelfType`; attribute/flag mismatch → `SemaContractMethodAttrMismatch`.
- Type arguments from the bound are substituted into contract member types, so requirements like `fn swap(self: A, other: B) -> B;` check against the actual `A`/`B` supplied in the bound.

**Diagnostics**

- Unknown contract name / not a contract.
- Wrong number of generic arguments on the bound or on the contract declaration.
- Unused contract generic parameters.
- Missing field/method, incompatible field/method signature, wrong `self`, or attribute mismatch.
- Short-form misuse on multi-parameter contracts (reports arity mismatch).

**Examples**

```sg
contract PairOps<A, B> { fn swap(self: A, other: B) -> B; }
contract C<T> { field v: T; }

fn f<X, Y: PairOps<X, Y>>(x: X, y: Y) -> Y { return x.swap(y); }
type Box<T: C<T>> = { value: T }

type Foo = { v: int }
// Box<Foo> fails: Foo lacks field `v` of type Foo (self-substitution requires `v: Foo`)
```

Contracts with zero parameters can still be used in short form: `fn tick<T: Clock>()`. Multiple bounds compose: `fn render<T: Drawable + Positionable<T>>(x: T);`.

---

## 3. Expressions & Statements

### 3.1. Variables

* Declaration: `let name: Type = expr;`
* Const declaration: `const NAME: Type = expr;` initializer is required and must be a compile-time constant; `pub const` exports it from the module.
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
* When `: Type` is supplied it must describe a valid type; malformed annotations surface as syntax errors (`SynExpectType` / `SynExpectExpression`).

Parser diagnostics:

* `SynForMissingIn` — `for`-in form lacks `in`.
* `SynForBadHeader` — mismatched semicolons in C-style `for`.

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
- Tagged constructors – `Tag(p)` such as `Some(x)`, `Success(v)`, error values; payload patterns recurse.
- `nothing` – matches the absence literal of type `nothing`.
- Conditional patterns – `x if x is int` where `x` is bound and condition is checked.

Examples:

```sg
compare v {
  Some(x) => print(x),
  nothing => print("empty")
}

compare r {
  Success(v) => print("ok", v),
  err        => print("err", err)
}
```

Notes:

- Arms are tried top-to-bottom; the first match wins.
- `=>` separates pattern from result expression and is only valid within `compare` arms and parallel constructs.
- Exhaustiveness for tagged unions is planned (compare should cover all declared tags or have `finally`); the current compiler does not enforce this yet. Untagged unions skip this check.
- If both a tag constructor and a function named `Ident` are in scope, using `Ident(...)` emits `SemaAmbiguousCtorOrFn`.

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
* `@override` *(fn)* — replaces an existing implementation for a target type. Only valid within `extern<T>` blocks. Invalid override contexts surface as `SemaFnOverride`. Incompatible with `@overload`.

  **Exception:** `@override` may be used outside `extern<T>` only if the target symbol is local to the current module (declared earlier in the same module) and previously had no body implementation.
* `@intrinsic` *(fn)* — marks function as a language intrinsic (implementation provided by runtime/compiler). Intrinsics are declared only as function declarations without body (`fn name(...): Ret;`) in the special core module (all files under `core/`) and made available to other code through standard library re-exports. User code cannot declare intrinsics outside `core`. Violations emit errors per §21.

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
* `@drop` *(statement expression)* — terminates the lifetime of the borrowed binding specified in the expression immediately. Only valid on expression statements (`@drop expr;`) and takes no arguments.

All string parameters (`"lock"`, `"cond"`) must be **field names or parameter names**, not expressions or function calls.

#### Closed Set Rule

Attributes are a **closed set** defined by the language. Tests, benchmarks, and timing measurements are implemented through the directive system (§13), not attributes. Attributes affect language semantics and ABI; directives are toolchain mechanisms that do not modify program semantics.

#### Applicability Matrix

| Attribute        |  Fn | Block | Type | Field | Param | Stmt |
| ---------------- | :-: | :---: | :--: | :---: | :---: | :--: |
| @pure            |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |
| @overload        |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |
| @override        |  ✅* |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |
| @intrinsic       |  ✅** |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |
| @backend         |  ✅  |   ✅   |   ❌  |   ❌   |   ❌   |  ❌  |
| @packed          |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |  ❌  |
| @align           |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |  ❌  |
| @raii            |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |  ❌  |
| @arena           |  ❌  |   ❌   |   ✅  |   ✅   |   ✅   |  ❌  |
| @weak            |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |
| @shared          |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |  ❌  |
| @atomic          |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |
| @readonly        |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |
| @hidden          |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |
| @noinherit       |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |
| @sealed          |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |  ❌  |
| @guarded_by      |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |
| @requires_lock   |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |
| @acquires_lock   |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |
| @releases_lock   |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |
| @waits_on        |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |
| @send            |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |  ❌  |
| @nosend          |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |  ❌  |
| @nonblocking     |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |
| @drop            |  ❌  |   ❌   |   ❌  |   ❌   |   ❌   |  ✅  |

*`@override` — only within `extern<T>` blocks.
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
* Intrinsics cannot have body; any attempts to provide implementation or call `@intrinsic` outside the `core` module → errors (§21).

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

#### 4.4.1. Contents of `extern<T>` Blocks

`extern<T>` blocks may declare **methods** and **fields** for a type:

* **Field declarations:** `field name: Type;` Attributes for fields (`@readonly`, `@hidden`, `@deprecated`, `@packed`, `@align`, `@arena`, `@weak`, `@shared`, `@atomic`, `@noinherit`, `@guarded_by`) are allowed and validated. A type annotation is required. Duplicated field names for the same target type are rejected (`SemaExternDuplicateField`).
* **Function definitions:** `fn name(params) -> RetType? { body }`
* **Function declarations:** `fn name(params) -> RetType?;`
* **Async functions:** `async fn name(params) -> RetType? { body }`
* **Attributes on functions:** `@pure`, `@overload`, `@override`, etc.

Any other item-level elements are **prohibited**: `let`, `type`, alias declarations, literal definitions, `import` statements, nested `extern` blocks, etc. These produce syntax error `E_ILLEGAL_ITEM_IN_EXTERN`.

**Examples:**

```sg
extern<Person> {
  // ✅ Allowed
  @readonly field id: int;

  fn age(self: &Person) -> int { return self.age; }
  
  pub fn name(self: &Person) -> string { return self.name; }
  
  @pure
  async fn to_json(self: &Person) -> string { /* ... */ }
  
  // ❌ Errors: E_ILLEGAL_ITEM_IN_EXTERN
  let x = 1;
  type Tmp = { a: int };
  alias Num = int | float;
}
```

#### 4.4.2. Visibility of Methods in `extern<T>`

**`pub` modifier is allowed on methods inside `extern<T>`** and controls export from the module, same as for regular items. Methods without `pub` are private by default.

**Two visibility levels:**
* **Private methods** (no `pub`): visible only within the current module; calling from other modules is a visibility error.
* **Public methods** (`pub fn`): accessible to consumers of the module and participate in method resolution across module boundaries.

**Override visibility rule:** When overriding an already-public method implementation for a type, the new definition must not reduce visibility. Attempting to override a public method with a private one emits `E_VISIBILITY_REDUCTION`.

**Examples:**

```sg
extern<Person> {
  // Private method (internal use only)
  fn age(self: &Person) -> int { return self.age; }
  
  // Public method (exported)
  pub fn name(self: &Person) -> string { return self.name; }
}

extern<Person> {
  pub fn __to(self: &Person, target: string) -> string { /* v1 */ }
}

extern<Person> {
  // ✅ OK: maintaining public visibility
  @override
  pub fn __to(self: &Person, target: string) -> string { /* v2 */ }
  
  // ❌ Error: E_VISIBILITY_REDUCTION
  @override
  fn __to(self: &Person, target: string) -> string { /* v3 */ }
}
```

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
* Inheritance checking: `heir` – checks inheritance relationship between types (see §6.3)
* Logical: `&& || !` – short-circuiting; operate only on `bool`.
* Indexing: `[]` → `__index __index_set`
* Unary: `+x -x` → `__pos __neg`
* Abs: `abs(x)` → `__abs`
* Casting: `expr to Type` → `__to(self, Type)` magic method; `print` simply casts each argument to `string` and concatenates.
  * `expr: Type` is shorthand for `expr to Type`. It's especially handy for literal annotations such as `1:int8`.
* Range: `for in` → `__range() -> Range<T>` where `Range<T>` yields `T` via `next()`.
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

**How operators are implemented.** Most operators (arithmetic, comparison, indexing, etc.) are implemented via magic methods that must be exposed inside an `extern<T>` block. The standard library ships those implementations in `core/intrinsics.sg` (module `core`): each method is marked `@intrinsic` so the compiler can lower it straight to the runtime. Sema never assumes the result type of `int + int` or `string * uint`—it always resolves the magic method on the left operand (following alias inheritance rules) and uses that signature as the single source of truth. If no method exists, the operator is rejected with `SemaInvalidBinaryOperands`. 

**Exceptions:** The `is` and `heir` operators are built-in compiler checks and do not use magic methods. They cannot be overridden via `extern<T>` blocks.

For example, integer addition is defined as

```sg
extern<int> {
    @intrinsic fn __add(self: int, other: int) -> int;
    @intrinsic fn __lt(self: int, other: int) -> bool;
}
```

Sema never assumes that `int + int` yields `int`. Instead, it resolves `__add` for the left operand's type (respecting overrides) and uses that signature as the source of truth. User-defined types opt in by providing their own `extern<MyType>` blocks; built-ins rely on the intrinsic versions provided by `core/intrinsics.sg` (module `core`).

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

### 6.3. Inheritance Checking Operator (`heir`)

The `heir` operator checks inheritance relationships between types and returns a `bool`. It verifies whether one type inherits from another. The check is performed both at compile time (for constant expressions) and at runtime (for dynamic checks).

**Syntax:**
```
Type1 heir Type2
```

**Rules:**
* `Child heir Base` → `true` if `Child` inherits from `Base` through any inheritance mechanism (struct extension, type inheritance, etc.)
* `T heir T` → `true` (reflexive: every type inherits from itself)
* Inheritance is transitive: if `A heir B` and `B heir C`, then `A heir C`
* Both operands must be type names (not values); using values emits `SemaExpectTypeOperand`
* The operator returns `bool` and can be used in both compile-time constant expressions and runtime checks
* Works for **any type**: struct extension (`type Child = Base : { ... }`), type aliases, unions, and other type relationships

**Precedence:** `heir` has the same precedence as comparison operators (`<`, `<=`, `==`, `!=`, `>=`, `>`, `is`) and is left-associative.

**Examples:**
```sg
type BasePerson = {
    name: string,
    age: int
};

type Employee = BasePerson : {
    id: uint,
    department: string
};

type Manager = Employee : {
    team_size: int
};

// Direct inheritance
let is_employee_base: bool = Employee heir BasePerson;  // true

// Transitive inheritance
let is_manager_base: bool = Manager heir BasePerson;     // true
let is_manager_employee: bool = Manager heir Employee;   // true

// Self-inheritance
let is_self: bool = Employee heir Employee;              // true

// Non-inheritance
let is_not: bool = BasePerson heir Employee;             // false

// Usage in conditions
if (Employee heir BasePerson) {
    print("Employee inherits from BasePerson");
}

// Works for any type, not just struct extension
type MyInt = int;
let is_int: bool = MyInt heir int;  // true (type inherits from base)

type Number = int;
let is_number: bool = Number heir Number;  // true (reflexive)
```

**Difference from `is` operator:**
* `is` checks the **runtime type** of a **value**: `x is int` checks if the value `x` has type `int`
* `heir` checks the **inheritance relationship** between **types**: `Employee heir BasePerson` checks if type `Employee` inherits from type `BasePerson`
* `is` operates on values; `heir` operates on type names

**Implementation note:** The `heir` operator is a built-in compiler check, not a magic method. Programmers cannot override this behavior via `extern<T>` blocks or magic methods. The compiler maintains type relationship information (including `structBases` map for struct extension) and performs the check both at compile time (for constant expressions) and at runtime (for dynamic checks). This is not a standard binary operator (`binOp`) but a special type relationship operator built into the language.

### 6.4. Assignment

* `=` move/assign; compound ops `+=` etc. desugar to method + assign if defined.

### 6.5. Cast Operator (`to`)

The `to` operator performs explicit type conversions with syntax `Expr to Type`.

**Precedence:** Postfix operators (`[]`, call, `.`, member-call like `.await()`, `to Type`) bind tightly before binary operators.

**Built-in cast rules:**

* **Identical type:** NOP (no operation).
* **Fixed → dynamic (same family):** Always allowed, e.g., `int32 to int`.
* **Dynamic → fixed (same family):** Explicit only; **checked**. If the value does not fit the target range the runtime raises a **trap** (no UB, no wraparound).
* **Between numeric families:** Explicit only with defined semantics and possible trap if the target cannot represent the value:
  * `int` ↔ `float`: standard conversion with potential precision loss; out-of-range values trap.
  * `int` ↔ `uint`: explicit only; negative values trap when casting to `uint`.
  * `float` ↔ `uint`: explicit only; fractional parts are truncated toward zero; out-of-range traps.
  * `float` ↔ `int`: explicit only; fractional parts are truncated toward zero; out-of-range traps.
* **Reference and pointer types:** `&T`, `&mut T`, and `*T` cannot be cast via `to` (compile error).
* **Tag constructors:** No casting to/from tags; use constructors and `compare` matching.

**Examples:**
```sg
let a: int32 = 300000;
let b: int = a to int;        // fixed→dynamic, always safe
let c: int16 = a to int16;    // may trap if value > int16 range
let d: float = 42 to float;   // int→float conversion
```

### 6.6. Custom Cast Protocol (`__to`)

User-defined types opt into casting by supplying `__to` overloads inside `extern<From>` blocks. The signature is strict: exactly two parameters (`self: From`, `target: To`) and the return type must be the same `To`. `__to` is guaranteed to either return a valid `To` or trap (never UB); if you need different behaviour (e.g., saturation/clamping), implement a separate function instead of overloading `__to`:

```sg
extern<From> {
  fn __to(self: From, target: To) -> To
}
```

Each target type gets its own overload; primitives in `core/intrinsics.sg` (module `core`) ship `@intrinsic` definitions while user code adds `@overload` bodies. The `to` operator drives resolution:

1. If `From` and `To` (after resolving aliases) are identical, the cast is a no-op.
2. Built-in numeric rules from §6.5 are consulted first (e.g., dynamic↔fixed conversions).
3. Otherwise the compiler looks for `__to` on the left operand’s type whose second parameter matches the resolved target type. Alias names participate in the lookup, so `type Gasoline = string` inherits `string -> string` conversions automatically. Any `__to` that adds extra parameters or returns anything other than the target type is rejected with a semantic error.
4. Multiple matches yield `E_AMBIGUOUS_CAST`; no match yields `E_NO_CAST`.

**Restrictions:**
* Direct calls to `__to` are forbidden; only `expr to Type` may invoke it.
* Reference and pointer types cannot define or consume casts.
* Casts never participate in function overload resolution; insert them explicitly when needed.

**Examples:**
```sg
type UserId = uint64;

extern<UserId> {
  fn __to(self: UserId, target: uint64) -> uint64 {
    return (self: uint64);
  }
}

extern<uint64> {
  fn __to(self: uint64, target: UserId) -> UserId {
    return (self: UserId);
  }
}

let uid: UserId = 42:uint64 to UserId;
let raw: uint64 = uid to uint64;
```

### 6.7. Saturating casts

Fixed-width numerics implement the `Bounded<T>` contract via static methods `__min_value/__max_value` defined in `core/intrinsics.sg` (module `core`). The stdlib exposes helpers:

```sg
fn min_value<T: Bounded<T>>() -> T { T.__min_value::<T>() }
fn max_value<T: Bounded<T>>() -> T { T.__max_value::<T>() }
```

The standard library provides `saturating_cast(from, to_proto)` overloads in `stdlib/saturating_cast.sg` for numeric types. The second argument is only used for its type (`to_proto`) and defines the result type. Semantics:

* For integer targets: clamp to `[min_value(target)..max_value(target)]`; negative inputs clamp to the target minimum (zero for unsigned).
* For floats: clamp to the finite range of the target precision using `min_value/max_value`.
* Each overload is concrete (no generics); missing pairs are a compile-time resolution error.

Use cases:

```sg
let x: int = 1_000;
let y: int8 = saturating_cast(x, 0:int8);   // 127
let z: uint8 = saturating_cast(-5, 0:uint8); // 0
```

If you need custom narrowing behaviour (rounding modes, error returns, etc.), write a dedicated helper; `__to` remains checked-and-trapping.

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

### 8.1. Preference of monomorphic overloads

Given a call `f(a1, ..., an)`, the set of candidate function signatures is first split into two groups:

* **Monomorphic candidates** — functions that do not require type argument inference for this call (either they have no type parameters or they are already fully instantiated).
* **Generic candidates** — functions that require inference of one or more type parameters from the actual arguments.

Overload resolution runs in two stages:

1. **Monomorphic stage.**  
   The algorithm from steps (1)–(6) above (arity filter, generic instantiation if needed, ownership adjustment, coercion cost graph, best candidate, qualifiers) is applied only to the monomorphic candidates.
   * If exactly one best candidate is found, it is selected and generic candidates are ignored.
   * If multiple candidates tie for the minimal total cost, the call is rejected as an ambiguous overload.
   * If no monomorphic candidate is applicable, proceed to the generic stage.

2. **Generic stage.**  
   The same algorithm is then applied to the generic candidates only.
   * If exactly one best candidate is found, it is selected.
   * If multiple candidates tie for the minimal total cost, the call is rejected as an ambiguous overload.
   * If no generic candidate is applicable either, the call is rejected with a “no suitable overload” error.

In other words, if at least one monomorphic overload is applicable, generic overloads are never considered for this call.

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

Restriction: `=>` is valid only in these `parallel` constructs and within `compare` arms (§3.6). Any other use triggers `SynFatArrowOutsideParallel`.

### 9.3. Backend Selection

`@backend("gpu")`/`@backend("cpu")` may annotate functions or blocks. If the target is unsupported, a diagnostic is emitted or a fallback is chosen by the compiler based on flags.

### 9.4. Tasks and spawn semantics

* `spawn expr` launches a new task to evaluate `expr` asynchronously. If `expr` has type `T`, `spawn expr` has type `Task<T>` (a join handle).
* `join(t: Task<T>) -> Erring<T, Cancelled>` waits for completion; on normal completion returns `Success(value)`, on cooperative cancellation returns error value.
* `t.cancel()` requests cooperative cancellation; tasks can check via `task::is_cancelled()`.
* Moving values into `spawn` consumes them (ownership semantics). Only `own` values may be moved into tasks.

### 9.5. Async/Await Model (Structured Concurrency)

Surge provides structured concurrency with async/await for managing asynchronous operations:

**Async Functions:**
```sg
async fn fetch_data(url: string) -> Erring<Data, Error> {
    let response = http_get(url).await();
    let response = compare response {
        Success(value) => value;
        err => return err;
    };
    return parse_response(response).await();
}

async fn process_multiple_urls(urls: string[]) -> Erring<Data[], Error> {
    let mut results: Data[] = [];
    for url in urls {
        let outcome = fetch_data(url).await();
        compare outcome {
            Success(data) => results.push(data);
            err => return err;
        };
    }
    return Success(results);
}
```

**Structured Concurrency Blocks:**
```sg
async {
    let task1 = spawn fetch_data("url1");
    let task2 = spawn fetch_data("url2");
    let task3 = spawn fetch_data("url3");

    let r1 = task1.await();
    let r2 = task2.await();
    let r3 = task3.await();

    // automatic cleanup on block exit
    // all spawned tasks are automatically cancelled if not awaited
}
```

**Key properties:**
* Async blocks provide automatic resource cleanup
* Tasks spawned within an async block are automatically cancelled when the block exits
* `await` is just a method call (`task.await()`) and can only be used within `async` functions or `async` blocks
* Async functions return `Future<T>`/`Task<T>` which must be awaited or spawned
* Structured concurrency ensures no "fire-and-forget" tasks that can leak

---

## 10. Standard Library Conventions

* `print(...args)` – variadic, casts each argument to `string` via `expr to string`, concatenates with spaces, appends newline.
* Core protocols provided for primitives and arrays: `__to`, `__abs`, numeric ops, comparisons, `__range`, `__index`.
* Types can `@override` their magic methods in `extern<T>` blocks; built-ins for **primitive** base types are sealed (cannot be overridden for the primitive itself).

---

## 11. Error Handling Model

Surge uses a modern error handling model based on the `Erring<T, E>` type, which represents computations that can either succeed with a value of type `T` or fail with an error of type `E`. This model provides explicit, type-safe error management without exceptions.

### Core Error Types

#### ErrorLike Contract

All error types must implement the `ErrorLike` contract:

```sg
pub contract ErrorLike {
    field message: string;
    field code: uint;
}
```

#### Base Error Type

```sg
pub type Error = { message: string, code: uint };
```

#### Success Tag and Erring Type

```sg
pub tag Success<T>(T);
pub type Erring<T, E: ErrorLike> = Success(T) | E;
```

An `Erring<T, E>` value represents either:
- `Success(value)` - a successful result containing a value of type `T`
- An error value of type `E` that implements `ErrorLike`

### Syntax Sugar

#### Error Type Syntax

Surge provides convenient syntax sugar for error types:

```sg
// These are equivalent:
fn compute() -> int! { ... }
fn compute() -> Erring<int, Error> { ... }
```

The `T!` syntax always means "T or Error" where Error is the base error type.

#### Auto-wrapping

In clear contexts, Surge automatically wraps return values:

```sg
fn maybe_return() -> Erring<int, Error> {
    if (condition) {
        return 42;  // Auto-wrapped to Success(42)
    } else {
        let e: Error = { message: "failed", code: 1:uint };
        return e;   // Error values pass through directly
    }
}
```

### Pattern Matching

Use `compare` expressions to handle all possible outcomes:

```sg
fn handle_result(result: Erring<int, Error>) -> int {
    return compare result {
        Success(value) => value;
        err => {
            panic("Operation failed: " + err.message);
            0  // Unreachable but required for type checking
        };
    };
}

// Use wildcards to catch all errors:
fn safe_operation(result: Erring<int, Error>) -> int {
    return compare result {
        Success(value) => value;
        _ => 0;  // Default value for any error
    };
}
```

### Built-in Methods

The `Erring` type provides useful methods:

```sg
extern<Erring<T, E>> {
    // Extract value or return default
    pub fn safe<T, E: ErrorLike>(self: Erring<T, E>) -> T {
        compare self {
            Success(v) => v;
            _ => default::<T>();
        };
    }

    // Terminate program on error
    pub fn exit<T, E: ErrorLike>(self: Erring<T, E>) -> nothing {
        compare self {
            Success(_) => return nothing;
            err => exit(err);
        };
    }
}
```

### Custom Error Types

Define domain-specific error types:

```sg
pub type ValidationError = {
    message: string;
    code: uint;
    field: string;
};

fn validate_input(input: string) -> Erring<string, ValidationError> {
    if (input.length() == 0) {
        let err: ValidationError = {
            message: "Input cannot be empty",
            code: 400:uint,
            field: "input"
        };
        return err;
    }
    return Success(input);
}
```

### Error Propagation

Manual error propagation:

```sg
fn complex_operation() -> Erring<string, Error> {
    let step1 = parse_input();
    let value1 = compare step1 {
        Success(v) => v;
        err => return err;  // Propagate error
    };

    let step2 = process_data(value1);
    let value2 = compare step2 {
        Success(v) => v;
        err => return err;
    };

    return Success(format_output(value2));
}
```

### Exhaustiveness Checking

The compiler ensures all cases are handled:

```sg
fn handle_result(result: Erring<int, Error>) {
    compare result {
        Success(value) => print(value to string);
        // Compiler error if error case is missing
        _ => print("Error occurred");
    };
}
```
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
// Type alias with override
type MyInt = int;
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
// Custom casting with type alias
type UserId = uint64;

extern<UserId> {
  fn __to(self: UserId, target: uint64) -> uint64 { return (self: uint64); }
}

extern<uint64> {
  fn __to(self: uint64, target: UserId) -> UserId { return (self: UserId); }
}

let uid: UserId = 42:uint64 to UserId;
let raw: uint64 = uid to uint64;
```

```sg
// Struct casting
type Point2D = { x: float32, y: float32 }
type Point3D = { x: float32, y: float32, z: float32 = 0.0 }

extern<Point2D> {
  fn __to(self: Point2D, target: Point3D) -> Point3D {
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
async fn process_data(urls: string[]) -> Erring<Data[], Error> {
    async {
        let tasks: Task<Erring<Data, Error>>[] = [];

        for url in urls {
            let task = spawn fetch_data(url);
            tasks.push(task);
        }

        let results: Erring<Data, Error>[] = [];
        for task in tasks {
            results.push(task.await());
        }

        return Success(results);
    }
}
```

```sg
// @noinherit field example
type Base = { @noinherit internal_id:uint64, name:string }
type Public = Base : { display:string } // field internal_id not inherited in Public
```

```sg
// Struct inheritance and heir operator
type BasePerson = { name: string, age: int }
type Employee = BasePerson : { id: uint, department: string }
type Manager = Employee : { team_size: int }

fn check_inheritance() -> bool {
  // Check direct inheritance
  let is_employee: bool = Employee heir BasePerson;  // true
  
  // Check transitive inheritance
  let is_manager_base: bool = Manager heir BasePerson;  // true
  
  // Check self-inheritance
  let is_self: bool = Employee heir Employee;  // true
  
  // Use in conditional
  if (Manager heir Employee) {
    print("Manager inherits from Employee");
  }
  
  return is_employee && is_manager_base;
}
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
// Test directive example with pragma directive
import stdlib/directives::test;

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
// core/intrinsics example (only in special module core)
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
import stdlib/directives::benchmark;

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
// Time measurement directive example (simplified)
import stdlib/directives::time;

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

### 13.2. Name Resolution in Directives

**Resolution rules:**

* The left-hand side of `.` in directive expressions (e.g., `test.eq`) **must** resolve to an **imported directive module**.
* The right-hand side must be a **public function** (`pub fn`) from that directive module.
* All other identifiers (e.g., `foo` in `test.eq(foo, 42)`) resolve normally from the **current module scope**.
* Directive module imports **must** have `pragma directive` at the top; otherwise `E_DIRECTIVE_NOT_A_DIRECTIVE_MODULE` is emitted.

**Scope:**

* Directives have **read-only access** to names available at their declaration site (module scope).
* Items declared **within** directives are **not visible** to the rest of the program.
* Directives do not modify program typization or ABI.

### 13.3. Directive Execution Modes

Directives are controlled by the `--directives` flag with four execution modes:

* `--directives=off` (default) — directive blocks are completely ignored by the parser, no diagnostics.
* `--directives=collect` — parser recognizes directive blocks and adds them to AST as `DirectiveBlock` nodes; no name resolution or type checking.
* `--directives=gen` — compiler generates hidden test module with synthesized function calls; participates in type checking but not execution.
* `--directives=run` — same as `gen`, plus executes the generated test functions during build; non-zero exit code on failures.

**Additional flags:**

* `--directives-filter=<ns1,ns2,...>` — process only specified namespaces (applies to `gen` and `run`).
* `--emit-directives=memory|cache` — where to store generated module (default: `memory` for in-memory only, `cache` to serialize to build cache).

**Execution model:**

In `gen` and `run` modes, directive blocks are transformed into hidden functions:

```sg
fn __generated_directives {
  fn __test_file_<hash>_1() { ::stdlib::directives::test::eq(::current::foo, 42); }
}
```

These functions participate in name resolution and type checking. In `run` mode, they are executed by the test runner; failures produce non-zero exit codes.

### 13.4. Built-in (Standard) Directives

* **`test:`** — executes test scenarios; standard conventions:
  * `test.eq(actual, expected) -> Erring<nothing, Error>`
  * `test.le(a, b)`, `test.ok(cond)`, etc.
* **`benchmark:`** — benchmarking (ignored in release builds by default unless explicitly enabled).
* **`time:`** — timing measurements (for local analysis, ignored in release builds by default).

### 13.5. Execution Model Equivalence

Directive blocks are transformed into hidden functions in a generated module. Each expression in a directive block becomes one function call.

**Transformation example:**

Source:
```sg
/// test:
/// SumIsCorrect:
///   test.eq(add(1, 2), 3);
```

is equivalent to creating a hidden wrapper function and calling it in the test runner:

```
fn __directive_test_SumIsCorrect__() -> Erring<nothing, Error> {
  return test.eq(add(1, 2), 3);
}
```

**Function naming:**

* Deterministic: `__<namespace>_file_<hash>_<seq>`
* Stable across incremental builds
* Namespace matches directive module name

**When executed:**

* In `gen` mode: generated functions participate in type checking only.
* In `run` mode: generated functions are executed by test runner; non-zero exit on failure.
* In `off` and `collect` modes: no generation occurs.

### 13.6. `pragma directive` and Directive Modules

Directive modules are ordinary Surge modules marked with `pragma directive` at the top of the file. They export functions that serve as handlers for directive blocks.

**Syntax:**

```sg
pragma directive

// Regular module code with exported functions
pub fn eq<T>(f: fn() -> T, expected: T) -> void { ... }
pub fn panics(f: fn() -> void) -> void { ... }
```

**Rules:**

* `pragma directive` must appear as the **first meaningful line** of the module file (after optional shebang).
* The directive module **namespace** is derived from the module import path. For example, importing `stdlib/directives::test` makes `test` the directive namespace.
* All public functions (`pub fn`) from the directive module are accessible in directive blocks via `<namespace>.<function>`.
* Directive modules are **ordinary modules**: they may import other modules, define types, and perform any valid Surge operations. No special restrictions apply.
* Directive modules **do not** modify the ABI of main program code; their functions execute only during directive execution.

**Example directive module:**

```sg
// stdlib/directives/test.sg
pragma directive

import stdlib::fmt;

pub fn eq<T>(f: fn() -> T, expected: T) -> void {
  let got = f();
  if got != expected {
    panic(fmt::format("eq failed: got {}, expected {}", got, expected));
  }
}

pub fn panics(f: fn() -> void) -> void {
  let ok = catch_unwind(f);
  if ok {
    panic("expected panic, but function returned normally");
  }
}
```

**Usage in code:**

```sg
import stdlib/directives::test;

fn foo() -> int { return 42 }

/// test:
/// test.eq(foo, 42)
```

The compiler generates hidden test functions that call the directive module functions with resolved identifiers from the current module.

### 13.7. Safety and Invariants

**Generated code isolation:**

* Directive-generated functions **never appear in final binary** without `--directives=run`.
* In `off` mode, directive behavior is identical to absence of directives (zero overhead).
* Directives have **no special privileges**: directive modules are ordinary modules with no special restrictions.

**Deterministic behavior:**

* Function names are deterministic and stable across incremental builds.
* Generated module references original sources via Spans for error reporting.
* Directive execution respects `--directives-filter`; unspecified namespaces are ignored.

**Future extensions:**

* Directive space aliases via `pragma directive name="alias"` (optional, not in M0).
* File-level directives without item attachment (future expansion).
* `--directives=list` mode for debugging (print discovered blocks).
* `--sandbox` profile for `run` mode (restrict I/O operations).

### 13.8. Target Directives (Conditional Compilation)

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

### 13.9. Examples

**Unit test:**
```sg
import stdlib/directives::test;

fn add(a: int, b: int) -> int { return a + b; }

/// test:
/// AddSmall:
///   test.eq(add(2, 3), 5);
```

**Benchmark:**
```sg
import stdlib/directives::benchmark;

/// benchmark:
/// AddBench:
///   benchmark.measure(|| { let mut s=0; for i:int in 0..1_000_000 { s+=i; } return s; });
```

**Custom directive:**
```sg
import mytools::lint;

/// lint:
/// lint.scan_module()
```

**CLI usage examples:**

```bash
# Ignore all directives (default)
surge build

# Parse and collect directive blocks
surge build --directives=collect

# Generate and type-check, don't run
surge build --directives=gen

# Run only test directives
surge build --directives=run --directives-filter=test

# Generate with cache storage
surge build --directives=gen --directives-filter=test,benchmark --emit-directives=cache
```

---

## 14. Precedence & Associativity

From highest to lowest:

1. `[]` (index), call `()`, member `.`, await `.await`, `to Type` (cast operator)
2. `+x -x !x` (prefix unary)
3. `* / %`
4. `+ -` (binary)
5. `<< >>` (bitwise shift)
6. `& ^ |` (bitwise operations)
7. `..` `..=` (range operators)
8. `< <= > >= == != is heir` (all comparison and type checking operators have same precedence, left-associative)
9. `&&`
10. `||`
11. `? :` (ternary, right-associative)
12. `??` (null coalescing)
13. `=` `+=` `-=` `*=` `/=` `%=` `&=` `|=` `^=` `<<=` `>>=` (assignment, right-associative)

**Type checking precedence:**
Type checking operators `is` and `heir` have the same precedence as equality operators. Use parentheses for complex expressions:
```sg
x is int && y is string           // OK
Employee heir BasePerson && flag  // OK
(x is int) == true                // explicit grouping recommended
(Employee heir BasePerson) == true // explicit grouping recommended
```

Short-circuiting for `&&` and `||` is guaranteed.

Note: `=>` is not a general expression operator; it is reserved for `parallel map` / `parallel reduce` (§9.2) and for arms in `compare` expressions (§3.6).

### Member access precedence

Member access `.`, await `.await`, and cast `to Type` are postfix operators and bind tightly together with function calls and indexing. This resolves ambiguous parses, e.g., `a.f()[i].g()` parses as `(((a.f())[i]).g)()`.

---

## 15. Name & Visibility

* Items are `pub` (public) or private by default. (Default: private.)
* `pub fn`, `pub type`, `pub let` export items from the module.

### 15.1. Resolving `Ident(...)`

- In expression or pattern position the form `Ident(...)` is ambiguous if both a tag constructor and a function named `Ident` are visible. The compiler emits `SemaAmbiguousCtorOrFn`.

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
type UserId<T> = int;
type ProductId<T> = int;

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

Directives are parsed as "out-of-band" nodes attached to the module. In `gen` and `run` modes, directive expressions are transformed into hidden functions in `__generated_directives` module. Each line becomes one function call to the imported directive module's handler functions.

```
Module     := PragmaDirective? (Item | DirectiveBlock)*
PragmaDirective := "pragma" "directive"
DirectiveBlock := "///" Namespace ":" Newline
                  ( "///" BodyLine Newline )+
Namespace  := Ident
BodyLine   := <Surge expression on single line>
Item       := Visibility? (Fn | AsyncFn | MacroDef | TagDecl | TypeDef | LiteralDef | AliasDef | ExternBlock | Import | Let)
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

* Built-ins for primitive base types are sealed; you cannot `@override` them directly. Use `type New = int;` and override on the type alias.
* Dynamic numerics (`int/uint/float`) allow large results; casts to fixed-width may trap.
* Attributes affecting memory layout and ABI (`@packed`, `@align`) are part of the language specification and cannot be replaced by directives. Directives do not modify type layout or ABI contracts.
* Concurrency contract attributes describe *analyzable requirements* and do not change language semantics at runtime. Violations may not always be statically checkable; in such cases the compiler emits `W_CONC_UNVERIFIED` and defers verification to linters or runtime debug tools.
* **Directive vs Attribute distinction**: Attributes are closed-set language features that affect compilation, type checking, or runtime behavior. Directives are extensible annotations that provide metadata for external tools without changing language semantics. Tests, benchmarks, and documentation have been moved from attributes to the directive system to maintain the distinction.
* **Pragma directive**: `pragma directive` marks a module as a directive module. The directive namespace is derived from the import path. In `--directives=off` (default), directive blocks have zero overhead.
* **Language intrinsics**: Intrinsics constitute a fixed, small set and are declared in the standard `core` module (files under `core/`). Their implementation is described in RUNTIME.md. Using `@intrinsic` outside this module is forbidden. They serve basic memory management operations (`rt_alloc`, `rt_free`, `rt_realloc`) and byte copying (`rt_memcpy`, `rt_memmove`).

## 21. Diagnostics Overview

Diagnostics now follow the numeric `diag.Code` families defined in `internal/diag/codes.go`; the old `E_*` mnemonics are retired.

**Lexical (1000–):**
- `LexUnknownChar`, `LexUnterminatedString`, `LexUnterminatedBlockComment`, `LexBadNumber`.

**Syntax (2000–):**
- Core: `SynUnexpectedToken`, `SynUnclosedDelimiter` (and specific paren/brace/bracket variants), `SynExpectSemicolon`, `SynPragmaPosition`.
- Loops: `SynForMissingIn`, `SynForBadHeader`.
- Modifiers/attributes: `SynModifierNotAllowed`, `SynAttributeNotAllowed`, `SynAsyncNotAllowed`.
- Types: `SynTypeExpectEquals`, `SynTypeExpectBody`, `SynTypeExpectUnionMember`, `SynTypeFieldConflict`, `SynTypeDuplicateMember`, `SynTypeNotAllowed`.
- Imports: `SynUnexpectedTopLevel`, `SynExpectIdentifier`, `SynExpectModuleSeg`, `SynExpectItemAfterDbl`, `SynExpectIdentAfterAs`, `SynEmptyImportGroup`.
- Type expressions: `SynExpectRightBracket`, `SynExpectType`, `SynExpectExpression`, `SynExpectColon`, `SynUnexpectedModifier`.
- Contextual: `SynIllegalItemInExtern`, `SynVisibilityReduction`, `SynFatArrowOutsideParallel`.

**Semantic (3000–):**
- Naming: `SemaDuplicateSymbol`, `SemaShadowSymbol`, `SemaUnresolvedSymbol`, `SemaModuleMemberNotFound`, `SemaModuleMemberNotPublic`, style hints `SemaFnNameStyle`/`SemaTagNameStyle`.
- Functions & intrinsics: `SemaFnOverride`, `SemaIntrinsicBadContext`, `SemaIntrinsicBadName`, `SemaIntrinsicHasBody`, `SemaAmbiguousCtorOrFn`.
- Types & expressions: `SemaTypeMismatch`, `SemaInvalidBinaryOperands`, `SemaInvalidUnaryOperand`, `SemaExpectTypeOperand`.
- Borrow checker scaffolding: `SemaBorrowConflict`, `SemaBorrowMutation`, `SemaBorrowMove`, `SemaBorrowThreadEscape`, `SemaBorrowImmutable`, `SemaBorrowNonAddressable`, `SemaBorrowDropInvalid`.

**I/O (4000–):**
- `IOLoadFileError`.

**Project/import graph (5000–):**
- `ProjDuplicateModule`, `ProjMissingModule`, `ProjSelfImport`, `ProjImportCycle`, `ProjInvalidModulePath`, `ProjInvalidImportPath`, `ProjDependencyFailed`.
- Module pragma consistency: `ProjMissingModulePragma`, `ProjInconsistentModuleName`, `ProjInconsistentNoStd`.
- Import path hygiene: `ProjWrongModuleNameInImport`.

**Observability (6000–):**
- `ObsTimings`.

Planned diagnostics for directives, concurrency contracts, exhaustive `compare`, and macro/runtime surfaces are not wired up yet in sema.
