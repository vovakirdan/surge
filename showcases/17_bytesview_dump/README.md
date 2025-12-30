## 17 — BytesView Dump

Demonstrates working with raw byte views of strings. This showcase is a placeholder for demonstrating low-level byte access patterns.

### What it demonstrates
- `BytesView` type for accessing UTF-8 bytes
- low-level string byte manipulation
- working with raw byte data from strings

### Run
surge run showcases/17_bytesview_dump/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <string.h>

void dump_bytes(const char *s) {
    int len = strlen(s);
    printf("String: %s\n", s);
    printf("Byte length: %d\n", len);
    printf("Bytes: ");
    for (int i = 0; i < len; i++) {
        printf("%02x ", (unsigned char)s[i]);
    }
    printf("\n");
}

int main() {
    const char *s = "Hello";
    dump_bytes(s);
    return 0;
}
```

**Rust:**
```rust
fn main() {
    let s = "Hello";
    let bytes = s.as_bytes();
    println!("String: {}", s);
    println!("Byte length: {}", bytes.len());
    print!("Bytes: ");
    for &b in bytes {
        print!("{:02x} ", b);
    }
    println!();
}
```

**Go:**
```go
package main

import "fmt"

func main() {
    s := "Hello"
    bytes := []byte(s)
    fmt.Printf("String: %s\n", s)
    fmt.Printf("Byte length: %d\n", len(bytes))
    fmt.Print("Bytes: ")
    for _, b := range bytes {
        fmt.Printf("%02x ", b)
    }
    fmt.Println()
}
```

