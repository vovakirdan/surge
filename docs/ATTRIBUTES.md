# Surge Language Attributes

Attributes in Surge are declarative annotations that allow you to specify additional properties and constraints for program elements (functions, types, fields, parameters). Attributes start with the `@` symbol and are placed before the declaration of an element.

## Contents

1. [General Principles](#general-principles)
2. [Function Attributes](#function-attributes)
3. [Type Attributes](#type-attributes)
4. [Field Attributes](#field-attributes)
5. [Block Attributes](#block-attributes)
6. [Attribute Conflicts](#attribute-conflicts)
7. [Diagnostic Codes](#diagnostic-codes)

---

## General Principles

### Syntax

```sg
@attribute
fn example() {}

@attribute(parameter)
type Data = { ... }

@attribute1
@attribute2
fn multiple_attributes() {}
```

### Applicability

Each attribute is only applicable to specific targets:

- **Fn** — functions and methods
- **Type** — type definitions
- **Field** — struct fields
- **Param** — function parameters
- **Block** — code blocks (extern<T>)
- **Stmt** — statements

Trying to apply an attribute to an invalid target will trigger a compile error.

### Validation

The Surge compiler performs three levels of attribute validation:

1. **Applicability to the Target** — checks that the attribute is used on the correct element
2. **Conflicts** — detects incompatible combinations of attributes
3. **Parameters** — validates attribute arguments

---

## Function Attributes

### @pure

**Target:** Functions  
**Parameters:** None

Marks a function as "pure" — has no side effects, does not alter global state, does not perform I/O. The result depends only on the input parameters.

```sg
@pure
fn add(x: int, y: int) -> int {
    return x + y;
}
```

**Guarantees:**
- The compiler may cache results
- Safe for parallel execution
- The optimizer may reorder calls

**Restrictions:**
- Cannot call non-@pure functions
- Cannot mutate global state
- Cannot perform I/O operations

---

### @overload

**Target:** Functions  
**Parameters:** None

Allows defining multiple signatures for a function with the same name. The first declaration should not have @overload.

```sg
fn process(x: int) -> int {
    return x * 2;
}

@overload
fn process(x: String) -> String {
    return x;
}

@overload
fn process(x: float, y: float) -> float {
    return x + y;
}
```

**Rules:**
- The first function declaration must not have @overload
- Signatures must be distinguishable by parameter types
- Call resolution happens statically at compile time

---

### @override

**Target:** Functions
**Parameters:** None

Overrides an existing function or method implementation.

```sg
type Base = { x: int };
type Derived = Base : { y: int };

extern<Derived> {
    @override
    fn to_string(self: &Derived) -> string {
        return "Derived";
    }
}
```

**Use cases:**

1. **Inside `extern<T>` blocks** — overrides a method for a type
2. **Outside `extern<T>` blocks** — overrides a local function declared earlier in the same module without a body

**Example of overriding local function:**

```sg
// Forward declaration (no body)
fn encode_frame(buf: &byte[], out: &mut byte[]) -> uint;

// ... (other code)

// Local implementation (same module)
@override
fn encode_frame(buf: &byte[], out: &mut byte[]) -> uint {
    // real body
    return 0:uint;
}
```

**Restrictions:**
- Signature must match the overridden function/method
- Incompatible with `@overload`

---

### @intrinsic

**Target:** Functions  
**Parameters:** None

The function is implemented directly by the compiler, with no body defined in Surge.

```sg
@intrinsic
fn __builtin_add(x: int, y: int) -> int;
```

**Restrictions:**
- Only in the `core` module
- Function must not have a body
- Allowed names: `rt_alloc`, `rt_free`, `rt_realloc`, `rt_memcpy`, `rt_memmove`, `__*`

---

### @entrypoint

**Target:** Functions
**Parameters:** Optional string — mode (`"argv"`, `"stdin"`)

Marks a function as the program's entry point (main).

```sg
@entrypoint
fn main() {
    println("Hello, Surge!");
}
```

**Modes:**

- **No mode** (`@entrypoint`): Function must be callable with no arguments. All parameters must have default values.
- **`"argv"`** (`@entrypoint("argv")`): Parameters without defaults must implement `FromArgv` contract (have `from_str(string) -> Erring<T, Error>` method).
- **`"stdin"`** (`@entrypoint("stdin")`): Parameters without defaults must implement `FromStdin` contract (have `from_str(string) -> Erring<T, Error>` method).
- **`"env"`**, **`"config"`**: Reserved for future use (will produce FUT7003/FUT7004 errors).

**Return type requirements:**

The return type must be one of:
- `nothing` (void)
- `int` (direct exit code)
- Any type implementing `ExitCode` contract (has `__to(self, int) -> int` method)

Built-in types with `ExitCode`:
- `Option<T>`: `Some(_)` → 0, `nothing` → 1
- `Erring<T, E>`: `Success(_)` → 0, `Error` → error code

**Example signatures:**

```sg
// No arguments
@entrypoint
fn main() { }

// Return exit code
@entrypoint
fn main() -> int { return 0; }

// Return Option (0 on success, 1 on nothing)
@entrypoint
fn main() -> int? { return Some(42); }

// Return Erring (0 on success, error code on failure)
@entrypoint
fn main() -> int! { return Success(0); }

// With command-line arguments (argv mode)
@entrypoint("argv")
fn main(count: int, name: string) -> int {
    // count and name are parsed from argv
    return 0;
}

// With default values (no mode needed)
@entrypoint
fn main(verbose: bool = false) { }

// Mixed: some from argv, some with defaults
@entrypoint("argv")
fn main(required: int, optional: string = "default") -> int {
    return required;
}
```

**Built-in types with `from_str`:**

The following types have built-in `from_str` implementations for `argv`/`stdin` modes:
- `int`, `uint`, `float`, `bool`, `string`
- Sized variants: `int8`, `int16`, `int32`, `int64`, `uint8`, `uint16`, `uint32`, `uint64`, `float32`, `float64`

**Diagnostic codes:**
- **SEM3121** — Unknown entrypoint mode
- **SEM3122** — `@entrypoint` without mode requires all parameters to have defaults
- **SEM3123** — Return type not convertible to exit code
- **SEM3124** — Parameter type missing `FromArgv` contract
- **SEM3125** — Parameter type missing `FromStdin` contract
- **FUT7003** — `@entrypoint("env")` reserved for future
- **FUT7004** — `@entrypoint("config")` reserved for future

---

### @backend

**Target:** Functions, blocks  
**Parameters:** String — target platform

Specifies on which platform the function should execute.

```sg
@backend("cpu")
fn sequential_process() { }

@backend("gpu")
fn parallel_process() { }

@backend("wasm")
fn web_function() { }

@backend("tpu")
fn ml_inference() { }
```

**Known platforms:**
- `"cpu"` — CPU execution
- `"gpu"` — GPU acceleration
- `"tpu"` — TPU for ML
- `"wasm"` — WebAssembly
- `"native"` — native code

**Validation:** The compiler warns about unknown platforms, but it's not an error.

---

### @nonblocking

**Target:** Functions  
**Parameters:** None

Function is guaranteed to be non-blocking — does not wait on mutexes, condition variables, or I/O.

```sg
@nonblocking
fn try_lock(lock: &Mutex) -> bool {
    // Only non-blocking operations
}
```

**Conflicts:** Incompatible with `@waits_on`

---

### @requires_lock

**Target:** Functions  
**Parameters:** String — name of the lock field

Function requires the specified lock to be held by the caller.

```sg
type ThreadSafe = {
    lock: Mutex,
    data: int,
};

extern<ThreadSafe> {
    @requires_lock("lock")
    fn get_data(self: &ThreadSafe) -> int {
        return self.data;
    }
}
```

---

### @acquires_lock

**Target:** Functions  
**Parameters:** String — name of the lock field

Function acquires the specified lock.

```sg
extern<ThreadSafe> {
    @acquires_lock("lock")
    fn lock_and_modify(self: &mut ThreadSafe) {
        // Acquires self.lock
    }
}
```

---

### @releases_lock

**Target:** Functions  
**Parameters:** String — name of the lock field

Function releases the specified lock.

```sg
extern<ThreadSafe> {
    @releases_lock("lock")
    fn unlock(self: &mut ThreadSafe) {
        // Releases self.lock
    }
}
```

---

### @waits_on

**Target:** Functions  
**Parameters:** String — name of the condition variable field

Function may block, waiting for the specified condition variable.

```sg
type Waitable = {
    condition: Condition,
    ready: bool,
};

extern<Waitable> {
    @waits_on("condition")
    fn wait_until_ready(self: &mut Waitable) {
        // May block on self.condition
    }
}
```

**Conflicts:** Incompatible with `@nonblocking`

---

### @deprecated

**Target:** Functions, types, fields  
**Parameters:** None (optionally: message)

Marks the element as deprecated. Compiler generates a warning on usage.

```sg
@deprecated
fn old_api() {
    // Deprecated function
}

@deprecated
type OldType = { ... };
```

---

### @hidden

**Target:** Functions, types, fields  
**Parameters:** None

Hides the element from the module's public API.

```sg
@hidden
fn internal_helper() {
    // Only visible inside the module
}

type Public = {
    @hidden
    internal_field: int, // usable only in methods, can't be accessed for read/write externally

    public_field: int, // accessible externally

    pub very_public_field: int, // accessible externally + on export
};
```

---

## Type Attributes

### @packed

**Target:** Types, fields  
**Parameters:** None

Packs a struct without alignment — fields are laid out sequentially in memory.

```sg
@packed
type CompactData = {
    flag: bool,     // offset 0
    value: int32,   // offset 1 (not 4!)
};
```

**Effects:**
- Minimal structure size
- Can reduce access performance
- Useful for serialization, protocols

**Conflicts:** Incompatible with `@align` on the same type

---

### @align

**Target:** Types, fields  
**Parameters:** Number — power of two (1, 2, 4, 8, 16, ...)

Specifies the alignment of a type or a field in memory.

```sg
@align(16)
type VectorData = {
    x: float,
    y: float,
    z: float,
    w: float,
};

type Mixed = {
    @align(8)
    aligned_field: int64,

    normal_field: int,
};
```

**Validation:**
- Parameter must be a positive power of two
- `@align(7)` → error SEM3064
- `@align(0)` → error SEM3064

**Conflicts:** Incompatible with `@packed` on the same type/field

---

### @sealed

**Target:** Types  
**Parameters:** None

Prevents inheritance and extension of the type.

```sg
@sealed
type FinalType = {
    data: int,
};

// ERROR: Cannot inherit from @sealed type
type Derived = FinalType : {
    extra: int,
};

extern<FinalType> {
    // ERROR: Cannot extend @sealed type
    fn violate_sealed(self: &FinalType) {
    }
}
```

**Usage:**
- API stability — prevents unexpected inheritance
- Optimization — the compiler knows the full set of subtypes

---

### @send

**Target:** Types  
**Parameters:** None

Type is safe to transfer between threads.

```sg
@send
type ThreadSafeData = {
    counter: AtomicInt,
};
```

**Requirements:**
- All fields must be @send
- No unmanaged pointers
- Atomic operations or synchronization

**Conflicts:** Incompatible with `@nosend`

---

### @nosend

**Target:** Types  
**Parameters:** None

Type cannot be transferred between threads.

```sg
@nosend
type LocalOnly = {
    ptr: own int,  // Unmanaged pointer
};
```

**Conflicts:** Incompatible with `@send`

---

### @raii

**Target:** Types  
**Parameters:** None

Type uses the RAII pattern — automatic resource management via constructor/destructor.

```sg
@raii
type File = {
    handle: int,
};

extern<File> {
    fn __init(path: String) -> File { ... }
    fn __drop(self: own File) { ... }
}
```

---

### @shared

**Target:** Types, fields  
**Parameters:** None

Shared ownership semantics (reference counting).

```sg
@shared
type SharedResource = {
    data: int,
};
```

---

### @noinherit

**Target:** Types, fields  
**Parameters:** None

On a type: prohibits other types from inheriting this type.
On a field: the field is not inherited by derived types.

```sg
@noinherit
type Standalone = {
    value: int,
};

type Base = {
    @noinherit
    private_field: int,

    public_field: int,
};

type Derived = Base : {
    // private_field not inherited
    // public_field is inherited
};
```

---

## Field Attributes

### @readonly

**Target:** Fields  
**Parameters:** None

Field is read-only after initialization.

```sg
type Container = {
    @readonly
    id: int,

    value: int,
};

fn modify(c: &mut Container) {
    c.id = 42;     // ERROR: cannot write to @readonly field
    print(c.id);   // OK
    c.value = 100; // OK
}
```

**Diagnostic:** SEM3075

---

### @atomic

**Target:** Fields  
**Parameters:** None

Field has atomic read/write operations.

```sg
type Counter = {
    @atomic
    count: int,
};
```

**Requirements:**
- Field type must support atomic operations
- Usually numeric types, pointers

---

### @weak

**Target:** Fields  
**Parameters:** None

Weak reference — does not increase reference count.

```sg
type Node = {
    @shared
    children: Array<Node>,

    @weak
    parent: Node,  // Weak reference, avoids cycles
};
```

---

### @guarded_by

**Target:** Fields  
**Parameters:** String — name of the lock field

Field access is protected by the specified lock.

```sg
type ThreadSafe = {
    lock: Mutex,

    @guarded_by("lock")
    protected_data: int,
};
```

**Validation:**
- Field `lock` must exist in the same type
- Field `lock` should be of type Mutex or RwLock (not checked yet)

---

### @arena

**Target:** Fields  
**Parameters:** String — arena name

Field is allocated in the specified memory arena.

```sg
type ArenaAllocated = {
    @arena("temp")
    temporary: int,

    @arena("persistent")
    persistent: int,
};
```

---

## Block Attributes

### @backend

**Target:** extern<T> blocks  
**Parameters:** String — target platform

All methods in the block execute on the specified platform.

```sg
@backend("gpu")
extern<Matrix> {
    fn multiply_gpu(self: &Matrix, other: &Matrix) -> Matrix {
        // GPU-accelerated multiplication
    }
}
```

---

## Attribute Conflicts

Some pairs of attributes are incompatible and cause compiler errors:

### @packed vs @align

```sg
// ERROR SEM3061: @packed conflicts with @align
@packed
@align(16)
type Conflict = { ... };
```

**Reason:** @packed removes padding, @align adds it — contradiction.

---

### @send vs @nosend

```sg
// ERROR SEM3062: @send conflicts with @nosend
@send
@nosend
type Conflict = { ... };
```

**Reason:** A type cannot be both thread-safe and not thread-safe at the same time.

---

### @nonblocking vs @waits_on

```sg
type T = { cond: Condition };

extern<T> {
    // ERROR SEM3063: @nonblocking conflicts with @waits_on
    @nonblocking
    @waits_on("cond")
    fn conflict(self: &mut T) { }
}
```

**Reason:** @nonblocking guarantees no blocking, @waits_on implies blocking.

---

## Diagnostic Codes

### Conflicts (3060-3063)

- **SEM3060** — General attribute conflict
- **SEM3061** — @packed conflicts with @align
- **SEM3062** — @send conflicts with @nosend
- **SEM3063** — @nonblocking conflicts with @waits_on

### Parameters (3064-3073)

- **SEM3064** — @align(N): N is not a power of two
- **SEM3065** — @align: invalid value (not a number)
- **SEM3066** — @backend: unknown platform
- **SEM3067** — @backend: invalid argument (not a string)
- **SEM3068** — @guarded_by: field not found
- **SEM3069** — @guarded_by: field is not Mutex/RwLock
- **SEM3070** — @requires_lock: field not found
- **SEM3071** — @waits_on: field not found
- **SEM3072** — Required parameter is missing
- **SEM3073** — Invalid parameter

### Semantic Violations (3074-3076)

- **SEM3074** — Attempt to extend @sealed type
- **SEM3075** — Attempt to write to @readonly field
- **SEM3076** — @pure violation: side effects

### Entrypoint Validation (3121-3125)

- **SEM3121** — Unknown @entrypoint mode (valid: `"argv"`, `"stdin"`)
- **SEM3122** — @entrypoint without mode requires all parameters to have default values
- **SEM3123** — Return type must be `nothing`, `int`, or implement `ExitCode` contract
- **SEM3124** — Parameter type does not implement `FromArgv` contract
- **SEM3125** — Parameter type does not implement `FromStdin` contract

### Future/Unsupported (7003-7004)

- **FUT7003** — @entrypoint("env") mode is reserved for future use
- **FUT7004** — @entrypoint("config") mode is reserved for future use

---

## Usage Examples

### Thread-safe Counter

```sg
@send
type ThreadSafeCounter = {
    lock: Mutex,

    @guarded_by("lock")
    value: int,
};

extern<ThreadSafeCounter> {
    @requires_lock("lock")
    fn get_value(self: &ThreadSafeCounter) -> int {
        return self.value;
    }

    @acquires_lock("lock")
    fn increment(self: &mut ThreadSafeCounter) {
        self.value = self.value + 1;
    }

    @releases_lock("lock")
    fn unlock(self: &mut ThreadSafeCounter) {
    }
}
```

### GPU-accelerated Processing

```sg
@backend("gpu")
type GPUMatrix = {
    @align(16)
    data: Array<float>,
};

@backend("gpu")
extern<GPUMatrix> {
    @nonblocking
    fn multiply(self: &GPUMatrix, other: &GPUMatrix) -> GPUMatrix {
        // GPU kernel
    }
}
```

### Immutable Config

```sg
@sealed
type Config = {
    @readonly
    app_name: String,

    @readonly
    version: int,

    @hidden
    internal_key: String,
};
```

### RAII Resource

```sg
@raii
@nosend
type FileHandle = {
    @readonly
    path: String,

    @hidden
    fd: int,
};

extern<FileHandle> {
    fn __init(path: String) -> FileHandle { ... }

    fn __drop(self: own FileHandle) {
        // Closes the file automatically
    }
}
```

---

## Conclusion

The Surge attribute system provides:

1. **Safety** — compile-time checks (readonly, guarded_by, send/nosend)
2. **Performance** — memory layout control (packed, align), specialization (backend)
3. **Documentation** — explicit contracts (pure, nonblocking, deprecated)
4. **Extensibility** — ability to add new attributes in the future

All attributes are validated by the compiler with clear diagnostic messages, helping you catch errors early in development.
