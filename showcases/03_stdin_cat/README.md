## 03 — Stdin cat (line-based)

Reads stdin and prints a greeting using the provided input.

### What it demonstrates
- `@entrypoint("stdin")` binding
- line/EOF-based input behavior
- simple string operations

### Run
echo Surge | surge run showcases/03_stdin_cat/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <string.h>

int main() {
    char name[256];
    if (fgets(name, sizeof(name), stdin) != NULL) {
        // Remove trailing newline if present
        size_t len = strlen(name);
        if (len > 0 && name[len - 1] == '\n') {
            name[len - 1] = '\0';
        }
        printf("Hello, %s\n", name);
    }
    return 0;
}
```

**Rust:**
```rust
use std::io;

fn main() {
    let mut name = String::new();
    io::stdin().read_line(&mut name).expect("Failed to read line");
    let name = name.trim_end();
    println!("Hello, {}", name);
}
```

**Go:**
```go
package main

import (
    "bufio"
    "fmt"
    "os"
)

func main() {
    reader := bufio.NewReader(os.Stdin)
    name, _ := reader.ReadString('\n')
    name = name[:len(name)-1] // Remove trailing newline
    fmt.Printf("Hello, %s\n", name)
}
```
