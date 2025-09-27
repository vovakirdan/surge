# Surge Language Specification (Draft 1)

> **Status:** Draft for review
> **Scope:** Full language surface for tokenizer → parser → semantics. VM/runtime details are out of scope here and live in RUNTIME.md / BYTECODE.md.

---

## 0. Design Goals

* **Safety by default:** explicit ownership/borrows; no hidden implicit mutability.
* **Deterministic semantics:** operator/overload resolution is fully specified.
* **Orthogonality:** features compose cleanly (e.g., generics + ownership + methods via `extern<T>`).
* **Compile-time rigor, runtime pragmatism:** statically-typed core with dynamic-width numeric families (`int`, `uint`, `float`).
* **Testability:** first-class doc-tests via `/// test:` directives.

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
import, newtype, type, literal, alias, extern, return, signal, compare, spawn,
true, false, nothing, is, finally, async, await,
@pure, @overload, @override, @backend
```

### 1.5. Literals

* Integer: `0`, `123`, `0xFF`, `0b1010`, underscores allowed for readability: `1_000`.
* Float: `1.0`, `0.5`, `1e-9`, `2.5e+10`.
* String: `"..."` (UTF-8), escape sequences `\n \t \" \\` and `\u{hex}`.
* Bool: `true`, `false`.
* Unit value: `()` is the single value of the `unit` type.
* Absence value: `nothing` denotes the absent variant of `Option<T>`.

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
  * `unit` – a type with a single value `()`.

**Coercions:**

* Fixed → dynamic of same family: implicit and lossless (e.g., `int32 -> int`).
* Dynamic → fixed **never implicit**. Explicit cast required; may trap if out of range.
* Between numeric families: no implicit. Explicit casts defined; may round or trap depending on target.

### 2.2. Arrays

`T[]` is a growable, indexable sequence of `T` with zero-based indexing.

* Indexing calls magic methods: `__index(i:int) -> T` and `__index_set(i:int, v:T) -> void`.
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

* **Newtype:** `newtype MyInt = int;` creates a distinct nominal type that inherits semantics of `int` but can override magic methods via `extern<MyInt>`. (Different from a pure alias.)
* **Struct:** `type Person { age:int, name:string, @readonly weight:float }`.

  * Fields are immutable unless variable is `mut`. `@readonly` forbids writes even through `mut` bindings.
* **Literal enums:** `literal Color = "black" | "white";` Only the listed literals are allowed values.
* **Type alias:** `alias Number = int | float;` a type that admits any member type; overload resolution uses the best matching member (§8).

### 2.6. Option and Result Types

* `Option<T>` – optional value. Constructors:
  * `Some(x)` wraps a present value of type `T`.
  * `nothing` denotes the absent case and has type `Option<T>` for some `T` determined by context.

Type-directed rules:

* `nothing` requires contextual typing to determine `T`; using `nothing` where `T` cannot be inferred is a type error.
* `compare` supports `nothing`/`Some(...)` patterns natively (§3.6).
* `Option<T>` is equivalent to `alias Option<T> = T | nothing;`

* `Result<T, E>` – recoverable error pattern provided by the standard library.

### `nothing` contextual typing

`nothing` is the absent variant for `Option<T>`. Its type must be inferred from context. If the context does not provide a target `Option<T>` to infer `T`, the compiler emits `E_AMBIGUOUS_NOTHING`.

**Function return equivalence:**
* `fn foo() { }` and `fn bar() { return nothing; }` are equivalent when the function has no explicit return type
* Functions without explicit return types return `unit`, so `return nothing;` is equivalent to `return ();`

**Variable declarations:**
* `let x: int;` gets default value (0 for int, "" for string, false for bool, etc.)
* This is different from `let x: Option<int>;` which would get `nothing` as the default

**Array homogeneity:**
* Arrays must be homogeneous: `let arr: Option<int>[] = [1, nothing, 3];` is valid - auto-wraps in `Some()`
* Elements `1` and `3` automatically wrap in `Some()` when target type is `Option<T>[]`
* Result: `[Some(1), nothing, Some(3)]`

**String conversion:**
* `nothing.__to_string() -> string { return ""; }`

Examples:

```
let x = nothing;                 // E_AMBIGUOUS_NOTHING
let y: Option<int> = nothing;    // OK
return nothing;                  // OK if function returns Option<T>
fn foo() { }                     // returns unit
fn bar() { return nothing; }     // also returns unit (equivalent)
```

### 2.7. Memory Management Model

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
* Also we can declare a variable without a value: `let x: Type;` - this will be a variable with a default value of the type, but we can assign to it later.
* Top-level `let` is allowed as an item; items are private by default and can be exported with `pub let`.

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
* If `: Type` is present it must be a valid type; if omitted the element type is inferred from the iterable.

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
* Update semantics: changes propagate in topological order per turn. The bound expression must be `@pure`; side-effects inside signal evaluation are disallowed.

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
- Option – `nothing` (absent) and `Some(p)` where `p` is a sub-pattern.
- Conditional patterns – `x if x is int` where `x` is bound and condition is checked.

Notes:

- Arms are tried top-to-bottom; the first match wins.
- `=>` separates pattern from result expression and is only valid within `compare` arms and parallel constructs.
- `nothing` has type `Option<T>` for some `T` determined by context; using `nothing` without sufficient type information is a type error at a later phase.

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

* Functions without `-> RetType` are considered to return `unit` and do not need explicit `return` statements.
  * `fn main() { ... }` - valid, no return needed
  * `fn main() { return (); }` - also valid, explicitly returns unit
* Functions with `-> RetType` must return a value of that type.
  * `fn answer() -> int { return 42; }`

### 4.2. Attributes

* `@pure` – function has no side effects, deterministic, cannot mutate non-local state; required for execution in certain parallel contexts and signals.
* `@overload` – declares an overload of an existing function name with a distinct signature.
* `@override` – replaces an existing implementation for a target (primarily used in `extern<T>` blocks for newtypes). Use sparingly.
* `@backend("cpu"|"gpu")` – execution target hint. Semantics: a backend-specific lowering may choose specialized code paths. If not supported, it is a no-op.
* `@test` – marks test functions or doc-test helpers.
* `@benchmark`, `@time` – benchmark/time helpers.
* `@deprecated("msg")` – marks items as deprecated with a message.

Parser behaviour:

* Unknown attributes are accepted syntactically but produce a warning `W_UNKNOWN_ATTRIBUTE` by default. A `--strict-attributes` mode may promote this to `E_UNKNOWN_ATTRIBUTE`.
* If an attribute is used on an unsupported target (e.g., `@test` on a type alias), emit `E_ILLEGAL_ATTRIBUTE_TARGET`.

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
* Result propagation: `expr?` — if `expr` is `Result<T,E>`, yields `T` or returns `Err(E)` from the current function (see §11).

### 6.2. Type Checking Operator (`is`)

The `is` operator performs runtime type checking and returns a `bool`. It checks the essential type identity, ignoring ownership modifiers:

**Rules:**
* `42 is int` → `true`
* `nothing is nothing` → `true`
* `Option<int> is int` → `false` (it's `Option<int>`, not `int`)
* `own T is T` → `true` (ownership doesn't change type essence)
* `mut T is T` → `true`
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

Union alias `alias Number = int | float` participates by expanding to candidates for each member type; the best member is chosen.

### Option Auto-wrapping

When the target type is `Option<T>` and the source is `T`, automatic wrapping in `Some()` occurs:

```sg
let x: Option<int> = 42;           // auto-wraps to Some(42)
let arr: Option<int>[] = [1, 2, 3]; // auto-wraps to [Some(1), Some(2), Some(3)]
let mixed: Option<int>[] = [1, nothing, 3]; // [Some(1), nothing, Some(3)]
```

This coercion has cost 1 in overload resolution (same as numeric literal fitting).

---

## 9. Concurrency Primitives

### 9.1. Channels

`channel<T>` is a typed FIFO. Core ops (from std):

* `make_channel<T>(cap:uint) -> own channel<T>`
* `send(ch:&channel<T>, v:own T)`
* `recv(ch:&channel<T>) -> Option<T>`         // blocking receive
* `try_recv(ch:&channel<T>) -> Option<T>`     // non-blocking receive
* `recv_timeout(ch:&channel<T>, ms:int) -> Option<T>`
* `close(ch:&channel<T>)`

The standard library also provides `choose { ... }` to select among ready operations. Channels are FIFO; fairness across multiple senders/receivers is not specified.

### 9.2. Parallel Map / Reduce

* `parallel map xs with args => func` executes `func` over `xs` elements concurrently; `func` must be `@pure`.
* `parallel reduce xs with init, args => func` reduces in parallel; `func` must be associative and `@pure`.

Grammar (surface):

```
ParallelMap    := "parallel" "map" Expr "with" ArgList "=>" Expr
ParallelReduce := "parallel" "reduce" Expr "with" Expr "," ArgList "=>" Expr
ArgList        := "(" (Expr ("," Expr)*)? ")" | "()"
```

Restriction: `=>` is valid only in these `parallel` constructs and within `compare` arms (§3.6). Any other use is a parse error `E_ARROW_USAGE`.

### 9.3. Backend Selection

`@backend("gpu")`/`@backend("cpu")` may annotate functions or blocks. If the target is unsupported, a diagnostic is emitted or a fallback is chosen by the compiler based on flags.

### 9.4. Tasks and spawn semantics

* `spawn expr` launches a new task to evaluate `expr` asynchronously. If `expr` has type `T`, `spawn expr` has type `Task<T>` (a join handle).
* `join(t: Task<T>) -> Result<T, Cancelled>` waits for completion; on normal completion returns `Ok(value)`, on cooperative cancellation returns `Err(Cancelled)`.
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

### Error Inheritance Model

Surge uses a simple inheritance model for errors instead of complex trait systems:

```sg
type Error {
    message: string;
    code: uint;
}

extern<Error> {
    fn throw(self: &Error) -> void;
    fn __to_string(self: &Error) -> string {
        return "Error " + self.code + ": " + self.message;
    }
}

// Custom errors via newtype:
newtype HTTPError = Error;
newtype FileError = Error;
newtype NetworkError = Error;

// Usage:
let http_access_denied: HTTPError = HTTPError {
    message: "Access denied",
    code: 401
};

let file_not_found: FileError = FileError {
    message: "File not found",
    code: 404
};
```

Newtype errors inherit all methods from the base `Error` type but can override specific behaviors:

```sg
extern<HTTPError> {
    @override
    fn __to_string(self: &HTTPError) -> string {
        return "HTTP Error " + self.code + ": " + self.message;
    }
}
```

### Recoverable errors: Result<T, E> and `?` propagation

The standard recoverable error type is `Result<T, E>` provided by the standard library:

```
type Result<T, E> = Ok(T) | Err(E)
```

Use the `?` operator to propagate `Err` early: `expr?` evaluates `expr` which must be `Result<T,E>`; if `Ok(v)` — yields `v`, if `Err(e)` — the current function returns `Err(e)` immediately (the function must return `Result<...,E>` or a compatible result type).

Example:

```
fn parse_int(s:string) -> Result<int, ParseError> { /* ... */ }
fn read_and_parse() -> Result<int, ParseError> {
  let line = read_line()?;      // if read_line() returns Err -> propagate
  let v = parse_int(line)?;     // propagate parse error
  return Ok(v);
}
```

Traps remain for unrecoverable faults (OOB, internal assertion, certain cast traps). A richer effects model may be added later; this draft uses Result + traps.

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
literal Color = "black" | "white";
alias Number = int | float;

fn show(c: Color) { print(c); }
fn absn(x: Number) -> Number { return abs(x); }
```

```sg
// Struct and readonly
type Person { age:int, name:string, @readonly weight:float }

fn birthday(mut p: Person) { p.age = p.age + 1; }
```

```sg
// Signals
signal total := sum(prices);
// any change to prices recomputes total (sum must be @pure)
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
fn classify_value(value: alias Number | string) {
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

---

## 13. Directives (`///`)

13.1 In any file, `/// test:` starts a test section composed of one or more named tests. Each test is a block of surge code executed separately.

```
/// test:
/// Test1:
///   test.equal(add(2, 3), 5);
/// Test2:
///   let a:int = 4;
///   let b:int = 0;
///   test.le(add(a, b), 4);
```

* Names must be unique within the file.
* Test harness collects and runs these in isolation; they have access to the module scope where they appear.

13.2 In any file, `/// benchmark:` starts a benchmark section composed of one or more named benchmarks. Each benchmark is a block of surge code executed separately.

```
/// benchmark:
/// Benchmark1:
///   benchmark.measure(add(2, 3), 5);
```

13.3 In any file, `/// time:` starts a time section composed of one or more named times. Each time is a block of surge code executed separately.

```
/// time:
/// Time1:
///   time.measure(add(2, 3), 5);
```
---

## 14. Precedence & Associativity

From highest to lowest:

1. `[]` (index), call `()`, member `.`, postfix `?`
2. `* / %`
3. `+ -`
4. `< <= > >= == != is` (all comparison operators have same precedence, left-associative)
5. `&&`
6. `||`
7. `=` `+=` `-=` `*=` `/=` (right-associative)

**Type checking precedence:**
Type checking with `is` has the same precedence as equality operators. Use parentheses for complex expressions:
```sg
x is int && y is string  // OK
(x is int) == true       // explicit grouping recommended
```

Short-circuiting for `&&` and `||` is guaranteed.

Note: `=>` is not a general expression operator; it is reserved for `parallel map` / `parallel reduce` (§9.2) and for arms in `compare` expressions (§3.6).

### Member access precedence

Member access `.` is a postfix operator and binds tightly together with function calls and indexing. This resolves ambiguous parses, e.g., `a.f()[i].g()` parses as `(((a.f())[i]).g)()`.

---

## 15. Name & Visibility

* Items are `pub` (public) or private by default. (Default: private.)
* `pub fn`, `pub type`, `pub literal`, `pub alias`, `pub let` export items from the module.

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

## 17. Open Questions / To Confirm

* Should `string` be `Copy` for small sizes or always `own`? (Proposed: always `own`.)
* Exact GPU backend surface (kernels, memory spaces). For now, `@backend` is a hint.
* Raw pointers `*T`: do we gate deref behind an `@unsafe` block? (Proposed: yes in later iteration; tokenizer still recognizes `*T`.)

---

## 18. Grammar Sketch (extract)

```
Module     := Item*
Item       := Visibility? (Fn | AsyncFn | NewtypeDef | TypeDef | LiteralDef | AliasDef | ExternBlock | Import | Let)
Visibility := "pub"
Fn         := Attr* "fn" Ident GenericParams? ParamList RetType? Block
AsyncFn    := Attr* "async" "fn" Ident GenericParams? ParamList RetType? Block
Attr       := "@pure" | "@overload" | "@override" | "@backend(" Str ")"
GenericParams := "<" Ident ("," Ident)* ">"
ParamList  := "(" (Param ("," Param)*)? ")"
Param      := Ident ":" Type | "..."
RetType    := "->" Type
NewtypeDef := "newtype" Ident "=" Type ";"
TypeDef    := "type" Ident StructBody ";"?
StructBody := "{" Field ("," Field)* "}"
Field      := Attr* Ident ":" Type
LiteralDef := "literal" Ident "=" LiteralAlt ("|" LiteralAlt)* ";"
LiteralAlt := Str
AliasDef   := "alias" Ident "=" Type ("|" Type)* ";"
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
Expr       := Compare | Spawn | ... (standard precedence)
Spawn      := "spawn" Expr
Compare    := "compare" Expr "{" Arm (";" Arm)* ";"? "}"
Arm        := Pattern "=>" Expr
Pattern    := "finally" | Ident | Literal | "nothing" | "Some" "(" Pattern ")" | Ident "if" Expr
Type       := Ownership? CoreType Suffix*
Ownership  := "own" | "&" | "&mut" | "*"
CoreType   := Ident ("<" Type ("," Type)* ">")?
Suffix     := "[]"
```

---

## 19. Compatibility Notes

* Built-ins for primitive base types are sealed; you cannot `@override` them directly. Use `newtype New = int;` and override on the newtype.
* Dynamic numerics (`int/uint/float`) allow large results; casts to fixed-width may trap.
---

*End of Draft 3*

---

## 20. Diagnostics Overview (selected)

Stable diagnostic codes used by the parser and early semantic checks:

* `E_GENERIC_UNDECLARED` — generic parameter used but not declared.
* `E_AMBIGUOUS_NOTHING` — `nothing` used without contextual type.
* `E_MOVE_BORROWED_TO_THREAD` — cannot move borrowed reference into spawned task.
* `E_SIGNAL_NOT_PURE` — signals require @pure expression.
* `E_ARROW_USAGE` — `=>` reserved for compare arms and parallel constructs.
* `E_FOR_MISSING_IN` — `for`-in missing `in` token.
* `E_FOR_BAD_HEADER` — malformed C-style `for` header.
* `E_ILLEGAL_ATTRIBUTE_TARGET` — attribute not allowed on this target.
* `W_UNKNOWN_ATTRIBUTE` — unknown attribute.
* `E_CHANNEL_CLOSED_ON_SEND` — send on closed channel returns Err(ChannelClosed).
* `E_CYCLIC_TOPLEVEL_INIT` — cyclic top-level initialization.
* `E_AMBIGUOUS_OVERLOAD` — ambiguous overload resolution.
* `E_MISMATCH_RESULT_USE` — Result used where plain value expected (use `?` or compare).
