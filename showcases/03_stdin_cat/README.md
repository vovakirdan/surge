## 03 — Stdin cat (line-based)

Reads stdin and prints a greeting using the provided input.

### What it demonstrates
- `@entrypoint("stdin")` binding
- line/EOF-based input behavior
- simple string operations

### Run
echo Surge | surge run showcases/03_stdin_cat/main.sg

### Similar in other languages
- C: `fgets()` / `getline()`
- Rust: `std::io::stdin().read_line(...)`
- Go: `bufio.NewReader(os.Stdin).ReadString('\n')`
