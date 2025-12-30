## 08 — Binary Search

Searches for a target value in a sorted array using binary search, returning the index if found or -1 otherwise.

### What it demonstrates
- binary search algorithm implementation
- working with sorted arrays
- while loops with bounds manipulation
- array references and borrowing

### Run
surge run showcases/08_binary_search/main.sg

### Similar in other languages

**C:**
```c
#include <stdio.h>

int binary_search(int arr[], int len, int target) {
    int lo = 0;
    int hi = len;
    while (lo < hi) {
        int mid = lo + (hi - lo) / 2;
        int v = arr[mid];
        if (v == target) {
            return mid;
        }
        if (v < target) {
            lo = mid + 1;
        } else {
            hi = mid;
        }
    }
    return -1;
}

int main() {
    int xs[] = {1, 3, 4, 7, 9, 12, 15};
    printf("%d\n", binary_search(xs, 7, 7));
    printf("%d\n", binary_search(xs, 7, 2));
    return 0;
}
```

**Rust:**
```rust
fn binary_search(arr: &[i32], target: i32) -> i32 {
    let mut lo = 0;
    let mut hi = arr.len();
    while lo < hi {
        let mid = lo + (hi - lo) / 2;
        let v = arr[mid];
        if v == target {
            return mid as i32;
        }
        if v < target {
            lo = mid + 1;
        } else {
            hi = mid;
        }
    }
    -1
}

fn main() {
    let xs = [1, 3, 4, 7, 9, 12, 15];
    println!("{}", binary_search(&xs, 7));
    println!("{}", binary_search(&xs, 2));
}
```

**Go:**
```go
package main

import "fmt"

func binarySearch(arr []int, target int) int {
    lo := 0
    hi := len(arr)
    for lo < hi {
        mid := lo + (hi-lo)/2
        v := arr[mid]
        if v == target {
            return mid
        }
        if v < target {
            lo = mid + 1
        } else {
            hi = mid
        }
    }
    return -1
}

func main() {
    xs := []int{1, 3, 4, 7, 9, 12, 15}
    fmt.Println(binarySearch(xs, 7))
    fmt.Println(binarySearch(xs, 2))
}
```

