## 05 — Collatz steps (with CLI parsing)

Computes the Collatz sequence length for a starting number (default: 27),
optionally accepting a value from argv.

### What it demonstrates
- `while` loop and arithmetic
- parsing from `string` into `int`
- working with `Erring` / recoverable errors
- periodic progress logging

### Run
surge run showcases/05_collatz/main.sg 27 "ignored"
(Any way of passing the desired number via argv works as long as parsing matches.)

### Similar in other languages
- C: `strtol()` + `while`
- Rust: `args().nth(1).parse::<i64>()`
- Go: `strconv.Atoi(os.Args[1])`
