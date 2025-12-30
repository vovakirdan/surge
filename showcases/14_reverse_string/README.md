## 14 — Reverse String

Reverses a string by code points, demonstrating Unicode-aware string reversal.

### What it demonstrates
- `reverse()` method on strings
- code point-based string operations
- working with Unicode characters (emoji, accented characters)

### Run
surge run showcases/14_reverse_string/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <wchar.h>
#include <locale.h>

void reverse_utf8(char *s) {
    setlocale(LC_ALL, "en_US.UTF-8");
    int len = strlen(s);
    char *reversed = (char*)malloc(len + 1);
    reversed[len] = '\0';
    
    // Reverse UTF-8 string properly
    int i = 0, j = len;
    while (i < len) {
        int cp_len = 1;
        if ((s[i] & 0xE0) == 0xC0) cp_len = 2;
        else if ((s[i] & 0xF0) == 0xE0) cp_len = 3;
        else if ((s[i] & 0xF8) == 0xF0) cp_len = 4;
        
        j -= cp_len;
        memcpy(reversed + j, s + i, cp_len);
        i += cp_len;
    }
    
    printf("s=%s\n", s);
    printf("r=%s\n", reversed);
    free(reversed);
}

int main() {
    char s[] = "ab🙂🇵🇱é";
    reverse_utf8(s);
    return 0;
}
```

**Rust:**
```rust
fn main() {
    let s = "ab🙂🇵🇱é";
    let r: String = s.chars().rev().collect();
    println!("s={}", s);
    println!("r={}", r);
}
```

**Go:**
```go
package main

import (
    "fmt"
    "unicode/utf8"
)

func reverseString(s string) string {
    runes := []rune(s)
    for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
        runes[i], runes[j] = runes[j], runes[i]
    }
    return string(runes)
}

func main() {
    s := "ab🙂🇵🇱é"
    r := reverseString(s)
    fmt.Printf("s=%s\n", s)
    fmt.Printf("r=%s\n", r)
}
```

