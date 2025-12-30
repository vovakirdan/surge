## 13 — Unicode Length Demo

Demonstrates the difference between code point length and byte length for Unicode strings, showing how multi-byte characters are handled.

### What it demonstrates
- Unicode string handling
- difference between `len()` (code points) and `bytes().len` (UTF-8 bytes)
- working with emoji and multi-byte characters
- `bytes()` method to access raw UTF-8 bytes

### Run
surge run showcases/13_unicode_len_demo/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <string.h>
#include <wchar.h>
#include <locale.h>

int main() {
    setlocale(LC_ALL, "en_US.UTF-8");
    char s[] = "A🇵🇱é🙂";
    
    // Code points (rough approximation - proper counting requires UTF-8 parsing)
    int len_cp = 0;
    for (char *p = s; *p; p++) {
        if ((*p & 0xC0) != 0x80) len_cp++;
    }
    
    int len_bytes = strlen(s);
    
    printf("s=%s\n", s);
    printf("code points=%d\n", len_cp);
    printf("bytes=%d\n", len_bytes);
    printf("hex=%02x\n", (unsigned char)s[0]);
    
    return 0;
}
```

**Rust:**
```rust
fn main() {
    let s = "A🇵🇱é🙂";
    
    let len_cp = s.chars().count();
    let len_bytes = s.len();
    
    println!("s={}", s);
    println!("code points={}", len_cp);
    println!("bytes={}", len_bytes);
    if let Some(b) = s.as_bytes().first() {
        println!("hex={:02x}", b);
    }
}
```

**Go:**
```go
package main

import (
    "fmt"
    "unicode/utf8"
)

func main() {
    s := "A🇵🇱é🙂"
    
    lenCP := utf8.RuneCountInString(s)
    lenBytes := len(s)
    
    fmt.Printf("s=%s\n", s)
    fmt.Printf("code points=%d\n", lenCP)
    fmt.Printf("bytes=%d\n", lenBytes)
    if len(s) > 0 {
        fmt.Printf("hex=%02x\n", s[0])
    }
}
```

