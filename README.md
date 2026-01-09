# <div align="center">

# **Surge Programming Language**

### *Clarity. Ownership. Structure. Simplicity — without fear.*

---

**Lessons learned from Rust, Go, and Python —
but Surge is not a clone of any of them.
It is a language built for people who love to code,
not for people who love to fight compilers.**


## Quickstart

It'll be prettier with VScode extension: [Surge](https://marketplace.visualstudio.com/items?itemName=VladimirKirdan.surge-syntax-highlighting).

If you're too lazy to read the documentation (or you've already read it), let's write the first program on Surge right away.

> **Note**
>
> Surge is currently in the *developers-only* stage.
> Be prepared for the fact that:
>
> * there is no "simple installer",
> * `hello world` may not work on the first try,
> * the function you need may not yet be implemented.
>
> This is normal for the current stage of the project.
> The issue board is always open — thank you for your interest and support 🙌
> Every your attempt to write a program and leave feedback is a huge contribution to the project.

---

### Prerequisites

> If you downloaded a release from the releases page, Go is **not** required.

Surge was officially tested on **only Linux x86_64**.
There are known issues with Apple Silicon (ARM64) with ABI (layout) LLVM.
Running on other Unix systems is possible, but not guaranteed.
Working on Windows is not supported. But you can try WSL2 to run Surge on Windows. 
We do it that way.

Required dependencies:

* `go >= 1.25.1`
* `clang >= 18.1.3`
* `gcc >= 13.3.0`
* `llvm` & `lld`
* good mood and desire to win

---

### Start

1. Clone the repository:

   ```bash
   git clone https://github.com/vovakirdan/surge.git
   cd surge
   ```

2. Build Surge:

   ```bash
   make build
   ```

   *(optional)*
   If you have Linux/MacOS, you can install Surge in the system:

   ```bash
   make install-system
   ```

   This is not required for the first run.
   The command simply copies the binary to the system directory and sets the `SURGE_STDLIB` environment variable for access to the standard library.

3. Create a file `hello.sg` and open it in any editor.

4. Write the classic Hello World:

   ```surge
   @entrypoint
   fn main() {
       print("Hello, World!");
   }
   ```

5. Run the program:

   ```bash
   surge run hello.sg
   ```

6. Or compile it to a binary:

   ```bash
   surge build hello.sg
   ./target/debug/hello
   ```

7. Congratulations — you have just written your first program on Surge 🎉
   You are great.

Detailed Quickstart is available in [docs/QUICKSTART.md](docs/QUICKSTART.md).

</div>

---

# Table of Contents

1. [What is Surge?](#what-is-surge)
2. [Why Surge? (The Heart of This Language)](#why-surge)
3. [Design Philosophy](#design-philosophy)
4. [Core Ideas](#core-ideas)
5. [A Taste of Surge — 40-line Example](#example)
6. [Modules & Project Model](#modules)
7. [The Surge Toolchain](#toolchain)
8. [Controlled Magic: The Attribute System](#attributes)
9. [Where Surge Is Going (Architecture & Vision)](#future)
10. [Documentation](#documentation)
11. [Status](#status)
12. [Big thanks to Go language](#thanks-to-go)

---

# <a name="what-is-surge"></a>1. What is Surge?

**Surge is a modern systems & application programming language** with strong static guarantees, clear ownership rules, and structured concurrency — but designed to feel *simple*, *obvious*, and *friendly*.

If you’ve ever wished Rust was more forgiving, Go was more expressive, and Python was more structured —
Surge lives somewhere in that triangle.

It is:

* strict, but not cruel,
* powerful, but not magical,
* safe by design, but still human-centric.

Surge doesn’t expect you to be a compiler engineer.
Surge expects you to be a programmer.

It’s written by a man who is just a programmer like you and know what it feels like to debug at 3 a.m., to fight a GC pause in the middle of a latency budget, or to wonder why a borrow checker chose violence today. Surge’s answer is: **be explicit, keep the rules small, keep the tone kind.**

It respects the lessons learned from those languages — borrow checking from Rust, clear tooling from Go, and the welcoming ergonomics of Python — while consciously avoiding their dogmas. Surge picks ideas because they serve the user, not because a committee decreed them.

---

# <a name="why-surge"></a>2. Why Surge?

### *(The Heart of This README — read this part slowly.)*

Surge was created from one simple desire:

> **“I want a language that helps me write good code —
> not a language that punishes me for not being a genius.”**

Let’s be honest:
most modern languages fall into two extremes:

* **too permissive** (your mistakes silently become bugs),
* **or too strict** (your mistakes become 37 compiler errors and existential crisis).

Surge tries to take the third path:

### ✔ Strict enough to protect you.

### ✔ Simple enough to not terrify you.

### ✔ Explicit enough to teach good style.

### ✔ Flexible enough to stay out of your way.

Surge is built on a handful of intentional decisions:

No philosophy slide decks, no mysterious “best practices” — just a language that explains itself while you type. Diagnostics come with numeric codes *and* actionable hints; tracing is built-in; the compiler wants to collaborate, not interrogate.

---

## **2.1 No Garbage Collector**

Not because GC is bad — it's great!
But Surge is about control. Predictability. Explicit memory ownership.

And ownership doesn’t have to be scary.
In Surge, it’s *clean, readable, boring* — exactly as it should be.

`own T` means “this value is mine”; `&T` means “I’m only looking”; `&mut T` means “I’m the only one mutating right now”.
Borrow scopes are lexical and obvious. If you need to end a borrow early, say so with an explicit `@drop binding;` — no hidden lifetimes.
Only owning values can cross task boundaries, so concurrency stays sound without requiring a theorem prover in your head.

Boring ownership is the best ownership.

---

## **2.2 Structured Concurrency**

Async/await in Surge is predictable:

* tasks don’t outlive their scope,
* no “task-and-forget”,
* clear ownership across tasks (only `own T` crosses the boundary),
* channels as first-class primitives,
* cancellation that actually returns a value (`Cancelled`) instead of silently tearing down state.

It’s a “grown-up” model, but expressed very simply.

Single-threaded cooperative scheduling today, a path to parallel backends tomorrow. Tasks are just `Task<T>` values; `.await()` is a method, not a keyword; `async { ... }` blocks enforce structured concurrency by waiting for every task. If you need to yield in a CPU-bound loop, `checkpoint().await()` is there instead of hoping for preemption.

---

## **2.3 No hidden magic**

Surge avoids magic except where it’s helpful.

The only intentional “escape hatch" is the attribute system —
and even there, attributes don't conjure behind your back.
They only prompt the compiler, without substituting the behavior.

Even the “magic” operators are honest: operators resolve to explicit magic methods like `__index`, `__add`, `__to`; you can open the stdlib and see their definitions. No hidden coercions, no implicit trait lookups — if the language does something for you, it tells you how.

---

## **2.4 extern<T> instead of methods inside types**

Surge separates:

* **data** (structs),
* **behavior** (methods in externally declared namespaces).

It keeps types clean and promotes clarity:
*what data is, is not coupled with what data does.*

`extern<T>` blocks are single-purpose: fields, functions, attributes. No nested declarations, no random top-level items. Methods are statically dispatched, generic parameters are explicit, and overrides are marked with `@override` so intent is obvious. The type stays a data container; behavior lives nearby, not inside.

---

## **2.5 Contracts instead of “classes”**

Structural typing without the ceremony.
If your type has the required fields/methods — it satisfies the contract.
No inheritance gymnastics.

```surge
contract Pet<T> {
    field name: string;
    field age: int;
    fn bark(self: T) -> nothing;
}

fn feed<T: Pet<T>>(pet: T) -> nothing {
    pet.bark();
}

type Dog = {
    name: string;
    age: int;
}

extern<Dog> {
    fn bark(self: &Dog) -> nothing {
        print("woof");
    }
}

let dog = Dog { name = "Rex", age = 3 };
feed(dog); // func accepts any value that satisfies the contract
```

---

## **2.6 A language built by a developer, not a language committee**

Surge isn’t trying to be next Rust, or next Go, or next Zig.
Surge is trying to be **Surge**:

* honest,
* understandable,
* helpful,
* consistent,
* and pleasant to write.

It’s a language written with the philosophy:

> **“Don't be afraid to make mistakes. The language will tell you where to go.”**

So Surge meets you where you are: readable syntax, lowercase keywords, diagnostics with context, and a module system you can sketch on a napkin. The goal is to let you focus on architecture and algorithms, not on appeasing a parser spirit.
Surge respects Rust’s ownership clarity, Go’s approachability, and Python’s readability, while consciously declining to copy their trade-offs wholesale.

---

# <a name="design-philosophy"></a>3. Design Philosophy

Surge is guided by a small set of principles:

### **Explicit over implicit**

If something is happening — you see it.

Type annotations are postfix (`name: Type`), casts are spelled out with `to`, and ownership modifiers (`own`, `&`, `&mut`, `*`) live in the type, not hidden behind sigils. Even the sugar (`T?`, `T!`) is strictly type-position-only.

### **Zero-cost abstractions (but understandable ones)**

If something looks simple — it *is* simple.

Magic methods are just functions; contracts are structural, not class hierarchies; async functions desugar to state machines you could almost write by hand. You can always trace where performance comes from.

### **Ownership clarity**

If a value moves — it’s visible.
If it borrows — it’s visible.

Borrow lifetimes are lexical, the borrow checker tells you where the conflict is, and only `own` values cross thread or task boundaries. When you need to end a borrow early, there is a literal `@drop expr;` statement instead of ritual incantations.

### **Structured concurrency**

Async code shouldn't be smuggled into memory.

`async fn` returns `Task<T>`, `.await()` is explicit, `spawn` returns a handle you must either await or store. No loose tasks leaking into the void. The event loop is cooperative, honest about blocking, and ready for a future parallel runtime without changing user code.

### **No surprises**

If the code looks like it should work, it works.
If Surge forbids it, it explains *why*, not “go think.”

Diagnostics carry numeric codes, human text, and fix suggestions. Tracing can be turned on to show every phase of compilation. The goal is transparency over cleverness.

### **Practical simplicity**

Simple ≠ dumb.
Simple means “I understand what’s happening here”.

Surge resists clever syntactic contortions. It prefers a couple more characters if they make intent obvious. That’s not minimalism for its own sake; it’s empathy for the reader — including Future You.

---

# <a name="core-ideas"></a>4. Core Ideas in Plain English

This is Surge in one breath:

* **Ownership:** predictable move semantics, no GC.
* **Borrowing:** clear `&T` / `&mut T` rules.
* **Contracts:** structural interfaces with zero ceremony.
* **Tags:** sum types a.k.a. tagged unions — built-in.
* **Async/await:** stackless coroutines + structured concurrency.
* **Tasks:** parent-scope lifetime, explicit cancellation.
* **Channels:** first-class primitive for async pipelines.
* **Extern blocks:** define behavior near types, not inside them.
* **Attributes:** the only allowed “magic”, controlled and explicit.
* **Diagnostics:** compiler is strict but polite.
* **Modules:** clean, simple, not a zoo of lib/bin/pkg.

---

# <a name="example"></a>5. A Taste of Surge — 40-line Example

Below is a realistic snippet combining:

* async/await,
* spawn,
* channels,
* ownership,
* tags,
* contracts,
* extern<T>,
* structured concurrency.

And yet — everything reads clearly.

```sg

// A simple contract – anything fetchable must implement fetch()
contract Fetchable {
    fn fetch(self: &Fetchable) -> Task<Erring<string, Error>>;
}

// Data type + external behavior
type Endpoint = { url: string };

extern<Endpoint> {
    async fn fetch(self: &Endpoint) -> Erring<string, Error> {
        let result = http_get(self.url).await();
        compare result {
            Success(text)  => return Success(text);
            err => return Error;
        }
    }
}

// Worker pipeline using channels
async fn pipeline(endpoints: Endpoint[]) -> Success<string>[] {
    let ch = make_channel<Erring<string, Error>>(10);

    // Producer: spawn fetchers
    async {
        for ep in endpoints {
            spawn async {
                let out = ep.fetch().await();
                send(&ch, out);
            };
        }
    };

    // Consumer: collect only Success results
    let mut results: Success<string>[] = [];

    // When channel closes, recv() returns nothing
    while let Some(msg) = recv(&ch) {
        compare msg {
            Success(v)  => results.push(Success(v));
            finally => { /* ignore failures */ }
        }
    }

    return results;
}
```

If this example looks readable —
that’s the whole point.
It shows ownership moves (`spawn` takes `ep` by value),
borrows (`recv(&ch)` is explicit),
and structural typing (`contract Fetchable`) without ornamentation.
You can drop `@drop` inside a loop if you need to end a borrow early, or mark the function `@failfast` to auto-cancel tasks on the first error — but only when you ask for it.

### Want to see more?

The [`showcases/`](showcases/) directory contains many runnable examples. Here are a few highlights:

*   **[Hello World](showcases/01_hello_world)** — Minimal entrypoint and printing.
*   **[Async Pipeline](showcases/async/04_pipeline_3stage)** — 3-stage processing using channels and tasks.
*   **[Tagged Unions](showcases/26_state_machine_tagged)** — State machine implementation using sum types.
*   **[Generics](showcases/28_generic_map_filter)** — Writing generic `map` and `filter` functions.
*   **[Error Handling](showcases/25_erring_parser)** — Robust input loop with `Result` types.
*   **[Contracts](showcases/29_contract_printable)** — Implementing structural interfaces for custom types.
*   **[BigInt Math](showcases/21_bigint_stress)** — Arbitrary precision integers (Fibonacci calculation).
*   **[Fan-out / Fan-in](showcases/async/02_fanout_fanin)** — Spawning multiple workers and aggregating results.
*   **[Timeout & Race](showcases/async/08_timeout_race)** — Advanced async control flow with deadlines.

---

# <a name="modules"></a>6. Modules & Project Model

Surge has **one** module system.

No:

* lib vs bin,
* packages vs crates vs assemblies,
* hidden “magical” directories.

Just **a module**.

Modules are declared implicitly by folder, or explicitly with:

```sg
pragma module::feature;
```

A module is simply:

* a named namespace,
* with its own files,
* importing other modules,
* producing either a binary or a library *depending only on presence of @entrypoint*,
* optionally unified across multiple files with `pragma module;` in each file when a directory is shared.

You can rename a module (`pragma module::bounded;`), declare `pragma no_std;` to live without the stdlib, or mark the whole directory as a directive module. No implicit “magic folders”; everything is spelled out at the top of the file.

That’s it.
No hierarchy madness. No guessing.

---

# <a name="toolchain"></a>7. The Surge Toolchain

### *(Transparency as a core feature)*

Surge ships with a tool that embraces openness:

```
surge diag        → run semantics, parse, typecheck, diagnostics
surge tokenize    → see raw tokens
surge parse       → show the full AST
surge fix         → auto-apply safe fixes
surge fmt         → format code
surge init        → create a basic project
surge build       → build an LLVM backend binary (clang/llvm required) or a VM wrapper with --backend=vm
```

LLVM builds are invoked with `surge build <path>` (the default). They emit MIR/LLVM dumps into `target/debug/.tmp/` when requested and invoke clang for linking. If clang/llvm are missing, the command prints an install hint for Ubuntu.

But the star of the show:

## **diag + tracing**

Surge includes one of the most transparent tracing systems of any modern compiler:

* phase-level timing,
* detail-level tracing for dependency resolution,
* debug-level AST walk tracing,
* Chrome Trace Viewer support,
* ring-buffer tracing for hang debugging,
* heartbeat events to locate infinite loops,
* ndjson output for CI.


`--trace-level` ranges from `phase` to `debug`, `--trace-mode` can stream, ring, or both, and a heartbeat keeps ticking even if the compiler stalls so you know where it froze.
Diagnostics include fix-suggestions where safe, and directive code lives in real Surge so tests and benchmarks are the same language you ship.

It’s not just diagnostics —
it’s *X-ray vision* for understanding your own code.

Attributes like `@pure` are enforced; concurrency contracts like `@guarded_by` are checked; lock ordering and task leaks are diagnosed. All of that is surfaced through `surge diag` with trace files you can load into Chrome Trace Viewer when you feel like spelunking.

Because a language should help you see more, not hide more.

---

# <a name="attributes"></a>8. Controlled Magic: The Attribute System

Surge has exactly **one** place where “magic” is allowed:
**attributes**.

Attributes are tiny compiler hints that declare intent:

* `@entrypoint`
* `@intrinsic`
* `@overload`
* `@pure`
* `@backend("gpu")`
* `@sealed`, `@packed`, `@align`, `@readonly`
* …and a few others.

But they never hide behavior.
They never rewrite your code.
They never perform silent transformations.

They are simply small, explicit knobs to help the compiler help you.

---

# <a name="future"></a>9. Where Surge Is Going

### *(Architecture & Vision)*

Surge is **young and evolving quickly**.
Not unstable — just alive.

Here’s the short roadmap:

### **v1.x**

* full frontend (AST, type system, semantics),
* VM execution backend,
* directive system (tests, benchmarks, docs),
* AST reflection for lints & analysis,
* improved concurrency primitives.

### **v1.5 → v2**

* real multithreading,
* LLVM backend for true native performance,
* macro system (structural code generation),
* improved channels & select,
* WASM backend.

Surge isn’t trying to promise the moon.
Surge is trying to **grow thoughtfully**:
clear steps, clear architecture, no feature bloat.

---

# <a name="documentation"></a>10. Documentation

All detailed docs live in:

```
/docs
    LANGUAGE.md
    DIRECTIVES.md
    ATTRIBUTES.md
    CONCURRENCY.md
    MODULES.md
    PRAGMA.md
    TRACING.md
    PARALLEL.md
```

This README is the philosophy,
docs are the how-to and the contracts.

If you want to know how ownership rules are enforced, how `extern<T>` behaves, how modules merge across files, or how async tasks are scoped, the `/docs` folder is the canonical, evolving spec. Surge keeps its documentation close to the code so the philosophy and the mechanics stay in sync.

---

# <a name="status"></a>11. Status

**Surge is actively developed.
It is young, evolving quickly, and open for exploration.**

The language is already capable of writing meaningful programs,
and the compiler is designed with strong correctness guarantees.

But the journey is just beginning —
and the main thing about this journey is that it's honest, open, and doesn't require you to be someone else.

Expect rough edges, but also expect the compiler to own them. If something is missing, it will say so. If something is wrong, it will explain. Surge would rather be transparent and slightly unfinished than opaque and “done”.

Write the code.
Be yourself.
Surge will back you up.

# <a name="thanks-to-go"></a> 12. **✨ Thanks to Go**

Surge owes a quiet but enormous debt to **Go**, and it deserves to be said explicitly.

Go taught me that a programming language can be **simple without being simplistic**, clean without being sterile, and powerful without performing acrobatics. It showed that developer experience matters just as much as raw performance — and sometimes even more.

Surge wouldn't look the way it does without Go’s influence:

* **Goroutines** inspired the structured concurrency model.
  Not by copying, but by understanding the value of lightweight, honest tasks.

* **The Go toolchain** demonstrated what “one tool, many commands” can feel like.
  No fragmented ecosystem, no guessing which binary to invoke.

* **Cobra** gave Surge’s CLI the confidence and ergonomics it needed.
  No hand-rolled parsers, no accidental complexity, no layers of ceremony.

* **Go’s project layout** taught us that a filesystem can *be* a module system,
  if the language is disciplined enough.

* **The testing and benchmarking framework** reminded us that correctness and performance
  should live right next to the code, not in a distant CI pipeline.

* **Go’s fuzzing tools** helped find parser edge cases long before the language had a name.

This isn’t about comparing languages or declaring spiritual successors.
It’s about gratitude.

Surge learns from Go the same way it learns from Rust and Python:
by taking the things that make life better,
and leaving behind everything that gets in the way of clarity.

**So yes — thank you, Go.
You helped shape Surge more than you know.**
