## 18 — Rope-like Concatenation

Compares naive string concatenation (which can be slow) with efficient join-based concatenation, demonstrating performance considerations for string building.

### What it demonstrates
- naive string concatenation in loops (potentially inefficient)
- efficient string building using `join()` with arrays
- performance considerations for string operations
- string slicing with ranges
- large-scale string operations

### Run
surge run showcases/18_rope_like_concat/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

void naive_concat() {
    char *s = (char*)malloc(1);
    s[0] = '\0';
    
    for (int i = 0; i < 50000; i = i + 1) {
        char *new_s = (char*)malloc(strlen(s) + 3);
        strcpy(new_s, s);
        strcat(new_s, "ha");
        free(s);
        s = new_s;
        
        if (i % 10000 == 0) {
            int len = strlen(s);
            int start = 0;
            int end = len > 20 ? len - 20 : 0;
            char tail[21];
            strncpy(tail, s + end, 20);
            tail[20] = '\0';
            printf("i=%d, tail=%s\n", i, tail);
        }
    }
    printf("length=%zu\n", strlen(s));
    free(s);
}

void join_builder() {
    char **parts = (char**)malloc(50000 * sizeof(char*));
    for (int i = 0; i < 50000; i = i + 1) {
        parts[i] = strdup("ha");
    }
    
    int total_len = 50000 * 2;
    char *s = (char*)malloc(total_len + 1);
    s[0] = '\0';
    for (int i = 0; i < 50000; i = i + 1) {
        strcat(s, parts[i]);
        free(parts[i]);
    }
    printf("length=%zu\n", strlen(s));
    free(parts);
    free(s);
}

int main() {
    naive_concat();
    join_builder();
    return 0;
}
```

**Rust:**
```rust
fn naive_concat() {
    let mut s = String::new();
    for i in 0..50000 {
        s.push_str("ha");
        if i % 10000 == 0 {
            let len = s.len();
            let start = 0;
            let end = if len > 20 { len - 20 } else { 0 };
            let tail = &s[end..];
            println!("i={}, tail={}", i, tail);
        }
    }
    println!("length={}", s.len());
}

fn join_builder() {
    let parts: Vec<&str> = (0..50000).map(|_| "ha").collect();
    let s = parts.join("");
    println!("length={}", s.len());
}

fn main() {
    naive_concat();
    join_builder();
}
```

**Go:**
```go
package main

import (
    "fmt"
    "strings"
)

func naiveConcat() {
    var s strings.Builder
    for i := 0; i < 50000; i = i + 1 {
        s.WriteString("ha")
        if i%10000 == 0 {
            str := s.String()
            start := 0
            end := len(str)
            if end > 20 {
                end = len(str) - 20
            }
            tail := str[end:]
            fmt.Printf("i=%d, tail=%s\n", i, tail)
        }
    }
    fmt.Printf("length=%d\n", s.Len())
}

func joinBuilder() {
    parts := make([]string, 50000)
    for i := 0; i < 50000; i = i + 1 {
        parts[i] = "ha"
    }
    s := strings.Join(parts, "")
    fmt.Printf("length=%d\n", len(s))
}

func main() {
    naiveConcat()
    joinBuilder()
}
```

