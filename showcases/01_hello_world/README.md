## 01 — Hello World

A minimal “does the language run?” program.

### What it demonstrates
- `@entrypoint` and exit codes (`int` return)
- `print`
- basic expressions and `to string`
- string concatenation

### Run
surge run showcases/01_hello_world/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>

int main() {
    printf("Hello, World!\n");
    printf("2 + 2 = %d\n", 2 + 2);
    return 0;
}
```

**Rust:**
```rust
fn main() {
    println!("Hello, World!");
    println!("2 + 2 = {}", 2 + 2);
}
```

**Go:**
```go
package main

import "fmt"

func main() {
    fmt.Println("Hello, World!")
    fmt.Printf("2 + 2 = %d\n", 2+2)
}
```
