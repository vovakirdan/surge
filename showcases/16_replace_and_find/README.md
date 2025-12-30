## 16 — Replace and Find

Demonstrates string search and replace operations: finding substrings, counting occurrences, and replacing them.

### What it demonstrates
- `find()` method to locate first occurrence
- `count()` method to count occurrences
- `replace()` method to replace all occurrences
- custom entrypoint function name (`enter` instead of `main`)

### Run
surge run showcases/16_replace_and_find/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

int find(const char *s, const char *needle) {
    char *pos = strstr(s, needle);
    if (pos == NULL) return -1;
    return pos - s;
}

int count(const char *s, const char *needle) {
    int cnt = 0;
    const char *p = s;
    size_t len = strlen(needle);
    while ((p = strstr(p, needle)) != NULL) {
        cnt++;
        p += len;
    }
    return cnt;
}

char* replace(const char *s, const char *old, const char *new) {
    int old_len = strlen(old);
    int new_len = strlen(new);
    int cnt = count(s, old);
    
    if (cnt == 0) return strdup(s);
    
    int result_len = strlen(s) + cnt * (new_len - old_len) + 1;
    char *result = (char*)malloc(result_len);
    result[0] = '\0';
    
    const char *p = s;
    const char *found;
    while ((found = strstr(p, old)) != NULL) {
        int prefix_len = found - p;
        strncat(result, p, prefix_len);
        strcat(result, new);
        p = found + old_len;
    }
    strcat(result, p);
    return result;
}

int main() {
    char s[] = "one two one two one";
    char needle[] = "one";
    char repl[] = "ONE";
    
    int first = find(s, needle);
    printf("First: %d\n", first);
    
    int cnt = count(s, needle);
    printf("Count: %d\n", cnt);
    
    char *replaced = replace(s, needle, repl);
    printf("Replaced: %s\n", replaced);
    free(replaced);
    
    return 0;
}
```

**Rust:**
```rust
fn main() {
    let s = "one two one two one";
    let needle = "one";
    let repl = "ONE";
    
    let first = s.find(needle).map(|i| i as i32).unwrap_or(-1);
    println!("First: {}", first);
    
    let count = s.matches(needle).count();
    println!("Count: {}", count);
    
    let replaced = s.replace(needle, repl);
    println!("Replaced: {}", replaced);
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
    s := "one two one two one"
    needle := "one"
    repl := "ONE"
    
    first := strings.Index(s, needle)
    fmt.Printf("First: %d\n", first)
    
    count := strings.Count(s, needle)
    fmt.Printf("Count: %d\n", count)
    
    replaced := strings.ReplaceAll(s, needle, repl)
    fmt.Printf("Replaced: %s\n", replaced)
}
```

