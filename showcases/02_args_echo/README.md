## 02 — Args echo

Reads command-line arguments and prints them back.

### What it demonstrates
- `@entrypoint("argv")` parameter binding
- converting numbers to string
- basic CLI-style I/O

### Run
surge run showcases/02_args_echo/main.sg 1 "Doe"

### Similar in other languages

**C:**
```c
#include <stdio.h>

int main(int argc, char** argv) {
    printf("argc = %d\n", argc);
    if (argc >= 2) {
        printf("name = %s\n", argv[1]);
    }
    return 0;
}
```

**Rust:**
```rust
use std::env;

fn main() {
    let args: Vec<String> = env::args().collect();
    println!("argc = {}", args.len());
    if args.len() >= 2 {
        println!("name = {}", args[1]);
    }
}
```

**Go:**
```go
package main

import (
    "fmt"
    "os"
)

func main() {
    fmt.Printf("argc = %d\n", len(os.Args))
    if len(os.Args) >= 2 {
        fmt.Printf("name = %s\n", os.Args[1])
    }
}
```
