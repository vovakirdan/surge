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
- Tuple and function types parse, but sema does not yet lower them. **Part of v1.**
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
* **Fixed-size numerics** (layout-specified, **part of v1**):

  * `int8, int16, int32, int64`, `uint8, uint16, uint32, uint64`.
  * `float16, float32, float64`.
* **Other primitives**:

  * `bool` – logical; no implicit cast to/from numeric.
  * `string` – immutable UTF-8 bytes with Unicode code point semantics (length and indexing are by code point).
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
* In an `extern<Type<T>>` block, `T` is the type parameter from the target type; methods inside the block may introduce their own type parameters `<U, ...>` but **cannot** redeclare the same names as the outer type parameters (shadowing is an error).

#### Type Argument Syntax

In **type annotations**, both standard and turbofish syntax are accepted:

```sg
let x: Box<int> = ...;      // standard
let y: Box::<int> = ...;    // turbofish (equivalent)
```

In **expressions** (function calls, method calls), only turbofish syntax is allowed for explicit type arguments:

```sg
// Function calls
let a = make::<int>();      // ok: explicit type arg
let b = id(42);             // ok: type arg inferred from argument
let c = make<int>();        // error: use '::<' syntax

// Method calls on generic types
let d = Foo::<int>.new();   // ok: explicit type arg on receiver
```

For generic struct literals, the type must be specified via annotation:

```sg
let e: Box<int> = { value: 42 };  // ok: type from annotation
let f: Box<int> = { 42 };         // ok: positional form
```

#### Type Inference Rules

Type parameters for generic functions are inferred **only from call arguments**, never from the expected return type:

```sg
fn make<T>() -> T;
fn id<T>(x: T) -> T;

fn test() {
    let a = id(42);           // ok: T=int inferred from argument
    let b: int = make();      // error: cannot infer T (no arguments)
    let c = make::<int>();    // ok: explicit type argument
}
```

When type parameters cannot be inferred from arguments, the compiler reports an error with a suggestion to use explicit turbofish syntax.

#### Type Parameters in extern Blocks

Type parameters from `extern<Type<T>>` are available to all methods without redeclaration:

```sg
type Foo<T> = {};

extern<Foo<T>> {
    fn new() -> Foo<T>;                    // T from extern
    fn map<U>(self: &Foo<T>, f: fn(T) -> U) -> Foo<U>;  // T from extern, U is method's own
}

// Calls:
Foo::<int>.new()                           // T=int
Foo::<int>.map::<string>(foo, transform)   // T=int, U=string
```

Method type parameters **cannot** shadow extern type parameters:

```sg
extern<Foo<T>> {
    fn bad<T>(t: T) -> Foo<T>;  // error: T shadows outer T
}
```

Generic monomorphization and instantiation are described in §16.1.

### 2.5. User-defined Types

* **Type alias (single target):** `type MyInt = int;` creates a distinct nominal type that inherits semantics of `int` but can override magic methods via `extern<MyInt>`. Multi-member aliases like `type A = T1 | T2` are **not supported**; use tagged unions instead (§2.8).
* **Struct:** `type Person = { age:int, name:string, @readonly weight:float }`.

  * Fields are immutable unless variable is `mut`. `@readonly` forbids writes even through `mut` bindings.
  * Struct literals may specify the type inline: `let p = Person { age: 25, name: "Alex" };`. The parser only treats `TypeName { ... }` as a typed literal when `TypeName` follows the CamelCase convention so that `while ready { ... }` still parses as a control-flow block.
  * When the type is known (either via `TypeName { ... }` or an explicit annotation on the binding), the short `{expr1, expr2}` form is allowed; expressions are matched to fields in declaration order. Wrap identifier expressions in parentheses (`{(ageVar), computeName()}`) when using positional literals so they are not mistaken for field names.
* **Literal enums:** `type Color = "black" | "white";` Only the listed literals are allowed values.
* **Enums:** Named constants with explicit or auto-incremented values.

```sg
// Integer enum with auto-increment
enum Color = {
    Red,      // 0
    Green,    // 1
    Blue      // 2
}

// Integer enum with explicit values
enum HttpStatus: int = {
    Ok = 200,
    NotFound = 404,
    ServerError = 500
}

// String enum (requires explicit values)
enum Status: string = {
    Active = "active",
    Inactive = "inactive"
}

// Usage: qualified names with Type::Variant syntax
let c: int = Color::Red;
let status: string = Status::Active;

// Enums can be imported from modules
import ./mymodule::HttpStatus;
let code: int = HttpStatus::Ok;
```

  * Enum variants are accessed via qualified names using `EnumName::VariantName` syntax.
  * Integer enums support auto-increment (starting from 0) or explicit values.
  * String enums require explicit values for all variants.
  * Enum types can be imported from modules and used cross-module.
  * Enums are lowered to type aliases and internal constants.

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

### 2.8. Tagged unions (aliases)

`type` can describe a sum type **only with tagged members**. Plain untagged unions like `type A = T1 | T2` are **not supported**.

```sg
tag Left(L); tag Right(R);
type Either<L, R> = Left(L) | Right(R)
```

Rules:
- Every member must be a tag constructor or `nothing`.
- Exhaustiveness checking for `compare` arms is planned but not yet enforced; treat it as a future validation.
- Use tags for stable, extensible APIs; untagged structural mixes are not allowed.

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

### 2.10. Tuple Types

**What:** Tuples are fixed-size heterogeneous collections of values, written as `(T1, T2, ...)`. They allow grouping multiple values together without defining a custom struct.

**Syntax:**
```surge
// Type annotations
fn coordinates() -> (int, int) {
    return (10, 20);
}

// Multiple return values
fn divmod(a: int, b: int) -> (int, int) {
    return (a / b, a % b);
}

// Let bindings
let pair: (string, bool) = ("hello", true);

// Nested tuples
let nested: ((int, int), string) = ((1, 2), "point");

// Single-element tuple (distinct from grouping)
let single: (int,) = (42,);
```

**Rules:**
- Empty tuple `()` is equivalent to the `unit` type (no-value return)
- Single-element tuple `(T,)` requires trailing comma to distinguish from grouping `(T)`
- Tuple elements can be any type, including other tuples, structs, or generic types
- Tuples are structural types: two tuples with the same element types in the same order are the same type
- Assignment requires exact type match: arity and element types must match

**Element Access:**
Tuple elements can be accessed using numeric indices:
```surge
let pair = (1, "hello");
let first = pair.0;   // int: 1
let second = pair.1;  // string: "hello"

// Nested tuple access
let nested = ((1, 2), 3);
let value = nested.0.1;  // int: 2
```

**Destructuring:**
Tuples can be destructured in let bindings:
```surge
let pair = (1, "hello");
let (x, y) = pair;  // x: int, y: string

// Inline destructuring
let (a, b) = (10, 20);
```

**Limitations:**
- Destructuring in compare patterns is limited
- Use tuples primarily for multiple return values and temporary groupings

**Example:**
```surge
fn min_max(a: int, b: int) -> (int, int) {
    if a < b {
        return (a, b);
    } else {
        return (b, a);
    }
}

fn process() {
    let result: (int, int) = min_max(5, 3);
    // result is (3, 5)
}
```

### 2.11. Memory Management Model

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

### 2.12. Contracts (structural interfaces) and bounds

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
- Tagged unions (`Option`, `Erring`, `Either`, …) → require an explicit initializer (`E_UNDEFINED_DEFAULT`).

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
* Range operands use overload resolution: `s[a..b]` resolves `__index(self, r: Range<int>)`.
  Range literals are bracketed, so this can also appear as `s[[a..b]]` without a dedicated slice syntax.
* Array slicing via ranges returns a view, not a copy; mutations through the view affect the base array.
* Array views are not resizable; push/pop/reserve operations are rejected at compile time (and still panic if bypassed).
* For strings, `s[i]` returns a `uint32` code point (code point indexing, not byte indexing).
* Negative indices count from the end for arrays and strings.
* `[..=]` is invalid (inclusive end requires an end bound).
* `[1..3,]` is an array literal (single element), not a range literal.

### 3.5. Signals (Reactive Bindings) — **Future Feature (v2+)**

> **Status:** Not supported in v1. Reserved for v2+.

* `signal name := expr;` — syntax reserved for reactive bindings
* Planned: automatic re-evaluation when dependencies change
* Planned: topological ordering, `@pure` requirement for expressions

**v1 behavior:** Lexer accepts keyword, but semantic analysis rejects with error message:
- "signal is not supported in v1, reserved for future use"

See §22.1 for detailed future specification.

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
- Exhaustiveness for tagged unions is planned (compare should cover all declared tags or have `finally`); the current compiler does not enforce this yet. Untagged unions are not supported.
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
* `@override` *(fn)* — replaces an existing implementation for a function or method. Incompatible with `@overload`.

  **Two use cases:**
  1. **Inside `extern<T>` blocks** — overrides a method for a type.
  2. **Outside `extern<T>` blocks** — overrides a local function declared earlier in the same module without a body implementation (forward declaration).
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

| Attribute        |  Fn | Block | Type | Field | Param | Stmt | Let |
| ---------------- | :-: | :---: | :--: | :---: | :---: | :--: | :-: |
| @pure            |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @overload        |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @override        |  ✅* |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @intrinsic       |  ✅** |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @backend         |  ✅  |   ✅   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @packed          |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |  ❌  |  ❌  |
| @align           |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |  ❌  |  ❌  |
| @raii            |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |  ❌  |  ❌  |
| @arena           |  ❌  |   ❌   |   ✅  |   ✅   |   ✅   |  ❌  |  ❌  |
| @weak            |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |  ❌  |
| @shared          |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |  ❌  |  ❌  |
| @atomic          |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |  ❌  |
| @readonly        |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |  ❌  |
| @deprecated      |  ✅  |   ❌   |   ✅  |   ✅   |   ❌   |  ❌  |  ✅  |
| @hidden          |  ✅  |   ❌   |   ✅  |   ✅   |   ❌   |  ❌  |  ✅  |
| @noinherit       |  ❌  |   ❌   |   ✅  |   ✅   |   ❌   |  ❌  |  ❌  |
| @sealed          |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |  ❌  |  ❌  |
| @entrypoint      |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @guarded_by      |  ❌  |   ❌   |   ❌  |   ✅   |   ❌   |  ❌  |  ❌  |
| @requires_lock   |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @acquires_lock   |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @releases_lock   |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @waits_on        |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @send            |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |  ❌  |  ❌  |
| @nosend          |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |  ❌  |  ❌  |
| @nonblocking     |  ✅  |   ❌   |   ❌  |   ❌   |   ❌   |  ❌  |  ❌  |
| @drop            |  ❌  |   ❌   |   ❌  |   ❌   |   ❌   |  ✅  |  ❌  |
| @failfast        |  ❌  |   ❌   |   ❌  |   ❌   |   ❌   |  ✅  |  ❌  |
| @copy            |  ❌  |   ❌   |   ✅  |   ❌   |   ❌   |  ❌  |  ❌  |

*`@override` — only within `extern<T>` blocks.
**`@intrinsic` — only on function declarations (FnDecl) without body.

#### Typical Conflicts

* `@overload` + first function declaration → `E_OVERLOAD_FIRST_DECL`
* `@override` + `@overload` on same declaration → `SemaAttrConflict` (3060)
* `@packed` + `@align(N)` on same declaration → `SemaAttrPackedAlign` (3061)
* `@sealed` + attempt to extend type → `SemaAttrSealedExtend` (3074)
* `@send` + `@nosend` on same type → `SemaAttrSendNosend` (3062)
* `@nonblocking` + `@waits_on` on same function → `SemaAttrNonblockingWaitsOn` (3063)

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
  type Num = int;
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

### 4.5. Macros — **Future Feature (v2+)**

> **Status:** Not supported in v1. Reserved for v2+.

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

**Implementation Status:** Macros are reserved for future iterations. The syntax and semantics are specified for completeness, but core implementation work should focus on other features first. Macros will be added in v2+ after the base language is stable.

**v1 behavior:** Lexer accepts keyword, but semantic analysis rejects with error message:
- "macro is planned for v2+"

See §22.2 for complete specification.

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
* Range creation: `start..end`, `start..=end` (binary operators) and range literals
  `[start..end]`, `[start..=end]`, `[start..]`, `[..end]`, `[..=end]`, `[..]`.
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
* Direct calls to `__to` are forbidden; only `expr to Type` or implicit conversion may invoke it.
* Reference and pointer types cannot define or consume casts.
* Explicit casts (`expr to Type` or `expr: Type`) never participate in function overload resolution; insert them explicitly when needed. However, implicit conversions (§6.6.1) do participate with lower priority.

#### 6.6.1. Implicit Conversions

The compiler automatically applies `__to` conversions in specific coercion sites when the target type is known and exactly one applicable `__to` method exists. This eliminates boilerplate explicit casts while preserving type safety.

**Coercion sites (automatic `__to` application):**

1. **Variable bindings:** `let x: T = expr` where `expr` has type `U` and `__to(U, T)` exists
2. **Function arguments:** `foo(arg)` where `arg` has type `U`, parameter expects type `T`, and `__to(U, T)` exists
3. **Return statements:** `return expr` where `expr` has type `U`, function returns `T`, and `__to(U, T)` exists
4. **Struct field initialization:** `Struct { field: expr }` where `expr` has type `U`, field expects type `T`, and `__to(U, T)` exists
5. **Array elements:** `[expr1, expr2]` where elements have type `U`, array expects type `T[]`, and `__to(U, T)` exists

**Resolution rules:**

* Only applied when the target type is explicitly known from context (type annotation, function signature, etc.)
* Exactly one `__to(source, target)` method must exist; ambiguity or absence reports an error
* No conversion chaining: `T -> U -> V` is never attempted; only single-step `T -> U` conversions
* Conversions are never applied in binary/unary operator resolution
* In function overload resolution, implicit conversion has lower priority (cost = 2) than:
  - Exact type match (cost = 0)
  - Literal coercion (cost = 1)
  - Numeric widening (cost = 1)

**Examples:**

```sg
type Meters = float;
type Feet = float;

extern<Meters> {
  fn __to(self: Meters, _: Feet) -> Feet {
    return (self * 3.28084): Feet;
  }
}

// Implicit conversion in variable binding
let distance_m: Meters = 100.0;
let distance_ft: Feet = distance_m;  // Calls __to(Meters, Feet) implicitly

// Implicit conversion in function argument
fn display_feet(f: Feet) { print(f); }
display_feet(distance_m);  // Calls __to(Meters, Feet) implicitly

// Implicit conversion in return
fn convert_to_feet(m: Meters) -> Feet {
  return m;  // Calls __to(Meters, Feet) implicitly
}

// Implicit conversion in struct field
type Measurement = { value_ft: Feet };
let m = Measurement { value_ft: distance_m };  // Calls __to implicitly

// Implicit conversion in array elements
let measurements: Feet[] = [distance_m, distance_m * 2.0];  // Each element converted
```

**Diagnostic codes:**

* `SemaNoConversion` (3098): No `__to(T, U)` conversion exists for required coercion
* `SemaAmbiguousConversion` (3099): Multiple `__to(T, U)` candidates found (should not occur in well-formed code due to Surge's overload/override semantics)

**Built-in implicit conversions:**

The prelude provides `@intrinsic __to` methods for common conversions:

* Numeric: `string -> int/uint/float`, `int -> string/float`, `uint -> string/int/float`, `float -> string/int/uint`, plus fixed-width conversions
* Boolean: `bool -> string/int`
* Within numeric families: `intN -> int`, `uintN -> uint`, `floatN -> float` (lossless widening)

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

### 7.2. Range Literals

Range literals use square brackets and optional bounds:

* `[..]`
* `[start..]`
* `[..end]`
* `[..=end]`
* `[start..end]`
* `[start..=end]`

Bounds are expressions; when present they must be `int` (no implicit casts).  
Inclusive end requires an end bound, so `[..=]` is invalid.  
A trailing comma turns it into an array literal: `[1..3,]` is a single-element array containing `1..3`.

### 7.3. Strings

* `string` stores UTF-8 bytes; constructors validate UTF-8 and normalize to NFC.
* `len(&s)` returns the number of Unicode code points.
* `s[i]` returns the code point at index `i` as `uint32` (negative indices count from the end).
* `s[[a..b]]` slices by code point indices and returns `string`. Omitted bounds default to `0`/`len`.
  Inclusive `..=` adds one to the end bound, indices are clamped, and `start > end` yields `""`.
* `bytes()` returns a `BytesView` over UTF-8 bytes. `len(&view)` returns byte length; `view[i]` returns `uint8`.
* Implementation detail: strings may be stored as a rope internally. Concatenation and slicing can return views, and byte access materializes a flat UTF-8 buffer lazily.

**Examples:**
```sg
let text = "Hello 👋 World";
print(len(&text) to string);          // 13 (code points)
print((text[6] to uint) to string);   // code point value for 👋
print(text[[1..4]]);                  // "ell"

let view = text.bytes();
print(len(&view) to string);          // 15 (UTF-8 bytes)
print((view[0] to uint) to string);   // 72 ('H')
```

### 7.4. String standard methods

The core prelude defines common string helpers as methods on `string`:

* `contains(needle: string) -> bool` — true if `needle` occurs.
* `find(needle: string) -> int` — first code point index, or `-1` if missing.
* `rfind(needle: string) -> int` — last code point index, or `-1` if missing.
* `starts_with(prefix: string) -> bool`, `ends_with(suffix: string) -> bool`.
* `split(sep: string) -> string[]` — empty `sep` splits into code points.
* `join(parts: string[]) -> string` — uses `self` as the separator.
* `trim()`, `trim_start()`, `trim_end()` — remove ASCII whitespace (`space`, `\\t`, `\\n`, `\\r`).
* `replace(old: string, new: string) -> string` — if `old` is empty, returns the original string.
* `reverse() -> string` — reverses by code points.
* `levenshtein(other: string) -> uint` — edit distance by code points.

---

### 7.5. Array standard methods

The core prelude defines array helpers as methods on `Array<T>`:

* `push(value: T) -> nothing`, `pop() -> Option<T>`, `reserve(new_cap: uint) -> nothing`
* `extend(other: &Array<T>) -> nothing`
* `slice(r: Range<int>) -> Array<T>` — thin wrapper over `self[r]` (view)
* `contains(value: &T) -> bool`, `find(value: &T) -> Option<uint>` (requires `__eq` on `T`; currently provided for `Array<int>`, `Array<uint>`, `Array<float>`, `Array<bool>`, `Array<string>`)
* `reverse_in_place() -> nothing`

Top-level helpers `array_push/array_pop/array_reserve` mirror the intrinsic operations.

For fixed-size arrays, `ArrayFixed<T, N>` provides `to_array() -> Array<T>`.

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

Tagged unions are not implicitly expanded by overload resolution; branch explicitly with `compare` on tag payloads.

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

### 9.2. Parallel Map / Reduce — **Future Feature (v2+)**

> **Status:** Not supported in v1. Requires multi-threading (v2+).

**Reserved syntax:**
```sg
parallel map xs with (args) => func
parallel reduce xs with init, (args) => func
```

**Planned semantics:**
- True parallelism on multiple OS threads
- Work-stealing scheduler
- Requires `@pure` functions
- Automatic load balancing

**v1 alternative:** Use `spawn` with channels for concurrent (not parallel) processing:
```sg
async fn concurrent_map<T, U>(xs: T[], f: fn(T) -> U) -> U[] {
    let mut tasks: Task<U>[] = [];
    for x in xs {
        tasks.push(spawn f(x));
    }
    return await_all(tasks);
}
```

**Note:** v1 uses single-threaded cooperative concurrency. See §9.4-9.5 for async/await model.

**Grammar (reserved):**
```
ParallelMap    := "parallel" "map" Expr "with" ArgList "=>" Expr
ParallelReduce := "parallel" "reduce" Expr "with" Expr "," ArgList "=>" Expr
ArgList        := "(" (Expr ("," Expr)*)? ")" | "()"
```

Restriction: `=>` is valid only in these `parallel` constructs and within `compare` arms (§3.6). Any other use triggers `SynFatArrowOutsideParallel`.

**v1 behavior:** Lexer/parser accept syntax, but semantic analysis rejects with error:
- "parallel requires multi-threading (v2+)"

See §22.3 for detailed v2+ specification.

### 9.3. Backend Selection

`@backend("cpu"|"gpu")` may annotate functions or blocks for execution target hints:

```sg
@backend("gpu")
fn matrix_multiply(a: Matrix, b: Matrix) -> Matrix {
    // GPU-optimized implementation
}
```

**Semantics:**
- Compiler attempts to lower code for specified backend
- If backend is unsupported or unavailable:
  - By default: falls back to CPU implementation with warning
  - With `--strict-backend`: compile error
- Backend attribute does not change semantics, only execution target

**v1 support:**
- `@backend("cpu")` — always supported
- `@backend("gpu")` — may emit warning if GPU backend unavailable

Future backends may include: `"simd"`, `"opencl"`, `"cuda"`, `"metal"`.

### 9.4. Tasks and Spawn Semantics

**Execution model:** v1 uses single-threaded cooperative scheduling. All tasks run on one OS thread, yielding control at `.await()` points. No preemption — use `checkpoint()` for long CPU work.

#### Spawn Expression

```sg
spawn expr
```

* `expr` must be `Task<T>` (result of calling `async fn`)
* Returns `Task<T>` — a handle to the spawned task
* Ownership of captured values transfers to the task
* Only `own T` values may cross task boundaries (no `&T` or `&mut T`)

**Example:**
```sg
let data: own Data = load();
let task: Task<Result> = spawn process(data);  // data moved
// data is invalid here
```

#### Task<T> API

```sg
extern<Task<T>> {
    // Wait for completion, returns result or Cancelled
    fn await(self: own Task<T>) -> T;

    // Request cancellation (cooperative)
    fn cancel(self: &Task<T>) -> nothing;

    // Check if completed (non-blocking)
    fn is_done(self: &Task<T>) -> bool;

    // Check if cancellation was requested
    fn is_cancelled(self: &Task<T>) -> bool;
}
```

#### Cancellation

Surge uses **interrupt-at-await** cancellation:

1. `task.cancel()` sets a cancellation flag
2. At the next `.await()` point, the task checks the flag
3. If cancelled, `.await()` returns `Cancelled` immediately
4. The task can handle or propagate `Cancelled`

**Cancelled tag:**
```sg
tag Cancelled();
```

**Example:**
```sg
async fn worker() {
    compare some_io().await() {
        Success(data) => process(data);
        Cancelled() => {
            cleanup();
            return Cancelled();
        }
        err => return err;
    }
}
```

#### Checkpoint for CPU-bound Work

Long CPU-bound loops don't yield. Use `checkpoint()`:

```sg
async fn heavy_compute() -> int {
    let mut sum = 0;
    for i in 0..10_000_000 {
        sum = sum + expensive(i);

        if (i % 1000 == 0) {
            checkpoint().await();  // Yield + check cancellation
        }
    }
    return sum;
}
```

* `checkpoint()` returns `Task<nothing>`
* Yields to scheduler, allows other tasks to run
* Checks cancellation flag, returns `Cancelled` if set

**Note:** For complete concurrency specification, see `CONCURRENCY.md`.

### 9.5. Async/Await Model (Structured Concurrency)

Surge provides structured concurrency with async/await for cooperative multitasking.

#### Async Functions

```sg
async fn name(params) -> RetType {
    // can use .await() inside
}
```

* `async fn` returns `Task<RetType>` implicitly
* Caller must `.await()` or `spawn` the result
* Cannot be called from sync context without `spawn`

**Example:**
```sg
async fn fetch_user(id: int) -> Erring<User, Error> {
    let response = http_get("/users/" + id).await();
    return parse_user(response);
}

async fn main() {
    let user = fetch_user(42).await();  // Direct await
    let task = spawn fetch_user(42);     // Background task
}
```

#### Async Blocks

```sg
async {
    // statements
}
```

* Creates anonymous `Task<T>` where `T` is the block's result type
* All tasks spawned inside are **owned by the block**
* Block waits for all spawned tasks before completing (structured concurrency)

**Example:**
```sg
async fn process_all(urls: string[]) -> Data[] {
    async {
        let mut tasks: Task<Data>[] = [];

        for url in urls {
            tasks.push(spawn fetch(url));
        }

        let mut results: Data[] = [];
        for task in tasks {
            results.push(task.await());
        }

        return results;
    }
}
```

#### Structured Concurrency Rules

**Rule 1:** Tasks cannot outlive their spawning scope.

```sg
async {
    let t1 = spawn work1();
    let t2 = spawn work2();
}  // Implicit: waits for t1 and t2 here
```

**Rule 2:** Returning a Task from async block transfers ownership.

```sg
fn start_background() -> Task<int> {
    return spawn compute();  // Caller owns task
}
```

**Rule 3:** Tasks spawned in `async fn` are scoped to that function.

```sg
async fn example() {
    let t = spawn work();
    // Implicit await before return
}  // t is awaited here
```

#### @failfast Attribute

For fail-fast error handling without boilerplate:

```sg
@failfast
async {
    let t1 = spawn may_fail();
    let t2 = spawn also_runs();

    // If any .await() returns Error:
    // 1. All sibling tasks are cancelled
    // 2. Block returns that Error immediately

    let r1 = t1.await();
    let r2 = t2.await();

    return Success((r1, r2));
}
```

**Semantics:**
- On first `Error` from any `.await()`:
  - All other spawned tasks receive cancel signal
  - Block returns the error immediately
- Successful tasks continue normally

#### Single-threaded Execution

**Important:** v1 uses single-threaded cooperative scheduling:
- All tasks run on one OS thread
- Tasks yield at `.await()` points only
- No preemption — long CPU work blocks other tasks
- Use `checkpoint()` to yield in CPU-bound loops

**Rationale:** Simpler implementation, zero-cost abstraction, prepares for v2+ parallelism.

**See also:** `CONCURRENCY.md` for complete specification with examples.

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
tag IntNum(int); tag FloatNum(float);
type Number = IntNum(int) | FloatNum(float);

fn show(c: Color) { print(c); }
fn absn(x: Number) -> Number {
  return compare x {
    IntNum(v)   => IntNum(abs(v)),
    FloatNum(v) => FloatNum(abs(v)),
  };
}
```

```sg
// Struct and readonly
type Person = { age:int, name:string, @readonly weight:float }

fn birthday(mut p: Person) { p.age = p.age + 1; }
```

```sg
// Async/await with error handling
async fn fetch_with_retry(url: string, retries: int) -> Erring<Data, Error> {
    for i in 0..retries {
        let result = fetch_data(url).await();
        compare result {
            Success(data) => return Success(data);
            err => {
                if (i == retries - 1) { return err; }
                // Retry
            }
        }
    }
}
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
// Tagged union construction
tag IntNum(int); tag FloatNum(float);
type Number = IntNum(int) | FloatNum(float);

let n1: Number = IntNum(42);
let n2: Number = FloatNum(3.14);
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
// Concurrent data processing with structured concurrency
async fn process_urls(urls: string[]) -> Erring<Data[], Error> {
    async {
        let mut tasks: Task<Erring<Data, Error>>[] = [];

        for url in urls {
            tasks.push(spawn fetch_data(url));
        }

        let mut results: Data[] = [];
        for task in tasks {
            compare task.await() {
                Success(data) => results.push(data);
                err => return err;  // Early return on error
            }
        }

        return Success(results);
    }
}

// With @failfast for cleaner error handling
@failfast
async fn process_urls_failfast(urls: string[]) -> Erring<Data[], Error> {
    async {
        let mut tasks: Task<Erring<Data, Error>>[] = [];
        for url in urls {
            tasks.push(spawn fetch_data(url));
        }

        let mut results: Data[] = [];
        for task in tasks {
            results.push(task.await());  // Auto-cancels on error
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

Union aliases (§2.8) use tagged composition only; untagged unions are not supported:

```sg
tag IntNum(int); tag FloatNum(float);
type Number = IntNum(int) | FloatNum(float);

extern<Number> {
  fn __add(a: Number, b: Number) -> Number {
    return compare (a, b) {
      (IntNum(x),   IntNum(y))   => IntNum(x + y),
      (IntNum(x),   FloatNum(y)) => FloatNum(x:float + y),
      (FloatNum(x), IntNum(y))   => FloatNum(x + y:float),
      (FloatNum(x), FloatNum(y)) => FloatNum(x + y),
    };
  }
}
```

Tagged unions provide clearer APIs and enable exhaustiveness checking; see §2.7 for constructors and §3.6 for matching rules.

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
UnionMember:= "nothing" | Ident "(" ParamTypes? ")"
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

---

## 22. Future Features (v2+)

The following features are reserved for future versions and are not supported in v1.

### 22.1. Signals (Reactive Bindings)

**Status:** Planned for v2+

**Syntax (reserved):**
```sg
signal name := expr;
```

**Planned semantics:**
- Reactive values that auto-update when dependencies change
- Requires `@pure` expressions (no side effects)
- Topological dependency resolution
- Update propagation in deterministic order

**Current v1 behavior:**
- Keyword is lexed but semantic analysis rejects with error
- "signal is not supported in v1, reserved for future use"

**Rationale:** Signals provide reactive programming capabilities similar to observables or reactive streams. The v2+ implementation will track dependencies automatically and ensure updates propagate in topological order without manual management.

**Example (planned):**
```sg
signal price := fetch_price();
signal tax := price * 0.2;
signal total := price + tax;

// When price updates, tax and total automatically recompute
```

### 22.2. Macros (Compile-time Metaprogramming)

**Status:** Planned for v2+

**Syntax (reserved):**
```sg
macro name(params...) { body }
```

**Planned features:**
- Compile-time code generation
- Parameter types: `expr`, `ident`, `type`, `block`, `meta`
- Built-in functions: `stringify()`, `type_name_of<T>()`, `size_of<T>()`, `align_of<T>()`
- AST manipulation for metaprogramming

**Current v1 behavior:**
- Keyword is lexed but semantic analysis rejects with error
- "macro is planned for v2+"

**Rationale:** Macros enable zero-cost abstractions and domain-specific languages. The design follows hygiene principles to avoid accidental variable capture and ensures macro expansion is deterministic.

**Example (planned):**
```sg
macro assert_eq(left: expr, right: expr) {
    if (!(left == right)) {
        panic("Assertion failed: " + stringify(left) + " != " + stringify(right));
    }
}

// Usage
fn test_something() {
    assert_eq(2 + 2, 4);  // Expands at compile time
}
```

### 22.3. Parallel Map/Reduce (Data Parallelism)

**Status:** Planned for v2+ (requires multi-threading)

**Syntax (reserved):**
```sg
parallel map collection with (args) => func
parallel reduce collection with init, (args) => func
```

**Planned semantics:**
- True parallelism on multiple OS threads
- Requires `@pure` functions (enforced statically)
- Work-stealing scheduler for automatic load balancing
- NUMA-aware allocation for performance
- Automatic chunking based on collection size

**Current v1 behavior:**
- Keywords are lexed but semantic analysis rejects with error
- "parallel requires multi-threading (v2+)"

**Rationale:** Data parallelism is essential for high-performance computing and AI backends. The v2+ implementation will use true OS-level threads, unlike v1's single-threaded cooperative concurrency.

**v1 alternative:** Use `spawn` with channels for concurrent (not parallel) processing:
```sg
async fn concurrent_map<T, U>(xs: T[], f: fn(T) -> U) -> U[] {
    let mut tasks: Task<U>[] = [];
    for x in xs {
        tasks.push(spawn f(x));
    }

    let mut results: U[] = [];
    for task in tasks {
        results.push(task.await());
    }
    return results;
}
```

**Note:** v1 uses single-threaded cooperative concurrency. See §9 Concurrency Primitives for async/await, spawn, and channels.

**Example (planned for v2+):**
```sg
let nums: int[] = [1, 2, 3, 4, 5];

// Parallel map: process elements on multiple threads
let squared = parallel map nums with (x) => x * x;

// Parallel reduce: aggregate with associative operation
let sum = parallel reduce nums with 0, (acc, x) => acc + x;
```

### 22.4. Comparison: v1 vs v2+

| Feature | v1 (current) | v2+ (planned) |
|---------|--------------|---------------|
| Async/await | ✅ Single-threaded | ✅ Single or multi-threaded |
| Spawn | ✅ Cooperative tasks | ✅ OS threads |
| Channels | ✅ Message passing | ✅ Thread-safe channels |
| Signals | ❌ Not supported | ✅ Reactive computations |
| Parallel map/reduce | ❌ Not supported | ✅ Data parallelism |
| Macros | ❌ Not supported | ✅ Compile-time codegen |
| Work-stealing | ❌ N/A | ✅ Automatic load balancing |
| Lock contracts | ⚠️ Parsed, not enforced | ✅ Static analysis |

### 22.5. Migration Path

When upgrading from v1 to v2+:

**Signals:**
- Convert manual state updates to `signal` declarations
- Ensure all signal expressions are `@pure`
- Remove explicit dependency management code

**Macros:**
- Replace code-generation scripts with compile-time macros
- Migrate build-time templating to macro system
- Use hygiene features to avoid naming conflicts

**Parallel:**
- Replace `spawn` loops with `parallel map` where appropriate
- Ensure functions are `@pure` and associative (for reduce)
- Test with thread sanitizers to catch data races

**Lock contracts:**
- Add `@guarded_by`, `@requires_lock` annotations
- Verify static analysis catches lock violations
- Update code to satisfy lock contracts

**Backward compatibility:**
- v1 code continues to work in v2+ (no breaking changes)
- New features are opt-in via explicit syntax
- Single-threaded mode remains available for predictability
