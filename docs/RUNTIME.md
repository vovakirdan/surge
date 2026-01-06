# Surge Runtime (VM)
[English](RUNTIME.md) | [Russian](RUNTIME.ru.md)

This document describes the current runtime for Surge v1. The runtime is a
single-threaded VM that interprets MIR and hosts async execution.

See also: `docs/IR.md`, `docs/CONCURRENCY.md`, `docs/ABI_LAYOUT.md`.

---

## 1. Overview

- The compiler lowers source code to MIR and executes it in the VM
  (`internal/vm`).
- Execution starts at the synthetic `__surge_start` produced from
  `@entrypoint` lowering.
- The VM is available via `surge run --backend=vm` and supports deterministic
  scheduling by default.

---

## 2. VM architecture

- **Direct MIR interpreter:** instructions and terminators are executed step by
  step (`VM.Step`).
- **Stack frames + locals:** each call pushes a `Frame` with locals and an
  instruction pointer.
- **Heap objects:** arrays, strings, structs, tagged unions, and other owned
  values live in the VM heap (`internal/vm/heap.go`).
- **ABI layout aware:** layout rules are provided by `layout.LayoutEngine`
  (see `docs/ABI_LAYOUT.md`).
- **Drop/RAII:** values are dropped explicitly in the VM; drop order is traced
  in tests and validated by intrinsics.

---

## 3. Host runtime interface

The VM talks to the outside world via `Runtime` (`internal/vm/runtime.go`):

- `Argv()` provides program arguments for `@entrypoint("argv")`.
- `StdinReadAll()` powers `@entrypoint("stdin")`.
- `Exit(code)` records the exit code and halts execution.

Implementations:

- `DefaultRuntime` uses OS argv/stdin.
- `TestRuntime` provides controlled inputs for tests.
- `RecordingRuntime` and `ReplayRuntime` enable deterministic record/replay.

---

## 4. Intrinsics and IO

Many core operations are implemented as VM intrinsics, including:

- numeric conversions and bounds checks
- string and array operations
- IO helpers (stdout write, stdin read)
- async primitives (task handles, channels, timers)

`@intrinsic` declarations in the standard library map to these runtime
implementations.

---

## 5. Async runtime

Async execution is handled by `internal/asyncrt`:

- **Single-threaded executor** with cooperative scheduling.
- **Deterministic FIFO** scheduling by default; optional fuzz scheduling with a
  fixed seed for reproducible interleavings.
- **Tasks are state machines** produced by async lowering (`poll` functions).
- **Scopes** track structured concurrency and child tasks.
- **Channels** are typed FIFO queues with blocking and non-blocking ops.
- **Timers** implement `sleep` and `timeout` using virtual time.

See `docs/CONCURRENCY.md` for the language-level model.

---

## 6. Determinism and replay

The VM can record and replay execution:

- `Recorder` writes a deterministic NDJSON log of intrinsics, exits, and panics.
- `Replayer` replays a log and validates every intrinsic result.
- `RecordingRuntime` wraps another runtime to capture argv/stdin, while
  `ReplayRuntime` serves recorded values and panics on mismatches.

This is used in golden tests and determinism checks.

---

## 7. Tracing and debugging

- `surge run --vm-trace` enables VM execution tracing.
- The VM exposes stop points (function name, block, instruction pointer) for
  debugging and test harnesses.

---

## 8. Known limitations (v1)

- No OS-thread parallelism (single-threaded runtime).
- `parallel` / `signal` are reserved and rejected.
