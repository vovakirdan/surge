# Surge Programming Language

> Lessons learned from Rust/Python/Go

**Surge** is a modern systems programming language designed for scientific computing, AI backends, and high-performance applications. It combines the safety of Rust-style ownership with the ergonomics of higher-level languages, while providing first-class support for reactive programming and structured concurrency.

## Key Features

### 🔒 **Memory Safety Without Garbage Collection**
- Pure ownership model with deterministic cleanup via RAII
- No garbage collector pauses - predictable performance for real-time systems
- Borrowing system prevents data races and memory corruption
- Zero-cost abstractions with compile-time memory management

### ⚡ **Performance-First Design**
- Monomorphization for zero-cost generics
- Dynamic-width numeric types (`int`, `uint`, `float`) with arbitrary precision
- Hybrid string implementation with O(1) amortized indexing
- GPU backend support with `@backend("gpu")` annotations

### 🔄 **Reactive Programming Built-in**
- `signal` bindings with automatic dependency tracking
- Declarative reactive state management
- Pure function requirements ensure predictable updates
- Topological sorting for efficient propagation

### 🚀 **Structured Concurrency**
- `async`/`await` with automatic resource cleanup
- Parallel primitives: `parallel map` and `parallel reduce`
- Task spawning with ownership-based thread safety
- No "fire-and-forget" tasks - everything is properly managed

### 🎯 **Type Safety and Ergonomics**
- Strong static typing with type inference
- Pattern matching with `compare` expressions
- Option types with automatic wrapping: `let x: Option<int> = 42`
- Result types with `?` operator for error propagation

## Language Overview

### Basic Syntax

```sg
// Variables and types
let name: string = "Surge";
let mut counter: int = 0;
let temperature: float = 98.6;

// Functions
fn fibonacci(n: int) -> int {
    if (n <= 1) { return n; }
    return fibonacci(n - 1) + fibonacci(n - 2);
}

// Async functions
async fn fetch_data(url: string) -> Result<Data, Error> {
    let response = http_get(url).await?;
    return parse_json(response);
}
```

### User-Defined Types

```sg
// Structs
type Person {
    name: string,
    age: int,
    @readonly id: uint
}

// Newtypes with custom behavior
newtype UserId = int;
extern<UserId> {
    fn validate(self: &UserId) -> bool {
        return self.value > 0;
    }
}

// Type aliases
alias Number = int | float;

// Literal enums
literal Color = "red" | "green" | "blue";
```

### Ownership and Borrowing

```sg
fn process_data(data: own Data) -> Result<Output, Error> {
    let borrowed = &data;        // Shared borrow
    let mut_ref = &mut data;     // Exclusive borrow

    // Move ownership to another function
    return transform(data);
}

// Thread safety through ownership
async fn concurrent_processing(items: own Vec<Item>) {
    async {
        let task1 = spawn process_chunk(items[0..100]);
        let task2 = spawn process_chunk(items[100..200]);

        let result1 = task1.await?;
        let result2 = task2.await?;

        // Automatic cleanup on block exit
    }
}
```

### Pattern Matching and Error Handling

```sg
fn handle_response(result: Result<Data, HttpError>) -> string {
    compare result {
        Ok(data)    => format_data(data),
        Err(error)  => handle_error(error),
        finally     => "Unknown response"
    }
}

// Type checking with 'is' operator
fn classify_value(value: Number) {
    compare value {
        x if x is int   => print("Integer: ", x),
        x if x is float => print("Float: ", x),
        finally         => print("Unknown number type")
    }
}
```

### Reactive Programming

```sg
// Reactive signals automatically update when dependencies change
signal total_price := base_price + tax_amount + shipping;
signal discount := if (total_price > 100) { 0.1 } else { 0.0 };
signal final_price := total_price * (1.0 - discount);

// Any change to base_price, tax_amount, or shipping automatically
// propagates through the dependency graph
```

### Parallel Computing

```sg
// Parallel map over collections
let results = parallel map numbers with (multiplier) => x * multiplier;

// Parallel reduction
let sum = parallel reduce values with 0, () => a + b;

// GPU computing
@backend("gpu")
fn matrix_multiply(a: Matrix, b: Matrix) -> Matrix {
    // Executed on GPU if available, CPU fallback otherwise
    // Implementation details...
}
```

### Error Handling

```sg
// Simple inheritance-based error model
type Error {
    message: string,
    code: uint
}

newtype NetworkError = Error;
newtype ParseError = Error;

// Result types with ? operator
fn load_config() -> Result<Config, Error> {
    let content = read_file("config.json")?;
    let config = parse_json(content)?;
    return Ok(config);
}
```

## Design Goals

- **Safety by default**: Explicit ownership/borrows, no hidden implicit mutability
- **Deterministic semantics**: Operator/overload resolution is fully specified
- **Orthogonality**: Features compose cleanly (generics + ownership + methods)
- **Compile-time rigor, runtime pragmatism**: Static typing with dynamic-width numerics
- **Testability**: First-class doc-tests via `/// test:` directives

## Target Domains

### Scientific Computing
- Predictable performance without GC pauses
- Arbitrary precision numeric types
- GPU computing support
- Memory layout control for HPC applications

### AI/ML Backends
- Efficient tensor operations
- Parallel processing primitives
- Structured concurrency for training pipelines
- Zero-copy data processing

### Systems Programming
- Memory safety without runtime overhead
- Fine-grained control over resource management
- Thread-safe programming by design
- Async I/O with structured concurrency

### Real-time Applications
- Deterministic memory management
- No unexpected allocation or collection pauses
- Predictable performance characteristics
- Low-latency reactive updates

## Development Status

Surge is currently in active development. The language specification is complete, and we're working on the implementation:

- ✅ Language specification (Draft 3)
- 🔄 Lexer and parser implementation
- 🔄 Type checker and semantic analysis
- ⏳ Bytecode compiler
- ⏳ Virtual machine runtime
- ⏳ Standard library

## Building and Installation

```bash
# Clone the repository
git clone https://github.com/vovakirdan/surge.git
cd surge

# Build the compiler and runtime
cargo build

# Run examples
cargo run -- examples/hello.sg

# Run tests
cargo test
```

## Documentation

- [Language Specification](docs/LANGUAGE.md) - Complete language reference
- [Getting Started](docs/getting-started.md) - Tutorial and examples
- [Standard Library](docs/stdlib.md) - Built-in functions and modules
- [Concurrency Guide](docs/concurrency.md) - Async programming and parallelism

*Surge: Reactive, parallel, and safe systems programming for the modern world.*