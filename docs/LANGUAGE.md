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
fn, let, mut, if, else, while, for, in, break, continue,
import, using, type, literal, alias, extern, return, signal,
true, false, nothing, @pure, @overload, @override, @backend
```

### 1.5. Literals

* Integer: `0`, `123`, `0xFF`, `0b1010`, underscores allowed for readability: `1_000`.
* Float: `1.0`, `0.5`, `1e-9`, `2.5e+10`.
* String: `"..."` (UTF-8), escape sequences `\n \t \" \\` and `\u{hex}`.
* Bool: `true`, `false`.
* Nothing: `nothing` (represents absence of value, similar to null/nil/none).

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
  * `string` – a sequence of Unicode scalar values (rune-like). Layout: dynamic array of code points.

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
* `&T` – shared borrow (immutable view).
* `*T` – raw pointer (unsafe operations; deref requires explicit `deref(*p)` call).

**Moves & Copies:**

* Primitive fixed-size types and `bool` are `Copy`; `string` and arrays are `own` by default (move). The compiler may optimize small-string copies, but semantics are move.
* Assignment `x = y;` moves if `y` is `own` and `T` not `Copy`. Borrowing uses `&` operator: `let r: &T = &x;`.

**Function parameters:**

* `fn f(x: own T)`: takes ownership.
* `fn f(x: &T)`: borrows; caller retains ownership; lifetime is lexical.
* `fn f(x: *T)`: raw; callee must not assume safety.

### 2.4. Generics

* Universal type parameter `T` denotes any type. Generic syntax:

  * Functions: `fn id(x: T) -> T { return x; }` - this will be a generic function
  * Extern methods: `extern<T> { ... }` - this will be a generic method

### 2.5. User-defined Types

* **Newtype:** `type MyInt = int;` creates a distinct nominal type that inherits semantics of `int` but can override magic methods via `extern<MyInt>`. (Different from a pure alias.)
* **Struct:** `type Person { age:int, name:string, @readonly weight:float }`.

  * Fields are immutable unless variable is `mut`. `@readonly` forbids writes even through `mut` bindings.
* **Literal enums:** `literal Color = "black" | "white";` Only the listed literals are allowed values.
* **Union alias:** `alias Number = int | float;` a type that admits any member type; overload resolution uses the best matching member (§8).

---

## 3. Expressions & Statements

### 3.1. Variables

* Declaration: `let name: Type = expr;`
* Mutability: `let mut x: Type = expr;` allows assignment `x = expr;` and in-place updates.
* Also we can declare a variable without a value: `let x: Type;` - this will be a variable with a default value of the type, but we can assign to it later.

### 3.2. Control Flow

* If: `if (cond) { ... } else if (cond) { ... } else { ... }`
* While: `while (cond) { ... }`
* For counter: `for (init; cond; step) { ... }` where each part may be empty.
* For-in iteration: `for item:T in xs:T[] { ... }` requires `__range()`.
* `break`, `continue`, `return expr?;`.

### 3.3. Semicolons

* Statements end with `;` except block bodies and control-structure headers.

### 3.4. Indexing & Slicing

* `arr[i]` desugars to `arr.__index(i)`.
* `arr[i] = v` desugars to `arr.__index_set(i, v)`.

### 3.5. Signals (Reactive Bindings)

* `signal name := expr;` binds `name` to the value of `expr`, re-evaluated automatically when any of its dependencies change.
* Signals are single-assignment; you cannot assign to `name` directly.
* Update semantics: changes propagate in topological order per turn. Side-effects inside re-evaluations are forbidden unless the function is marked `@pure` (signals implicitly require purity).

---

## 4. Functions & Methods

### 4.1. Function Declarations

```
fn name(params) -> RetType? { body }
params := name:Type (, ...)* | ... (variadic)
```

* Example: `fn add(a:int, b:int) -> int { return a + b; }`
* Variadics: `fn print(...args) { ... }`

**Return type semantics:**

* Functions without `-> RetType` have no return type and do not need explicit `return` statements
  * `fn main() { ... }` - valid, no return needed
  * `fn main() { return nothing; }` - also valid, explicitly returns nothing
* Functions with `-> RetType` must return a value of that type
  * `fn add() -> int { return 42; }` - must return int
* The `nothing` keyword represents absence of value and can be used in return statements for functions without return types

### 4.2. Attributes

* `@pure` – function has no side effects, deterministic, cannot mutate non-local state; required for execution in certain parallel contexts.
* `@overload` – declares an overload of an existing function name with a distinct signature.
* `@override` – replaces an existing implementation for a target (primarily used in `extern<T>` blocks for newtypes). Use sparingly.
* `@backend("cpu"|"gpu")` – execution target hint. Semantics: a backend-specific lowering may choose specialized code paths. If not supported, it is a no-op.

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
* `using pkg` – brings all public items of `pkg` into the current scope (discouraged in libraries).

**Name resolution order:** local scope > explicit imports > `using`-brought names > prelude/std.

---

## 6. Operators & Magic Methods

### 6.1. Operator Table

* Arithmetic: `+ - * / %` → `__add __sub __mul __div __mod`
* Comparison: `< <= == != >= >` → `__lt __le __eq __ne __ge __gt` (must return `bool`)
* Logical: `&& || !` – short-circuiting; operate only on `bool`.
* Indexing: `[]` → `__index __index_set`
* Unary: `+x -x` → `__pos __neg`
* Abs: `abs(x)` → `__abs`
* To-string: used by `print` → `__to_string() -> string`
* Range: `for in` → `__range() -> Range<T>` where `Range<T>` yields `T` via `next()`.

### 6.2. Assignment

* `=` move/assign; compound ops `+=` etc. desugar to method + assign if defined.

---

## 7. Literals & Inference

### 7.1. Numeric Literal Typing

* Integer literals default to `int`.
* Float literals default to `float`.
* Suffixes allowed: `123:int32`, `1.0:float32` to select fixed types.

### 7.2. String & Rune

* `string` stores Unicode scalar values; `"\u{1F600}"` represents a single code point.

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

---

## 9. Concurrency Primitives

### 9.1. Channels

`channel<T>` is a typed FIFO. Core ops (from std): `make_channel<T>(cap:uint) -> own channel<T>`, `send(ch:&channel<T>, v:own T)`, `recv(ch:&channel<T>) -> T|none`.

### 9.2. Parallel Map / Reduce

* `parallel map xs with args => func` executes `func` over `xs` elements concurrently; `func` must be `@pure` or side-effect constraints must be satisfied by the backend.
* `parallel reduce xs with init, args => func` reduces in parallel; `func` must be associative and `@pure`.

### 9.3. Backend Selection

`@backend("gpu")`/`@backend("cpu")` may annotate functions or blocks. If the target is unsupported, a diagnostic is emitted or a fallback is chosen by the compiler based on flags.

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
type MyInt = int;
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

1. `[]` (index), call `()`, member `.` (future), unary `! + -`
2. `* / %`
3. `+ -`
4. `< <= > >= == !=`
5. `&&`
6. `||`
7. `=` `+=` `-=` `*=` `/=` (right-associative)

Short-circuiting for `&&` and `||` is guaranteed.

---

## 15. Name & Visibility

* Items are `pub` (public) or private by default. (Default: private.)
* `pub fn`, `pub type`, `pub literal`, `pub alias` export items from the module.

---

## 16. Open Questions / To Confirm

* Should `string` be `Copy` for small sizes or always `own`? (Proposed: always `own`.)
* Exact GPU backend surface (kernels, memory spaces). For now, `@backend` is a hint.
* Raw pointers `*T`: do we gate deref behind an `@unsafe` block? (Proposed: yes in later iteration; tokenizer still recognizes `*T`.)

---

## 17. Grammar Sketch (extract)

```
Module     := Item*
Item       := Fn | TypeDef | LiteralDef | AliasDef | ExternBlock | Import | Using
Fn         := Attr* "fn" Ident GenericParams? ParamList RetType? Block
Attr       := "@pure" | "@overload" | "@override" | "@backend(" Str ")"
GenericParams := "<" Ident ("," Ident)* ">"
ParamList  := "(" (Param ("," Param)*)? ")"
Param      := Ident ":" Type | "..."
RetType    := "->" Type
TypeDef    := "type" Ident "=" Type ";" | "type" Ident StructBody ";"?
StructBody := "{" Field ("," Field)* "}"
Field      := Attr* Ident ":" Type
LiteralDef := "literal" Ident "=" LiteralAlt ("|" LiteralAlt)* ";"
LiteralAlt := Str
AliasDef   := "alias" Ident "=" Type ("|" Type)* ";"
ExternBlock:= "extern<" Type ">" Block
Import     := "import" Path ("::" Ident ("as" Ident)?)? ";"
Using      := "using" Path ";"
Path       := Ident ("/" Ident)*
Block      := "{" Stmt* "}"
Stmt       := Let | While | For | If | Expr ";" | Break ";" | Continue ";" | Return ";" | Signal ";"
Let        := "let" ("mut")? Ident (":" Type)? "=" Expr
While      := "while" "(" Expr ")" Block
For        := "for" "(" Expr? ";" Expr? ";" Expr? ")" Block | "for" Ident ":" Type "in" Expr Block
If         := "if" "(" Expr ")" Block ("else" If | "else" Block)?
Return     := "return" Expr?
Signal     := "signal" Ident ":=" Expr
Expr       := ... (standard precedence)
Type       := Ownership? CoreType Suffix*
Ownership  := "own" | "&" | "*"
CoreType   := Ident ("<" Type ("," Type)* ">")?
Suffix     := "[]"
```

---

## 18. Compatibility Notes

* Built-ins for primitive base types are sealed; you cannot `@override` them directly. Use `type New = int;` and override on the newtype.
* Dynamic numerics (`int/uint/float`) allow large results; casts to fixed-width may trap.

---

*End of Draft 1*
