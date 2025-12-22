# SURGE_IS_NOT.md

> Surge is not here to win a cosplay contest.
> Surge is here to be Surge.

Surge respects Rust, Go, TypeScript, Python, and the others.
Without them, the language simply wouldn't exist. But it **doesn't try to be their clone**.

This file is a short guide to **what Surge is NOT**, with a touch of self-irony and small code examples.

---

## Surge is not Rust

Yes, we know you'll compare them anyway.  
That's fine. We do too.

### Arrays: we know `[T; N]`, we chose `T[N]`

In Rust:

```rust
let xs: [i32; 3] = [1, 2, 3];
```

In Surge:

```sg
let xs: int32[3] = [1, 2, 3];
```

We know about the `[T; N]` syntax, but we chose `T[N]` because:

* The type "feels" like specifying a **shape** (`int[3]`, `int[10][10]`),
* it visually pairs better with `T[]` and `Option<T>`/`Erring<T, E>`,
* and because the author simply wanted it that way. Sometimes that's reason enough.

### Result vs Erring: it's not just a rename

In Rust:

```rust
fn parse(s: &str) -> Result<i32, Error> { ... }
```

In Surge:

```sg
tag Success<T>(T);

type Erring<T, E: ErrorLike> = Success(T) | E;

fn parse(s: string) -> Erring<int, Error> {  // or int!
    if (s == "42") { 
        return Success(42); 
    }
    let e: Error = { message: "bad", code: 1 };
    return e; // error goes as-is
}
```

And sugar:

```sg
fn parse(s: string) -> int! { ... }  // == Erring<int, Error>
```

We moved away from a direct `Result<T, E>` clone because:

* we want to use **`Success<T>`** instead of `Ok<T>` — it's semantically more honest,
* we want **errors to be returnable directly**, without `Err(...)`,
* we want a single standard `T!` in types (no zoo of `T!CustomError!Whatever`),
* and because we already have `compare`, which plays nicely with tagged unions.

Surge is not Rust: we borrowed the "result-or-error" idea, but built our own model around `Erring` and tags.

### Option vs `T?`

In Rust:

```rust
fn head(xs: &[i32]) -> Option<i32> { ... }
```

In Surge:

```sg
tag Some<T>(T);
type Option<T> = Some(T) | nothing;

fn head<T>(xs: T[]) -> Option<T> {
    if (xs.len == 0) { return nothing; }
    return Some(xs[0]);
}

let maybe_num: int? = head([1, 2, 3]);  // == Option<int>
```

Again: familiar idea, our own implementation. And yes, we **don't have** an `expr?` operator; if you want magic, you'll have to write `compare`.

### match vs compare

In Rust:

```rust
match result {
    Ok(v)  => println!("{}", v),
    Err(e) => println!("err: {}", e),
}
```

In Surge:

```sg
compare result {
    Success(v) => print("ok", v);
    err        => print("err", err);
}
```

We know about `match`. We just called it `compare` to:

* emphasize the **comparison of shape/value**,
* not carry Rust expectations about exhaustiveness and patterns (we have our own plan).

### No, the last line is not a `return`

In Rust you can silently put an expression at the end of a function, forget the `;` — and it becomes a return.

In Surge:

```sg
fn answer() -> int {
    return 42;
}
```

Yes, `return` is mandatory.
Yes, even if it's the last line.
Yes, even if you're feeling lazy.

Who even came up with the idea that "a line without a semicolon" should mean return? We like explicitness a bit more than saving one character.

### Of course, `finally` can be replaced with `_`

In `compare` we have a special word `finally`:

```sg
compare result {
    Success(v) => print(v);
    finally    => print("fallback");
}
```

But if you prefer `_` in the Rust spirit — go ahead:

```sg
compare result {
    Success(v) => print(v);
    _          => print("fallback");
}
```

`finally` is just a more conversational way to say "everything else".
Want strictness — use `_`. Want drama — use `finally`.

### Traits vs Contracts + extern<T>

In Rust:

```rust
trait Display {
    fn fmt(&self) -> String;
}

impl Display for Point {
    fn fmt(&self) -> String { ... }
}
```

In Surge:

```sg
contract Display<T> {
    fn fmt(self: &T) -> string;
}

type Point = { x: int, y: int };

extern<Point> {
    fn fmt(self: &Point) -> string {
        return "(" + self.x + ", " + self.y + ")";
    }
}
```

Why this way:

* **contracts** — purely structural requirements (if the type "fits" — it fits),
* `extern<T>` keeps behavior near the type, but not inside,
* no hierarchies, inheritance, blanket-impl magic — everything is simple and visible.

Surge is not Rust: we love the "trait-like" abstraction idea, but made them simpler and less magical.

### We're not thrilled about generics either

Honestly: nobody wakes up in the morning thinking "how can I write more `<T>` today?".

In Surge:

```sg
fn id<T>(x: T) -> T {
    return x;
}

let a = id(42);    // T is inferred as int
let b = id("hi");  // T is inferred as string
```

Where it's obvious — the compiler infers types itself.
Where it's not obvious — it honestly says: "I'm not sure, please write `::<int>` yourself".

```sg
fn make<T>() -> T;

let x = make();        // error: cannot infer T from return type
let y = make::<int>(); // now everyone understands
```

We don't love generics either. So where it's "sort of obvious", you can omit them — until it becomes non-obvious. And then you'll have to finish the sentence.

### Lifetimes vs boring ownership

We have:

* `own T` — I own it,
* `&T` — I'm looking,
* `&mut T` — I'm the only one changing it.

There's no lifetimes as a syntactic beast — no zoo of `'a`, `'static` and the like jumping around for you. Borrow rules exist, but they're **lexical and obvious**. If you need to end a borrow early — there's explicit `@drop`.

We know the Rust tradition is strong. We just decided that **a developer's night's sleep also matters**.

### Oops, looks like we don't have `int128`

Surge has a family of dynamic `int/uint/float` and fixed `int32`, `uint64`, `float32`, `float64`, and friends. `int128` hasn't arrived yet.

If at some point you need `int128`, a good question is:
**what problem were you actually trying to solve?**

- cryptography? maybe you need a good library, not `int128`;
- money? try fixed-point decimal, not a bit hammer;
- just "sounds cool"? we can't help you there.

---

## Surge is not TypeScript

Yes, we also know that `interface Foo { ... }` is convenient.
That's why we have **contracts**, just without npm and `node_modules` bundled in.

### `interface` vs `contract`

In TypeScript:

```ts
interface User {
  id: number;
  name: string;
}
```

In Surge:

```sg
contract UserLike<T> {
    field id: int;
    field name: string;
}
```

The meaning is similar, but:

* contracts live at the **language** level, not tsc configuration,
* only strict `field` and `fn` — no default values inside,
* this is a **structural contract**, not a class.

### `as` vs `to`

TypeScript loves `value as Type`.  

In Surge:

```sg
let x: int32 = 300000;
let a: int   = x to int;    // fine
let b: int16 = x to int16;  // may panic if it doesn't fit
```

We use `to` because you convert `Egg to Bird`, not `Egg as Bird`.
`as` sounds like we just closed our eyes and made an agreement.

And yes, be careful: not all `Egg` can become `Bird`. Some might become `Amphibian` during conversion (read — cause a runtime error if the value doesn't fit in the target type).

### type annotations: `name: Type`, not `Type name`

In TypeScript:

```ts
let user: User;
```

In Surge:

```sg
let user: User;
```

Yes, here everything is like in TS and Rust: `name: Type`. This is a conscious choice — **type after name** allows:

* easier reading of chains,
* simpler perception of generics and ownership modifiers (`name: own T`, `arg: &mut Foo`).

And yes, we know we could have done `Type name`. We just didn't.

### `Promise<T>` vs `Task<T>`

In TypeScript:

```ts
async function load(): Promise<Data> { ... }
await load();
```

In Surge:

```sg
async fn load() -> Data {
    ...
}

let task: Task<Data> = load();
let data = task.await();
```

`Task<T>` is a regular type, `await()` is a method, not a separate operator; the async/await model is built into types and structured concurrency, not bolted on top of everything.

### `any`, `unknown`, `never` vs meaningful types

In TypeScript you can write `any` and simply **turn off** the type system.

In Surge:

```sg
// No direct analogue to any
// If you really need "anything" — that's a separate story and contract.
```

The type system doesn't give you a single "back door" that magically bypasses all checks. If you want to fool the compiler, you'll have to do it honestly and explicitly.

### Decorators vs attributes

In TypeScript:

```ts
@Log
class Service { ... }
```

In Surge:

```sg
@pure
fn add(x: int, y: int) -> int { return x + y; }
```

We have attributes, but they are:

* a **closed set** (you can't suddenly invent `@Magic` and rewrite half the language),
* only hints to the compiler — no hidden transformations,
* statically checked for compatibility and applicability.

Surge is not TypeScript: we love structure, but we don't love compile-time magic.

---

## Surge is not Go

Go gave Surge a lot — simplicity, tooling, the philosophy of "tooling as part of the language".
But from there our paths diverge.

### GC vs ownership

In Go:

```go
type User struct {
    ID   int
    Name string
}
```

and then GC handles everything.

In Surge:

```sg
type User = { id: int, name: string };

fn handle_user(user: own User) {
    // user will be freed when it goes out of scope
}
```

Life without GC:

* no pauses,
* explicit ownership (`own T`, `&T`, `&mut T`),
* uniform rules that work for both system code and high-level.

### Everything has to start somewhere

We really liked Go's idea that everything has a default value.

Surge has a built-in `default<T>() -> T`:

```sg
@intrinsic
fn default<T>() -> T;

let x: int = default<int>();  // 0
let ok: bool = default<bool>(); // false
```

And even:

```sg
type Point = { x: int, y: int };

let p: Point = default<Point>(); // both fields default
```

We really love when "nothing" is still **something meaningful**.
Even if it's just zero. Or an empty string. Or `nothing`.

### `error` vs `Erring<T, E>`

In Go:

```go
val, err := Do()
if err != nil {
    return err
}
```

In Surge:

```sg
fn do() -> int! {
    ...
}

let result = do();
compare result {
    Success(v) => print("ok", v);
    err        => print("err:", err);
}
```

We don't split result and error with a comma — we have one type that honestly says "either value or error".

### Tooling: similar UX, different meaning

`go fmt`, `go vet`, `go build` →

`surge fmt`, `surge diag`, `surge parse`, `surge tokenize`, `surge fix`.

But:

* `diag` — is an honest frontend with semantics and diagnostics,
* there are tracing modes, Chrome Trace Viewer, ndjson for CI,
* everything is tuned for transparency, not just "build a binary fast".

---

## Surge is not Python

Surge is heavily inspired by the Python feeling: "code reads like text".
But it doesn't share Python's love for "let everything sort itself out at runtime".

### Exceptions vs Erring

In Python:

```py
def parse(s: str) -> int:
    return int(s)  # ValueError to taste
```

In Surge:

```sg
fn parse(s: string) -> int! {
    if (s == "42") { return 42; }
    let e: Error = { message: "bad", code: 1 };
    return e;
}
```

We don't have hidden throws — everything is in the type. The error is visible in the signature, not in production.

### Yes, we have `nothing`

Python loves `None`, other languages love `null`.  
Everyone has their own philosophy of "nothing".

Surge has an honest separate type `nothing` with a single value `nothing`.

```sg
fn do_nothing() -> nothing {
    return nothing;
}
```

Sometimes you just need to return "nothing" — not "0", not `false`, not an empty string.
Just "there's nothing here", with no subtext.

`nothing` is not an error and not a special case.
It's simply the absence of a value, which is honestly visible in the type.

### Dynamic vs static (but without fanaticism)

* In Python types are optional and easily ignored.
* In Surge types are a basic tool, but sugar (`T?`, `T!`, tagged unions, literal enums) makes it tolerable for humans.

And yes, a Data Scientist who sees Surge might say:
"You seriously think this is normal syntax?" —
we'll just let them look at Rust and say: "wait, it gets better".

### Magic dunder methods vs honest magic methods

In Python:

```py
class Vector:
    def __add__(self, other): ...
```

In Surge:

```sg
extern<Vector> {
    fn __add(self: &Vector, other: &Vector) -> Vector { ... }
}
```

We have `__add`, `__index`, `__to` and others — also "magic", but:

* they're visible as regular functions,
* you can read them, override them in `extern<T>`,
* built-ins for primitives are sealed, but aliases can be extended.

### Surge strings

Python strings are famously flexible and come with `s[a:b]` slicing and a huge built-in API.
Surge strings stay explicit: UTF-8 storage with **code point** indexing, range slicing via `s[[a..b]]`, and a focused standard method set (`contains`, `find`, `split`, `trim`, `replace`, `reverse`, `levenshtein`).
Same idea — different rules, fewer surprises.

Surge is not Python: we love readability, but in exchange we ask for a bit of discipline.

---

## Surge is not C++

Some patterns overlap (RAII, no GC, systems programming), but that's where you can neatly pack up the similarities.

* No macro preprocessor.
* No dozen rules about ODR, ADL and friends.
* No UB as a normally accepted norm.

Instead:

* Rust-level ownership model, but without lifetimes syntax,
* strict but understandable type system,
* attributes instead of hardcore metaprogramming magic.

---

## Surge is not Java / C#

We don't live in a world of classes and inheritance.

* **no classes** in the familiar sense — there are types (`type`) and contracts (`contract`),
* **no behavior inheritance** — we have struct extension (`Base : { extra }`) and tagged unions,
* **no "everything is an object"** — there are regular values with an ownership model.

Methods hang on `extern<T>`, not inside classes. Fields are just fields, without getters/setters for every breath.

---

## Surge is not Zig

We know and love the idea "compact, honest systems language".
But we have different priorities.

* We don't have global, ubiquitous `comptime` — we're building **directives** and future **macros** as a controlled layer on top of the language.
* We invest heavily in **concurrency and diagnostics**: structured async tasks, channels, lock contracts, tracing.
* We clearly separate:

  * **attributes** — change semantics and are checked by the compiler,
  * **directives** — don't change the program, but add tooling (tests, benchmarks, docs).

---

## Surge is not Haskell

Yes, we know about monads.
Yes, `Erring<T, E>` really wants to become "yet another one".
No, we don't have `do`-notation and laziness by default.

* `compare` — simple pattern matching, not a category-theoretic ritual.
* contracts — are **structural interfaces**, not typeclass hierarchies,
* async/await — is `Task<T>` and `.await()`, not an `IO` monad (at least not officially 😉).

---

## Surge is not C

C is a low-level language used for writing system software.
Surge is a high-level language used for writing system software.

But yes, we have enums, and they work like in C.

I should warn you: Surge wasn't designed as the language you'll use to launch a rocket into space. But you can try. Tell us if you like it.

---

## Surge is not "just a scripting language"

Surge is a **systems and application language**:

* without GC, with honest ownership, RAII and a future LLVM backend;
* with async/await, tasks, channels and structured concurrency;
* with a complete story: from tokens and AST to VM and, later, native code.

But at the same time:

* it tries to be readable,
* diagnostics try to be human,
* tracing gives you X-ray vision into your code, not just a stack trace.

## A tiny language tour (or what Surge actually IS)

Yes, we've talked a lot about who Surge **isn't**.  
Time to talk a bit about what it actually can do.

### nothing: when "nothing" means "nothing"

We have the type `nothing` and the single value `nothing`.

```sg
fn maybe_find(id: int) -> int? {
    if (id == 42) {
        return 42;
    }
    return nothing;
}
```

When you see `-> nothing` or `-> T?`, you don't guess "what are they returning here — `null`, `0`, or an empty string?".
If it says `nothing`, that's literally "nothing". And that's good.

### Tuples: just ordinary ones, with `.0`, `.1`, `.2`

Our tuples don't search for the meaning of life — they just group values.

```sg
fn coords() -> (int, int) {
    return (10, 20);
}

let p: (int, int) = coords();
let x = p.0;
let y = p.1;
```

* `(int, int)` — type,
* `(10, 20)` — value,
* `.0`, `.1`, `.2` — field access.

No magical records, named fields, or hidden constructors.
Need names — take `type` and a proper struct; tuples are only for cases when the context is more important than naming the field.

### Inheritance is hard. Namespaces are even harder.

We thought long about how to give "just enough" so that:

* you can extend types,
* without turning the language into a verbose zoo of `class Base<T> : IThing, IWhatever`.

In Surge extension looks like this:

```sg
type Person = { name: string, age: int };

type Employee = Person : {
    id: uint,
    department: string,
};
```

* `Employee` inherits `Person`'s fields,
* you can add new ones,
* but we don't descend into the abyss of complex hierarchies.

If you catch yourself really missing virtual tables and diamond inheritance — maybe you're just tired and need a day off.

### Generics: we don't love them either, but they're useful

Yes, we have generics. They're neat and try not to get in the way.

```sg
fn id<T>(x: T) -> T {
    return x;
}

let a = id(42);        // T=int
let b = id("hello");   // T=string
```

Where everything is obvious — types are inferred:

```sg
fn head<T>(xs: T[]) -> Option<T> { ... }

let h = head([1, 2, 3]); // T=int
```

Where "obvious" only exists in the author's head, and the compiler doesn't have a crystal ball — it'll honestly ask for a hint:

```sg
fn make<T>() -> T;

let x = make();        // error: can't infer T
let y = make::<int>(); // now everything's clear
```

We don't adore generics either.
So where we can, we let you not write them.
And where it gets too weird without them — we ask you to write them right away and explicitly.

### Numbers: dynamic and fixed, but without fetishism

Surge distinguishes:

* families `int`, `uint`, `float`,
* fixed `int32`, `uint64`, `float32`, `float64`,
* and yet doesn't try to be an encyclopedia of bit sizes.

If you're not writing a cryptographic library or a low-level format, `int` and `float` are usually enough.

If you suddenly feel an urgent need for `int128`, look at your code and ask yourself:
maybe a different tool is needed there?

### Directives: "why?" — "because it's convenient"

Directives are like little scripts inside your code: tests, benchmarks, docs, lints, and other useful things.

```sg
/// test:
/// Addition:
///     test.eq(add(2, 3), 5);
fn add(x: int, y: int) -> int { return x + y; }
```

They:

* don't change program semantics,
* live in `///` comments,
* can be run via `--directives` flags (`collect`, `gen`, `run`),
* and use the same Surge, not a separate mini-language.

Why all this?
Take our word for it — **it's more convenient this way**.
If you don't believe us — just try it.

### Three loops: two `for` and one `while`

We didn't try to pick one "correct" loop.

We have:

```sg
// C-style for
for (let i: int = 0; i < 10; i = i + 1) {
    ...
}

// for-in
for x: int in xs {
    ...
}

// while
while (cond) {
    ...
}
```

Yes, three loops.
Yes, two kinds of `for`.
Yes, we periodically think about adding another one — just for the collection.
But we're holding back for now.

### 30+ attributes (which you'll barely notice)

Surge has a fairly rich attribute system: `@pure`, `@overload`, `@override`, `@entrypoint`, `@send`, `@nosend`, `@packed`, `@align`, `@readonly`, `@guarded_by`, `@nonblocking`, and a good dozen more.

But:

* the set is closed — you can't just invent `@wizard`,
* each attribute is strictly checked for applicability,
* most of them live where libraries and runtime need them, not where every other user does.

Yes, we don't love magic.
Yes, we still have over thirty attributes.
No, they don't do "something indescribable" for you.
They just help the compiler better understand your intentions.

### Type conversion: `to` and honest `__to`

As we already said, we use `to`:

```sg
let x: int32 = 300000;
let a: int   = x to int;    // fixed -> dynamic, safe
let b: int16 = x to int16;  // may panic if it doesn't fit
```

Under the hood this calls `__to(self, target)` from `extern<T>` or a built-in intrinsic.

So:

* `Egg to Bird` — normal attempt,
* `Egg to Amphibian` — your personal responsibility,
* `int to int16` might at some point honestly say: "no, I don't fit" and cause a panic.

`__to` never gives you a "slightly wrong result".
It either returns a correct value or crashes execution — but never pretends everything is fine.

### What does `@pure` actually do?

```sg
@pure
fn add(x: int, y: int) -> int {
    return x + y;
}
```

`@pure` is not a magic optimizer.

It simply:

* tells the compiler: "this function has no side effects",
* allows paranoid mode to check that you're not writing to global variables or doing I/O,
* gives the green light to aggressive optimizations and caching.

So `@pure` doesn't make the function pure.
It just politely warns: "if it turns out to be impure — that's your problem now, but the compiler will try to notice".

### `@overload`: yes. What's the problem?

```sg
fn print_int(x: int) { ... }

@overload
fn print_int(x: string) { ... }
```

Yes, we have `@overload`.
Yes, we're aware of all the memes about function overloading and overload resolution pain.

We just:

* required the first version of the function to be without `@overload`,
* made a clear and deterministic candidate selection algorithm,
* forbade mixing `@override` and `@overload` in the same place.

If you don't abuse it — everything works simply.
If you do abuse it — that's also a form of communication with your future self.

---

## Surge is also not:

Since we started:

* Surge is not PHP — we don't have `$` before every variable, but we do have `@` before attributes.
* Surge is not Brainfuck — though sometimes that's how you might perceive an early version of any specification.
* Surge is not Excel — we'll leave formulas for later, first let's finish the compiler.
* Surge is not YAML — spaces matter, but not **that much**.
* Surge is not Jira — we don't have tickets, only diagnostics with codes.

And yes, Surge isn't even "the next Rust", "the next Go", or "the next Zig".
Surge is just a language that honestly says:

> "Write code. Making mistakes is allowed.
> I'll just help you understand *where* exactly."
