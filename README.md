# <div align="center">

# **Surge Programming Language**

### *Clarity. Ownership. Structure. Simplicity — without fear.*

---

**Lessons learned from Rust, Go, and Python —
but Surge is not a clone of any of them.
It is a language built for people who love to code,
not for people who love to fight compilers.**

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

---

# <a name="why-surge"></a>2. Why Surge?

### *(The Heart of This README — read this part slowly.)*

Surge was created from one simple desire:

> **“I want a language that helps me write good code —
> not a language that punishes me for not being a genius.”**

Let’s be honest:
most modern languages fall into two extremes:

* **too permissive** (your mistakes silently become bugs),
* **or too strict** (your mistakes become 37 compiler errors and existential crysis).

Surge tries to take the third path:

### ✔ Strict enough to protect you.

### ✔ Simple enough to not terrify you.

### ✔ Explicit enough to teach good style.

### ✔ Flexible enough to stay out of your way.

Surge is built on a handful of intentional decisions:

---

## **2.1 No Garbage Collector**

Not because GC is bad — it's great!
But Surge is about control. Predictability. Explicit memory ownership.

And ownership doesn’t have to be scary.
In Surge, it’s *clean, readable, boring* — exactly as it should be.

Boring ownership is the best ownership.

---

## **2.2 Structured Concurrency**

Async/await in Surge is predictable:

* tasks don’t outlive their scope,
* no “spawn-and-forget”,
* clear ownership across tasks,
* channels as first-class primitives.

It’s a “grown-up” model, but expressed very simply.

---

## **2.3 No hidden magic**

Surge avoids magic except where it’s helpful.

The only intentional “escape hatch" is the attribute system —
and even there, attributes don't conjure behind your back.
They only prompt the compiler, without substituting the behavior.

---

## **2.4 extern<T> instead of methods inside types**

Surge separates:

* **data** (structs),
* **behavior** (methods in externally declared namespaces).

It keeps types clean and promotes clarity:
*what data is, is not coupled with what data does.*

---

## **2.5 Contracts instead of “classes”**

Structural typing without the ceremony.
If your type has the required fields/methods — it satisfies the contract.
No inheritance gymnastics.

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

---

# <a name="design-philosophy"></a>3. Design Philosophy

Surge is guided by a small set of principles:

### **Explicit over implicit**

If something is happening — you see it.

### **Zero-cost abstractions (but understandable ones)**

If something looks simple — it *is* simple.

### **Ownership clarity**

If a value moves — it’s visible.
If it borrows — it’s visible.

### **Structured concurrency**

Async code shouldn't be smuggled into memory.

### **No surprises**

If the code looks like it should work, it works.
If Surge forbids it, it explains *why*, not “go think.”

### **Practical simplicity**

Simple ≠ dumb.
Simple means “I understand what’s happening here”.

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
// Tags as simple union types
tag Ok<T>(value: T);
tag Err(message: string);

// A simple contract – anything fetchable must implement fetch()
contract Fetchable {
    fn fetch(self: &Fetchable) -> Task<Ok<string> | Err>;
}

// Data type + external behavior
type Endpoint = { url: string };

extern<Endpoint> {
    async fn fetch(self: &Endpoint) -> Ok<string> | Err {
        let result = http_get(self.url).await();
        compare result {
            Ok(text)  => return Ok(text);
            Err(msg) => return Err("Network error: " + msg);
        }
    }
}

// Worker pipeline using channels
async fn pipeline(endpoints: Endpoint[]) -> Ok<string>[] {
    let ch = make_channel<Ok<string> | Err>(10);

    // Producer: spawn fetchers
    async {
        for ep in endpoints {
            spawn async {
                let out = ep.fetch().await();
                send(&ch, out);
            };
        }
    };

    // Consumer: collect only Ok results
    let mut results: Ok<string>[] = [];

    // When channel closes, recv() returns nothing
    while let Some(msg) = recv(&ch) {
        compare msg {
            Ok(v)  => results.push(Ok(v));
            Err(_) => { /* ignore failures */ }
        }
    }

    return results;
}
```

If this example looks readable —
that’s the whole point.

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
pragma module("my.project.core");
```

A module is simply:

* a named namespace,
* with its own files,
* importing other modules,
* producing either a binary or a library *depending only on presence of @entrypoint*.

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
surge build       → (stub) future VM/LLVM build pipeline
```

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

It’s not just diagnostics —
it’s *X-ray vision* for understanding your own code.

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
```

This README is the philosophy,
docs are the how-to and the contracts.

---

# <a name="status"></a>11. Status

**Surge is actively developed.
It is young, evolving quickly, and open for exploration.**

The language is already capable of writing meaningful programs,
and the compiler is designed with strong correctness guarantees.

But the journey is just beginning —
and the main thing about this journey is that it's honest, open, and doesn't require you to be someone else.

Write the code.
Be yourself.
Surge will back you up.
