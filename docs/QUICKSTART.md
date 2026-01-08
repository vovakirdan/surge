# QUICKSTART

This document is a **practical introduction to Surge**.

It is not a language reference.
It is not a specification.
It is not a showcase gallery.

This is a small book of **runnable examples** that show how to write real programs in Surge and what to expect from the language.

Copy the code. Run it. Change it. Break it. Fix it.

---

## 1. Your first program

Let’s start with the smallest possible Surge program.

```surge
@entrypoint
fn main() {
    print("Hello, World!");
}
```

Save this as `hello.sg` and run:

```bash
surge run hello.sg
```

Or compile it into a binary:

```bash
surge build hello.sg
./target/debug/hello
```

### What is `@entrypoint`?

`@entrypoint` marks **the function where program execution starts**.

* The function name does **not** have to be `main`
* There must be **exactly one** entrypoint
* This is not magic — the compiler just generates a small wrapper and calls your function

Think of `@entrypoint` as:

> “This is where my program begins.”

---

## 2. Variables, functions, and control flow

Surge looks familiar if you come from C, Go, or Rust.

```surge
fn add(a: int, b: int) -> int {
    return a + b;
}

@entrypoint
fn main() {
    let x: int = 10;
    let y: int = 32;

    let result = add(x, y);
    print(result to string); // yes, something converting TO target. Not as (something). Not from (something). Expression expands to call method __to(result, string).
}
```

Mutable variables must be marked explicitly:

```surge
let mut sum: int = 0;

for i in 0..10 {
    sum = sum + i;
}

print(sum to string);
```

Control flow is straightforward:

```surge
if sum > 10 {
    print("big");
} else {
    print("small");
}
```

No hidden returns.
`return` is always explicit.
So this is Rusty:
```rust
fn foo() -> int {
    let x = 42;
    x
}
```

But this is Surge:
```surge
fn foo() -> int {
    let x = 42;
    return x;
}
```

---

## 3. Types you will use all the time

You don’t need to learn many types to be productive.

Common built-in types:

* `int`, `uint`, `float`
* `bool`
* `string`
* arrays: `T[]`

Fixed-width types:

* `int8`, `int16`, `int32`, `int64`
* `uint8`, `uint16`, `uint32`, `uint64`
* `float16`, `float32`, `float64`

Example:

```surge
@entrypoint
fn main() {
    let numbers: int[] = [1, 2, 3, 4];

    print(len(numbers) to string);
    print(numbers[0] to string);

    let text: string = "hello";
    print(len(text) to string);
}
```

Arrays are indexed from zero.
Strings are UTF-8 and indexed by **code points**, not bytes.

This means that reversing a string will give you the phrase backwards:
```surge
let text: string = "hello";
print(text.reverse()); // "olleh"
```

As expected.

---

## 4. Ownership and borrowing (in one page)

Surge has **no garbage collector**.
Instead, it uses explicit ownership and borrowing.

You will see three forms very often:

* `T` / `own T` — owning value
* `&T` — shared borrow (read-only)
* `&mut T` — exclusive mutable borrow

Yes, I also got confused at first, but it's actually quite simple.

### A common mistake

```surge
fn push_value(xs: int[], value: int) {
    xs.push(value);
}
```

This looks fine, but it **does not compile**.

You may see an error like:

```
error: cannot mutate borrowed value (SEM3022)
help: consider taking '&mut int[]'
```

Why?
Because `xs` is passed **by value**, and mutation requires exclusive access.

### The correct version

```surge
fn push_value(xs: &mut int[], value: int) {
    xs.push(value);
}

@entrypoint
fn main() {
    let mut data: int[] = [1, 2, 3];
    push_value(&mut data, 4);
    print(len(data) to string);
}
```

Rules of thumb:

* If a function **reads**, use `&T`
* If a function **mutates**, use `&mut T`
* If a function **takes ownership**, use `own T`

The compiler enforces this.
That’s why many bugs never make it to runtime.

---

## 5. Structs and simple data modeling

You define data using `type`.

```surge
type User = {
    name: string,
    age: int,
};

fn birthday(user: &mut User) {
    user.age = user.age + 1;
}

@entrypoint
fn main() {
    let mut u: User = { name = "Alice", age = 30 };
    birthday(&mut u);
    print(u.age to string);
}
```

Structs are plain data.
Behavior lives in functions.

But of course you can define methods on structs:
```surge
type User = {
    name: string,
    age: int,
}

extern<User> {
    fn birthday(self: &User) {
        self.age = self.age + 1;
    }
}

@entrypoint
fn main() {
    let mut u: User = { name = "Alice", age = 30 };
    u.birthday();
    print(u.age to string);
}
```

And this will lower to simple function call. Because I like functions.

---

## 6. Option — when something may not exist

Surge does not have `null`. 
Does not have `None`.
No `void`.
At first it seemed like a bad idea, but it's actually quite simple.

If something may not exist, use `Option<T>` (short form: `T?`).

```surge
fn head(xs: int[]) -> int? { // here the returned object may not exist
    if len(xs) == 0 {
        return nothing;
    }
    return xs[0];
}
```

Handling an `Option` is explicit:

```surge
@entrypoint
fn main() {
    let xs: int[] = [];

    let value = head(xs);

    compare value {
        Some(v) => print(v to string);
        nothing => print("empty");
    }
}
```

No surprises.
No hidden null checks.

---

## 7. Erring — errors are values

Surge does not use exceptions.

Errors are ordinary values of type `Erring<T, Error>`
(short form: `T!`).

```surge
fn parse_int(s: string) -> int! {
    if s == "42" {
        return 42;
    }

    let err: Error = {
        message = "not a number",
        code = 1,
    };

    return err;
}
```

Handling errors:

```surge
@entrypoint
fn main() {
    let result = parse_int("hello");

    compare result {
        Success(v) => print(v to string);
        err => {
            print("error: " + err.message);
        }
    }
}
```

No `try`.
No `catch`.
The type tells you what can fail.

Everything that was not expected - panics.

---

## 8. Pattern matching with `compare`

`compare` is Surge’s pattern matching construct.

It works with:

* `Option`
* `Erring`
* tagged unions
* simple conditions

Example:

```surge
fn describe(x: int?) -> string {
    return compare x {
        Some(v) if v > 0 => "positive";
        Some(v) => "non-positive";
        nothing => "missing";
    };
}
```

`compare` is exhaustive for tagged types.
If you forget a case, the compiler tells you.

---

## 9. Entrypoint with command-line arguments

`@entrypoint` can parse arguments for you.

```surge
@entrypoint("argv")
fn main(name: string, times: int = 1) {
    for i in 0..times {
        print("Hello " + name);
    }
}
```

Run it like this:

```bash
surge run greet.sg -- Alice 3
```

### Important note

This is **just syntax sugar**.

Conceptually, the compiler:

* reads `argv`
* parses values
* calls your function

Something like (pseudocode):

```
__surge_start:
    let args = argv();
    let name = args[0];
    let times = args[1];
    main(name, times);
```

You can always write the parsing logic yourself if you want.

---

## 10. Async: first contact

Async in Surge is explicit and structured.

```surge
async fn fetch_data() -> string {
    return "data";
}

@entrypoint
fn main() {
    let t = fetch_data();

    compare t.await() {
        Success(v) => print(v);
        Cancelled() => print("cancelled");
    }
}
```

An `async fn` returns a `Task<T>`.
Calling `.await()` waits for it.

---

## 11. Spawning tasks

You can run tasks concurrently using `task`.

```surge
async fn work(id: int) -> int {
    return id * 2;
}

@entrypoint
fn main() {
    let t1 = task work(1);
    let t2 = task work(2);

    let r1 = t1.await();
    let r2 = t2.await();

    print("done");
}
```

Important properties:

* Tasks do **not** outlive their scope
* You must await them or return them
* The compiler enforces this

This is called **structured concurrency**.
Yes, it's a finite state machine.

---

## 12. Channels

Channels let tasks communicate.

```surge
async fn producer(ch: &Channel<int>) {
    for i in 0..5 {
        ch.send(i);
    }
    ch.close();
}

async fn consumer(ch: &Channel<int>) {
    while true {
        let v = ch.recv();
        compare v {
            Some(x) => print(x to string);
            nothing => return;
        }
    }
}

@entrypoint
fn main() {
    let ch = make_channel<int>(2);

    task producer(&ch);
    task consumer(&ch);
}
```

Channels are typed.
Sending and receiving are explicit suspension points.

---

## 13. Putting it together

Here is a small program that combines several ideas:

```surge
async fn parse_and_send(ch: &Channel<int>, text: string) {
    let r = parse_int(text);

    compare r {
        Success(v) => ch.send(v);
        err => print("skip: " + err.message);
    }
}

@entrypoint("argv")
fn main(values: string[]) {
    let ch = make_channel<int>(4);

    async {
        for v in values {
            task parse_and_send(&ch, v);
        }
        ch.close();
    };

    let mut sum: int = 0;

    while true {
        let v = ch.recv();
        compare v {
            Some(x) => sum = sum + x;
            nothing => break;
        }
    }

    print("sum = " + (sum to string));
}
```

This program shows:

* entrypoint arguments
* error handling
* async tasks
* channels
* ownership and borrowing

---

## 14. What to read next

If you want to go deeper:

* [`docs/LANGUAGE.md`](LANGUAGE.md) — language overview and syntax
* [`docs/CONCURRENCY.md`](CONCURRENCY.md) — async, tasks, channels
* [`docs/DIRECTIVES.md`](DIRECTIVES.md) — tests and scenarios in code
* [`showcases/`](../showcases/) — larger examples

This quickstart is intentionally shallow.
Its goal is to help you **start writing**, not to explain everything.

---

Happy hacking 🚀
