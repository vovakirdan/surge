## 12 — Histogram

Counts the frequency of digits (0-9) in an array and prints a histogram showing the count for each digit.

### What it demonstrates
- frequency counting with fixed-size arrays
- `ArrayFixed` type usage
- bounds checking (0 <= v && v < 10)
- string concatenation and conversion
- while loops for iteration

### Run
surge run showcases/12_histogram/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>

void int_histogram(int arr[], int len) {
    int freq[10] = {0};
    for (int i = 0; i < len; i = i + 1) {
        int v = arr[i];
        if (0 <= v && v < 10) {
            freq[v] = freq[v] + 1;
        }
    }

    for (int b = 0; b < 10; b = b + 1) {
        printf("%d: %d\n", b, freq[b]);
    }
}

int main() {
    int arr[] = {1, 2, 3, 2, 2, 9, 0, 1, 9, 9, 9, 5, 5, 5, 5};
    int_histogram(arr, 15);
    return 0;
}
```

**Rust:**
```rust
fn int_histogram(arr: &[i32]) {
    let mut freq = [0; 10];
    for &v in arr {
        if 0 <= v && v < 10 {
            freq[v as usize] = freq[v as usize] + 1;
        }
    }

    for (b, &count) in freq.iter().enumerate() {
        println!("{}: {}", b, count);
    }
}

fn main() {
    let arr = [1, 2, 3, 2, 2, 9, 0, 1, 9, 9, 9, 5, 5, 5, 5];
    int_histogram(&arr);
}
```

**Go:**
```go
package main

import "fmt"

func intHistogram(arr []int) {
    freq := [10]int{}
    for _, v := range arr {
        if 0 <= v && v < 10 {
            freq[v] = freq[v] + 1
        }
    }

    for b := 0; b < 10; b = b + 1 {
        fmt.Printf("%d: %d\n", b, freq[b])
    }
}

func main() {
    arr := []int{1, 2, 3, 2, 2, 9, 0, 1, 9, 9, 9, 5, 5, 5, 5}
    intHistogram(arr)
}
```

