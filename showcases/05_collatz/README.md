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

**C:**
```c
#include <stdio.h>
#include <stdlib.h>

int main(int argc, char** argv) {
    int n = 27;
    if (argc >= 2) {
        char* endptr;
        long parsed = strtol(argv[1], &endptr, 10);
        if (*endptr == '\0') {
            n = (int)parsed;
        }
    }

    int steps = 0;
    while (n != 1) {
        if (n % 2 == 0) {
            n = n / 2;
        } else {
            n = 3 * n + 1;
        }
        steps = steps + 1;
        if (steps % 10 == 0) {
            printf("Done %d steps: %d\n", steps, n);
        }
    }
    printf("Steps = %d\n", steps);
    return 0;
}
```

**Rust:**
```rust
use std::env;

fn main() {
    let mut n = 27i64;
    if let Some(arg) = env::args().nth(1) {
        if let Ok(parsed) = arg.parse::<i64>() {
            n = parsed;
        }
    }

    let mut steps = 0;
    while n != 1 {
        if n % 2 == 0 {
            n = n / 2;
        } else {
            n = 3 * n + 1;
        }
        steps = steps + 1;
        if steps % 10 == 0 {
            println!("Done {} steps: {}", steps, n);
        }
    }
    println!("Steps = {}", steps);
}
```

**Go:**
```go
package main

import (
    "fmt"
    "os"
    "strconv"
)

func main() {
    n := 27
    if len(os.Args) >= 2 {
        if parsed, err := strconv.Atoi(os.Args[1]); err == nil {
            n = parsed
        }
    }

    steps := 0
    for n != 1 {
        if n%2 == 0 {
            n = n / 2
        } else {
            n = 3*n + 1
        }
        steps = steps + 1
        if steps%10 == 0 {
            fmt.Printf("Done %d steps: %d\n", steps, n)
        }
    }
    fmt.Printf("Steps = %d\n", steps)
}
```
