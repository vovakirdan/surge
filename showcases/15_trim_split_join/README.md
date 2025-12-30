## 15 — Trim, Split, Join

Demonstrates string manipulation operations: trimming whitespace, splitting into parts, and joining with a separator.

### What it demonstrates
- `trim()` method to remove whitespace
- `split()` method to split strings by separator
- `join()` method to concatenate array elements with separator
- f-string formatting
- `format()` function with `fmt_arg()`

### Run
surge run showcases/15_trim_split_join/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <ctype.h>

void trim(char *s) {
    char *start = s;
    while (isspace((unsigned char)*start)) start++;
    
    char *end = s + strlen(s) - 1;
    while (end > start && isspace((unsigned char)*end)) end--;
    end[1] = '\0';
    
    if (start != s) {
        memmove(s, start, end - start + 2);
    }
}

int split(const char *s, const char *sep, char **parts, int max_parts) {
    int count = 0;
    char *copy = strdup(s);
    char *token = strtok(copy, sep);
    
    while (token != NULL && count < max_parts) {
        parts[count++] = strdup(token);
        token = strtok(NULL, sep);
    }
    free(copy);
    return count;
}

char* join(char **parts, int count, const char *sep) {
    if (count == 0) return strdup("");
    
    int total_len = 0;
    int sep_len = strlen(sep);
    for (int i = 0; i < count; i++) {
        total_len += strlen(parts[i]);
        if (i < count - 1) total_len += sep_len;
    }
    
    char *result = (char*)malloc(total_len + 1);
    result[0] = '\0';
    for (int i = 0; i < count; i++) {
        strcat(result, parts[i]);
        if (i < count - 1) strcat(result, sep);
    }
    return result;
}

int main() {
    char s[] = "   Hello,\tSurge \n  world!   ";
    printf("Original: %s\n", s);
    
    trim(s);
    printf("trimmed='%s'\n", s);
    
    char *parts[10];
    int count = split(s, " ", parts, 10);
    
    char *normalized = join(parts, count, ", ");
    printf("normalized='%s'\n", normalized);
    
    for (int i = 0; i < count; i++) free(parts[i]);
    free(normalized);
    return 0;
}
```

**Rust:**
```rust
fn main() {
    let s = "   Hello,\tSurge \n  world!   ";
    let trimmed = s.trim();
    let parts: Vec<&str> = trimmed.split_whitespace().collect();
    let normalized = parts.join(", ");
    
    println!("Original: {}", s);
    println!("trimmed='{}'", trimmed);
    println!("normalized='{}'", normalized);
}
```

**Go:**
```go
package main

import (
    "fmt"
    "strings"
)

func main() {
    s := "   Hello,\tSurge \n  world!   "
    trimmed := strings.TrimSpace(s)
    parts := strings.Fields(trimmed)
    normalized := strings.Join(parts, ", ")
    
    fmt.Printf("Original: %s\n", s)
    fmt.Printf("trimmed='%s'\n", trimmed)
    fmt.Printf("normalized='%s'\n", normalized)
}
```

