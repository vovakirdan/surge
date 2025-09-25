# Surge — reactive, parallel-first language (interpreter + bytecode VM)

**Surge** is a new language prototype with:
- **Reactive state** (`signal` + `:=`) and automatic recomputation
- **Parallel-by-default** execution for pure functions (thread pool, futures)
- **Strict static typing** with simple, cheap memory model (arenas + RC)
- **Standard math from the box** (`sqrt`, `pow`, `sin/cos`, ...)
- **Built-in HTTP server** for easy service-building
- **Live comments / doctests** (`///! test:`) next to code

This repo contains the **front-end** (lexer, parser, type checker), the **bytecode compiler** (AST → SBC), and the **VM interpreter** executing Surge ByteCode.

> Early stages: expect stubs. We build iteratively by **Phases**.

---

## Layout

```

surge/
├─ cmd/
│  ├─ surge/            # run .sg or .sbc (interpreter/runner)
│  ├─ surgec/           # compile .sg → .sbc
│  └─ surgetest/        # run doctests from ///! blocks
├─ include/             # public/internal headers
├─ src/                 # C sources (core, front, back, runtime, testing, extras)
├─ stdlib/              # Surge .sg helpers (HTTP/maths/parallel sugar)
├─ examples/            # Sample Surge programs
├─ tests/               # Golden tests per subsystem
├─ docs/                # LANGUAGE.md, BYTECODE.md, RUNTIME.md, HTTP.md, ROADMAP.md
└─ Makefile

```

You may also create **Phase** directories to isolate experiments:

```

PhaseA/         # sandbox for Phase A (lexer, parser demos)
PhaseB/         # sandbox for Phase B (types, sema)
PhaseC/         # ...
PhaseX/foo.sg   # any .sg files; 'make test' will try to run doctests here

````

`make test` scans `Phase*/` and runs doctests using `surgetest`.

---

## Build (Ubuntu/WSL2)

Requirements:
- `gcc` (or `clang`), `make`, `pthread`, Linux `epoll`

Commands:
```bash
# Release build
make

# Debug build
make dev

# With sanitizers (ASan+UBSan)
make SAN=1
````

Binaries (after build):

```
build/bin/surge
build/bin/surgec
build/bin/surgetest
```

---

## Running

If `surge` already supports direct `.sg` interpretation:

```bash
make run FILE=examples/hello.sg
```

If you first compile to bytecode `.sbc`:

```bash
make compile FILE=examples/hello.sg
make runbc FILE=build/out/hello.sbc
```

---

## Doctests (Live comments)

Write tests right in your `.sg` files:

```sg
///! test: add(2,3) == 5
fn add(a:int, b:int) -> int { a + b }
```

Run all doctests in Phase sandboxes:

```bash
make test
```

---

## Phased Development

We iterate by **phases**:

* **Phase A** — Lexer/Parser & diagnostics
* **Phase B** — Types/Semantics
* **Phase C** — Bytecode design & .sbc format
* **Phase D** — VM & memory (arenas + RC)
* **Phase E** — AST→SBC compiler
* **Phase F** — std::math + native builtin calls
* **Phase G** — Reactive graph (signals, `:=`)
* **Phase H** — Task scheduler, futures, `parallel map/reduce`
* **Phase I** — Built-in HTTP (epoll), JSON, per-request arenas
* **Phase J** — Doctest runner
* **Phase K** — Modules/Imports + cache
* **Phase L** — Perf & optimizations
* **Phase M** — Stability & limits
* **Phase N** — Docs & DX

Each phase can drop playground files into `PhaseX/`, and tests can be tied to those samples.

---

## Coding Guidelines

* C11, strict warnings, pedantic (`-Wall -Wextra -Werror -Wpedantic`).
* Threading with `pthread`, I/O with `epoll`.
* Memory: **arenas** for short-lived objects (e.g., per-request), **RC heap** for long-lived.
* English-only comments/messages in code.
* Keep front-end, compiler, VM, runtime **independent** where possible.

---

## Roadmap (short)

1. Minimal front-end → eval expressions → VM skeleton
2. SBC format + disassembler
3. std::math + purity tagging → parallel primitives
4. signals/`:=` + propagation engine
5. HTTP server + JSON + per-request arenas
6. doctests + examples

See `docs/ROADMAP.md` for detailed milestones.

---

## License

TBD (pick later: MIT/Apache-2.0).
