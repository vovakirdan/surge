## 02 — Args echo

Reads command-line arguments and prints them back.

### What it demonstrates
- `@entrypoint("argv")` parameter binding
- converting numbers to string
- basic CLI-style I/O

### Run
surge run showcases/02_args_echo/main.sg 1 "Doe"

### Similar in other languages
- C: `int main(int argc, char** argv) { ... }`
- Rust: `std::env::args()`
- Go: `os.Args`
