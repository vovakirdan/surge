## 07 — Linear Search

Searches for a target value in an array using linear search, returning the index of the first occurrence or -1 if not found.

### What it demonstrates
- `for` loops with array iteration
- basic array indexing and comparison
- function return values
- f-string formatting

### Run
surge run showcases/07_linear_search/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>

int linear_search(int arr[], int len, int target) {
    for (int i = 0; i < len; i++) {
        if (arr[i] == target) {
            return i;
        }
    }
    return -1;
}

int main() {
    int arr[] = {5, 9, 2, 9, 1};
    int idx = linear_search(arr, 5, 9);
    printf("Index of 9: %d\n", idx);
    return 0;
}
```

**Rust:**
```rust
fn linear_search(arr: &[i32], target: i32) -> i32 {
    for (i, &v) in arr.iter().enumerate() {
        if v == target {
            return i as i32;
        }
    }
    -1
}

fn main() {
    let arr = [5, 9, 2, 9, 1];
    let idx = linear_search(&arr, 9);
    println!("Index of 9: {}", idx);
}
```

**Go:**
```go
package main

import "fmt"

func linearSearch(arr []int, target int) int {
    for i, v := range arr {
        if v == target {
            return i
        }
    }
    return -1
}

func main() {
    arr := []int{5, 9, 2, 9, 1}
    idx := linearSearch(arr, 9)
    fmt.Printf("Index of 9: %d\n", idx)
}
```

