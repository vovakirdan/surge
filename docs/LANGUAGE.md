# Surge Language Specification (Draft 8)
[English](LANGUAGE.md) | [Russian](LANGUAGE.ru.md)

> **Status:** Draft for review
> **Scope:** Full language surface for tokenizer → parser → semantics. VM/runtime details are out of scope here and live in docs/IR.md and docs/RUNTIME.md.
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

### Implementation Snapshot

- Keywords match `internal/token/kind.go`: `fn, let, const, mut, own, if, else, while, for, in, break, continue, return, import, as, type, contract, tag, enum, extern, pub, async, compare, select, race, finally, channel, spawn, true, false, signal, parallel, map, reduce, with, macro, pragma, to, heir, is, field, nothing`. `signal`/`parallel` are reserved (`FutSignalNotSupported` / `FutParallelNotSupported`) and `macro` is rejected by the parser (`FutMacroNotSupported`).
- The type checker resolves `int`, `uint`, `float`, fixed-width numerics (`int8`, `uint64`, `float32`, ...), `bool`, `string`, `nothing`, `unit`, ownership/ref forms (`own T`, `&T`, `&mut T`), slices `T[]`, and sized arrays `T[N]` with constant `N`. Raw pointers (`*T`) are allowed only in `extern` and `@intrinsic` declarations.
- Tuple and function types are supported in sema and runtime lowering.
- Tags and tagged unions are implemented. `Option` and `Erring` are standard aliases built on `Some`/`Success` tags plus `nothing`/error types; `ErrorLike` and `Error` live in the prelude; `compare` exhaustiveness is enforced for tagged unions.
- Enums are implemented as nominal integer-backed types with explicit variants.
- Contracts (trait-like structural interfaces) are parsed and checked in sema: declaration syntax is enforced, bounds on functions/types are resolved, short/long forms are validated by arity, and structural satisfaction (fields/methods) is verified on calls, type instantiations, assignments, and returns.
- Directives are parsed and namespace-validated when enabled (`--directives=collect|gen|run`); `run` executes a stub runner (SKIPPED). No directive codegen/type-checking yet.
- Diagnostics use the `Lex*`/`Syn*`/`Sema*` numeric codes from `internal/diag/codes.go` instead of the earlier `E_*` placeholders. Legacy `E_*` labels that remain in examples below are descriptive placeholders; see §21 for the codes the compiler actually emits today.

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
pub, fn, let, const, mut, own, if, else, while, for, in, break, continue,
import, as, type, contract, tag, enum, extern, return, signal, compare, select, race, spawn, channel,
parallel, map, reduce, with, to, heir, is, async, macro, pragma, field,
true, false, nothing
```

Attribute names (e.g., `pure`, `override`, `packed`) are identifiers that appear after `@` rather than standalone keywords.

### 1.5. Literals

* Integer: `0`, `123`, `0xFF`, `0b1010`, underscores allowed for readability: `1_000`.
* Float: `1.0`, `0.5`, `1e-9`, `2.5e+10`.
* String: `"..."` (UTF-8), escape sequences `\n \t \" \\` and `\u{hex}`.
* Bool: `true`, `false`.
* Absence value: `nothing` is the single "no value" literal used for void/null/absent semantics.

**F-strings (implemented):** `f"Hello {name}"` desugars to a call to
`format("Hello {}", fmt_arg(name))`. `{{` and `}}` escape literal braces.

---

## 2. Types

Types are written postfixed: `name: Type`.

### 2.1. Primitive Families

The type checker currently recognises built-in `int`, `uint`, `float`, `bool`, `string`, `nothing`, and `unit`, plus fixed-width numerics (`int8..int64`, `uint8..uint64`, `float16..float64`). Fixed-width types are first-class in sema/runtime and participate in layout and numeric rules.

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

Type binding: postfix `[]`/`[N]` binds tighter than prefix `&/own/*`.
So `&T[]` means `&(T[])` (reference to array), while `(&T)[]` means an array of references.
You can also be explicit: `Array<&T>` and `&Array<T>` are both allowed.

* Indexing calls magic methods: `__index(self, index)` and `__index_set(self, index, value)`. For `Array<T>`, the intrinsic signatures are `__index(self: &Array<T>, index: int) -> &T` and `__index_set(self: &mut Array<T>, index: int, value: T) -> nothing`.
* Iterable if `extern<T[]> { __range() -> Range<T> }` is provided (stdlib provides this for arrays).

### 2.3. Ownership & References

* `own T` – owning value, moved by default.
* `&T` – shared immutable borrow (read-only view).
* `&mut T` – exclusive mutable borrow.
* `*T` – raw pointer (backend-only; unmanaged, no ownership or lifetime guarantees).

`own expr` is an explicit move: it produces a value of type `own T` from an expression of type `T`.
For non-`Copy` types, `own` is required when assigning or passing into `own T` slots.
For `Copy` types, `T` and `own T` are compatible without an explicit `own`.

**Raw pointers (restricted):** `*T` exists syntactically but is reserved for backend/FFI use. In v1/v2, raw pointers are **not permitted in user code**; only `extern<...>` signatures and `@intrinsic` declarations may mention them. A future version may enable `*T` behind `unsafe {}` blocks (out of scope here).

Borrowing rules:

* While a `&mut T` exists, no other `&` or `&mut` borrows to the same value may exist.
* While any `&T` borrows exist, mutation of the underlying value is forbidden (the value is frozen).
* Lifetimes are lexical; the compiler emits diagnostics for aliasing violations. When you need to end a borrow early, use `@drop binding;` — it marks the specific expression statement as a drop point and releases the corresponding borrow before the end of the enclosing block.

**Moves & Copies:**

* Copy types include `bool`, `int`/`uint`/`float` (all widths), `unit`, `nothing`, raw pointers (`*T`), and shared references (`&T`). `string`, arrays, tuples, structs, unions, and `&mut T` are not Copy unless marked `@copy`.
* `T` and `own T` are distinct in the type system; implicit compatibility exists only for `Copy` types.
* Assignment `x = y;` moves if `y` is `own` and `T` not `Copy`. Borrowing uses `&`/`&mut` operators: `let r: &T = &x;`, `let m: &mut T = &mut x;`.

**Function parameters:**

* `fn f(x: own T)`: takes ownership.
* `fn f(x: &T)`: shared-borrows; caller retains ownership; lifetime is lexical.
* `fn f(x: &mut T)`: exclusive mutable borrow.
* `fn f(x: *T)`: raw pointer parameter (backend-only; allowed only in `extern`/`@intrinsic` declarations).

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
* `extern<T>` blocks: `extern<T> { fn len(self: &T) -> uint; }`

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
let e: Box<int> = { value = 42 };  // ok: type from annotation
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
  * Struct literals may specify the type inline: `let p = Person { age = 25, name = "Alex" };`. Field assignment uses `=`; legacy `field: value` is still parsed but discouraged. The parser only treats `TypeName { ... }` as a typed literal when `TypeName` follows the CamelCase convention so that `while ready { ... }` still parses as a control-flow block.
  * Struct literals without an inline type require an unambiguous expected struct type. For unions like `Erring<T, Error>`, write `Error { ... }` or add a binding annotation (e.g., `let e: Error = { message = "bad", code = 1:uint };`).
  * When the type is known (either via `TypeName { ... }` or an explicit annotation on the binding), the short `{expr1, expr2}` form is allowed; expressions are matched to fields in declaration order. Wrap identifier expressions in parentheses (`{(ageVar), computeName()}`) when using positional literals so they are not mistaken for field names.
* **Enums:** `enum Name = { ... }` for integer enums (auto or explicit) and `enum Name: string = { ... }` for string enums.

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
  * Enum variants are constants of the base type; `Enum::Variant` yields a value of that base type.

#### Recursive data structures

Recursive value types are not supported in v1/v2, even through wrappers like `Option<own T>`. Value layouts must be finite, and `own T` does not introduce indirection.

Use explicit handles (indices) and an external storage container instead. This keeps ownership simple and makes lists/trees/graphs safe and predictable.

```sg
type NodeId = uint;

type Node = {
    next: NodeId?,   // Option<NodeId>
    data: int,
}

type Nodes = Node[]; // storage

fn push(nodes: &mut Node[], data: int, next: NodeId?) -> NodeId {
    let id: NodeId = len(nodes);
    nodes.push(Node { next = next, data = data });
    return id;
}
```

```sg
fn walk(nodes: &Node[], start: NodeId?) -> nothing {
    let mut id: NodeId? = start;
    while true {
        compare id {
            Some(i) => {
                let n: Node = nodes[(i to int)];
                print(n.data to string);
                id = n.next;
            };
            nothing => { return nothing; };
        };
    }
}
```

Future versions may add explicit heap indirection or `unsafe`-gated pointers, but those are out of scope for v1/v2.

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
  * raw pointers (`*T`, backend-only) → `nothing`; references `&T`/`&mut T` have no default (compile-time error on `default`);
  * arrays/slices → element-wise `default<Elem>()`, empty slice for dynamic;
  * structs → recursively default every field/base; aliases unwrap to their target;
  * unions → only if a `nothing` variant is present (e.g. `Option`).
  * **VM:** `default<T>()` is supported in the v1 runtime.
- `@hidden` on fields is parsed but no access restrictions are enforced yet; `@readonly` fields stay immutable after construction.
- Assigning from a child to its base is forbidden (types remain nominal).
- Field name clashes trigger `SynTypeFieldConflict` during parsing.
- Methods defined in `extern<Base>` are visible on `Child`. Override behaviour lives in `extern<Child>` with `@override` marking intentional replacements.
- Struct extensions resolve inherited fields (including extern fields) during semantic analysis, and the resulting layout includes them.

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
- `Some` and `Success` are provided by the core prelude (from `core/option.sg` and `core/result.sg`) and are always in scope without an explicit `tag` item. `Error` is a core struct type, not a tag.

Tags participate in alias unions as variants. They may declare generic parameters ahead of the payload list: `tag Some<T>(T);` introduces a tag family parameterised by `T`.

### 2.8. Tagged unions (aliases)

`type` can describe a sum type **only with tagged members**. Plain untagged unions like `type A = T1 | T2` are **not supported**.

```sg
tag Left(L); tag Right(R);
type Either<L, R> = Left(L) | Right(R)
```

Rules:
- Every member must be a tag constructor or `nothing`.
- Exhaustiveness checking for `compare` arms is enforced for tagged unions.
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
  if (len(xs) == 0) { return nothing; }
  return Some(xs[0]);
}

fn parse(s: string) -> Erring<int, Error> { // also `int!` sugar is available
  if (s == "42") { return Success(42); }
  let e: Error = { message = "bad", code = 1:uint };
  return e; // Error value returned directly
}

compare head([1, 2, 3]) {
  Some(v) => print(v to string);
  nothing => print("empty");
}

compare parse("x") {
  Success(v) => print("ok " + (v to string));
  err        => print("err " + err.message);
}
```

Rules:

- Construction is explicit in expressions: use `Some(...)` or `Success(...)`. In function returns, a bare `T` is accepted as `Some(T)`/`Success(T)` and `nothing` is accepted for `Option<T>`.
- `T?` is sugar for `Option<T>`; `T!` is sugar for `Erring<T, Error>` (type sugar only; no `expr?` propagation operator).
- `nothing` remains the shared absence literal for both Option and other contexts (§2.6). Exhaustiveness checking for tagged unions is enforced.
- `panic(msg)` materialises `Error { message = msg, code = 1:uint }` and calls intrinsic `exit(Error)`.

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
Tuple destructuring in `let` bindings is not supported yet. Bind the tuple and access fields via `.0`, `.1`, etc.

**Limitations:**
- Tuple patterns are supported in `compare` arms, but not in `let` bindings.
- Use tuples primarily for multiple return values and temporary groupings.

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

#### Borrow Rules (Aliasing XOR Mutation)

The borrow checker enforces a fundamental invariant: **at any point in time, a value may have either multiple shared borrows OR exactly one mutable borrow, but never both**. This principle, sometimes called "aliasing XOR mutation," prevents data races and iterator invalidation at compile time.

**The Two Laws:**

1. **Shared borrows (`&T`) freeze the value.** While any `&T` reference exists, the underlying value cannot be mutated or moved. Multiple `&T` borrows to the same value may coexist—they only require read access.

2. **Mutable borrows (`&mut T`) are exclusive.** While a `&mut T` reference exists, no other borrow (shared or mutable) may exist to the same value, and the original binding cannot be accessed directly until the borrow ends.

**Conflict Examples:**

```sg
let mut x: int = 10;
let r1: &int = &x;       // shared borrow starts
let r2: &int = &x;       // OK: multiple shared borrows allowed
x = 20;                  // ERROR: cannot mutate while shared-borrowed
@drop r1;
@drop r2;                // borrows end
x = 20;                  // OK: no active borrows

let mut y: int = 5;
let m: &mut int = &mut y;  // exclusive borrow starts
let r: &int = &y;          // ERROR: cannot take shared borrow while mutable borrow active
@drop m;                   // borrow ends
let r: &int = &y;          // OK
```

**Places and Projections:**

The borrow checker tracks not just bindings, but *places*—addressable locations that may be projections of a base binding (fields, array indices, dereferences). Borrowing `&x.field` creates a borrow on a sub-place of `x`. Conflicting access to overlapping places is detected:

```sg
type Point = { x: int, y: int };
let mut p: Point = Point { x = 1, y = 2 };
let rx: &int = &p.x;      // borrow of p.x
let ry: &int = &p.y;      // OK: disjoint field
p.x = 10;                 // ERROR: p.x is borrowed
p.y = 20;                 // ERROR: p is partially frozen (conservative)
```

**Lexical Lifetimes and Early Drop:**

Borrow lifetimes in Surge are currently lexical—they extend to the end of the enclosing scope. To release a borrow early, use the `@drop` directive:

```sg
let mut data: string = "hello";
{
    let r: &string = &data;
    // use r...
    @drop r;               // explicit early release
    data = "world";        // OK: borrow ended
}
```

Future versions may introduce non-lexical lifetimes (NLL) for more precise tracking.

#### Implicit Borrows and Moves

Surge automatically inserts borrow and move operations in specific contexts to reduce syntactic noise while preserving ownership clarity.

**Operator Calls (Magic Methods):**

Binary and unary operators are desugared to magic method calls. These methods typically expect references, and Surge implicitly borrows operands when needed:

```sg
let a: string = "hello";
let b: string = " world";
let c: string = a + b;    // desugars to __add(&a, &b) -> own string
```

The `+` operator on strings calls `extern<string> { fn __add(self: &string, other: &string) -> string; }`. The compiler:
1. Sees that `__add` expects `&string` for both parameters
2. Implicitly borrows `a` and `b` (equivalent to `__add(&a, &b)`)
3. Returns an owned `string` result

Since the borrows are temporary (used only for the duration of the call), they are released immediately after the operator completes. This allows chained operations:

```sg
let x: string = a + b + c;  // each intermediate result is owned, operands are borrowed
```

**Method Calls:**

The same implicit borrow applies to method receivers and arguments:

```sg
fn len(self: &string) -> uint;

let s: string = "test";
let n: uint = len(s);      // calls HasLength.__len()
```

**Temporary Value Promotion (Rvalue Materialization):**

When a function expects `&T` (shared reference) but receives an rvalue (temporary value), Surge can "materialize" the temporary and borrow it. This is currently supported for **string literals and temporary strings** with **immutable borrows only**:

```sg
fn process(s: &string) -> int;

process("literal");           // OK: string literal materialized, borrowed as &string
process(get_string());        // OK: temporary string materialized, borrowed as &string
```

**Restrictions on Mutable Borrows of Temporaries:**

Mutable borrows (`&mut T`) require an addressable location. Temporaries are not addressable, so attempting to pass an rvalue where `&mut T` is expected is an error:

```sg
fn modify(s: &mut string);

modify("literal");            // ERROR: cannot take mutable reference to temporary
let mut x: string = "hello";
modify(x);                    // OK: x is implicitly borrowed as &mut x
```

**Move vs Borrow by Parameter Type:**

The compiler infers move or borrow based on parameter types:

| Parameter Type | Argument Type | Action |
|---------------|---------------|--------|
| `own T` | `T` / `own T` | Move (or copy if `Copy` type) |
| `&T` | `T` | Implicit shared borrow |
| `&T` | `&T` | Pass through (no extra borrow) |
| `&mut T` | `mut T` | Implicit exclusive borrow |
| `&mut T` | `&mut T` | Pass through (no extra borrow) |

**Ownership for Operators Summary:**

- Arithmetic on primitives (`int`, `float`): operands are `Copy`, no ownership transfer
- String concatenation: operands borrowed (`&string`), result owned (`own string`)
- Comparison operators: operands borrowed, result is `bool` (Copy)
- Assignment `=`: right-hand side is moved (or copied for `Copy` types)
- Compound assignment `+=`, etc.: left-hand side mutably borrowed, right-hand side borrowed or copied

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
- Arrays → empty instance for dynamic arrays; element-wise defaults for fixed-size arrays.
- Structs → allowed only when every field type is defaultable (field defaults are optional; non-defaultable fields make `let x: Struct;` an error).
- Tagged unions → defaultable only if the union includes `nothing` (e.g., `Option<T>`). Others require an explicit initializer.

Top-level `let` initialization and cycles:

* Top-level `let` items are executed at module initialization time in textual order within a module.
* Cyclic initialization among top-level `let`s is a compile-time error.

### 3.2. Control Flow

* If: `if (cond) { ... } else if (cond) { ... } else { ... }`
* While: `while (cond) { ... }`
* For counter: `for (init; cond; step) { ... }` where each part may be empty.
* For-in iteration: `for item:T in xs:T[] { ... }` requires `__range()`. **VM:** array iteration uses `__range()` + `Range.next()` and is supported in v1.
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

**v1 behavior:** Parsed, but semantic analysis rejects with `FutSignalNotSupported` (`"signal" is not supported in v1`).

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
- Tagged constructors – `Tag(p)` such as `Some(x)` or `Success(v)`; payload patterns recurse.
- `nothing` – matches the absence literal of type `nothing`.
- Conditional patterns – `x if x is int` where `x` is bound and condition is checked.

Examples:

```sg
compare v {
  Some(x) => print(x to string);
  nothing => print("empty");
}

compare r {
  Success(v) => print("ok " + (v to string));
  err        => print("err " + err.message);
}
```

Notes:

- Arms are tried top-to-bottom; the first match wins.
- Arm bodies may be expressions or `{ ... }` block expressions; the block result is the arm value.
- `=>` separates pattern from result expression and is only valid within `compare` arms, `select`/`race` arms, and parallel constructs.
- Exhaustiveness for tagged unions is enforced: arms must cover all variants or include `finally`. Redundant `finally` emits `SemaRedundantFinally`. Untagged unions are not supported.
- If both a tag constructor and a function named `Ident` are in scope, using `Ident(...)` emits `SemaAmbiguousCtorOrFn`.

---

## 4. Functions & Methods

### 4.1. Function Declarations

```
fn name(params) -> RetType? { body }
params := (Param ("," Param)*)?
Param  := ("...")? name:Type
```

* Example: `fn add(a:int, b:int) -> int { return a + b; }`
* Variadics: `fn log(...args: string) { ... }`
* Nested function declarations inside blocks are not supported yet (`FutNestedFnNotSupported`). Declare functions at module scope or inside `extern<T>` blocks.

Variadics:
* `...args: T` is allowed only as the last parameter.
* It desugars to `args: T[]` (owning). For borrowed arrays, write `args: &T[]` explicitly.
* `...args: &T` desugars to an array of references: `args: (&T)[]`.
* At call sites, trailing arguments are packed into a `T[]`.
* Overload resolution treats variadic candidates as matching variable arity with an added cost versus exact-arity matches.

**Return type semantics:**

* Functions without `-> RetType` return `nothing` and may omit explicit `return` statements.
  * `fn main() { ... }` - valid, implicit `return nothing;`
  * `fn main() { return nothing; }` - also valid, explicitly returns the absence value
* Functions with `-> RetType` must return a value of that type.
  * `fn answer() -> int { return 42; }`

### 4.2. Attributes

Attributes are a **closed set** provided by the language. Unknown attributes are
errors. For the full list, targets, and current status see `docs/ATTRIBUTES.md`.

Current parsing rules:
- Attributes must appear immediately before the declaration they apply to.
- Only statement attribute: `@drop expr;` (no arguments).
- Only async-block attribute: `@failfast`.

Enforced highlights (v1):
- `@overload` and `@override` control redeclarations and require matching signatures.
- `@intrinsic` is declaration-only; `@entrypoint` validates mode and signature.
- Layout: `@packed`, `@align`.
- Concurrency: `@guarded_by`, `@requires_lock`, `@acquires_lock`, `@releases_lock`,
  `@waits_on`, `@nonblocking`, `@send`, `@nosend`.
- Field access: `@readonly`, `@atomic`.
- Visibility and warnings: `@hidden`, `@deprecated`.

Parsed-only (no semantic effect yet):
`@pure`, `@raii`, `@arena`, `@shared`, `@weak`, `@backend`.

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
* **Sealed types:** `extern<T>` is forbidden when `T` is marked `@sealed`.

Any other item-level elements are **prohibited**: `let`, `type`, alias declarations, literal definitions, `import` statements, nested `extern` blocks, etc. These produce syntax error `SynIllegalItemInExtern`.

**Examples:**

```sg
extern<Person> {
  // ✅ Allowed
  @readonly field id: int;

  fn age(self: &Person) -> int { return self.age; }
  
  pub fn name(self: &Person) -> string { return self.name; }
  
  @pure
  async fn to_json(self: &Person) -> string { /* ... */ }
  
  // ❌ Errors: SynIllegalItemInExtern
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

**Override visibility rule:** When overriding an already-public method implementation for a type, the new definition must not reduce visibility. Attempting to override a public method with a private one emits `SynVisibilityReduction`.

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
  
  // ❌ Error: SynVisibilityReduction
  @override
  fn __to(self: &Person, target: string) -> string { /* v3 */ }
}
```

### 4.5. Macros — **Future Feature (v2+)**

> **Status:** Not supported in v1. The parser rejects `macro` items (`FutMacroNotSupported`).

Macros are reserved for future compile-time metaprogramming. The v1 compiler
does not parse or expand macro items. For tooling metadata today, use
directives (§13) instead.

See §22.2 for the current (non-normative) roadmap notes.

---

## 5. Modules & Imports

### 5.1. Files & Modules

Each file is a module. Folder hierarchy maps to module paths.

### 5.2. Importing

* Module or submodule: `import math/trig;`
* Module alias: `import math/trig as trig;`
* Specific item: `import math/trig::sin;`
* Aliasing: `import math/trig::sin as sine;`
* Wildcard: `import math/trig::*;`
* Group: `import math/trig::{sin, cos as c};`
* Relative paths are allowed: `import ./local/module;`, `import ../shared/math;`

**Name resolution order:** local scope > explicit imports > prelude/std.

---

## 6. Operators & Magic Methods

### 6.1. Operator Table

* Arithmetic: `+ - * / %` → `__add __sub __mul __div __mod`
* Comparison: `< <= == != >= >` → `__lt __le __eq __ne __ge __gt` (must return `bool`)
* Type checking: `is` – checks type identity (see §6.2)
* Inheritance checking: `heir` – checks inheritance between a value's type and a target type (see §6.3)
* Logical: `&& || !` – short-circuiting; operate only on `bool`.
* Indexing: `[]` → `__index __index_set`
* Unary: `+x -x` → `__pos __neg`
* Abs: `abs(x)` → `__abs`
* Casting: `expr to Type` → `__to(self, Type)` magic method; `print` expects a single `string` argument (use an explicit cast for non-strings).
  * `expr: Type` is shorthand for `expr to Type`. It's especially handy for literal annotations such as `1:int8`.
* Range: `for in` → `__range() -> Range<T>` where `Range<T>` yields `T` via `next()`.
* Compound assignment: `+= -= *= /= %= &= |= ^= <<= >>=` → corresponding operation + assign.
* Ternary: `condition ? true_expr : false_expr` → conditional expression.
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

### 6.1.1. Numeric Arithmetic Rules

**Rule A: No implicit promotions.** Numeric operators require operands of the **same type**. Mixed arithmetic is a semantic error (int vs intN, int vs float, floatN vs intN, int8 vs int16, etc.).

Invalid:
```sg
let a: int = 1;
let b: int8 = 2;
let _ = a + b; // error

let _ = 1 + 0.1; // error (int + float)
let _ = (1:int8) + (2:int16); // error (different fixed widths)
```

Explicit conversions are required:
```sg
let c: int = a + (b to int);
let d: float = (a to float) + 0.1;
```

**Rule B: Integer division.** `int / int -> int` truncates toward zero:
* `2 / 3 == 0`
* `-2 / 3 == 0`
* `2 / -3 == 0`
* `-2 / -3 == 0`

**Rule C: Fixed-size checked arithmetic.** For `intN`/`uintN`, arithmetic is checked. If the exact result does not fit the destination type, the runtime **panics** (same philosophy as `to`). Division by zero also panics. Safe wrappers (checked/saturating) will be provided later in a math package.

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

The `is` operator performs runtime type checking and returns a `bool`. It checks the essential type identity of a **value**, ignoring ownership modifiers:

**Rules:**
* `42 is int` → `true`
* `nothing is nothing` → `true`
* `Option<int> is int` → `false` (it's `Option<int>`, not `int`)
* `own T is T` → `true` (ownership doesn't change type essence)
* Mutability of a binding does not affect `is`: `let mut x:T; (x is T) == true`.
* `&T is T` → `false` (reference is different type)
* `*T is T` → `false` (raw pointer is a distinct type; backend-only)
* Left operand must be a **value**; `T is T` is invalid because types are not values.
* Right operand must be a **type** (or a tag name when checking a tagged union).

**Examples:**
```sg
let x: int = 42;
let y: Option<int> = Some(42);
let z: &int = &x;

print((x is int) to string);         // true
print((y is int) to string);         // false
print((y is Option<int>) to string); // true
print((z is int) to string);         // false
print((z is &int) to string);        // true
```

### 6.3. Inheritance Checking Operator (`heir`)

The `heir` operator checks inheritance relationships between a **value's type** and a target type, returning a `bool`. The check is performed both at compile time (for constant expressions) and at runtime (for dynamic checks).

**Syntax:**
```
Expr heir Type
```

**Rules:**
* `value heir Base` → `true` if the type of `value` inherits from `Base` through any inheritance mechanism (struct extension, aliases, unions, etc.)
* `x heir T` → `true` when `x` has type `T` (reflexive)
* Inheritance is transitive: if a value has type `A`, `A` inherits `B`, and `B` inherits `C`, then the value is `heir C`
* Left operand must be a **value**; using a type name on the left emits `type X cannot be used as a value`
* Right operand must be a **type**; using a value on the right emits `SemaExpectTypeOperand`
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

let employee: Employee = { name = "A", age = 0, id = 1, department = "X" };
let manager: Manager = { name = "B", age = 0, id = 2, department = "Y", team_size = 5 };

// Direct inheritance
let is_employee_base: bool = employee heir BasePerson;  // true

// Transitive inheritance
let is_manager_base: bool = manager heir BasePerson;     // true
let is_manager_employee: bool = manager heir Employee;   // true

// Self-inheritance
let is_self: bool = employee heir Employee;              // true

// Non-inheritance
let base: BasePerson = { name = "C", age = 0 };
let is_not: bool = base heir Employee;                   // false

// Usage in conditions
if (employee heir BasePerson) {
    print("Employee inherits from BasePerson");
}

// Works for any type, not just struct extension
type MyInt = int;
let n: MyInt = 0;
let is_int: bool = n heir int;  // true (alias inherits from base)

type Number = int;
let num: Number = 0;
let is_number: bool = num heir Number;  // true (reflexive)
```

**Difference from `is` operator:**
* `is` checks the **runtime type** of a **value**: `x is int` checks if the value `x` has type `int`
* `heir` checks the **inheritance relationship** between a **value's type** and a target type: `employee heir BasePerson` checks if the type of `employee` inherits from `BasePerson`
* Both operators take a value on the left; `is` checks type identity while `heir` checks inheritance

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
* **Reference and pointer types:** `&T` and `&mut T` cannot be cast via `to` (compile error). Raw pointers `*T` are backend-only and have no `to` casts in user code.
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
4. Multiple matches yield `SemaAmbiguousConversion`; no match yields `SemaTypeMismatch` for explicit casts (or `SemaNoConversion` at implicit-conversion sites).

**Restrictions:**
* Direct calls to `__to` are forbidden; only `expr to Type` or implicit conversion may invoke it.
* Reference types cannot define or consume casts; raw pointers `*T` are backend-only and not part of the cast system.
* Explicit casts (`expr to Type` or `expr: Type`) never participate in function overload resolution; insert them explicitly when needed. However, implicit conversions (§6.6.1) do participate with lower priority.

#### 6.6.1. Implicit Conversions

The compiler automatically applies `__to` conversions in specific coercion sites when the target type is known and exactly one applicable `__to` method exists. This eliminates boilerplate explicit casts while preserving type safety. For function arguments, implicit `__to` is opt-in via `@allow_to` on the callee or the specific parameter.

**Coercion sites (automatic `__to` application):**

1. **Variable bindings:** `let x: T = expr` where `expr` has type `U` and `__to(U, T)` exists
2. **Function arguments:** `foo(arg)` where `arg` has type `U`, parameter expects type `T`, the callee or parameter allows `@allow_to`, and `__to(U, T)` exists
3. **Return statements:** `return expr` where `expr` has type `U`, function returns `T`, and `__to(U, T)` exists
4. **Struct field initialization:** `Struct { field = expr }` where `expr` has type `U`, field expects type `T`, and `__to(U, T)` exists
5. **Array elements:** `[expr1, expr2]` where elements have type `U`, array expects type `T[]`, and `__to(U, T)` exists

**Resolution rules:**

* Only applied when the target type is explicitly known from context (type annotation, function signature, etc.)
* Exactly one `__to(source, target)` method must exist; ambiguity or absence reports an error
* No conversion chaining: `T -> U -> V` is never attempted; only single-step `T -> U` conversions
* Conversions are never applied in binary/unary operator resolution
* In function overload resolution (when `@allow_to` is enabled), implicit conversion has lower priority (cost = 2) than:
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

// Implicit conversion in function argument (opt-in)
@allow_to
fn display_feet(f: Feet) { print(f to string); }
display_feet(distance_m);  // Calls __to(Meters, Feet) implicitly

// Implicit conversion in return
fn convert_to_feet(m: Meters) -> Feet {
  return m;  // Calls __to(Meters, Feet) implicitly
}

// Implicit conversion in struct field
type Measurement = { value_ft: Feet };
let m = Measurement { value_ft = distance_m };  // Calls __to implicitly

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
* In a fixed-size context (annotation, parameter type, etc.), a literal is accepted only if the value fits; otherwise it is a compile-time error.

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

ABI: `Range<T>` is opaque runtime state; see `docs/ABI_LAYOUT.md` (Range ABI).

### 7.3. Strings

* `string` stores UTF-8 bytes; runtime constructors validate UTF-8 and normalize to NFC (string literals are assumed valid UTF-8).
* `len(s)` returns the number of Unicode code points as `uint`.
* `s[i]` returns the code point at index `i` as `uint32` (negative indices count from the end).
* `s[[a..b]]` slices by code point indices and returns `string`. Omitted bounds default to `0`/`len`.
  Inclusive `..=` adds one to the end bound, indices are clamped, and `start > end` yields `""`.
* `bytes()` returns a `BytesView` over UTF-8 bytes. `len(view)` returns byte length; `view[i]` returns `uint8`.
* Implementation detail: strings may be stored as a rope internally. Concatenation and slicing can return views, and byte access materializes a flat UTF-8 buffer lazily.
* ABI: layout/pointer/length contracts live in `docs/ABI_LAYOUT.md` (String ABI, BytesView ABI).

**Examples:**
```sg
let text = "Hello 👋 World";
print(len(text) to string);          // 13 (code points)
print((text[6] to uint) to string);   // code point value for 👋
print(text[[1..4]]);                  // "ell"

let view = text.bytes();
print(len(view) to string);          // 15 (UTF-8 bytes)
print((view[0] to uint) to string);   // 72 ('H')
```

### 7.4. String standard methods

The core prelude defines common string helpers as methods on `string`:

* `contains(needle: string) -> bool` — true if `needle` occurs.
* `find(needle: string) -> int` — first code point index, or `-1` if missing.
* `count(needle: string) -> int` — number of occurrences (overlapping).
* `rfind(needle: string) -> int` — last code point index, or `-1` if missing.
* `starts_with(prefix: string) -> bool`, `ends_with(suffix: string) -> bool`.
* `split(sep: string) -> string[]` — empty `sep` splits into code points.
* `join(parts: string[]) -> string` — uses `self` as the separator.
* `trim()`, `trim_start()`, `trim_end()` — remove ASCII whitespace (`space`, `\\t`, `\\n`, `\\r`).
* `replace(old: string, new: string) -> string` — if `old` is empty, returns the original string.
* `reverse() -> string` — reverses by code points.
* `levenshtein(other: string) -> uint` — edit distance by code points.
* `string.from_str(s: &string) -> Erring<string, Error>` — identity parse used by `@entrypoint("argv")`.

---

### 7.5. Array standard methods

The core prelude defines array helpers as methods on `Array<T>`:

* `push(value: T) -> nothing`, `pop() -> Option<T>`, `reserve(new_cap: uint) -> nothing`
* `with_len(length: uint) -> Array<T>`, `with_len_value(length: uint, value: T) -> Array<T>`
* `from_range(r: Range<T>) -> Array<T>`
* `extend(other: &Array<T>) -> nothing`
* `slice(r: Range<int>) -> Array<T>` — thin wrapper over `self[r]` (view)
* `contains(value: &T) -> bool`, `find(value: &T) -> Option<uint>` (requires `__eq` on `T`; currently provided for `Array<int>`, `Array<uint>`, `Array<float>`, `Array<bool>`, `Array<string>`)
* `reverse_in_place() -> nothing`

Top-level helpers `array_push/array_pop/array_reserve` mirror the intrinsic operations.

For fixed-size arrays, `ArrayFixed<T, N>` provides `with_len`, `with_len_value`, and `to_array() -> Array<T>`.

ABI: layout and view rules are defined in `docs/ABI_LAYOUT.md` (Array ABI, Array Slice View ABI, ArrayFixed).

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

Implicit `Some(...)`/`Success(...)` injection happens only in specific contexts (bindings, returns, struct fields, array elements). Overload resolution never inserts tags, so `Option<T>` and `T` remain distinct during call selection (§2.9).

---

## 9. Concurrency Primitives

### 9.1. Channels

`Channel<T>` is a typed FIFO provided by core intrinsics. Core ops:

* `make_channel<T>(capacity: uint) -> own Channel<T>` (helper)
* `Channel<T>::new(capacity: uint) -> own Channel<T>`
* `send(self: &Channel<T>, value: own T)` — blocking send
* `recv(self: &Channel<T>) -> Option<T>` — blocking receive
* `try_send(self: &Channel<T>, value: own T) -> bool` — non-blocking send
* `try_recv(self: &Channel<T>) -> Option<T>` — non-blocking receive
* `close(self: &Channel<T>)`

Methods are invoked on the channel value (`ch.send(v)`, `ch.recv()`, `ch.try_recv()`). `recv`/`try_recv` return `nothing` when the channel is closed (and `try_recv` also returns `nothing` when empty).

Channels are FIFO; fairness across multiple senders/receivers is not specified.

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

The execution model for `parallel` is **not specified yet**; v1 only reserves
the syntax.

**v1 alternative:** Use `spawn` with channels for concurrent (not parallel) processing:
```sg
async fn concurrent_map<T, U>(xs: T[], f: fn(T) -> U) -> U[] {
    let mut tasks: Task<U>[] = [];
    for x in xs {
        tasks.push(spawn f(x));
    }

    let mut results: U[] = [];
    // NOTE: v1 rejects await inside loops; unroll or refactor to recursion.
    for task in tasks {
        compare task.await() {
            Success(v) => results.push(v);
            Cancelled() => return [];
        };
    }
    return results;
}
```

**Note:** v1 uses single-threaded cooperative concurrency. See §9.4-9.5 for async/await model.

Restriction: `=>` is valid only in these `parallel` constructs and within `compare`/`select`/`race` arms (§3.6). Any other use triggers `SynFatArrowOutsideParallel`.

**v1 behavior:** Parsed, but semantic analysis rejects with `FutParallelNotSupported` (`"parallel" requires multi-threading`).

See §22.3 for the current v2+ status note.

### 9.3. Backend Selection

`@backend("cpu"|"gpu"|"tpu"|"wasm"|"native")` may annotate functions.
The compiler validates the string literal and warns on unknown targets. There is
no codegen or execution switching yet; the attribute is advisory only.

Block-level `@backend` is reserved and not parsed in v1.

### 9.4. Tasks and Spawn Semantics

**Execution model:** v1 uses single-threaded cooperative scheduling. All tasks
run on one OS thread, yielding control at `.await()` points. No preemption; use
`checkpoint().await()` for long CPU work.

#### Spawn Expression

```sg
spawn expr
```

- `expr` must be `Task<T>` (an `async fn` call or `async { ... }` block result).
- Returns `Task<T>` — a handle to the spawned task.
- Captured values are moved into the task; `@nosend` types are rejected.

**Example:**
```sg
let data: own string = load();
let task: Task<string> = spawn process(data); // data moved

compare task.await() {
    Success(v) => print(v);
    Cancelled() => print("cancelled");
};
```

#### Task<T> API

```sg
extern<Task<T>> {
    fn clone(self: &Task<T>) -> Task<T>;
    fn cancel(self: &Task<T>) -> nothing;
    fn await(self: own Task<T>) -> TaskResult<T>;
}
```

```sg
tag Cancelled();
pub type TaskResult<T> = Success(T) | Cancelled;
```

#### Cancellation

Surge uses **cooperative cancellation**:

1. `task.cancel()` sets a cancellation flag.
2. At the next suspension point (`.await()`, `checkpoint()`, channel send/recv, timeout),
   the task observes the flag.
3. If cancelled, the awaited result is `Cancelled()`.

#### Checkpoint, Sleep, Timeout

```sg
checkpoint() -> Task<nothing>
sleep(ms: uint) -> Task<nothing>
timeout<T>(t: Task<T>, ms: uint) -> TaskResult<T>
```

- `checkpoint()` yields and checks cancellation.
- `sleep` suspends for the specified duration.
- `timeout` returns `Success(value)` on time, `Cancelled()` on deadline.
- Timers are driven by the async runtime: virtual time by default, real time via
  `surge run --real-time`.

#### Select and Race

`select` is an expression (valid only in async functions/blocks) that waits on multiple awaitable operations and
returns the result of the chosen arm. Arms are checked top-to-bottom and the
first ready arm wins (deterministic tie-break). If `default` is present and no
arms are ready, `default` executes immediately; without `default`, the task
parks until an arm becomes ready. `select` does **not** cancel losing arms.
`default`, when present, must be the last arm.
`default` is a contextual arm label and does not reserve the identifier outside
`select`/`race`.

`race` has the same syntax and selection rules, but cancels losing **Task arms**
after a winner is chosen (non-task arms are not cancelled).

```sg
let v = select {
    ch.recv() => 1;
    sleep(10).await() => 2;
    default => 0;
};

let r = race {
    t1.await() => 1;
    t2.await() => 2;
};
```

### 9.5. Async/Await Model (Structured Concurrency)

Surge provides structured concurrency with async/await for cooperative
multitasking.

#### Async Functions

```sg
async fn name(params) -> RetType {
    // can use .await() inside
}
```

- `async fn` returns `Task<RetType>`.
- `.await()` is allowed in async functions/blocks and in `@entrypoint`.
- `.await()` returns `TaskResult<RetType>`.

**Example:**
```sg
async fn fetch_user(id: int) -> string {
    compare http_get("/users/" + id).await() {
        Success(body) => return body;
        Cancelled() => return "";
    };
}

async fn main() {
    compare fetch_user(42).await() {
        Success(body) => print(body);
        Cancelled() => return nothing;
    };

    let task = spawn fetch_user(42); // Background task
}
```

#### Async Blocks

```sg
async {
    // statements
}
```

- Creates `Task<T>` where `T` is the block's result type.
- The block is a **scope**: spawned tasks are joined before it completes.

**Example:**
```sg
async fn process_all(urls: string[]) -> Data[] {
    async {
        let mut tasks: Task<Data>[] = [];
        for url in urls {
            tasks.push(spawn fetch(url));
        }

        let mut results: Data[] = [];
        // NOTE: v1 rejects await inside loops; unroll or refactor to recursion.
        for task in tasks {
            compare task.await() {
                Success(v) => results.push(v);
                Cancelled() => return [];
            };
        }
        return results;
    }
}
```

#### Structured Concurrency Rules

- Tasks spawned in a normal scope must be awaited or returned before leaving
  the scope (`SemaTaskNotAwaited`).
- Returning or passing a `Task<T>` transfers responsibility for awaiting it.
- `Task<T>` cannot be stored in module-level variables (`SemaTaskEscapesScope`).

#### `@failfast` Attribute

`@failfast` on async blocks/functions cancels sibling tasks when any child
completes as `Cancelled()`. The scope then completes as `Cancelled()`.

#### Current Limitations

- `await` inside loops is rejected during async lowering.
- v1 is single-threaded; tasks yield only at `.await()`.

**See also:** `docs/CONCURRENCY.md` for more examples.

## 10. Standard Library Conventions

* `print(value: string)` – prints a single string with newline.
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
        let e: Error = { message = "failed", code = 1:uint };
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
    if (len(input) == 0) {
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
  print((x to string) + " " + (y to string));
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
// Enum and tagged union
enum Color: string = {
  Black = "black",
  White = "white",
}
tag IntNum(int); tag FloatNum(float);
type Number = IntNum(int) | FloatNum(float);

fn show(c: string) { print(c); }
fn absn(x: Number) -> Number {
  return compare x {
    IntNum(v)   => IntNum(abs(v));
    FloatNum(v) => FloatNum(abs(v));
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
async fn fetch_once(url: string) -> Erring<Data, Error> {
    compare fetch_data(url).await() {
        Success(res) => return res;
        Cancelled() => {
            let e: Error = { message = "cancelled", code = 1:uint };
            return e;
        }
    };
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
    return { x = self.x, y = self.y, z = 0.0 };
  }
}

let p3 = ({x = 1.0, y = 2.0}: Point2D) to Point3D;
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
  if (len(xs) == 0) { return nothing; }
  return Some(xs[0]);
}

fn demo_option() {
  let v = maybe_head([1, 2, 3]);
  compare v {
    nothing   => print("empty");
    Some(x)   => print(x to string);
  }
}
```

```sg
// Channels (blocking + try)
let ch = make_channel::<int>(0);
// spawn omitted; assume a sender exists
let v = ch.recv();          // Option<int>
compare ch.try_recv() {
  nothing => print("empty");
  Some(x) => print(x to string);
}
```

```sg
// Compare with conditional patterns and finally
tag IntVal(int);
tag Text(string);
type IntOrString = IntVal(int) | Text(string) | nothing;

fn classify_value(value: IntOrString) {
  compare value {
    IntVal(x) if x >= 0 => print("non-negative " + (x to string));
    IntVal(x) => print("negative " + (x to string));
    nothing   => print("absent");
    finally   => print("default case");
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
        // NOTE: v1 rejects await inside loops; unroll or refactor to recursion.
        for task in tasks {
            compare task.await() {
                Success(res) => {
                    compare res {
                        Success(data) => results.push(data);
                        err => return err; // Early return on error
                    };
                };
                Cancelled() => {
                    let e: Error = { message = "cancelled", code = 1:uint };
                    return e;
                };
            };
        }

        return Success(results);
    }
}

// With @failfast, a cancelled child cancels its siblings.
@failfast
async fn process_urls_failfast(urls: string[]) -> Erring<Data[], Error> {
    async {
        let mut tasks: Task<Erring<Data, Error>>[] = [];
        for url in urls {
            tasks.push(spawn fetch_data(url));
        }

        let mut results: Data[] = [];
        // NOTE: v1 rejects await inside loops; unroll or refactor to recursion.
        for task in tasks {
            compare task.await() {
                Success(res) => {
                    compare res {
                        Success(data) => results.push(data);
                        err => return err;
                    };
                };
                Cancelled() => {
                    let e: Error = { message = "cancelled", code = 1:uint };
                    return e;
                };
            };
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
  let employee: Employee = { name = "A", age = 0, id = 1, department = "X" };
  let manager: Manager = { name = "B", age = 0, id = 2, department = "Y", team_size = 5 };
  let base: BasePerson = { name = "C", age = 0 };

  // Check direct inheritance
  let is_employee: bool = employee heir BasePerson;  // true
  
  // Check transitive inheritance
  let is_manager_base: bool = manager heir BasePerson;  // true
  
  // Check self-inheritance
  let is_self: bool = employee heir Employee;  // true
  let is_not: bool = base heir Employee;  // false
  
  // Use in conditional
  if (manager heir Employee) {
    print("Manager inherits from Employee");
  }
  
  return is_employee && is_manager_base && is_self && !is_not;
}
```

```sg
// @sealed type example
@sealed
type Conn = { fd:int, @noinherit secret:uint64 }

type SafeConn = Conn : { tag:string } // error: SemaAttrSealedExtend
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
return_42(); // error: SemaUnresolvedSymbol
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

**VM behavior:** `rt_memcpy` panics on overlapping ranges; use `rt_memmove` when overlap is possible.

```sg
// Benchmark directive example
import stdlib/directives::benchmark;

fn factorial(n: int) -> int {
  return compare n {
    0 => 1;
    1 => 1;
    x => x * factorial(x - 1);
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

Directives are structured doc-comment blocks for tool-driven scenarios (tests,
benchmarks, lint checks). They do **not** affect program semantics or ABI.

Current behavior (v1):
- Parsed only when `--directives=collect|gen|run` is enabled.
- Each directive block attaches to the next item in the file.
- Namespace is validated against imports, and the imported module must have
  `pragma directive`.
- `--directives=run` executes a stub runner (prints SKIPPED). No codegen or
  directive body type-checking yet.

Syntax (v1):

```sg
import stdlib/directives::test;

/// test:
/// test.eq(add(1, 2), 3)
fn add(a: int, b: int) -> int { return a + b; }
```

CLI:

```bash
surge diag --directives=off|collect|gen|run --directives-filter=test,bench
```

- `off` (default): ignore directives completely.
- `collect`: parse and validate namespaces.
- `gen`: same as collect (reserved for future codegen).
- `run`: stub execution; `--directives-filter` applies here.

For full details and roadmap, see `docs/DIRECTIVES.md`.

## 14. Precedence & Associativity

From highest to lowest:

1. `[]` (index), call `()`, member `.`, await `.await()`, `to Type` (cast operator)
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
12. `=` `+=` `-=` `*=` `/=` `%=` `&=` `|=` `^=` `<<=` `>>=` (assignment, right-associative)

**Type checking precedence:**
Type checking operators `is` and `heir` have the same precedence as equality operators. Use parentheses for complex expressions:
```sg
x is int && y is string           // OK
employee heir BasePerson && flag  // OK
(x is int) == true                // explicit grouping recommended
(employee heir BasePerson) == true // explicit grouping recommended
```

Short-circuiting for `&&` and `||` is guaranteed.

Note: `=>` is not a general expression operator; it is reserved for `parallel map` / `parallel reduce` (§9.2) and for arms in `compare`/`select`/`race` expressions (§3.6).

### Member access precedence

Member access `.`, await `.await()`, and cast `to Type` are postfix operators and bind tightly together with function calls and indexing. This resolves ambiguous parses, e.g., `a.f()[i].g()` parses as `(((a.f())[i]).g)()`.

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

The following features are supported in v1; restrictions are noted per section.

### 17.1. Union Types

Union aliases (§2.8) use tagged composition only; untagged unions are not supported:

```sg
tag IntNum(int); tag FloatNum(float);
type Number = IntNum(int) | FloatNum(float);

extern<Number> {
  fn __add(a: Number, b: Number) -> Number {
    return compare (a, b) {
      (IntNum(x),   IntNum(y))   => IntNum(x + y);
      (IntNum(x),   FloatNum(y)) => FloatNum(x:float + y);
      (FloatNum(x), IntNum(y))   => FloatNum(x + y:float);
      (FloatNum(x), FloatNum(y)) => FloatNum(x + y);
    };
  }
}
```

Tagged unions provide clearer APIs and enable exhaustiveness checking; see §2.7 for constructors and §3.6 for matching rules.

### 17.2. Tuple Types

```sg
compare (a, b) {
  (x, y) if x is int && y is float => print(((x to float) + y) to string);
  finally => print("unsupported");
}
```

Tuples group multiple values together:

```sg
type Point = (float, float);

fn get_coordinates() -> (float, float) {
    return (10.5, 20.3);
}

let coords: (float, float) = get_coordinates();
let x = coords.0;
let y = coords.1;
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
* Raw pointers `*T`: reserved for backend/FFI in v1/v2; future work may gate them behind `unsafe {}` blocks.

---

## 19. Grammar Sketch (extract)

Note: `macro` items are reserved for v2+; the v1 parser rejects them (`FutMacroNotSupported`).
Directives are parsed only when enabled and follow the syntax in `docs/DIRECTIVES.md`.

```
Module     := Pragma* (Item | DirectiveBlock)*
Pragma     := "pragma" Ident ("," Ident)*   // see docs/PRAGMA.md for supported forms
DirectiveBlock := "///" Namespace ":" Newline
                  ( "///" NamespaceLine Newline )+
Namespace  := Ident
NamespaceLine := Namespace ("." | "::") <rest of line>
Item       := Visibility? (Fn | AsyncFn | TagDecl | TypeDecl | EnumDecl | ContractDecl | ExternBlock | Import | Const | Let)
Visibility := "pub"
Fn         := FnDef | FnDecl
FnDef      := Attr* "fn" Ident GenericParams? ParamList RetType? Block
FnDecl     := Attr* "fn" Ident GenericParams? ParamList RetType? ";"
AsyncFn    := Attr* "async" "fn" Ident GenericParams? ParamList RetType? Block
Attr       := "@" Ident ("(" (Expr ("," Expr)*)? ")")?
GenericParams := "<" Ident ("," Ident)* ">"
ParamList  := "(" (Param ("," Param)*)? ")"
Param      := Attr* "..."? Ident ":" Type ("=" Expr)?
RetType    := "->" Type
TagDecl    := "tag" Ident GenericParams? "(" ParamTypes? ")" ";"
TypeDecl   := Attr* "type" Ident GenericParams? "=" TypeBody ";"
EnumDecl   := Attr* "enum" Ident GenericParams? (":" Type)? "=" "{" EnumVariant ("," EnumVariant)* ","? "}" ";"?
EnumVariant:= Ident ("=" Expr)?
ContractDecl := Attr* "contract" Ident GenericParams? "{" ContractMember* "}" ";"?
ContractMember := Attr* "field" Ident ":" Type ";" | Attr* FnDecl
TypeBody   := StructBody | UnionBody | Type
StructBody := "{" Field ("," Field)* "}"
Field      := Attr* Ident ":" Type
UnionBody  := UnionMember ("|" UnionMember)*
UnionMember:= "nothing" | Ident "(" ParamTypes? ")"
ExternBlock:= "extern<" Type ">" Block
Import     := "import" Path ( "as" Ident | "::" ImportSpec )? ";"
ImportSpec := "*" | Ident ("as" Ident)? | "{" ImportName ("," ImportName)* ","? "}"
ImportName := Ident ("as" Ident)?
Path       := ("." | ".." | Ident) ("/" ("." | ".." | Ident))*
Block      := "{" Stmt* "}"
Stmt       := Const | Let | While | For | If | Spawn ";" | Async | Expr ";" | Break ";" | Continue ";" | Return ";" | Signal ";"
Const      := "const" Ident (":" Type)? "=" Expr ";"
Let        := "let" ("mut")? Ident (":" Type)? ("=" Expr)? ";"
While      := "while" "(" Expr ")" Block
For        := "for" "(" Expr? ";" Expr? ";" Expr? ")" Block | "for" Ident (":" Type)? "in" Expr Block
If         := "if" "(" Expr ")" Block ("else" If | "else" Block)?
Return     := "return" Expr?
Signal     := "signal" Ident ":=" Expr
Async      := "async" "{" Stmt* "}"
Expr       := Compare | Select | Race | Spawn | Parallel | TypeHeirPred | TupleLit | ... (standard precedence)
Parallel   := "parallel" "map" Expr "with" ArgList "=>" Expr
          | "parallel" "reduce" Expr "with" Expr "," ArgList "=>" Expr
ArgList    := "(" (Expr ("," Expr)*)? ")" | "()"
TypeHeirPred := "(" Expr " heir " CoreType ")"
TupleLit   := "(" Expr ("," Expr)+ ")"
AwaitExpr  := Expr "." "await" "(" ")"   // awaits a Task; valid in async fn/block and @entrypoint
Spawn      := "spawn" Expr
Compare    := "compare" Expr "{" Arm (";" Arm)* ";"? "}"
Arm        := Pattern "=>" Expr
Select     := "select" "{" SelectArm (";" SelectArm)* ";"? "}"
Race       := "race" "{" SelectArm (";" SelectArm)* ";"? "}"
SelectArm  := Expr "=>" Expr | "default" "=>" Expr
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
Suffix         := "[]" | "[" Expr "]"
```

**Note:** `@intrinsic` requires declaration-only functions and special intrinsic types (see `docs/ATTRIBUTES.md`).

---

## 20. Compatibility Notes

* Built-ins for primitive base types are sealed; you cannot `@override` them directly. Use `type New = int;` and override on the type alias.
* Dynamic numerics (`int/uint/float`) allow large results; casts to fixed-width may trap.
* Attributes affecting memory layout and ABI (`@packed`, `@align`) are part of the language specification and cannot be replaced by directives. Directives do not modify type layout or ABI contracts.
* Concurrency contract attributes describe analyzable requirements and do not change runtime semantics. When lock state cannot be proven, the compiler emits `SemaLockUnverified` warnings.
* **Directive vs Attribute distinction**: Attributes are closed-set language features that affect compilation, type checking, or runtime behavior. Directives are extensible annotations that provide metadata for external tools without changing language semantics. Tests, benchmarks, and documentation have been moved from attributes to the directive system to maintain the distinction.
* **Pragma directive**: `pragma directive` marks a module as a directive module. The directive namespace is derived from the import path. In `--directives=off` (default), directive blocks have zero overhead.
* **Language intrinsics**: Intrinsics are a fixed, small set primarily declared in `core/` and implemented by the VM backend (see surge_vm.md). `@intrinsic` is allowed anywhere, but only known intrinsics are supported by backends.

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

Some diagnostic codes are reserved for future features (macros, signals, parallel). See `internal/diag/codes.go` for the authoritative list.

---

## 22. Future Features (v2+)

The following keywords are **reserved** and not implemented in v1. Semantics are
TBD; the notes below describe current compiler behavior only.

### 22.1. Signals (`signal`)

- Parsed, but semantic analysis rejects with `FutSignalNotSupported`.

### 22.2. Macros (`macro`)

- Parser rejects `macro` items with `FutMacroNotSupported`.

### 22.3. Parallel map/reduce (`parallel map`, `parallel reduce`)

- Parsed, but semantic analysis rejects with `FutParallelNotSupported`.

### 22.4. Compatibility Notes

- v1 remains single-threaded; `parallel` is not a stable API.
- Lock contract attributes are partially enforced (see `docs/ATTRIBUTES.md`).
